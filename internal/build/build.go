// Package build implements pigo build: a small static site generator
// that builds ./src into ./docs. It is a Go port of piko's build
// (https://github.com/learnpoint/piko/blob/main/build.js).
package build

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ekmwest/pigo/internal/cli"
)

func Command() cli.Command {
	return cli.Command{
		Name:    "build",
		Summary: "Build the site in ./src into ./docs",
		Help:    printHelp,
		Run:     run,
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: pigo build

Build the site in ./src into ./docs. Run it from the project folder.

Pages (.html, .svg, .md) can have frontmatter, a layout from
src/_layouts, {{ variables }} and <!-- include.html --> comments that
insert files from src/_includes. Markdown pages become .html files.
Other files are copied as they are.

Only files whose content changed are written. Files in docs/ without a
source are deleted, except CNAME, .nojekyll and site_content.json.
If any page has an error, all errors are printed and nothing is written.
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("build", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {} // we print help ourselves, so it goes to the right stream
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(stdout)
			return 0
		}
		// flag has already printed the error, e.g. "flag provided but not defined: -x".
		printHelp(stderr)
		return 2
	}
	if fset.NArg() > 0 {
		fmt.Fprintln(stderr, "pigo build: unexpected arguments")
		printHelp(stderr)
		return 2
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "pigo build: %v\n", err)
		return 1
	}
	return Build(dir, stdout, stderr)
}

// Build builds dir/src into dir/docs and returns the process exit code.
func Build(dir string, stdout, stderr io.Writer) int {
	start := time.Now()

	src := filepath.Join(dir, "src")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "pigo build: no src folder in %s\n", dir)
		fmt.Fprintln(stderr, "Run build from the project folder (the one that contains src/).")
		return 1
	}

	s, problems := render(src)
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(stderr, p)
		}
		word := "errors"
		if len(problems) == 1 {
			word = "error"
		}
		fmt.Fprintf(stderr, "pigo build: %d %s, nothing written\n", len(problems), word)
		return 1
	}

	written, deleted, err := syncDocs(filepath.Join(dir, "docs"), s, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "pigo build: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "built docs/ in %d ms (%d written, %d deleted)\n",
		time.Since(start).Milliseconds(), written, deleted)
	return 0
}
