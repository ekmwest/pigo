package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	const usageStart = "Usage: pigo <command> [arguments]"

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "no arguments", args: nil, wantCode: 0, wantStdout: usageStart},
		{name: "-h", args: []string{"-h"}, wantCode: 0, wantStdout: usageStart},
		{name: "-help", args: []string{"-help"}, wantCode: 0, wantStdout: usageStart},
		{name: "--help", args: []string{"--help"}, wantCode: 0, wantStdout: usageStart},
		{name: "help", args: []string{"help"}, wantCode: 0, wantStdout: usageStart},
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "dev\n"},
		{name: "command -h", args: []string{"version", "-h"}, wantCode: 0, wantStdout: "Usage: pigo version"},
		{name: "help command", args: []string{"help", "version"}, wantCode: 0, wantStdout: "Usage: pigo version"},
		{name: "help serve", args: []string{"help", "serve"}, wantCode: 0, wantStdout: "Usage: pigo serve"},
		{name: "unknown command", args: []string{"bogus"}, wantCode: 2, wantStderr: `unknown command "bogus"`},
		{name: "unknown command shows usage", args: []string{"bogus"}, wantCode: 2, wantStderr: usageStart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

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

// The linker silently ignores -X for a variable that doesn't exist, so a rename
// or move of main.version would only show up as "dev" in release builds.
func TestVersionLdflags(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found on PATH")
	}

	bin := filepath.Join(t.TempDir(), "pigo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command(goBin, "build", "-ldflags", "-X main.version=v1.2.3", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("pigo version: %v\n%s", err, out)
	}
	if got, want := string(out), "v1.2.3\n"; got != want {
		t.Errorf("pigo version = %q, want %q", got, want)
	}
}
