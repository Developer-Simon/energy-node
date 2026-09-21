package bundlefetch

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits on what one archive may unpack to. Package variables so tests can
// lower them. The archive is not verified yet at this point (the updater does
// that later, as root), so an archive is treated as untrusted input.
var (
	maxExtractedBytes int64 = 1 << 30
	maxEntries              = 20000
)

// safeJoin resolves an archive entry name below root, refusing absolute
// paths and anything that would climb out.
func safeJoin(root, name string) (string, error) {
	rel := path.Clean(strings.TrimPrefix(name, "./"))
	if rel == "." {
		return root, nil
	}
	if path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path escapes the bundle")
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if !strings.HasPrefix(target, filepath.Clean(root)+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the bundle")
	}
	return target, nil
}

// extractArchive unpacks a make_bundle.sh .tar.gz into dest. Only
// directories and regular files are accepted: a bundle contains nothing
// else, and a symlink or device entry is either a build bug or an attack.
func extractArchive(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return installFailed(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return invalid("not a gzip stream: " + err.Error())
	}
	defer gz.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return installFailed(err)
	}

	tr := tar.NewReader(gz)
	var written int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid("reading tar entry: " + err.Error())
		}
		entries++
		if entries > maxEntries {
			return tooLarge(fmt.Sprintf("more than %d entries", maxEntries))
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return invalid(fmt.Sprintf("entry %q: %v", hdr.Name, err))
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return installFailed(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return installFailed(err)
			}
			// Keep the executable bits, drop setuid/setgid/sticky and any
			// group/other write bit; always leave the owner able to read
			// and write so the swap and the later copy into the job work.
			perm := hdr.FileInfo().Mode().Perm()&0o755 | 0o600
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return installFailed(err)
			}
			n, copyErr := io.Copy(out, io.LimitReader(tr, maxExtractedBytes-written+1))
			written += n
			if cerr := out.Close(); copyErr == nil {
				copyErr = cerr
			}
			if written > maxExtractedBytes {
				return tooLarge(fmt.Sprintf("more than %d bytes unpacked", maxExtractedBytes))
			}
			if copyErr != nil {
				return installFailed(copyErr)
			}
		default:
			return invalid(fmt.Sprintf("entry %q has unsupported type %d", hdr.Name, hdr.Typeflag))
		}
	}
}
