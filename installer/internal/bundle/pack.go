package bundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// HasSignature reports whether an unpacked bundle carries a
// manifest.json.sig. Callers use it to tell "unsigned" (no file) from
// "forged" (file present, does not verify): only the former may be
// accepted without the release key.
func HasSignature(bundleDir string) bool {
	_, err := os.Stat(filepath.Join(bundleDir, "manifest.json.sig"))
	return err == nil
}

// PackDir writes the contents of srcDir (not srcDir itself) to archivePath
// as a .tar.gz that ExtractArchive and the node-side tar both accept. Only
// directories and regular files are packed; a bundle contains nothing else.
// It exists so the bundled (already unpacked) bundle can go through the
// same Deploy path as an archive the operator picked or built.
func PackDir(srcDir, archivePath string) (err error) {
	out, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", archivePath, err)
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if walkErr != nil {
		return fmt.Errorf("packing %s: %w", srcDir, walkErr)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
