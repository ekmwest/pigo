package help

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ekmwest/pigo/internal/cli"
)

func testCommands() []cli.Command {
	var cmds []cli.Command
	cmds = []cli.Command{
		Command(func() []cli.Command { return cmds }),
		{
			Name:    "greet",
			Summary: "Say hello",
			Help:    func(w io.Writer) { fmt.Fprintln(w, "greet help") },
			Run:     func(args []string, stdout, stderr io.Writer) int { return 0 },
		},
	}
	return cmds
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "lists commands", args: nil, wantCode: 0, wantStdout: "  greet     Say hello\n"},
		{name: "lists itself", args: nil, wantCode: 0, wantStdout: "  help      Show help for pigo or a command\n"},
		{name: "command help", args: []string{"greet"}, wantCode: 0, wantStdout: "greet help\n"},
		{name: "own help via -h", args: []string{"-h"}, wantCode: 0, wantStdout: "Usage: pigo help [command]"},
		{name: "own help via name", args: []string{"help"}, wantCode: 0, wantStdout: "Usage: pigo help [command]"},
		{name: "unknown command", args: []string{"bogus"}, wantCode: 2, wantStderr: `unknown command "bogus"`},
		{name: "too many arguments", args: []string{"a", "b"}, wantCode: 2, wantStderr: "too many arguments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := testCommands()[0].Run(tt.args, &stdout, &stderr)

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
