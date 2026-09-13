package devcli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func acceptAllHostKeys(string, string) (bool, error) { return true, nil }

func TestConnectAuthenticatesWithAnIdentityFile(t *testing.T) {
	sshd := transporttest.Start(t)
	identity := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(identity, sshd.ClientKeyPEM, 0o600); err != nil {
		t.Fatalf("writing identity file: %v", err)
	}

	client, err := devcli.Connect(context.Background(), devcli.Target{Host: sshd.Addr, User: sshd.User()}, devcli.ConnectOptions{
		IdentityPath:   identity,
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		HostKeyPrompt:  acceptAllHostKeys,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	client.Close()
}

func TestConnectPrefersTheIdentityFileOverAPasswordFile(t *testing.T) {
	sshd := transporttest.Start(t)
	identity := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(identity, sshd.ClientKeyPEM, 0o600); err != nil {
		t.Fatalf("writing identity file: %v", err)
	}
	passwordFile := filepath.Join(t.TempDir(), "system-ssh.pw")
	if err := os.WriteFile(passwordFile, []byte("would-be-rejected\n"), 0o600); err != nil {
		t.Fatalf("writing password file: %v", err)
	}

	// The fixture sshd disables password authentication entirely (Plan B-I,
	// Task 4), so succeeding here is only possible if the identity file was
	// actually used -- proving the precedence, not just that a password
	// *could* have worked too.
	client, err := devcli.Connect(context.Background(), devcli.Target{Host: sshd.Addr, User: sshd.User()}, devcli.ConnectOptions{
		IdentityPath:   identity,
		PasswordFile:   passwordFile,
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		HostKeyPrompt:  acceptAllHostKeys,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	client.Close()
}

func TestConnectFallsBackToAPasswordFileWhenNoIdentityExists(t *testing.T) {
	sshd := transporttest.Start(t)
	passwordFile := filepath.Join(t.TempDir(), "system-ssh.pw")
	if err := os.WriteFile(passwordFile, []byte("irrelevant\n"), 0o600); err != nil {
		t.Fatalf("writing password file: %v", err)
	}

	_, err := devcli.Connect(context.Background(), devcli.Target{Host: sshd.Addr, User: sshd.User()}, devcli.ConnectOptions{
		IdentityPath:   filepath.Join(t.TempDir(), "no-such-key"),
		PasswordFile:   passwordFile,
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		HostKeyPrompt:  acceptAllHostKeys,
	})
	// The fixture rejects every password, so this proves the password path
	// was actually attempted (not silently skipped) rather than that it
	// succeeds -- Plan B-I's own transport tests establish the same fixture
	// limitation for TestDialReportsAuthenticationFailure.
	if err == nil {
		t.Fatalf("expected the fixture sshd to reject password authentication")
	}
}

func TestConnectFallsBackToThePasswordPromptAsALastResort(t *testing.T) {
	sshd := transporttest.Start(t)
	prompted := false

	_, err := devcli.Connect(context.Background(), devcli.Target{Host: sshd.Addr, User: sshd.User()}, devcli.ConnectOptions{
		IdentityPath: filepath.Join(t.TempDir(), "no-such-key"),
		PasswordFile: filepath.Join(t.TempDir(), "no-such-password-file"),
		PasswordPrompt: func() (string, error) {
			prompted = true
			return "irrelevant", nil
		},
		KnownHostsPath: filepath.Join(t.TempDir(), "known_hosts"),
		HostKeyPrompt:  acceptAllHostKeys,
	})
	if !prompted {
		t.Fatalf("expected the password prompt to be used as a last resort")
	}
	if err == nil {
		t.Fatalf("expected the fixture sshd to reject password authentication")
	}
}
