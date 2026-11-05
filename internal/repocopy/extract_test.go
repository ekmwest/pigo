package repocopy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const top = "owner-repo-abc123/"

type entry struct {
	name     string
	typeflag byte
	body     string
	mode     int64
	linkname string
}

func file(name, body string) entry { return entry{name: name, typeflag: tar.TypeReg, body: body} }
func dir(name string) entry        { return entry{name: name, typeflag: tar.TypeDir} }

// tarball builds a gzipped tarball shaped like GitHub's: a pax global header
// followed by everything under a single top-level folder.
func tarball(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	global := &tar.Header{
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "abc123"},
	}
	if err := tw.WriteHeader(global); err != nil {
		t.Fatal(err)
	}
	for _, e := range append([]entry{dir(top)}, entries...) {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			Size:     int64(len(e.body)),
			Linkname: e.linkname,
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
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

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("reading %s: %v", path, err)
		return
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists, want it missing (err = %v)", path, err)
	}
}

func TestExtractStripsTopFolder(t *testing.T) {
	dest := t.TempDir()
	data := tarball(t,
		file(top+"index.html", "hi"),
		dir(top+"src/"),
		file(top+"src/a.txt", "a"),
		dir(top+"empty/"),
	)
	n, err := extract(bytes.NewReader(data), dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("files = %d, want 2", n)
	}
	assertFile(t, filepath.Join(dest, "index.html"), "hi")
	assertFile(t, filepath.Join(dest, "src", "a.txt"), "a")
	if info, err := os.Stat(filepath.Join(dest, "empty")); err != nil || !info.IsDir() {
		t.Errorf("empty dir not created: %v", err)
	}
	assertMissing(t, filepath.Join(dest, "owner-repo-abc123"))
	assertMissing(t, filepath.Join(dest, "pax_global_header"))
}

func TestExtractExcludesRootOnly(t *testing.T) {
	dest := t.TempDir()
	entries := []entry{
		dir(top + ".github/"),
		dir(top + ".github/workflows/"),
		file(top+".github/workflows/ci.yml", "x"),
		file(top+"docs/README.md", "docs"),
		file(top+"sub/LICENSE", "sub"),
		file(top+"sub/.github/x", "subgh"),
		file(top+"README", "plain"),
		file(top+".githubx", "near miss"),
	}
	for _, e := range excluded {
		if e != ".github" { // added as a folder above
			entries = append(entries, file(top+e, "x"))
		}
	}
	if _, err := extract(bytes.NewReader(tarball(t, entries...)), dest); err != nil {
		t.Fatal(err)
	}
	for _, e := range excluded {
		assertMissing(t, filepath.Join(dest, e))
	}
	assertFile(t, filepath.Join(dest, "docs", "README.md"), "docs")
	assertFile(t, filepath.Join(dest, "sub", "LICENSE"), "sub")
	assertFile(t, filepath.Join(dest, "sub", ".github", "x"), "subgh")
	assertFile(t, filepath.Join(dest, "README"), "plain")
	assertFile(t, filepath.Join(dest, ".githubx"), "near miss")
}

func TestExtractRejectsUnsafePaths(t *testing.T) {
	names := []string{
		top + "../evil",
		top + "../../evil",
		top + "a/../../evil",
		top + "/abs",
		top + `a\..\..\evil`,
	}
	if runtime.GOOS == "windows" {
		names = append(names, top+"C:/evil", top+"NUL")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := extract(bytes.NewReader(tarball(t, file(name, "x"))), dest)
			if err == nil {
				t.Fatal("extract succeeded, want an unsafe path error")
			}
			assertMissing(t, filepath.Join(parent, "evil"))
		})
	}
}

func TestExtractSkipsLinks(t *testing.T) {
	dest := t.TempDir()
	data := tarball(t,
		file(top+"a.txt", "a"),
		entry{name: top + "sym", typeflag: tar.TypeSymlink, linkname: "../../outside"},
		entry{name: top + "hard", typeflag: tar.TypeLink, linkname: top + "a.txt"},
		file(top+"b.txt", "b"),
	)
	n, err := extract(bytes.NewReader(data), dest)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("files = %d, want 2", n)
	}
	assertFile(t, filepath.Join(dest, "a.txt"), "a")
	assertFile(t, filepath.Join(dest, "b.txt"), "b")
	assertMissing(t, filepath.Join(dest, "sym"))
	assertMissing(t, filepath.Join(dest, "hard"))
}

func TestExtractKeepsExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no executable bit")
	}
	dest := t.TempDir()
	data := tarball(t,
		entry{name: top + "run.sh", typeflag: tar.TypeReg, body: "#!/bin/sh", mode: 0o755},
		file(top+"plain.txt", "x"),
	)
	if _, err := extract(bytes.NewReader(data), dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh mode = %v, want executable", info.Mode())
	}
	info, err = os.Stat(filepath.Join(dest, "plain.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("plain.txt mode = %v, want not executable", info.Mode())
	}
}
