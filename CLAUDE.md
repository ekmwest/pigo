# pigo

pigo is a CLI tool written in Go.


## Principles

- Style: Use idiomatic Go patterns whenever possible.
- Readability: The code should be simple, explicit, and easy to read. Avoid clever abstractions.
- Dependencies: Prefer the Go standard library. Only add a third-party dependency if really needed.


## Commands

- `pigo serve`: launch a static web server for the current folder on localhost (`-p` sets the port).
    - Live reload is always on: browsers refresh when any file in the served folder is added, changed or deleted. Dotfiles and editor temp files are ignored. `--no-reload` turns it off.
    - Don't write to stdout or stderr in response to a file change or a reload (including request logs for the reload stream). If output is redirected to a file inside the served folder, each write is itself a change, causing an endless reload loop.
    - Caching is disabled on purpose: responses send `Cache-Control: no-store`, and conditional request headers (`If-Modified-Since`, `If-None-Match`) are ignored. Browsers cache by URL, and `localhost:<port>` points at a different folder every time serve is restarted elsewhere, so a revalidated copy (a 304) can be a page from another folder.
- `pigo version`: prints the version. It is set at build time with `-ldflags "-X main.version=vX.Y.Z"` (the `version` variable in `main.go`; release builds inject the git tag, `v` included). Without it, the output is `dev`.
- `pigo help`: prints help documentation.
- `pigo update`: self-updates the cli from the latest GitHub release (`releases/latest` tag; refuses on `dev`; plain inequality, no semver). Downloads the archive for GOOS/GOARCH and `checksums.txt` for that exact tag, verifies the sha256, and swaps the executable via a temp file next to it. On windows the running exe is renamed to `pigo.exe.old` first; a leftover `.old` is removed on the next update run.


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
