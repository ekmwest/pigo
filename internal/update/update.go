package update

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ekmwest/pigo/internal/cli"
)

const (
	latestURL   = "https://api.github.com/repos/ekmwest/pigo/releases/latest"
	downloadURL = "https://github.com/ekmwest/pigo/releases/download"
	httpTimeout = 2 * time.Minute
)

func Command(v string) cli.Command {
	return cli.Command{
		Name:    "update",
		Summary: "Update pigo to the latest release",
		Help:    printHelp,
		Run: func(args []string, stdout, stderr io.Writer) int {
			return run(v, args, stdout, stderr)
		},
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: pigo update

Replace pigo with the latest release from GitHub.
The download is verified against the release checksums.
`)
}

func run(v string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
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
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "pigo update: unexpected arguments")
		printHelp(stderr)
		return 2
	}

	opts := options{
		current:     v,
		latestURL:   latestURL,
		downloadURL: downloadURL,
		goos:        runtime.GOOS,
		goarch:      runtime.GOARCH,
		client:      &http.Client{Timeout: httpTimeout},
	}
	// A dev build is refused before touching the executable or the network.
	if v != "dev" {
		exe, err := executable()
		if err != nil {
			fmt.Fprintf(stderr, "pigo update: %v\n", err)
			return 1
		}
		opts.exe = exe
	}
	if err := update(opts, stdout); err != nil {
		fmt.Fprintf(stderr, "pigo update: %v\n", err)
		return 1
	}
	return 0
}

// options holds everything update depends on, so tests can point it at a
// local server and a temporary executable.
type options struct {
	current     string // version of the running pigo
	latestURL   string // GitHub API endpoint for the latest release
	downloadURL string // base URL for release assets, followed by /<tag>/<file>
	exe         string // path of the executable to replace
	goos        string
	goarch      string
	client      *http.Client
}

func update(opts options, stdout io.Writer) error {
	if opts.current == "dev" {
		return errors.New("this is a development build; install a release with the install script to use update")
	}

	// An update on windows leaves the replaced exe behind as .old, because
	// a running exe can't be deleted. By now that process has usually exited.
	os.Remove(opts.exe + ".old")

	userAgent := "pigo/" + opts.current
	latest, err := latestTag(opts.client, opts.latestURL, userAgent)
	if err != nil {
		return err
	}
	if latest == opts.current {
		fmt.Fprintf(stdout, "pigo is already up to date (%s)\n", latest)
		return nil
	}

	// The exact tag rather than releases/latest, so the files can't come from
	// a release published after the tag lookup.
	base := opts.downloadURL + "/" + url.PathEscape(latest) + "/"
	asset := assetName(opts.goos, opts.goarch)

	checksums, err := download(opts.client, base+"checksums.txt", userAgent)
	if err != nil {
		return err
	}
	archive, err := download(opts.client, base+asset, userAgent)
	if errors.Is(err, errNotFound) {
		return fmt.Errorf("no release build for %s/%s in %s", opts.goos, opts.goarch, latest)
	}
	if err != nil {
		return err
	}
	if err := verify(archive, checksums, asset); err != nil {
		return err
	}
	bin, err := extract(archive, opts.goos)
	if err != nil {
		return fmt.Errorf("extracting %s: %w", asset, err)
	}

	if err := replaceExecutable(opts.exe, bin, opts.goos == "windows"); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "updated %s -> %s\n", opts.current, latest)
	return nil
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the pigo executable: %w", err)
	}
	// Replace the real file, not a symlink pointing at it.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("finding the pigo executable: %w", err)
	}
	return exe, nil
}
