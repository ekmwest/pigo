package dev

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRebuild(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	writeFiles(t, dir, map[string]string{
		"src/_layouts/base.html": "<main>{{ content }}</main>",
		"src/index.html":         "---\nlayout: base.html\n---\nv1",
		"src/old.txt":            "old",
	})

	var stdout, stderr bytes.Buffer
	check := func(step string, wantReload bool, wantStdout, wantStderr string) {
		t.Helper()
		if got := rebuild(dir, &stdout, &stderr); got != wantReload {
			t.Errorf("%s: reload = %v, want %v", step, got, wantReload)
		}
		if !strings.Contains(stdout.String(), wantStdout) {
			t.Errorf("%s: stdout = %q, want it to contain %q", step, stdout.String(), wantStdout)
		}
		if wantStderr == "" && stderr.Len() > 0 || !strings.Contains(stderr.String(), wantStderr) {
			t.Errorf("%s: stderr = %q, want %q", step, stderr.String(), wantStderr)
		}
		stdout.Reset()
		stderr.Reset()
	}

	check("first build", true, "written, 0 deleted)", "")

	// index.html has a <main>, so site_content.json changes with it.
	writeFiles(t, dir, map[string]string{"src/index.html": "---\nlayout: base.html\n---\nv2"})
	check("edit", true, "(2 written, 0 deleted)", "")
	if got := readTree(t, docs)["index.html"]; got != "<main>v2</main>" {
		t.Errorf("docs/index.html = %q", got)
	}

	// The save changes the timestamp but not the content.
	writeFiles(t, dir, map[string]string{"src/index.html": "---\nlayout: base.html\n---\nv2"})
	check("save without changes", false, "(0 written, 0 deleted)", "")

	before := readTree(t, docs)
	beforeTimes := modTimes(t, docs)
	writeFiles(t, dir, map[string]string{"src/index.html": "---\nlayout: missing.html\n---\nv3"})
	check("build error", false, "", "pigo dev: 1 error, nothing written")
	if !maps.Equal(readTree(t, docs), before) || !maps.Equal(modTimes(t, docs), beforeTimes) {
		t.Errorf("docs/ changed after a failed build")
	}

	writeFiles(t, dir, map[string]string{"src/index.html": "---\nlayout: base.html\n---\nv3"})
	check("fix", true, "(2 written, 0 deleted)", "")

	if err := os.Remove(filepath.Join(dir, "src", "old.txt")); err != nil {
		t.Fatal(err)
	}
	check("delete", true, "deleted docs/old.txt\n", "")
}

func TestRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"extra"}, &stdout, &stderr); code != 2 {
		t.Errorf("arguments: exit code = %d, want 2", code)
	}
	if code := run([]string{"--port", "0"}, &stdout, &stderr); code != 2 {
		t.Errorf("port 0: exit code = %d, want 2", code)
	}
	if code := run([]string{"-p", "0"}, &stdout, &stderr); code != 2 {
		t.Errorf("-p 0: exit code = %d, want 2", code)
	}
	stdout.Reset()
	if code := run([]string{"-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Usage: pigo dev") {
		t.Errorf("-h: exit code %d, stdout %q", code, stdout.String())
	}
}

func TestRunMissingSrc(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "pigo dev: no src folder in ") || !strings.Contains(stderr.String(), "Run dev from the project folder") {
		t.Errorf("stderr = %q", stderr.String())
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
	walkFiles(t, dir, func(rel, p string, _ fs.DirEntry) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[rel] = string(b)
	})
	return files
}

func modTimes(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	times := map[string]time.Time{}
	walkFiles(t, dir, func(rel, _ string, d fs.DirEntry) {
		info, err := d.Info()
		if err != nil {
			t.Fatal(err)
		}
		times[rel] = info.ModTime()
	})
	return times
}

func walkFiles(t *testing.T, dir string, fn func(rel, p string, d fs.DirEntry)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		fn(filepath.ToSlash(rel), p, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
