package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config describes how to reach a node. Exactly one of Password or
// PrivateKeyPEM should be set; PrivateKeyPEM wins if both are.
type Config struct {
	Host            string // "host" or "host:port"; a bare host implies port 22
	User            string
	Password        string
	PrivateKeyPEM   []byte
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration // zero means 10s
}

// Client holds one SSH connection that Run and the SFTP helpers in
// sftp.go reuse for every subsequent operation (E2): a fresh session per
// command, but a single TCP connection and handshake for the whole run.
type Client struct {
	conn *ssh.Client
}

func Dial(ctx context.Context, cfg Config) (*Client, error) {
	auth, err := authMethod(cfg)
	if err != nil {
		return nil, err
	}
	addr := cfg.Host
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	dialer := net.Dialer{Timeout: timeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	clientConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: cfg.HostKeyCallback,
		Timeout:         timeout,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(rawConn, addr, clientConfig)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	return &Client{conn: ssh.NewClient(sshConn, chans, reqs)}, nil
}

func authMethod(cfg Config) (ssh.AuthMethod, error) {
	switch {
	case len(cfg.PrivateKeyPEM) > 0:
		signer, err := ssh.ParsePrivateKey(cfg.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("parsing private key: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	case cfg.Password != "":
		return ssh.Password(cfg.Password), nil
	default:
		return nil, fmt.Errorf("transport: neither a password nor a private key was given")
	}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// cancelWaitGrace bounds how long Run waits for session.Wait to return once
// ctx is cancelled. Closing the SSH channel ends the remote's stdio, which a
// process actively reading or writing it observes as teardown immediately --
// but a process that never touches stdio again (sleep, and most of a long
// apt-get run) will not notice until it next does, or exits on its own,
// which can be far later than the caller is willing to wait.
const cancelWaitGrace = 3 * time.Second

// Run executes command in a fresh SSH session on the held connection,
// streaming its stdout and stderr to the given writers. Cancelling ctx tries
// SIGTERM first -- most sshd configurations disable the SSH "signal"
// request, so this usually has no effect -- then closes the session, which
// ends its stdio; Run returns ctx.Err() once that happens or after
// cancelWaitGrace, whichever comes first, rather than waiting out however
// long the remote command takes to actually exit.
func (c *Client) Run(ctx context.Context, command string, stdout, stderr io.Writer) error {
	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("opening session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(command); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		session.Close()
		select {
		case <-done:
		case <-time.After(cancelWaitGrace):
			// The remote process did not react to the channel closing within
			// the grace period; done is still buffered (size 1) so the
			// goroutine above will not leak once session.Wait eventually
			// returns.
		}
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// BuildCommand prefixes command with an inline "KEY='value'" assignment for
// each entry of env, quoted for POSIX sh. Bootstrap scripts read their
// configuration from EN_* environment variables; ssh.Session.Setenv depends
// on the server's AcceptEnv allowlist, which a stock sshd does not grant for
// custom names, so the assignment travels as part of the command string
// instead -- "KEY=val cmd" is POSIX sh's own syntax for a one-shot
// environment override on a single simple command. Keys are sorted so the
// resulting string is deterministic and easy to assert against in tests.
func BuildCommand(env map[string]string, command string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(shellQuote(env[k]))
		b.WriteByte(' ')
	}
	b.WriteString(command)
	return b.String()
}

// shellQuote wraps s in single quotes for POSIX sh, escaping each embedded
// quote character by ending the quoted string, inserting a backslash-escaped
// quote, and reopening a new quoted string right after it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellQuote exposes shellQuote for other installer packages that build a
// remote command line directly, such as internal/bundle's Deploy and
// VerifyRemote.
func ShellQuote(s string) string {
	return shellQuote(s)
}
