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
If docs/CNAME exists and src/CNAME doesn't, it is copied to src/.
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
	res, err := Build(dir)
	if !Print("build", res, err, stdout, stderr) {
		return 1
	}
	return 0
}

// ErrNoSrc means the folder given to Build has no src folder.
var ErrNoSrc = errors.New("no src folder")

// Result describes what a build did.
type Result struct {
	Written int
	// Deleted lists the deleted files as slash paths relative to docs/.
	Deleted []string
	// Problems are the errors found while rendering, each prefixed with
	// its source file. If there are any, nothing was written.
	Problems []string
	// CopiedCNAME means docs/CNAME was copied to the missing src/CNAME.
	CopiedCNAME bool
	Duration    time.Duration
}

// Build builds dir/src into dir/docs. The error is ErrNoSrc (wrapped) or
// a failure while writing docs/; problems in the source are reported in
// the result instead.
func Build(dir string) (Result, error) {
	start := time.Now()
	var res Result

	src := filepath.Join(dir, "src")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return res, fmt.Errorf("%w in %s", ErrNoSrc, dir)
	}

	s, problems := render(src)
	if len(problems) > 0 {
		res.Problems = problems
		res.Duration = time.Since(start)
		return res, nil
	}

	docs := filepath.Join(dir, "docs")
	var err error
	res.CopiedCNAME, err = copyCNAMEToSrc(src, docs)
	if err != nil {
		res.Duration = time.Since(start)
		return res, fmt.Errorf("copy docs/CNAME to src/CNAME: %w", err)
	}
	res.Written, res.Deleted, err = syncDocs(docs, s)
	res.Duration = time.Since(start)
	return res, err
}

// Print prints the outcome of Build as the command cmd and reports
// whether the build succeeded.
func Print(cmd string, res Result, err error, stdout, stderr io.Writer) bool {
	if errors.Is(err, ErrNoSrc) {
		fmt.Fprintf(stderr, "pigo %s: %v\n", cmd, err)
		fmt.Fprintf(stderr, "Run %s from the project folder (the one that contains src/).\n", cmd)
		return false
	}

	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p)
		}
		word := "errors"
		if len(res.Problems) == 1 {
			word = "error"
		}
		fmt.Fprintf(stderr, "pigo %s: %d %s, nothing written\n", cmd, len(res.Problems), word)
		return false
	}

	if res.CopiedCNAME {
		fmt.Fprintln(stdout, "Copied docs/CNAME to src/CNAME")
	}
	for _, d := range res.Deleted {
		fmt.Fprintf(stdout, "deleted docs/%s\n", d)
	}
	if err != nil {
		fmt.Fprintf(stderr, "pigo %s: %v\n", cmd, err)
		return false
	}
	fmt.Fprintf(stdout, "built docs/ in %d ms (%d written, %d deleted)\n",
		res.Duration.Milliseconds(), res.Written, len(res.Deleted))
	return true
}
