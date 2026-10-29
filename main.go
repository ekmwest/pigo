package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ekmwest/pigo/internal/cli"
	"github.com/ekmwest/pigo/internal/help"
	"github.com/ekmwest/pigo/internal/serve"
	"github.com/ekmwest/pigo/internal/upgrade"

	// Aliased because the name "version" is taken by the variable below,
	// which must keep that name for -ldflags "-X main.version=...".
	versioncmd "github.com/ekmwest/pigo/internal/version"
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var commands []cli.Command
	commands = []cli.Command{
		help.Command(func() []cli.Command { return commands }),
		serve.Command(),
		upgrade.Command(version),
		versioncmd.Command(version),
	}

	if len(args) == 0 {
		args = []string{"help"}
	}
	switch args[0] {
	case "-h", "-help", "--help":
		args = []string{"help"}
	}

	for _, c := range commands {
		if c.Name == args[0] {
			return c.Run(args[1:], stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "pigo: unknown command %q\n\n", args[0])
	help.Usage(stderr, commands)
	return 2
}
