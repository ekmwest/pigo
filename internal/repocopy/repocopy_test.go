package repocopy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRepo(t *testing.T) {
	valid := map[string][2]string{
		"owner/repo": {"owner", "repo"},
		"a-b/c.d_e":  {"a-b", "c.d_e"},
	}
	for in, want := range valid {
		owner, repo, err := parseRepo(in)
		if err != nil || owner != want[0] || repo != want[1] {
			t.Errorf("parseRepo(%q) = %q, %q, %v; want %q, %q", in, owner, repo, err, want[0], want[1])
		}
	}

	invalid := []string{
		"",
		"owner",
		"owner/",
		"/repo",
		"owner/repo/",
		"owner/repo/tree/main",
		"owner/repo@main",
		"owner/repo#main",
		"owner/..",
		"owner/.",
		"github.com/owner/repo",
		"https://github.com/owner/repo",
		"git@github.com:owner/repo.git",
	}
	for _, in := range invalid {
		if _, _, err := parseRepo(in); err == nil {
			t.Errorf("parseRepo(%q) succeeded, want an error", in)
		}
	}
}

// server serves body as the tarball of owner/repo and counts requests.
func server(t *testing.T, status int, body []byte) (options, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/repos/owner/repo/tarball" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return options{apiURL: srv.URL, client: srv.Client(), userAgent: "pigo/test"}, &requests
}

func runCopy(opts options, dest string) error {
	return copyRepo(context.Background(), opts, "owner/repo", dest, io.Discard)
}

func TestCopyIntoEmptyDir(t *testing.T) {
	opts, _ := server(t, http.StatusOK, tarball(t, file(top+"a.txt", "a"), file(top+"README.md", "r")))
	dest := t.TempDir()
	if err := runCopy(opts, dest); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dest, "a.txt"), "a")
	assertMissing(t, filepath.Join(dest, "README.md"))
}

func TestCopyCreatesMissingDir(t *testing.T) {
	opts, _ := server(t, http.StatusOK, tarball(t, file(top+"a.txt", "a")))
	dest := filepath.Join(t.TempDir(), "x", "y")
	if err := runCopy(opts, dest); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dest, "a.txt"), "a")
}

func TestCopyRefusesNonEmptyDir(t *testing.T) {
	opts, requests := server(t, http.StatusOK, tarball(t, file(top+"a.txt", "a")))
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCopy(opts, dest)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v, want a not empty error", err)
	}
	if *requests != 0 {
		t.Errorf("made %d requests, want none", *requests)
	}
	assertMissing(t, filepath.Join(dest, "a.txt"))
}

func TestCopyRefusesFile(t *testing.T) {
	opts, _ := server(t, http.StatusOK, nil)
	dest := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(dest, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCopy(opts, dest); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want a not a directory error", err)
	}
}

func TestCopyNotFound(t *testing.T) {
	opts, _ := server(t, http.StatusNotFound, nil)
	dest := filepath.Join(t.TempDir(), "new")
	err := runCopy(opts, dest)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want a not found error", err)
	}
	assertMissing(t, dest)
}

func TestCopyRollsBack(t *testing.T) {
	data := tarball(t,
		file(top+"a.txt", "a"),
		file(top+"sub/b.txt", "b"),
		file(top+"../evil", "x"),
	)

	t.Run("existing dir", func(t *testing.T) {
		opts, _ := server(t, http.StatusOK, data)
		dest := t.TempDir()
		if err := runCopy(opts, dest); err == nil {
			t.Fatal("copy succeeded, want an error")
		}
		entries, err := os.ReadDir(dest)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("dest has %d entries after rollback, want 0", len(entries))
		}
	})

	t.Run("created dir", func(t *testing.T) {
		opts, _ := server(t, http.StatusOK, data)
		parent := t.TempDir()
		dest := filepath.Join(parent, "a", "b", "c")
		if err := runCopy(opts, dest); err == nil {
			t.Fatal("copy succeeded, want an error")
		}
		assertMissing(t, filepath.Join(parent, "a"))
	})
}
