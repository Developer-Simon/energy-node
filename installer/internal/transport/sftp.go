package transport

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/pkg/sftp"
)

// UploadFile copies the local file at localPath to remotePath on the node,
// creating remotePath's parent directory if needed and setting mode
// explicitly -- SFTP's default create mode depends on the server's umask,
// which must not be trusted for files that will hold secrets.
func (c *Client) UploadFile(localPath, remotePath string, mode os.FileMode) error {
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", localPath, err)
	}
	defer local.Close()
	return c.uploadReader(local, remotePath, mode)
}

// UploadBytes writes data to remotePath -- for content that only exists in
// memory, such as the embedded manifest public key or a staged password.
func (c *Client) UploadBytes(data []byte, remotePath string, mode os.FileMode) error {
	return c.uploadReader(bytes.NewReader(data), remotePath, mode)
}

func (c *Client) uploadReader(r io.Reader, remotePath string, mode os.FileMode) error {
	client, err := sftp.NewClient(c.conn)
	if err != nil {
		return fmt.Errorf("opening SFTP session: %w", err)
	}
	defer client.Close()

	if dir := path.Dir(remotePath); dir != "." && dir != "/" {
		if err := client.MkdirAll(dir); err != nil {
			return fmt.Errorf("creating remote directory %s: %w", dir, err)
		}
	}

	remote, err := client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("creating %s: %w", remotePath, err)
	}
	defer remote.Close()

	if _, err := io.Copy(remote, r); err != nil {
		return fmt.Errorf("writing %s: %w", remotePath, err)
	}
	return remote.Chmod(mode)
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
