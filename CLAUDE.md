# pigo

pigo is a CLI tool written in Go.


## Principles

- Style: Use idiomatic Go patterns whenever possible.
- Readability: The code should be simple, explicit, and easy to read. Avoid clever abstractions.
- Dependencies: Prefer the Go standard library. Only add a third-party dependency if really needed.


## Commands

- `pigo serve`: launch a static web server for the current folder on localhost (`-p, --port`; an explicit port is used as given, otherwise 4444 falls back to the next free port up to 4453).
    - Live reload is always on: browsers refresh when any file in the served folder is added, changed or deleted. Dotfiles, editor temp files and `node_modules` folders are ignored (`node_modules` is still served, just not watched). `--no-reload` turns it off. The polling watcher lives in `internal/watch` (shared with dev).
    - Only visible tabs hold a reload stream (SSE): the script opens it while `document.visibilityState` is visible and closes it on `visibilitychange` to hidden, because browsers allow only 6 HTTP/1.1 connections per host. The Broadcaster keeps a version (server start time + a counter `Notify` increments). It is written into the injected script and sent as the first event of every stream. A page reloads on a reload event, or when a stream's version differs from its own. That covers changes missed while hidden and server restarts.
    - Don't write to stdout or stderr in response to a file change or a reload (including request logs for the reload stream). If output is redirected to a file inside the served folder, each write is itself a change, causing an endless reload loop.
    - Every 404 (missing file, folder without `index.html`, dotfile path) is answered with `<root>/404.html` as `text/html` with status 404, like GitHub Pages (`notFoundPage` intercepts the status the file handler writes). The file is read on every 404; live reload is injected into it too. Without a readable `404.html`, Go's plain text 404 stays. `/404.html` itself is a normal 200.
    - Caching is disabled on purpose: responses send `Cache-Control: no-store`, and conditional request headers (`If-Modified-Since`, `If-None-Match`) are ignored. Browsers cache by URL, and `localhost:<port>` points at a different folder every time serve is restarted elsewhere, so a revalidated copy (a 304) can be a page from another folder.
- `pigo copy owner/repo [folder]`: copies the files of a GitHub repo (default branch, no git history) into a folder, like a project template (`internal/repocopy`).
    - Input is only `owner/repo`; URLs, branches and subpaths are rejected. Without a folder, the current folder is used; a missing folder is created.
    - The target must be empty (hidden files count). Otherwise copy refuses and changes nothing.
    - Downloads `https://api.github.com/repos/owner/repo/tarball` once and extracts it while streaming (stdlib `archive/tar` + `os.Root`; no temp archive, no external tar). The archive's top-level folder is stripped.
    - Root-only excludes (LICENSE, README.md, .github, …) are the `excluded` list in `internal/repocopy/repocopy.go`.
    - Only regular files and directories are extracted; a path that would land outside the target aborts the copy. A 404 means the repo doesn't exist or is private.
    - On any failure (including Ctrl+C), everything written is removed, so the folder is left as it was.
