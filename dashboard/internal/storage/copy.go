package storage

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// pair is one tree a drive job copies: every regular file under src lands
// under dst. skip holds top-level names that are left out on both sides.
// An empty src means there is nothing to copy.
type pair struct {
	src  string
	dst  string
	skip map[string]bool
}

// copyPairs copies every pair.
func copyPairs(pairs []pair) error {
	for _, p := range pairs {
		if err := copyTree(p.src, p.dst, p.skip); err != nil {
			return err
		}
	}
	return nil
}

// verifyPairs compares every pair by size and SHA-256, file by file.
func verifyPairs(pairs []pair) error {
	for _, p := range pairs {
		if err := verifyTree(p.src, p.dst, p.skip); err != nil {
			return err
		}
	}
	return nil
}

// copyTree writes src into dst file by file, overwriting files of the same
// name. A single file goes to dst as a file. Nothing in dst is removed. A
// missing src copies nothing.
func copyTree(src, dst string, skip map[string]bool) error {
	if src == "" || missing(src) {
		return nil
	}
	if isRegularFile(src) {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o750)
		}
		if skip[topName(rel)] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o750)
		case d.Type().IsRegular():
			return copyFile(path, target)
		default:
			return fmt.Errorf("storage: %s: unsupported file type", rel)
		}
	})
}

// verifyTree checks that every regular file under src exists under dst with
// the same size and SHA-256. Extra files under dst are ignored.
func verifyTree(src, dst string, skip map[string]bool) error {
	if src == "" || missing(src) {
		return nil
	}
	if isRegularFile(src) {
		same, err := sameFile(src, dst)
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("storage: %s differs from the copy", filepath.Base(src))
		}
		return nil
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." || skip[topName(rel)] {
			if d.IsDir() && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("storage: %s: unsupported file type", rel)
		}
		same, err := sameFile(path, filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("storage: %s differs from the copy", rel)
		}
		return nil
	})
}

// copyFile writes the content of src to dst with mode 0640 and fsync.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// sameFile compares size and SHA-256 of two regular files.
func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if fa.Size() != fb.Size() {
		return false, nil
	}
	ha, err := hashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := hashFile(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func hashFile(path string) ([32]byte, error) {
	var sum [32]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// resolveDir returns the real directory behind p. A missing p gives "".
func resolveDir(p string) (string, error) {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return filepath.EvalSymlinks(p)
	}
	return p, nil
}

// moveAside renames a real directory at p to dst, so the symlink can take its
// place. A symlink or a missing path is left alone and reports false.
func moveAside(p, dst string) (bool, error) {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if !fi.IsDir() {
		return false, fmt.Errorf("storage: %s is neither a directory nor a symlink", p)
	}
	if err := os.Rename(p, dst); err != nil {
		return false, err
	}
	return true, nil
}

// removeSymlink deletes p when it is a symlink. Anything else stays.
func removeSymlink(p string) error {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return os.Remove(p)
}

func isRegularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

func missing(p string) bool {
	_, err := os.Stat(p)
	return errors.Is(err, fs.ErrNotExist)
}

// topName returns the first element of a relative path.
func topName(rel string) string {
	return strings.SplitN(rel, string(filepath.Separator), 2)[0]
}
