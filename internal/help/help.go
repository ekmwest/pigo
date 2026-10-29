package help

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ekmwest/pigo/internal/cli"
)

// Command returns the help command. cmds is called when help runs and
// must return every pigo command, including help itself.
func Command(cmds func() []cli.Command) cli.Command {
	return cli.Command{
		Name:    "help",
		Summary: "Show help for pigo or a command",
		Help:    printHelp,
		Run: func(args []string, stdout, stderr io.Writer) int {
			return run(cmds(), args, stdout, stderr)
		},
	}
}

func Usage(w io.Writer, cmds []cli.Command) {
	fmt.Fprint(w, "Usage: pigo <command> [arguments]\n\nCommands:\n")
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-9s %s\n", c.Name, c.Summary)
	}
	fmt.Fprint(w, "\nRun \"pigo help <command>\" for details on a command.\n")
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: pigo help [command]

Without a command, list all commands.
With a command, show help for that command.
`)
}

func run(cmds []cli.Command, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {} // we print help ourselves, so it goes to the right stream
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(stdout)
			return 0
		}
		// flag has already printed the error, e.g. "flag provided but not defined: -x".
		printHelp(stderr)
		return 2
	}

	switch fs.NArg() {
	case 0:
		Usage(stdout, cmds)
		return 0
	case 1:
		name := fs.Arg(0)
		for _, c := range cmds {
			if c.Name == name {
				c.Help(stdout)
				return 0
			}
		}
		fmt.Fprintf(stderr, "pigo help: unknown command %q\n\n", name)
		Usage(stderr, cmds)
		return 2
	default:
		fmt.Fprintln(stderr, "pigo help: too many arguments")
		printHelp(stderr)
		return 2
	}
}
