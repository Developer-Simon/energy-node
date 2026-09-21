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

func TestDownloadFileRetrievesRemoteContent(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	remote := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remote, []byte(`{"hello":"welt"}`), 0o644); err != nil {
		t.Fatalf("writing remote fixture: %v", err)
	}

	local := filepath.Join(t.TempDir(), "downloaded.json")
	if err := client.DownloadFile(remote, local); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}

	got, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != `{"hello":"welt"}` {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestDownloadFileReportsAMissingRemoteFile(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	err := client.DownloadFile(filepath.Join(t.TempDir(), "missing.json"), filepath.Join(t.TempDir(), "out.json"))
	if err == nil {
		t.Fatalf("expected an error for a missing remote file")
	}
}

func TestUploadFileProgressReportsEveryByteAndKeepsContent(t *testing.T) {
	requireSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	payload := bytes.Repeat([]byte("0123456789abcdef"), 512*1024) // 8 MiB, several SFTP packets
	local := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatalf("writing local fixture: %v", err)
	}
	remotePath := "/tmp/energy-node-installer-test/progress.bin"
	t.Cleanup(func() { _ = client.RemoveRemote(remotePath) })

	var last, total int64
	calls := 0
	err := client.UploadFileProgress(local, remotePath, 0o600, func(done, size int64) {
		if done < last {
			t.Errorf("progress went backwards: %d after %d", done, last)
		}
		last, total = done, size
		calls++
	})
	if err != nil {
		t.Fatalf("UploadFileProgress: %v", err)
	}
	if last != int64(len(payload)) || total != int64(len(payload)) || calls < 2 {
		t.Fatalf("progress ended at %d/%d after %d calls, want %d/%d over several calls", last, total, calls, len(payload), len(payload))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sum bytes.Buffer
	if err := client.Run(ctx, "wc -c < "+remotePath, &sum, &bytes.Buffer{}); err != nil {
		t.Fatalf("wc: %v", err)
	}
	if got := trimNewline(sum.String()); got != "8388608" {
		t.Fatalf("remote size = %s, want 8388608", got)
	}
}

// A manual install can leave a file in the state directory that the SSH user
// may not open for writing (root-owned selection.json in a directory the user
// owns). The user may still replace it, so the upload must.
func TestUploadBytesReplacesAnExistingFileTheUserCannotWrite(t *testing.T) {
	requireSFTPServer(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write any file, the scenario cannot be reproduced")
	}
	sshd := transporttest.Start(t)
	client := dialTestSSHD(t, sshd)

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "selection.json")
	if err := os.WriteFile(remotePath, []byte("alt"), 0o444); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if err := client.UploadBytes([]byte("neu"), remotePath, 0o644); err != nil {
		t.Fatalf("UploadBytes over a read-only file: %v", err)
	}
	got, err := os.ReadFile(remotePath)
	if err != nil || string(got) != "neu" {
		t.Fatalf("content = %q, %v, want neu", got, err)
	}
	info, _ := os.Stat(remotePath)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "selection.json.*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}
