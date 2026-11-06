package build

import (
	"bytes"
	"flag"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the expected output of the golden tests")

// TestGolden builds each testdata/<case>/src and compares the result:
//   - before/ (optional) is the docs/ folder before the build.
//   - docs/ is the expected docs/ after a successful build.
//   - stderr.txt, if present, makes it an error case: build must fail
//     with that output and leave docs/ exactly as before/.
//   - stdout.txt is the expected output, with the time as "N ms".
func TestGolden(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			testGolden(t, filepath.Join("testdata", e.Name()))
		})
	}
}

func testGolden(t *testing.T, caseDir string) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	copyTree(t, filepath.Join(caseDir, "src"), filepath.Join(dir, "src"))
	before := filepath.Join(caseDir, "before")
	hasBefore := exists(before)
	if hasBefore {
		copyTree(t, before, docs)
	}
	beforeTimes := modTimes(t, docs)

	var stdout, stderr bytes.Buffer
	code := buildAndPrint(dir, &stdout, &stderr)

	stderrFile := filepath.Join(caseDir, "stderr.txt")
	if exists(stderrFile) {
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		compareFile(t, stderrFile, stderr.String())
		if stdout.Len() > 0 {
			t.Errorf("stdout = %q, want nothing", stdout.String())
		}
		if !hasBefore {
			if exists(docs) {
				t.Errorf("docs/ was created")
			}
			return
		}
		if got, want := readTree(t, docs), readTree(t, before); !maps.Equal(got, want) {
			t.Errorf("docs/ changed:\n got %v\nwant %v", got, want)
		}
		if got := modTimes(t, docs); !maps.Equal(got, beforeTimes) {
			t.Errorf("timestamps in docs/ changed")
		}
		return
	}

	if code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
	elapsed := regexp.MustCompile(`in \d+ ms`)
	compareFile(t, filepath.Join(caseDir, "stdout.txt"), elapsed.ReplaceAllString(stdout.String(), "in N ms"))

	want := filepath.Join(caseDir, "docs")
	if *update {
		if err := os.RemoveAll(want); err != nil {
			t.Fatal(err)
		}
		copyTree(t, docs, want)
		return
	}
	got, wantTree := readTree(t, docs), readTree(t, want)
	for _, name := range slices.Sorted(maps.Keys(wantTree)) {
		if g, ok := got[name]; !ok {
			t.Errorf("missing docs/%s", name)
		} else if g != wantTree[name] {
			t.Errorf("docs/%s:\n got %q\nwant %q", name, g, wantTree[name])
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		if _, ok := wantTree[name]; !ok {
			t.Errorf("unexpected docs/%s", name)
		}
	}
}

func TestUnchangedFilesNotRewritten(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"src/_layouts/base.html": "<title>{{ title }}</title><main>{{ content }}</main>",
		"src/index.html":         "---\nlayout: base.html\ntitle: Home\n---\nHome",
		"src/about.md":           "Hello",
		"src/logo.png":           "PNG1",
		"src/style.css":          "body {}",
	})
	var stdout, stderr bytes.Buffer
	if code := buildAndPrint(dir, &stdout, &stderr); code != 0 {
		t.Fatalf("first build: exit code %d, stderr: %s", code, stderr.String())
	}

	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	docs := filepath.Join(dir, "docs")
	for name := range modTimes(t, docs) {
		if err := os.Chtimes(filepath.Join(docs, name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	// about.html has no <main>, so site_content.json stays the same.
	// logo.png keeps its size, so its bytes must be compared.
	writeFiles(t, dir, map[string]string{"src/about.md": "Bye", "src/logo.png": "PNG2"})
	stdout.Reset()
	if code := buildAndPrint(dir, &stdout, &stderr); code != 0 {
		t.Fatalf("second build: exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(2 written, 0 deleted)") {
		t.Errorf("stdout = %q, want 2 written", stdout.String())
	}
	changed := map[string]bool{"about.html": true, "logo.png": true}
	for name, mtime := range modTimes(t, docs) {
		if got := mtime.Equal(old); got == changed[name] {
			t.Errorf("docs/%s: rewritten = %v, want %v", name, !got, changed[name])
		}
	}
}

func TestMissingSrc(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := buildAndPrint(dir, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no src folder in "+dir) || !strings.Contains(stderr.String(), "project folder") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if exists(filepath.Join(dir, "docs")) {
		t.Errorf("docs/ was created")
	}
}

func TestRunRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"extra"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	stdout.Reset()
	if code := run([]string{"-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Usage: pigo build") {
		t.Errorf("-h: exit code %d, stdout %q", code, stdout.String())
	}
}

// compareFile compares got with the content of file, or with -update,
// writes got to file.
func compareFile(t *testing.T, file, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s:\n got %q\nwant %q", filepath.Base(file), got, want)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree returns the content of every file under dir, by slash path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	files := readTree(t, from)
	writeFiles(t, to, files)
}

// modTimes returns the modification time of every file under dir, or
// nothing if dir doesn't exist.
func modTimes(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	times := map[string]time.Time{}
	if !exists(dir) {
		return times
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		times[filepath.ToSlash(rel)] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return times
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// buildAndPrint builds dir like the build command and returns its exit code.
func buildAndPrint(dir string, stdout, stderr io.Writer) int {
	res, err := Build(dir)
	if !Print("build", res, err, stdout, stderr) {
		return 1
	}
	return 0
}