- `pigo build`: small static site generator, builds `./src` into `./docs` (`internal/build`). A Go port of piko's build (https://github.com/learnpoint/piko/blob/main/build.js); existing piko sites must produce the same output, except for the differences listed here.
    - No arguments or flags. Only `src/` is required; the top-level `src/_includes` and `src/_layouts` are optional and never copied.
    - Pages (`.html`, `.svg`, `.md`): flat frontmatter (no YAML library), markdown via goldmark (GFM, raw HTML allowed; `.md` becomes `.html`), `layout` with `{{ content }}`, `{{ name || default }}` variables, and include comments like `<!-- card.html, { src: "a.png" } -->`. Props are parsed, never evaluated. Other files are copied.
    - piko's regexes are ported as they are, with JavaScript semantics for `.`, `\s` and `trim()` (`jstext.go`). The lookahead in piko's `<main>` regex is hand-ported (`mainTagContent`).
    - `build.Build` returns a `Result` (written count, deleted files, problems, duration) and prints nothing. `build.Print` prints it, so build and dev print the same lines.
    - All or nothing: everything is rendered in memory first. Any problem (missing layout/include, layout without `{{ content }}`, invalid props, include cycle, bad frontmatter line, output clash) is printed with its source file, nothing is written, exit 1.
    - Writes a file only when its content differs, so unchanged files keep their timestamps (dev's reload signal depends on this). No timestamp logic, no force flag.
    - Deletes files and folders in `docs/` without a source, except root `CNAME`, `.nojekyll` and `site_content.json`. Writes to `src/` only to copy a root `docs/CNAME` to a missing `src/CNAME` (exact name, never overwrites), printed as `Copied docs/CNAME to src/CNAME`; it happens with the other writes, so a failed build writes nothing. Creates `docs/.nojekyll`, and writes `docs/site_content.json` (`type Page`) in piko's `JSON.stringify(x, null, 4)` format.
    - Golden tests: `internal/build/testdata/<case>/` holds `src/`, optionally `before/` (the existing `docs/`), and either the expected `docs/` plus `stdout.txt`, or `stderr.txt` for error cases. Optionally `src-after/` (the expected `src/` after the build); without it, `src/` must be unchanged. Run `go test ./internal/build -update` to regenerate, then review the diff.
- `pigo dev`: builds `./src` into `./docs`, serves `./docs` with live reload, and rebuilds when `./src` changes (`internal/dev`). A Go port of piko's dev (https://github.com/learnpoint/piko/blob/main/dev.js), built from build and serve.
    - Only `-p, --port`, which works exactly like serve's. No `--no-reload` (that's what build + serve are for) and no arguments. `src/` is required (same error as build).
    - Serves `docs/` with serve's handler (no-store, hidden dotfiles, no listings). Logs only failed requests (404, 5xx), never the reload stream.
    - Watches only `src/` with `internal/watch`. Rebuilds run inside the watcher's callback, so they never overlap. The watcher ignores dotfiles, editor temp files and `node_modules` folders, which build still copies, so a change to only such a file doesn't rebuild until something else changes.
    - Reloads browsers only when a build wrote or deleted at least one file. There is no watcher on `docs/`: build's counts are the signal (`build.Result`).
    - Build errors are printed, `docs/` stays as it was (build is all or nothing), nothing reloads. A failed first build still starts the server and watcher. After each build, it prints build's own lines (via `build.Print`).
- `pigo version`: prints the version. It is set at build time with `-ldflags "-X main.version=vX.Y.Z"` (the `version` variable in `main.go`; release builds inject the git tag, `v` included). Without it, the output is `dev`.
- `pigo help`: prints help documentation.
- `pigo upgrade`: self-upgrades the cli from the latest GitHub release (`releases/latest` tag; refuses on `dev`; plain inequality, no semver). Downloads the archive for GOOS/GOARCH and `checksums.txt` for that exact tag, verifies the sha256, and swaps the executable via a temp file next to it. On windows the running exe is renamed to `pigo.exe.old` first; a leftover `.old` is removed on the next upgrade run.


## Structure

- The `main` package lives at the repository root.
- Command implementations live in packages under `internal/`.


## Conventions

- Comments explain why, not what. Don't write comments that restate the code.
- Before considering a task done, run `go vet ./...` and `go test ./...`.
- Code must be `gofmt`-formatted.
- Return errors with context (`fmt.Errorf("...: %w", err)`); don't panic. User-facing errors go to stderr, followed by a non-zero exit code.
- Write tests for logic that can be tested without the network.
- LF line endings everywhere (see `.editorconfig` and `.gitattributes`).


## Workflow

- For anything beyond a small change, propose a plan before writing code.
- Keep changes small and focused. Don't refactor unrelated code.
- Don't commit. Changes are reviewed and committed manually.
