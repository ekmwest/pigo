package cli

import "io"

type Command struct {
	Name string
	// Summary is a single line, shown in the command list.
	Summary string
	Help    func(w io.Writer)
	// Run gets the arguments after the command name and returns the
	// process exit code.
	Run func(args []string, stdout, stderr io.Writer) int
}
