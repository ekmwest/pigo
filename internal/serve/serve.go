package serve

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/ekmwest/pigo/internal/cli"
)

const (
	host        = "127.0.0.1"
	defaultPort = 8080
	// Only the default port falls back; an explicit --port is used as given.
	defaultPortTries = 10
	shutdownTimeout  = 5 * time.Second
)

func Command() cli.Command {
	return cli.Command{
		Name:    "serve",
		Summary: "Serve the current folder over HTTP",
		Help:    printHelp,
		Run:     run,
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: pigo serve [--port N] [--no-reload]

Serve the files in the current folder over HTTP on localhost.
Open pages reload when a file in the folder changes.

Flags:
  --port N      port to listen on (default 8080; if 8080 is busy,
                the next free port up to 8089 is used)
  --no-reload   don't reload pages when files change
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {} // we print help ourselves, so it goes to the right stream
	port := fset.Int("port", defaultPort, "")
	noReload := fset.Bool("no-reload", false, "")
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
		fmt.Fprintln(stderr, "pigo serve: unexpected arguments")
		printHelp(stderr)
		return 2
	}
	if *port < 1 || *port > 65535 {
		fmt.Fprintf(stderr, "pigo serve: invalid port %d\n", *port)
		return 2
	}

	tries := defaultPortTries
	fset.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			tries = 1
		}
	})

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "pigo serve: get current folder: %v\n", err)
		return 1
	}

	ln, err := listen(*port, tries, func(addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	})
	if err != nil {
		fmt.Fprintf(stderr, "pigo serve: %v\n", err)
		return 1
	}

	actual := ln.Addr().(*net.TCPAddr).Port
	if actual != *port {
		fmt.Fprintf(stdout, "Port %d is in use, using %d.\n", *port, actual)
	}
	fmt.Fprintf(stdout, "Serving %s at http://localhost:%d\n", dir, actual)
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling once shutdown starts, so a second
	// Ctrl+C kills the process instead of waiting for open requests.
	context.AfterFunc(ctx, stop)

	h := newHandler(dir, log.New(stdout, "", log.Ltime))
	if !*noReload {
		b := newBroadcaster()
		context.AfterFunc(ctx, b.close)
		h = withReload(h, b)
		go func() {
			if err := watch(ctx, dir, pollInterval, b.notify); err != nil {
				fmt.Fprintf(stderr, "pigo serve: live reload disabled: %v\n", err)
			}
		}()
	}
	if err := serve(ctx, ln, h, stdout); err != nil {
		fmt.Fprintf(stderr, "pigo serve: %v\n", err)
		return 1
	}
	return 0
}

// listen tries port, port+1, ... until one succeeds or tries runs out.
// Any listen error moves on to the next port, because detecting
// "address in use" specifically isn't portable across platforms.
func listen(port, tries int, listenFn func(addr string) (net.Listener, error)) (net.Listener, error) {
	var err error
	for i := 0; i < tries; i++ {
		var ln net.Listener
		ln, err = listenFn(net.JoinHostPort(host, fmt.Sprint(port+i)))
		if err == nil {
			return ln, nil
		}
	}
	if tries == 1 {
		return nil, err
	}
	return nil, fmt.Errorf("no free port between %d and %d: %w", port, port+tries-1, err)
}

func serve(ctx context.Context, ln net.Listener, h http.Handler, stdout io.Writer) error {
	srv := &http.Server{Handler: h}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	fmt.Fprintln(stdout, "Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func newHandler(dir string, logger *log.Logger) http.Handler {
	files := http.FileServer(noListingFS{http.Dir(dir)})
	return logRequests(logger, noCache(hideDotfiles(files)))
}

func logRequests(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Printf("%s %s %d", r.Method, r.URL.Path, rec.status)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// noCache turns off browser caching. Revalidating isn't enough: the same
// localhost URL may serve a different folder after a restart, and an older
// file there would make If-Modified-Since return the other folder's cached
// page as 304. The conditional headers are dropped because browsers that
// cached pages before no-store keep sending them.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r = r.Clone(r.Context())
		r.Header.Del("If-Modified-Since")
		r.Header.Del("If-None-Match")
		next.ServeHTTP(w, r)
	})
}

// hideDotfiles keeps files like .git/ and .env from being exposed by accident.
func hideDotfiles(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, seg := range strings.Split(r.URL.Path, "/") {
			if strings.HasPrefix(seg, ".") {
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// noListingFS reports directories without an index.html as missing, so
// http.FileServer responds 404 instead of listing their contents.
type noListingFS struct {
	fs http.FileSystem
}

func (n noListingFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.IsDir() {
		return f, nil
	}

	index, err := n.fs.Open(path.Join(name, "index.html"))
	if err != nil {
		f.Close()
		return nil, fs.ErrNotExist
	}
	index.Close()
	return f, nil
}
