// Package transport holds the SSH/SFTP client the installer uses to reach a
// node: connection setup with pinned host keys, running bootstrap steps, and
// uploading or removing files. See
// .docs/superpowers/specs/2026-09-05-installationsanwendung-design.md, E2.
package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Prompt is called the first time a host's key is seen and must return
// whether the operator trusts it. fingerprint is in the usual
// "SHA256:base64" form (see ssh.FingerprintSHA256).
type Prompt func(hostname, fingerprint string) (bool, error)

// HostKeyStore pins host keys to a known_hosts-formatted file so a later
// connection to the same host detects a swapped key instead of silently
// accepting it.
type HostKeyStore struct {
	path string
}

// NewHostKeyStore does not touch the filesystem; the file is created lazily
// by Callback so constructing a store is never an error.
func NewHostKeyStore(path string) *HostKeyStore {
	return &HostKeyStore{path: path}
}

// Callback builds an ssh.HostKeyCallback that trusts an unknown host only
// after prompt returns true, and rejects outright a host whose key differs
// from a previously pinned one -- prompting there would defeat the point of
// pinning: that is exactly the case a machine-in-the-middle produces.
func (s *HostKeyStore) Callback(prompt Prompt) (ssh.HostKeyCallback, error) {
	if err := ensureFileExists(s.path); err != nil {
		return nil, fmt.Errorf("host key store %s: %w", s.path, err)
	}
	check, err := knownhosts.New(s.path)
	if err != nil {
		return nil, fmt.Errorf("reading host key store %s: %w", s.path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("host key for %s changed since it was first trusted: %w", hostname, err)
		}
		fingerprint := ssh.FingerprintSHA256(key)
		trusted, promptErr := prompt(hostname, fingerprint)
		if promptErr != nil {
			return promptErr
		}
		if !trusted {
			return fmt.Errorf("host key for %s rejected by operator", hostname)
		}
		return appendHostKey(s.path, hostname, key)
	}, nil
}

func ensureFileExists(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func appendHostKey(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{hostname}, key))
	return err
}
