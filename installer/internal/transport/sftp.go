package transport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
)

// UploadFile copies the local file at localPath to remotePath on the node,
// creating remotePath's parent directory if needed and setting mode
// explicitly -- SFTP's default create mode depends on the server's umask,
// which must not be trusted for files that will hold secrets.
func (c *Client) UploadFile(localPath, remotePath string, mode os.FileMode) error {
	return c.UploadFileProgress(localPath, remotePath, mode, nil)
}

// UploadFileProgress is UploadFile that reports how many bytes have been
// written so far and the file's total size. onProgress may be nil; it is
// called from the copy loop, so it must return quickly. With concurrent
// writes the callback can be invoked from several goroutines, but done only
// ever grows.
func (c *Client) UploadFileProgress(localPath, remotePath string, mode os.FileMode, onProgress func(done, total int64)) error {
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", localPath, err)
	}
	defer local.Close()
	var reader io.Reader = local
	if onProgress != nil {
		info, err := local.Stat()
		if err != nil {
			return fmt.Errorf("reading size of %s: %w", localPath, err)
		}
		reader = &progressReader{r: local, total: info.Size(), onProgress: onProgress}
	}
	return c.uploadReader(reader, remotePath, mode)
}

// progressReader counts the bytes read through it. Reads come from one
// goroutine (sftp's concurrent writer reads sequentially and fans the chunks
// out), so no locking is needed.
type progressReader struct {
	r          io.Reader
	total      int64
	done       int64
	onProgress func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.onProgress(p.done, p.total)
	}
	return n, err
}

// UploadBytes writes data to remotePath -- for content that only exists in
// memory, such as the embedded manifest public key or a staged password.
func (c *Client) UploadBytes(data []byte, remotePath string, mode os.FileMode) error {
	return c.uploadReader(bytes.NewReader(data), remotePath, mode)
}

func (c *Client) uploadReader(r io.Reader, remotePath string, mode os.FileMode) error {
	// Without concurrent writes every 32 KiB packet waits for its
	// acknowledgement, so a high-latency link (Tailscale, Wi-Fi) crawls.
	client, err := sftp.NewClient(c.conn, sftp.UseConcurrentWrites(true))
	if err != nil {
		return fmt.Errorf("opening SFTP session: %w", err)
	}
	defer client.Close()

	if dir := path.Dir(remotePath); dir != "." && dir != "/" {
		if err := client.MkdirAll(dir); err != nil {
			return fmt.Errorf("creating remote directory %s: %w", dir, err)
		}
	}

	// Write next to the target and rename over it. Opening an existing
	// remotePath with O_TRUNC fails when the SSH user may not write that
	// file -- a manual install leaves a root-owned selection.json in a
	// directory the user owns -- while replacing it only needs the
	// directory. The rename also means a reader never sees a half-written
	// file.
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("choosing a temporary name for %s: %w", remotePath, err)
	}
	tmpPath := remotePath + ".tmp-" + hex.EncodeToString(suffix)

	remote, err := client.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmpPath, err)
	}
	// Chmod before writing any content: the server just created the file
	// at its own default mode (mode & ^umask, typically 0644), and a caller
	// staging a secret must never leave a window where those bytes sit on
	// disk at a world-readable mode.
	if err := remote.Chmod(mode); err != nil {
		remote.Close()
		client.Remove(tmpPath)
		return fmt.Errorf("setting mode of %s: %w", tmpPath, err)
	}
	if _, err := io.Copy(remote, r); err != nil {
		remote.Close()
		client.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", remotePath, err)
	}
	if err := remote.Close(); err != nil {
		client.Remove(tmpPath)
		return fmt.Errorf("writing %s: %w", remotePath, err)
	}
	if err := client.PosixRename(tmpPath, remotePath); err != nil {
		// Servers without the posix-rename extension refuse to rename over
		// an existing file; remove the target first.
		client.Remove(remotePath)
		if err := client.Rename(tmpPath, remotePath); err != nil {
			client.Remove(tmpPath)
			return fmt.Errorf("replacing %s: %w", remotePath, err)
		}
	}
	return nil
}

// RemoveRemote deletes a file on the node. A file that is already gone is
// not an error, which keeps cleanup calls simple after a step failed partway
// through staging a secret.
func (c *Client) RemoveRemote(remotePath string) error {
	client, err := sftp.NewClient(c.conn)
	if err != nil {
		return fmt.Errorf("opening SFTP session: %w", err)
	}
	defer client.Close()

	if err := client.Remove(remotePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", remotePath, err)
	}
	return nil
}

// DownloadFile copies remotePath from the node to localPath, creating
// localPath's parent directory if needed. It is the mirror of UploadFile,
// added for RunFetchConfig (Plan B-II) -- Plan B-I never needed the download
// direction.
func (c *Client) DownloadFile(remotePath, localPath string) error {
	client, err := sftp.NewClient(c.conn)
	if err != nil {
		return fmt.Errorf("opening SFTP session: %w", err)
	}
	defer client.Close()

	remote, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("opening remote %s: %w", remotePath, err)
	}
	defer remote.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("creating local directory for %s: %w", localPath, err)
	}
	local, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating local file %s: %w", localPath, err)
	}
	defer local.Close()

	if _, err := io.Copy(local, remote); err != nil {
		return fmt.Errorf("downloading %s: %w", remotePath, err)
	}
	return nil
}
