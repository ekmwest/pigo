package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "-h prints help", args: []string{"-h"}, wantCode: 0, wantStdout: "Usage: pigo serve [--port N] [--no-reload]"},
		{name: "-h mentions --no-reload", args: []string{"-h"}, wantCode: 0, wantStdout: "--no-reload   don't reload"},
		{name: "--no-reload is a known flag", args: []string{"--no-reload", "extra"}, wantCode: 2, wantStderr: "unexpected arguments"},
		{name: "unknown flag", args: []string{"-x"}, wantCode: 2, wantStderr: "flag provided but not defined: -x"},
		{name: "extra argument", args: []string{"extra"}, wantCode: 2, wantStderr: "unexpected arguments"},
		{name: "port too low", args: []string{"--port", "0"}, wantCode: 2, wantStderr: "invalid port 0"},
		{name: "port too high", args: []string{"--port", "70000"}, wantCode: 2, wantStderr: "invalid port 70000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Command().Run(tt.args, &stdout, &stderr)

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

type stubListener struct {
	net.Listener
	addr stubAddr
}

func (l stubListener) Addr() net.Addr { return l.addr }

type stubAddr string

func (a stubAddr) Network() string { return "tcp" }
func (a stubAddr) String() string  { return string(a) }

// fakeListen fails for the addresses in busy and succeeds for all others.
func fakeListen(busy ...string) func(addr string) (net.Listener, error) {
	return func(addr string) (net.Listener, error) {
		for _, b := range busy {
			if addr == b {
				return nil, errors.New("listen " + addr + ": address in use")
			}
		}
		return stubListener{addr: stubAddr(addr)}, nil
	}
}

func TestListen(t *testing.T) {
	tests := []struct {
		name     string
		tries    int
		busy     []string
		wantAddr string
		wantErr  string // substring; "" means no error
	}{
		{name: "first port free", tries: 10, wantAddr: "127.0.0.1:8080"},
		{name: "falls back", tries: 10, busy: []string{"127.0.0.1:8080", "127.0.0.1:8081"}, wantAddr: "127.0.0.1:8082"},
		{name: "explicit port busy", tries: 1, busy: []string{"127.0.0.1:8080"}, wantErr: "listen 127.0.0.1:8080: address in use"},
		{
			name:  "all ports busy",
			tries: 3,
			busy:  []string{"127.0.0.1:8080", "127.0.0.1:8081", "127.0.0.1:8082"},
			// The last attempt's error is the one reported.
			wantErr: "no free port between 8080 and 8082: listen 127.0.0.1:8082",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := listen(8080, tt.tries, fakeListen(tt.busy...))

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got := ln.Addr().String(); got != tt.wantAddr {
				t.Errorf("addr = %q, want %q", got, tt.wantAddr)
			}
		})
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

func TestHandler(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"index.html":     "root index",
		"a.txt":          "hello",
		".env":           "SECRET=1",
		".git/config":    "[core]",
		"sub/index.html": "sub index",
		"empty/b.txt":    "b",
	})

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string // substring
	}{
		{path: "/a.txt", wantStatus: http.StatusOK, wantBody: "hello"},
		{path: "/", wantStatus: http.StatusOK, wantBody: "root index"},
		{path: "/sub/", wantStatus: http.StatusOK, wantBody: "sub index"},
		{path: "/empty/", wantStatus: http.StatusNotFound},
		{path: "/empty/b.txt", wantStatus: http.StatusOK, wantBody: "b"},
		{path: "/.env", wantStatus: http.StatusNotFound},
		{path: "/.git/config", wantStatus: http.StatusNotFound},
		{path: "/missing", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var logs bytes.Buffer
			h := newHandler(dir, log.New(&logs, "", 0))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			// http.FileServer drops Cache-Control on its own error responses.
			if got := rec.Header().Get("Cache-Control"); tt.wantStatus == http.StatusOK && got != "no-store" {
				t.Errorf("Cache-Control = %q, want %q", got, "no-store")
			}
			wantLog := fmt.Sprintf("GET %s %d\n", tt.path, tt.wantStatus)
			if logs.String() != wantLog {
				t.Errorf("log = %q, want %q", logs.String(), wantLog)
			}
		})
	}
}

// A browser that cached a page from one folder must get the other folder's
// page after serve restarts elsewhere on the same port, even when that file is
// older and the browser revalidates with If-Modified-Since.
func TestNoCacheAcrossFolders(t *testing.T) {
	dirA := t.TempDir()
	writeFiles(t, dirA, map[string]string{"index.html": "folder A"})
	rec := httptest.NewRecorder()
	newHandler(dirA, log.New(io.Discard, "", 0)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	lastModified := rec.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("folder A response has no Last-Modified")
	}

	dirB := t.TempDir()
	writeFiles(t, dirB, map[string]string{"index.html": "folder B"})
	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dirB, "index.html"), older, older); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-Modified-Since", lastModified)
	rec = httptest.NewRecorder()
	newHandler(dirB, log.New(io.Discard, "", 0)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "folder B" {
		t.Errorf("body = %q, want %q", got, "folder B")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestServeShutdown(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "hello"})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, ln, newHandler(dir, log.New(io.Discard, "", 0)), &stdout)
	}()

	resp, err := http.Get("http://" + ln.Addr().String() + "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
	if !strings.Contains(stdout.String(), "Shutting down") {
		t.Errorf("stdout = %q, want it to contain %q", stdout.String(), "Shutting down")
	}
}
