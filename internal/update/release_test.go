package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

type archiveEntry struct {
	name string
	body string
	dir  bool
}

func makeZip(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		name := e.name
		if e.dir {
			name += "/"
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.dir {
			hdr = &tar.Header{Name: e.name + "/", Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestAssetName(t *testing.T) {
	tests := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "pigo_windows_amd64.zip"},
		{"windows", "arm64", "pigo_windows_arm64.zip"},
		{"linux", "amd64", "pigo_linux_amd64.tar.gz"},
		{"darwin", "arm64", "pigo_darwin_arm64.tar.gz"},
	}
	for _, tt := range tests {
		if got := assetName(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("assetName(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	amd64 := strings.Repeat("a", 64)
	arm64 := strings.Repeat("b", 64)
	sums := amd64 + "  pigo_linux_amd64.tar.gz\n" + arm64 + "  pigo_linux_arm64.tar.gz\n"

	tests := []struct {
		name      string
		checksums string
		file      string
		want      string
		wantErr   string // substring; "" means no error
	}{
		{name: "first entry", checksums: sums, file: "pigo_linux_amd64.tar.gz", want: amd64},
		{name: "similar name", checksums: sums, file: "pigo_linux_arm64.tar.gz", want: arm64},
		{name: "binary mode marker", checksums: amd64 + " *pigo_windows_amd64.zip\n", file: "pigo_windows_amd64.zip", want: amd64},
		{name: "uppercase hex", checksums: strings.ToUpper(amd64) + "  pigo_linux_amd64.tar.gz\n", file: "pigo_linux_amd64.tar.gz", want: amd64},
		{name: "blank and odd lines skipped", checksums: "\nnot a checksum line at all\n" + sums, file: "pigo_linux_arm64.tar.gz", want: arm64},
		{name: "missing", checksums: sums, file: "pigo_darwin_arm64.tar.gz", wantErr: "no checksum for pigo_darwin_arm64.tar.gz"},
		{name: "empty file", checksums: "", file: "pigo_linux_amd64.tar.gz", wantErr: "no checksum"},
		{name: "too short", checksums: "abc123  pigo_linux_amd64.tar.gz\n", file: "pigo_linux_amd64.tar.gz", wantErr: "malformed checksum"},
		{name: "not hex", checksums: strings.Repeat("z", 64) + "  pigo_linux_amd64.tar.gz\n", file: "pigo_linux_amd64.tar.gz", wantErr: "malformed checksum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checksumFor(strings.NewReader(tt.checksums), tt.file)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("checksum = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	archive := []byte("archive contents")
	good := []byte(sha256Hex(archive) + "  pigo_linux_amd64.tar.gz\n")
	bad := []byte(sha256Hex([]byte("something else")) + "  pigo_linux_amd64.tar.gz\n")

	if err := verify(archive, good, "pigo_linux_amd64.tar.gz"); err != nil {
		t.Errorf("verify with matching checksum: %v", err)
	}
	if err := verify(archive, bad, "pigo_linux_amd64.tar.gz"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("verify with wrong checksum: err = %v, want checksum mismatch", err)
	}
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name    string
		archive func(t *testing.T) []byte
		goos    string
		want    string
		wantErr string // substring; "" means no error
	}{
		{
			name:    "tar.gz",
			archive: func(t *testing.T) []byte { return makeTarGz(t, archiveEntry{name: "pigo", body: "new binary"}) },
			goos:    "linux", want: "new binary",
		},
		{
			name: "tar.gz with ./ prefix and other files",
			archive: func(t *testing.T) []byte {
				return makeTarGz(t, archiveEntry{name: "README", body: "x"}, archiveEntry{name: "./pigo", body: "new binary"})
			},
			goos: "darwin", want: "new binary",
		},
		{
			name:    "zip",
			archive: func(t *testing.T) []byte { return makeZip(t, archiveEntry{name: "pigo.exe", body: "new exe"}) },
			goos:    "windows", want: "new exe",
		},
		{
			name:    "tar.gz missing binary",
			archive: func(t *testing.T) []byte { return makeTarGz(t, archiveEntry{name: "other", body: "x"}) },
			goos:    "linux", wantErr: "pigo not found",
		},
		{
			name:    "tar.gz binary in subdirectory",
			archive: func(t *testing.T) []byte { return makeTarGz(t, archiveEntry{name: "dist/pigo", body: "x"}) },
			goos:    "linux", wantErr: "pigo not found",
		},
		{
			name:    "tar.gz directory named pigo",
			archive: func(t *testing.T) []byte { return makeTarGz(t, archiveEntry{name: "pigo", dir: true}) },
			goos:    "linux", wantErr: "pigo not found",
		},
		{
			name:    "zip missing binary",
			archive: func(t *testing.T) []byte { return makeZip(t, archiveEntry{name: "pigo", body: "x"}) },
			goos:    "windows", wantErr: "pigo.exe not found",
		},
		{
			name:    "zip directory named pigo.exe",
			archive: func(t *testing.T) []byte { return makeZip(t, archiveEntry{name: "pigo.exe", dir: true}) },
			goos:    "windows", wantErr: "pigo.exe not found",
		},
		{
			name:    "corrupt tar.gz",
			archive: func(t *testing.T) []byte { return []byte("not gzip") },
			goos:    "linux", wantErr: "reading gzip",
		},
		{
			name:    "corrupt zip",
			archive: func(t *testing.T) []byte { return []byte("not zip") },
			goos:    "windows", wantErr: "reading zip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extract(tt.archive(t), tt.goos)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("extracted %q, want %q", got, tt.want)
			}
		})
	}
}
