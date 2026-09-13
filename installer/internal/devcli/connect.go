package devcli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// ConnectOptions configures how Connect authenticates and how it handles an
// unknown host key. Leaving PasswordPrompt/HostKeyPrompt nil uses a real
// terminal; tests always override both (see this task's rationale).
type ConnectOptions struct {
	IdentityPath   string
	PasswordFile   string
	PasswordPrompt func() (string, error)
	KnownHostsPath string
	HostKeyPrompt  transport.Prompt
	Timeout        time.Duration
}

// Connect dials target, resolving authentication in order: an identity file,
// then a password file, then an interactive prompt (this task's rationale
// explains why in that order for this project). Host keys are pinned via
// transport.HostKeyStore (TOFU) at opts.KnownHostsPath.
func Connect(ctx context.Context, target Target, opts ConnectOptions) (*transport.Client, error) {
	cfg := transport.Config{Host: target.Host, User: target.User, Timeout: opts.Timeout}

	if opts.IdentityPath != "" {
		key, err := os.ReadFile(opts.IdentityPath)
		switch {
		case err == nil:
			cfg.PrivateKeyPEM = key
		case !os.IsNotExist(err):
			return nil, fmt.Errorf("reading identity file %s: %w", opts.IdentityPath, err)
		}
	}

	if len(cfg.PrivateKeyPEM) == 0 {
		password, err := resolvePassword(opts)
		if err != nil {
			return nil, err
		}
		cfg.Password = password
	}

	store := transport.NewHostKeyStore(opts.KnownHostsPath)
	prompt := opts.HostKeyPrompt
	if prompt == nil {
		prompt = defaultHostKeyPrompt
	}
	callback, err := store.Callback(prompt)
	if err != nil {
		return nil, fmt.Errorf("setting up host key store %s: %w", opts.KnownHostsPath, err)
	}
	cfg.HostKeyCallback = callback

	return transport.Dial(ctx, cfg)
}

func resolvePassword(opts ConnectOptions) (string, error) {
	if opts.PasswordFile != "" {
		raw, err := os.ReadFile(opts.PasswordFile)
		switch {
		case err == nil:
			return strings.TrimRight(string(raw), "\n"), nil
		case !os.IsNotExist(err):
			return "", fmt.Errorf("reading password file %s: %w", opts.PasswordFile, err)
		}
	}
	prompt := opts.PasswordPrompt
	if prompt == nil {
		prompt = defaultPasswordPrompt
	}
	return prompt()
}

// defaultPasswordPrompt reads a masked password from the real terminal.
// golang.org/x/term works the same way on Windows, macOS and Linux without
// cgo, which the Windows/macOS build of this CLI depends on (E6).
func defaultPasswordPrompt() (string, error) {
	fmt.Fprint(os.Stderr, "Password: ")
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(raw), nil
}

// defaultHostKeyPrompt asks the operator on the real terminal whether to
// trust a host key seen for the first time. An empty or non-"yes" answer
// (including EOF on a non-interactive terminal) declines -- host key pinning
// only protects against a machine-in-the-middle if declining is the default.
func defaultHostKeyPrompt(hostname, fingerprint string) (bool, error) {
	fmt.Fprintf(os.Stderr, "The authenticity of host %q cannot be established.\nKey fingerprint: %s\nTrust this host? [yes/NO] ", hostname, fingerprint)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "yes", nil
}

// DefaultIdentityPath returns ~/.ssh/id_ed25519 if it exists, for callers
// that do not pass --identity explicitly.
func DefaultIdentityPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	path := filepath.Join(home, ".ssh", "id_ed25519")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// DefaultKnownHostsPath returns this CLI's own host key store, separate from
// the system's ~/.ssh/known_hosts: this installer pins keys for nodes it
// manages itself and should not be affected by (or affect) the operator's
// unrelated SSH habits.
func DefaultKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config", "energy-node-installer", "known_hosts"), nil
}
