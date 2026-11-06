package build

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	siteContentFile = "site_content.json"
	noJekyllFile    = ".nojekyll"
)

// syncDocs makes docs match s. Files are written only when their content
// changed, so unchanged files keep their timestamps. It prints a line
// for every deleted file to stdout.
func syncDocs(docs string, s *site, stdout io.Writer) (written, deleted int, err error) {
	if err := os.MkdirAll(docs, 0o755); err != nil {
		return 0, 0, err
	}

	// Deleting first also clears the way when a path changes between file
	// and folder.
	deleted, err = deleteStale(docs, s, stdout)
	if err != nil {
		return 0, deleted, err
	}

	for _, d := range s.dirs {
		if err := os.MkdirAll(filepath.Join(docs, filepath.FromSlash(d)), 0o755); err != nil {
			return written, deleted, err
		}
	}

	for _, f := range s.files {
		dst := filepath.Join(docs, filepath.FromSlash(f.path))
		var changed bool
		if f.copyFrom != "" {
			changed, err = copyIfChanged(f.copyFrom, dst)
		} else {
			changed, err = writeIfChanged(dst, f.content)
		}
		if err != nil {
			return written, deleted, err
		}
		if changed {
			written++
		}
	}

	// GitHub Pages skips folders starting with "_" unless this file exists.
	noJekyll := filepath.Join(docs, noJekyllFile)
	if _, err := os.Lstat(noJekyll); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(noJekyll, nil, 0o644); err != nil {
			return written, deleted, err
		}
		written++
	}

	siteContent, err := encodePages(collectPages(s.files))
	if err != nil {
		return written, deleted, fmt.Errorf("encode %s: %w", siteContentFile, err)
	}
	changed, err := writeIfChanged(filepath.Join(docs, siteContentFile), siteContent)
	if err != nil {
		return written, deleted, err
	}
	if changed {
		written++
	}
	return written, deleted, nil
}

// deleteStale removes the files and folders in docs that have no source.
func deleteStale(docs string, s *site, stdout io.Writer) (int, error) {
	wantFiles := map[string]bool{}
	for _, f := range s.files {
		wantFiles[f.path] = true
	}
	wantDirs := map[string]bool{}
	for _, d := range s.dirs {
		wantDirs[d] = true
	}

	deleted := 0
	var staleDirs []string
	err := filepath.WalkDir(docs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(docs, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			// Walk into it anyway, so that each deleted file is printed.
			if !wantDirs[rel] {
				staleDirs = append(staleDirs, p)
			}
			return nil
		}
		if wantFiles[rel] || isKept(rel) {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		deleted++
		fmt.Fprintf(stdout, "deleted docs/%s\n", rel)
		return nil
	})
	if err != nil {
		return deleted, err
	}

	// Walk order lists a folder before its contents, so reversed, every
	// folder is empty by the time it is removed.
	for _, d := range slices.Backward(staleDirs) {
		if err := os.Remove(d); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// isKept reports whether a file in docs without a source must stay.
// CNAME is GitHub Pages' custom domain; piko matches it in any case.
func isKept(rel string) bool {
	return strings.EqualFold(rel, "CNAME") || rel == noJekyllFile || rel == siteContentFile
}

func writeIfChanged(dst string, content []byte) (bool, error) {
	if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, content) {
		return false, nil
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// copyIfChanged copies src to dst unless dst already has the same
// content. Sizes are compared first, bytes only if the sizes match.
func copyIfChanged(src, dst string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.Mode().IsRegular() && dstInfo.Size() == srcInfo.Size() {
		same, err := sameContent(src, dst)
		if err != nil {
			return false, err
		}
		if same {
			return false, nil
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return false, fmt.Errorf("copy to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func sameContent(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	bufA := make([]byte, 64*1024)
	bufB := make([]byte, 64*1024)
	for {
		na, errA := readChunk(fa, bufA)
		if errA != nil {
			return false, errA
		}
		nb, errB := readChunk(fb, bufB)
		if errB != nil {
			return false, errB
		}
		if !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		if na < len(bufA) {
			// Equal and short: both files ended here.
			return true, nil
		}
	}
}

// readChunk fills buf as far as the file allows. A short read means the
// end of the file, which is not an error.
func readChunk(f *os.File, buf []byte) (int, error) {
	n, err := io.ReadFull(f, buf)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return n, nil
	}
	return n, err
}
