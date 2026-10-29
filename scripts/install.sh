#!/bin/sh
# Installs the latest pigo release for macOS or linux into ~/.local/bin.
#
#   curl -fsSL https://raw.githubusercontent.com/ekmwest/pigo/main/scripts/install.sh | sh

set -eu

base_url="https://github.com/ekmwest/pigo/releases/latest/download"
bin_dir="$HOME/.local/bin"

fail() {
	echo "pigo install: $1" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

# Everything runs from main, called on the last line, so a download cut short
# by a network error can't leave sh executing half a script.
main() {
	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported OS: $(uname -s) (on Windows, use install.ps1)" ;;
	esac

	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported CPU architecture: $(uname -m)" ;;
	esac

	# A shell running under Rosetta reports x86_64 on Apple Silicon; prefer the
	# native build.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
		if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
			arch=arm64
		fi
	fi

	need curl
	need tar
	need awk
	if command -v sha256sum >/dev/null 2>&1; then
		sha256="sha256sum"
	elif command -v shasum >/dev/null 2>&1; then
		sha256="shasum -a 256"
	else
		fail "required command not found: sha256sum or shasum"
	fi

	archive="pigo_${os}_${arch}.tar.gz"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t pigo)" || fail "could not create a temporary directory"
	trap 'rm -rf "$tmp"' EXIT

	echo "Downloading $archive"
	curl -fsSL -o "$tmp/$archive" "$base_url/$archive" ||
		fail "download failed: $base_url/$archive"
	curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" ||
		fail "download failed: $base_url/checksums.txt"

	expected="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
	[ -n "$expected" ] || fail "no checksum for $archive in checksums.txt"
	actual="$($sha256 "$tmp/$archive" | awk '{ print $1 }')"
	if [ "$actual" != "$expected" ]; then
		fail "checksum mismatch for $archive (expected $expected, got $actual)"
	fi

	tar -xzf "$tmp/$archive" -C "$tmp" pigo || fail "could not extract $archive"

	mkdir -p "$bin_dir" || fail "could not create $bin_dir"
	# Copy next to the target, then rename: the rename is atomic, so a running
	# pigo keeps working and nobody sees a half-written binary.
	cp "$tmp/pigo" "$bin_dir/pigo.tmp" || fail "could not write to $bin_dir"
	chmod 755 "$bin_dir/pigo.tmp"
	mv -f "$bin_dir/pigo.tmp" "$bin_dir/pigo" || fail "could not install to $bin_dir/pigo"

	echo "Installed $("$bin_dir/pigo" version) to $bin_dir/pigo"

	case ":$PATH:" in
	*":$bin_dir:"*) ;;
	*)
		echo
		echo "$bin_dir is not on your PATH. Add this line to your shell profile"
		echo "(~/.zshrc, ~/.bashrc, ~/.profile, ...) and open a new terminal:"
		echo
		echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
		;;
	esac
}

main "$@"
