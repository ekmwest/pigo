package upgrade

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertNoTempFiles checks that no .pigo-upgrade-* file is left in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".pigo-upgrade-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pigo")
	writeFile(t, exe, "old")

	if err := replaceExecutable(exe, []byte("new"), false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, exe); got != "new" {
		t.Errorf("exe = %q, want %q", got, "new")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(exe)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
		}
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Errorf(".old exists, want none outside windows (err = %v)", err)
	}
	assertNoTempFiles(t, dir)
}

func TestReplaceExecutableWindows(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pigo.exe")
	writeFile(t, exe, "old")
	writeFile(t, exe+".old", "older")

	if err := replaceExecutable(exe, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, exe); got != "new" {
		t.Errorf("exe = %q, want %q", got, "new")
	}
	if got := readFile(t, exe+".old"); got != "old" {
		t.Errorf(".old = %q, want %q", got, "old")
	}
	assertNoTempFiles(t, dir)
}

func TestReplaceExecutableWindowsOldInUse(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pigo.exe")
	writeFile(t, exe, "old")
	// A non-empty directory can't be removed on any OS, which stands in for
	// the exe of a still running pigo on windows.
	if err := os.Mkdir(exe+".old", 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(exe+".old", "f"), "x")

	err := replaceExecutable(exe, []byte("new"), true)
	if err == nil || !strings.Contains(err.Error(), "an older pigo is still running (e.g. pigo serve); close it and try again") {
		t.Fatalf("err = %v, want the still running message", err)
	}
	if got := readFile(t, exe); got != "old" {
		t.Errorf("exe = %q, want it untouched", got)
	}
	assertNoTempFiles(t, dir)
}

func TestReplaceExecutableMissingDir(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "gone", "pigo")

	if err := replaceExecutable(exe, []byte("new"), false); err == nil || !strings.Contains(err.Error(), "replacing") {
		t.Errorf("err = %v, want a replacing error", err)
	}
}

func TestReplaceErrorPermissionHint(t *testing.T) {
	if err := replaceError("/usr/local/bin/pigo", os.ErrPermission, false); !strings.Contains(err.Error(), "sudo pigo upgrade") {
		t.Errorf("unix hint missing: %v", err)
	}
	if err := replaceError(`C:\Program Files\pigo\pigo.exe`, os.ErrPermission, true); !strings.Contains(err.Error(), "Run as administrator") {
		t.Errorf("windows hint missing: %v", err)
	}
}
