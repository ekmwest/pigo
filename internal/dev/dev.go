// Package dev implements pigo dev: build ./src into ./docs, serve ./docs
// with live reload, and rebuild when ./src changes. It is a Go port of
// piko's dev (https://github.com/learnpoint/piko/blob/main/dev.js).
package dev

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ekmwest/pigo/internal/build"
	"github.com/ekmwest/pigo/internal/cli"
	"github.com/ekmwest/pigo/internal/serve"
	"github.com/ekmwest/pigo/internal/watch"
)

func Command() cli.Command {
	return cli.Command{
		Name:    "dev",
		Summary: "Build ./src into ./docs, serve it, rebuild on change",
		Help:    printHelp,
		Run:     run,
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: pigo dev [--port N]

Build the site in ./src into ./docs, serve ./docs over HTTP on
localhost, and rebuild when a file in ./src changes. Open pages reload
when a rebuild changes docs/. Run it from the project folder.

If a build fails, the errors are printed and the last good docs/ is
still served. Fixing the source is enough to rebuild.

Flags:
  --port N   port to listen on (default 8080; if 8080 is busy,
             the next free port up to 8089 is used)
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("dev", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {} // we print help ourselves, so it goes to the right stream
	port := fset.Int("port", serve.DefaultPort, "")
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
		fmt.Fprintln(stderr, "pigo dev: unexpected arguments")
		printHelp(stderr)
		return 2
	}
	if *port < 1 || *port > 65535 {
		fmt.Fprintf(stderr, "pigo dev: invalid port %d\n", *port)
		return 2
	}
	portSet := false
	fset.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portSet = true
		}
	})

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "pigo dev: get current folder: %v\n", err)
		return 1
	}

	// A failed first build doesn't stop dev: the source can be fixed
	// while it runs. Only a missing src/ is fatal.
	res, err := build.Build(dir)
	build.Print("dev", res, err, stdout, stderr)
	if errors.Is(err, build.ErrNoSrc) {
		return 1
	}

	ln, err := serve.Listen(*port, portSet)
	if err != nil {
		fmt.Fprintf(stderr, "pigo dev: %v\n", err)
		return 1
	}

	docs := filepath.Join(dir, "docs")
	actual := ln.Addr().(*net.TCPAddr).Port
	if actual != *port {
		fmt.Fprintf(stdout, "Port %d is in use, using %d.\n", *port, actual)
	}
	fmt.Fprintf(stdout, "Serving %s at http://localhost:%d\n", docs, actual)
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling once shutdown starts, so a second
	// Ctrl+C kills the process instead of waiting for open requests.
	context.AfterFunc(ctx, stop)

	b := serve.NewBroadcaster()
	context.AfterFunc(ctx, b.Close)
	h := serve.WithReload(serve.NewHandler(docs, log.New(stdout, "", log.Ltime), serve.LogFailed), b)

	// Watch calls onChange from its own loop, so rebuilds never overlap.
	go func() {
		err := watch.Watch(ctx, filepath.Join(dir, "src"), watch.PollInterval, func() {
			if rebuild(dir, stdout, stderr) {
				b.Notify()
			}
		})
		if err != nil {
			fmt.Fprintf(stderr, "pigo dev: watching src: %v\n", err)
		}
	}()

	if err := serve.Serve(ctx, ln, h, stdout); err != nil {
		fmt.Fprintf(stderr, "pigo dev: %v\n", err)
		return 1
	}
	return 0
}

// rebuild builds dir, prints the result, and reports whether browsers
// should reload. Build writes only files whose content changed, so its
// counts tell whether docs/ changed; no second watcher on docs/ is needed.
func rebuild(dir string, stdout, stderr io.Writer) bool {
	res, err := build.Build(dir)
	ok := build.Print("dev", res, err, stdout, stderr)
	return ok && res.Written+len(res.Deleted) > 0
}
