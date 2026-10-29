package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// maxSize caps downloads and extracted files, so a broken or hostile server
// can't fill up memory.
const maxSize = 100 << 20

var errNotFound = errors.New("not found")

func latestTag(client *http.Client, url, userAgent string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("checking for the latest release: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for the latest release: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusTooManyRequests:
		// Unauthenticated API calls are limited to 60 an hour per IP address.
		return "", fmt.Errorf("checking for the latest release: GitHub refused the request (%s), probably the API rate limit; try again later", resp.Status)
	default:
		return "", fmt.Errorf("checking for the latest release: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSize)).Decode(&release); err != nil {
		return "", fmt.Errorf("checking for the latest release: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("checking for the latest release: response has no tag_name")
	}
	return release.TagName, nil
}

func download(client *http.Client, url, userAgent string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("downloading %s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	data, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	return data, nil
}

func assetName(goos, goarch string) string {
	if goos == "windows" {
		return "pigo_" + goos + "_" + goarch + ".zip"
	}
	return "pigo_" + goos + "_" + goarch + ".tar.gz"
}

func verify(archive, checksums []byte, name string) error {
	want, err := checksumFor(bytes.NewReader(checksums), name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", name, want, got)
	}
	return nil
}

// checksumFor returns the sha256 of name from a checksums file in sha256sum
// format: one "<hex>  <file>" per line.
func checksumFor(r io.Reader, name string) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		// sha256sum marks files read in binary mode with a leading "*".
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != 2*sha256.Size {
			return "", fmt.Errorf("checksums.txt: malformed checksum for %s: %q", name, fields[0])
		}
		return sum, nil
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading checksums.txt: %w", err)
	}
	return "", fmt.Errorf("checksums.txt has no checksum for %s", name)
}

func extract(archive []byte, goos string) ([]byte, error) {
	if goos == "windows" {
		return extractZip(archive, "pigo.exe")
	}
	return extractTarGz(archive, "pigo")
}

// extractZip returns the contents of the regular file name at the root of
// the archive.
func extractZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("reading zip: %w", err)
	}
	for _, f := range zr.File {
		if path.Clean(f.Name) != name || !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		defer rc.Close()
		b, err := readLimited(rc)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s not found in archive", name)
}

// extractTarGz returns the contents of the regular file name at the root of
// the archive.
func extractTarGz(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reading gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in archive", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if path.Clean(hdr.Name) != name || hdr.Typeflag != tar.TypeReg {
			continue
		}
		b, err := readLimited(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		return b, nil
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("larger than %d MB", maxSize>>20)
	}
	return b, nil
}
