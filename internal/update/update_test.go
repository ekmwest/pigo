package update

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "dev build refused", version: "dev", wantCode: 1, wantStderr: "pigo update: this is a development build"},
		{name: "-h prints help", version: "v1.2.3", args: []string{"-h"}, wantCode: 0, wantStdout: "Usage: pigo update"},
		{name: "extra argument", version: "v1.2.3", args: []string{"extra"}, wantCode: 2, wantStderr: "unexpected arguments"},
		{name: "unknown flag", version: "v1.2.3", args: []string{"-x"}, wantCode: 2, wantStderr: "flag provided but not defined: -x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Command(tt.version).Run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr == "" && stderr.Len() > 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// release is a fake GitHub serving one release on a local test server.
type release struct {
	tag       string
	files     map[string][]byte // asset name -> contents
	apiStatus int               // 0 means 200
}

func (r release) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/latest" {
			if r.apiStatus != 0 {
				w.WriteHeader(r.apiStatus)
				return
			}
			w.Write([]byte(`{"tag_name":"` + r.tag + `"}`))
			return
		}
		name, ok := strings.CutPrefix(req.URL.Path, "/download/"+r.tag+"/")
		data, found := r.files[name]
		if !ok || !found {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testOptions(t *testing.T, srv *httptest.Server, current, goos, goarch string) options {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "pigo")
	writeFile(t, exe, "old binary")
	return options{
		current:     current,
		latestURL:   srv.URL + "/latest",
		downloadURL: srv.URL + "/download",
		exe:         exe,
		goos:        goos,
		goarch:      goarch,
		client:      srv.Client(),
	}
}

func linuxRelease(t *testing.T, tag string) release {
	archive := makeTarGz(t, archiveEntry{name: "pigo", body: "new binary"})
	return release{
		tag: tag,
		files: map[string][]byte{
			"pigo_linux_amd64.tar.gz": archive,
			"checksums.txt":           []byte(sha256Hex(archive) + "  pigo_linux_amd64.tar.gz\n"),
		},
	}
}

func TestUpdate(t *testing.T) {
	srv := linuxRelease(t, "v0.0.2").serve(t)
	opts := testOptions(t, srv, "v0.0.1", "linux", "amd64")

	var stdout bytes.Buffer
	if err := update(opts, &stdout); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "updated v0.0.1 -> v0.0.2\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := readFile(t, opts.exe); got != "new binary" {
		t.Errorf("exe = %q, want %q", got, "new binary")
	}
}

func TestUpdateWindows(t *testing.T) {
	archive := makeZip(t, archiveEntry{name: "pigo.exe", body: "new exe"})
	srv := release{
		tag: "v0.0.2",
		files: map[string][]byte{
			"pigo_windows_arm64.zip": archive,
			"checksums.txt":          []byte(sha256Hex(archive) + "  pigo_windows_arm64.zip\n"),
		},
	}.serve(t)
	opts := testOptions(t, srv, "v0.0.1", "windows", "arm64")

	var stdout bytes.Buffer
	if err := update(opts, &stdout); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, opts.exe); got != "new exe" {
		t.Errorf("exe = %q, want %q", got, "new exe")
	}
	if got := readFile(t, opts.exe+".old"); got != "old binary" {
		t.Errorf(".old = %q, want %q", got, "old binary")
	}
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	srv := linuxRelease(t, "v0.0.2").serve(t)
	opts := testOptions(t, srv, "v0.0.2", "linux", "amd64")
	writeFile(t, opts.exe+".old", "leftover")

	var stdout bytes.Buffer
	if err := update(opts, &stdout); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "pigo is already up to date (v0.0.2)\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := readFile(t, opts.exe); got != "old binary" {
		t.Errorf("exe = %q, want it untouched", got)
	}
	if _, err := os.Stat(opts.exe + ".old"); !os.IsNotExist(err) {
		t.Errorf("leftover .old not removed (err = %v)", err)
	}
}

func TestUpdateErrors(t *testing.T) {
	tests := []struct {
		name    string
		release func(t *testing.T) release
		current string
		goarch  string
		wantErr string
	}{
		{
			name:    "dev build",
			release: func(t *testing.T) release { return linuxRelease(t, "v0.0.2") },
			current: "dev", goarch: "amd64",
			wantErr: "development build",
		},
		{
			name: "checksum mismatch",
			release: func(t *testing.T) release {
				r := linuxRelease(t, "v0.0.2")
				r.files["checksums.txt"] = []byte(sha256Hex([]byte("other")) + "  pigo_linux_amd64.tar.gz\n")
				return r
			},
			current: "v0.0.1", goarch: "amd64",
			wantErr: "checksum mismatch",
		},
		{
			name:    "no build for platform",
			release: func(t *testing.T) release { return linuxRelease(t, "v0.0.2") },
			current: "v0.0.1", goarch: "riscv64",
			wantErr: "no release build for linux/riscv64 in v0.0.2",
		},
		{
			name: "missing checksums",
			release: func(t *testing.T) release {
				r := linuxRelease(t, "v0.0.2")
				delete(r.files, "checksums.txt")
				return r
			},
			current: "v0.0.1", goarch: "amd64",
			wantErr: "checksums.txt",
		},
		{
			name: "rate limited",
			release: func(t *testing.T) release {
				r := linuxRelease(t, "v0.0.2")
				r.apiStatus = http.StatusForbidden
				return r
			},
			current: "v0.0.1", goarch: "amd64",
			wantErr: "rate limit",
		},
		{
			name: "no tag",
			release: func(t *testing.T) release {
				return linuxRelease(t, "")
			},
			current: "v0.0.1", goarch: "amd64",
			wantErr: "no tag_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.release(t).serve(t)
			opts := testOptions(t, srv, tt.current, "linux", tt.goarch)

			var stdout bytes.Buffer
			err := update(opts, &stdout)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if stdout.Len() > 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if got := readFile(t, opts.exe); got != "old binary" {
				t.Errorf("exe = %q, want it untouched", got)
			}
		})
	}
}
