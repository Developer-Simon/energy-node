package transport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

func requireSFTPServer(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-sftp-server to run this test")
	}
}

func TestUploadFileRoundTrip(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	local := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(local, []byte("geheim\n"), 0o600); err != nil {
		t.Fatalf("writing local fixture: %v", err)
	}

	remotePath := "/tmp/energy-node-installer-test/nested/secret.pw"
	if err := client.UploadFile(local, remotePath, 0o600); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	t.Cleanup(func() { _ = client.RemoveRemote(remotePath) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remotePath, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading uploaded file back: %v", err)
	}
	if stdout.String() != "geheim\n" {
		t.Fatalf("unexpected content: %q", stdout.String())
	}

	// Mode 0600 matters: this is exactly the path a step's password file
	// takes (Umgang mit Geheimnissen in the spec). A permissive mode here
	// would leak the secret to any other local user on the node.
	var modeOut bytes.Buffer
	if err := client.Run(ctx, "stat -c %a "+remotePath, &modeOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := trimNewline(modeOut.String()); got != "600" {
		t.Fatalf("unexpected remote mode: %q", got)
	}
}

func TestUploadBytesWritesExactContent(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	remotePath := "/tmp/energy-node-installer-test/pubkey.pem"
	content := []byte("-----BEGIN PUBLIC KEY-----\n")
	if err := client.UploadBytes(content, remotePath, 0o644); err != nil {
		t.Fatalf("UploadBytes: %v", err)
	}
	t.Cleanup(func() { _ = client.RemoveRemote(remotePath) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remotePath, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stdout.String() != string(content) {
		t.Fatalf("unexpected content: %q", stdout.String())
	}
}

func TestRemoveRemoteIsIdempotent(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	if err := client.RemoveRemote("/tmp/energy-node-installer-test/never-existed"); err != nil {
		t.Fatalf("removing an already-absent file must not be an error: %v", err)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
