package serve

import (
	"context"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"time"
)

const pollInterval = 300 * time.Millisecond

type fileState struct {
	modTime time.Time
	size    int64
}

// watch polls dir every interval and calls onChange once the folder has
// changed and then stayed quiet for a full interval, so a save that touches
// several files (or a build writing many) results in a single call.
// Polling is used instead of OS notifications to stay on the standard
// library and behave the same on every platform and file system.
func watch(ctx context.Context, dir string, interval time.Duration, onChange func()) error {
	prev, err := scan(dir)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pending := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		cur, err := scan(dir)
		if err != nil {
			// The folder may be briefly unreadable, e.g. while being replaced;
			// try again on the next tick.
			continue
		}
		if !maps.Equal(prev, cur) {
			pending = true
		} else if pending {
			pending = false
			onChange()
		}
		prev = cur
	}
}

// scan returns the state of every regular file under dir that isn't ignored.
func scan(dir string) (map[string]fileState, error) {
	files := make(map[string]fileState)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			// Entries can vanish between listing and stat; that's a change the
			// next scan will see, not an error.
			return nil
		}
		if path != dir && ignored(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files[path] = fileState{modTime: info.ModTime(), size: info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// ignored reports whether a file or folder name should not trigger a reload.
// Dotfiles are skipped because the server doesn't serve them (see
// hideDotfiles); the rest are temp files that editors write while saving.
func ignored(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	for _, suffix := range []string{"~", ".swp", ".swx", ".tmp", "___jb_tmp___", "___jb_old___"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	if len(name) > 1 && strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#") {
		return true // Emacs auto-save
	}
	return name == "4913" // Vim checks folder writability with this name
}
