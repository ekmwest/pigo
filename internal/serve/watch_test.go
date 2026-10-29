package serve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestIgnored(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "index.html", want: false},
		{name: "style.css", want: false},
		{name: "4913.html", want: false},
		{name: "#", want: false},
		{name: ".git", want: true},
		{name: ".DS_Store", want: true},
		{name: ".index.html.swp", want: true},
		{name: ".#index.html", want: true},
		{name: "index.html~", want: true},
		{name: "index.html.swp", want: true},
		{name: "index.html.swx", want: true},
		{name: "upload.tmp", want: true},
		{name: "#index.html#", want: true},
		{name: "4913", want: true},
		{name: "index.html___jb_tmp___", want: true},
		{name: "index.html___jb_old___", want: true},
	}

	for _, tt := range tests {
		if got := ignored(tt.name); got != tt.want {
			t.Errorf("ignored(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"index.html":      "root",
		"sub/a.css":       "a",
		"sub/deep/b.js":   "b",
		".git/config":     "[core]",
		".cache/x/y.html": "y",
		"index.html~":     "backup",
		"sub/a.css.swp":   "swap",
	})

	files, err := scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for p := range files {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	slices.Sort(got)
	want := []string{"index.html", "sub/a.css", "sub/deep/b.js"}
	if !slices.Equal(got, want) {
		t.Errorf("scan = %v, want %v", got, want)
	}
}

func TestScanMissingDir(t *testing.T) {
	if _, err := scan(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("err = nil, want an error")
	}
}

// startWatch runs watch on dir and returns a channel that receives a value
// for every onChange call.
func startWatch(t *testing.T, dir string, interval time.Duration) <-chan struct{} {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	changes := make(chan struct{}, 10)
	done := make(chan error, 1)
	go func() {
		done <- watch(ctx, dir, interval, func() { changes <- struct{}{} })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("watch = %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("watch did not return after cancel")
		}
	})
	// Give watch time to take its initial snapshot before the test writes.
	time.Sleep(interval)
	return changes
}

func expectChanges(t *testing.T, changes <-chan struct{}, want int, quiet time.Duration) {
	t.Helper()
	got := 0
	timeout := time.After(5 * time.Second)
	for got < want {
		select {
		case <-changes:
			got++
		case <-timeout:
			t.Fatalf("got %d change(s), want %d", got, want)
		}
	}
	select {
	case <-changes:
		t.Fatalf("got more than %d change(s)", want)
	case <-time.After(quiet):
	}
}

func TestWatch(t *testing.T) {
	const interval = 20 * time.Millisecond

	t.Run("edit fires once", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"index.html": "v1"})
		changes := startWatch(t, dir, interval)

		writeFiles(t, dir, map[string]string{"index.html": "version 2"})
		expectChanges(t, changes, 1, 10*interval)
	})

	t.Run("new file in new folder fires once", func(t *testing.T) {
		dir := t.TempDir()
		changes := startWatch(t, dir, interval)

		writeFiles(t, dir, map[string]string{"sub/new.html": "new"})
		expectChanges(t, changes, 1, 10*interval)
	})

	t.Run("delete fires once", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"a.txt": "a"})
		changes := startWatch(t, dir, interval)

		if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
			t.Fatal(err)
		}
		expectChanges(t, changes, 1, 10*interval)
	})

	t.Run("burst of writes fires once", func(t *testing.T) {
		// The burst must be shorter than the interval, so no scan pair
		// inside it can look quiet.
		const interval = 200 * time.Millisecond
		dir := t.TempDir()
		changes := startWatch(t, dir, interval)

		for i := range 5 {
			writeFiles(t, dir, map[string]string{"out.html": string(make([]byte, i+1))})
			time.Sleep(5 * time.Millisecond)
		}
		expectChanges(t, changes, 1, 5*interval)
	})

	t.Run("ignored files fire nothing", func(t *testing.T) {
		dir := t.TempDir()
		changes := startWatch(t, dir, interval)

		writeFiles(t, dir, map[string]string{
			".git/index":     "x",
			"index.html.swp": "x",
			"index.html~":    "x",
		})
		expectChanges(t, changes, 0, 10*interval)
	})

	t.Run("missing dir returns error", func(t *testing.T) {
		err := watch(context.Background(), filepath.Join(t.TempDir(), "missing"), interval, func() {})
		if err == nil {
			t.Error("err = nil, want an error")
		}
	})
}
