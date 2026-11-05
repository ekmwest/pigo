package repocopy

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// extract writes the regular files and directories of a gzipped tarball into
// dest, without the archive's top-level folder and the excluded root entries.
// It returns the number of files written.
func extract(r io.Reader, dest string) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("reading gzip: %w", err)
	}
	defer gz.Close()

	// os.Root refuses any path that resolves outside dest, which backs up the
	// explicit check below.
	root, err := os.OpenRoot(dest)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	files := 0
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return files, fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		_, rel, ok := strings.Cut(hdr.Name, "/")
		if !ok || rel == "" {
			continue // the top-level folder itself
		}
		rel = path.Clean(rel)
		if strings.Contains(rel, `\`) || !filepath.IsLocal(filepath.FromSlash(rel)) {
			return files, fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}
		if isExcluded(rel) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(rel, 0o755); err != nil {
				return files, err
			}
		case tar.TypeReg:
			if err := writeFile(root, rel, hdr, tr); err != nil {
				return files, err
			}
			files++
		}
		// Symlinks, hard links and devices are skipped.
	}
}

func isExcluded(rel string) bool {
	for _, e := range excluded {
		if rel == e || strings.HasPrefix(rel, e+"/") {
			return true
		}
	}
	return false
}

func writeFile(root *os.Root, rel string, hdr *tar.Header, r io.Reader) error {
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	perm := os.FileMode(0o644)
	if hdr.Mode&0o111 != 0 {
		perm = 0o755
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", rel, err)
	}
	return nil
}
