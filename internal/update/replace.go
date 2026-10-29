package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// replaceExecutable swaps exe for a file with the given contents. The new
// file is written next to exe first, so the final rename is atomic and
// nobody ever sees a half-written binary.
func replaceExecutable(exe string, data []byte, windows bool) error {
	mode := fs.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".pigo-update-*")
	if err != nil {
		return replaceError(exe, err, windows)
	}
	// Fails harmlessly once the temp file has been renamed into place.
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tmp.Name(), err)
	}

	if !windows {
		// A running binary keeps its own copy of the old file open, so it
		// can be replaced underneath it.
		if err := os.Rename(tmp.Name(), exe); err != nil {
			return replaceError(exe, err, windows)
		}
		return nil
	}

	// Windows can't overwrite or delete a running exe, but it can rename it.
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// The temp file was just created in this folder, so it isn't the
		// folder's permissions: .old is the exe of a pigo started before
		// the previous update.
		return fmt.Errorf("an older pigo is still running (e.g. pigo serve); close it and try again: %w", err)
	}
	if err := os.Rename(exe, old); err != nil {
		return replaceError(exe, err, windows)
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		// Put the working binary back.
		os.Rename(old, exe)
		return replaceError(exe, err, windows)
	}
	return nil
}

func replaceError(exe string, err error, windows bool) error {
	if !errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("replacing %s: %w", exe, err)
	}
	if windows {
		return fmt.Errorf("no permission to replace %s; run the command from a terminal opened with \"Run as administrator\": %w", exe, err)
	}
	return fmt.Errorf("no permission to replace %s; run \"sudo pigo update\", or reinstall with the install script (it installs to ~/.local/bin): %w", exe, err)
}
