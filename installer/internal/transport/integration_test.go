package transport

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func dialTestSSHD(t *testing.T, sshd *transporttest.SSHD) *Client {
	t.Helper()
	store := NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Dial(ctx, Config{
		Host:            sshd.Addr,
		User:            sshd.User(),
		PrivateKeyPEM:   sshd.ClientKeyPEM,
		HostKeyCallback: callback,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestDialAndRunAgainstRealSSHD(t *testing.T) {
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	var stdout, stderr bytes.Buffer
	// The env-var assignment is a prefix on this whole command line, so a
	// literal "$GREETING" right here would expand against the *outer*
	// shell's environment, before the prefix takes effect -- exactly the
	// pitfall BuildCommand's own doc comment calls out. A real step script
	// does not hit this: it is a separate file bash reads and expands
	// *after* being exec'd with the prefixed environment already in place,
	// which invoking a nested shell here reproduces.
	cmd := BuildCommand(map[string]string{"GREETING": "hallo welt"}, `sh -c 'echo "$GREETING"'`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Run(ctx, cmd, &stdout, &stderr); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, stderr.String())
	}
	if got := stdout.String(); got != "hallo welt\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunSeparatesStdoutAndStderr(t *testing.T) {
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Run(ctx, `echo "on stdout"; echo "on stderr" >&2`, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stdout.String() != "on stdout\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.String() != "on stderr\n" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunReportsNonZeroExit(t *testing.T) {
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Run(ctx, "exit 7", &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected an error for a non-zero exit status")
	}
}

func TestDialReportsAuthenticationFailure(t *testing.T) {
	sshd := transporttest.Start(t)
	store := NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, _ := store.Callback(func(string, string) (bool, error) { return true, nil })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The fixture server disables password authentication entirely, so this
	// proves Dial surfaces an authentication failure as an error instead of
	// hanging or succeeding silently -- not that this specific password is
	// wrong, which a real sshd cannot safely be made to check in a test
	// without a disposable system account.
	_, err := Dial(ctx, Config{
		Host:            sshd.Addr,
		User:            sshd.User(),
		Password:        "irrelevant",
		HostKeyCallback: callback,
	})
	if err == nil {
		t.Fatalf("expected authentication to fail")
	}
}

func TestRunCancellationEndsTheSession(t *testing.T) {
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	runCtx, runCancel := context.WithCancel(context.Background())
	runCancel()

	start := time.Now()
	err := client.Run(runCtx, "sleep 5", &bytes.Buffer{}, &bytes.Buffer{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected Run to report the cancellation")
	}
	// sleep does not touch its stdio, so it never notices the channel
	// closing -- Run must give up after cancelWaitGrace rather than block
	// for the remaining ~5s until the remote process exits on its own.
	if elapsed >= 5*time.Second {
		t.Fatalf("Run took %s to return after cancellation; it waited out the remote command instead of bounding the wait", elapsed)
	}
}
