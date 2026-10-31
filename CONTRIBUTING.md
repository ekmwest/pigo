To install pigo as a user, see the [readme](README.md#install). This file is for working on pigo itself.


## Getting started

1. Install go (the version in `go.mod` or newer): https://go.dev/doc/install
2. Clone the repo and enter it:

       git clone https://github.com/ekmwest/pigo.git
       cd pigo

3. Check that everything builds and passes:

       go vet ./...
       go test ./...


## Running the dev version

Set up a `pigo-dev` command that builds your local source and runs it in the current folder. It takes the same arguments as `pigo`, so you can use it anywhere you would use the released version:

    pigo-dev serve
    pigo-dev help

Replace `<path to repo>` below with the folder you cloned into.

windows (powershell): add this to your profile (`notepad $PROFILE`, create the file if it doesn't exist), then open a new terminal:

    function pigo-dev {
        go build -C <path to repo>
        if ($LASTEXITCODE -eq 0) { & <path to repo>\pigo.exe @args }
    }

If powershell says running scripts is disabled, run `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` once.

macos and linux: add this to `~/.zshrc` or `~/.bashrc`, then open a new terminal:

    pigo-dev() { go build -C <path to repo> && <path to repo>/pigo "$@"; }

Go caches builds, so when nothing has changed this takes a fraction of a second. The binary is written to the repo root (ignored by git), and `pigo-dev version` prints `dev`. Release builds get their version from the git tag.


## Before you commit

- Format the code: `gofmt -w .` (most editors do this on save).
- Run `go vet ./...` and `go test ./...`.


## Releasing

Push a version tag. The github workflow (`.github/workflows/release.yml`) will build and publish it.

    git tag v0.1.0
    git push origin v0.1.0

Tags containing a `-` (like `v0.1.0-test`) are published as pre-releases.


## Deleting a release

1. On github, open Releases, open the release and delete it.
2. Delete the tag on github and locally:

       git push origin --delete v0.1.0
       git tag -d v0.1.0
