package repocopy

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ekmwest/pigo/internal/cli"
)

// excluded are left out when they sit at the repo root. A directory entry
// excludes everything under it.
var excluded = []string{
	"LICENSE",
	"LICENCE",
	"CNAME",
	"README.md",
	"CODE_OF_CONDUCT.md",
	"CONTRIBUTING.md",
	"SECURITY.md",
	".gitignore",
	".gitattributes",
	".github",
}

const (
	apiURL = "https://api.github.com"
	// The body is streamed within this timeout, so it must cover a large repo.
	httpTimeout = 10 * time.Minute
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

func Command(v string) cli.Command {
	return cli.Command{
		Name:    "copy",
		Summary: "Copy a GitHub repo's files into a folder",
		Help:    printHelp,
		Run: func(args []string, stdout, stderr io.Writer) int {
			return run(v, args, stdout, stderr)
		},
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `Usage: pigo copy owner/repo [folder]

Copy the files of a GitHub repo (default branch, no git history) into
folder, like a project template. Without folder, copy into the current
folder. A missing folder is created; an existing one must be empty.

These are left out at the repo root:
  %s
`, strings.Join(excluded, ", "))
}

func run(v string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("copy", flag.ContinueOnError)
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
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, "pigo copy: expected owner/repo and an optional folder")
		printHelp(stderr)
		return 2
	}
	repo := fs.Arg(0)
	dest := "."
	if fs.NArg() == 2 {
		dest = fs.Arg(1)
	}

	// Ctrl+C cancels the download, so the partial copy is rolled back
	// instead of left behind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	opts := options{
		apiURL:    apiURL,
		client:    &http.Client{Timeout: httpTimeout},
		userAgent: "pigo/" + v,
	}
	if err := copyRepo(ctx, opts, repo, dest, stdout); err != nil {
		fmt.Fprintf(stderr, "pigo copy: %v\n", err)
		return 1
	}
	return 0
}

// options holds everything copyRepo depends on, so tests can point it at a
// local server.
type options struct {
	apiURL    string
	client    *http.Client
	userAgent string
}

func parseRepo(s string) (owner, repo string, err error) {
	owner, repo, _ = strings.Cut(s, "/")
	if !repoPattern.MatchString(s) || repo == "." || repo == ".." {
		return "", "", fmt.Errorf("invalid repository %q: expected owner/repo, e.g. ekmwest/pigo", s)
	}
	return owner, repo, nil
}

func copyRepo(ctx context.Context, opts options, repoArg, dest string, stdout io.Writer) error {
	owner, repo, err := parseRepo(repoArg)
	if err != nil {
		return err
	}
	exists, err := checkTarget(dest)
	if err != nil {
		return err
	}

	// The response is checked before anything is created, so a missing repo
	// leaves no trace.
	body, err := fetchTarball(ctx, opts, owner, repo)
	if err != nil {
		return err
	}
	defer body.Close()

	created := ""
	if !exists {
		created, err = topMissing(dest)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dest, err)
		}
	}

	n, err := extract(body, dest)
	if err != nil {
		if created != "" {
			os.RemoveAll(created)
		} else {
			clearDir(dest)
		}
		return fmt.Errorf("copying %s/%s: %w", owner, repo, err)
	}
	fmt.Fprintf(stdout, "copied %s/%s into %s (%d files)\n", owner, repo, dest, n)
	return nil
}

// checkTarget reports whether dest exists. An existing dest must be an empty
// directory.
func checkTarget(dest string) (bool, error) {
	info, err := os.Stat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", dest)
	}
	f, err := os.Open(dest)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != io.EOF {
		if err != nil {
			return false, fmt.Errorf("reading %s: %w", dest, err)
		}
		return false, fmt.Errorf("%s is not empty", dest)
	}
	return true, nil
}

// topMissing returns the outermost directory that MkdirAll(dest) would create,
// so a rollback removes exactly the directories copy made.
func topMissing(dest string) (string, error) {
	p, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	top := ""
	for {
		_, err := os.Stat(p)
		if err == nil {
			return top, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		top = p
		parent := filepath.Dir(p)
		if parent == p {
			return top, nil
		}
		p = parent
	}
}

// clearDir removes everything in dir. It's only used on a dir that was empty
// before the copy started.
func clearDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

func fetchTarball(ctx context.Context, opts options, owner, repo string) (io.ReadCloser, error) {
	url := opts.apiURL + "/repos/" + owner + "/" + repo + "/tarball"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading %s/%s: %w", owner, repo, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", opts.userAgent)

	resp, err := opts.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s/%s: %w", owner, repo, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, fmt.Errorf("repository %s/%s not found (it doesn't exist or is private)", owner, repo)
	case http.StatusForbidden, http.StatusTooManyRequests:
		resp.Body.Close()
		// Unauthenticated API calls are limited to 60 an hour per IP address.
		return nil, fmt.Errorf("downloading %s/%s: GitHub refused the request (%s), probably the API rate limit; try again later", owner, repo, resp.Status)
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("downloading %s/%s: %s", owner, repo, resp.Status)
	}
}
