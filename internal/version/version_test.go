package version

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // exact match
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "prints version", args: nil, wantCode: 0, wantStdout: "1.2.3\n"},
		{name: "-h prints help", args: []string{"-h"}, wantCode: 0, wantStdout: "Usage: pigo version\n\nPrint the version of pigo.\n"},
		{name: "extra argument", args: []string{"extra"}, wantCode: 2, wantStderr: "unexpected arguments"},
		{name: "unknown flag", args: []string{"-x"}, wantCode: 2, wantStderr: "flag provided but not defined: -x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Command("1.2.3").Run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
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
