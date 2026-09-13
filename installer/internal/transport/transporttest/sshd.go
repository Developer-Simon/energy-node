// Package transporttest starts a real, throwaway OpenSSH server for
// integration tests across the installer module -- transport, bundle and
// steps all exercise their SSH-facing code against it instead of a mock.
// Modelled on net/http/httptest: an ordinary importable package that
// happens to take *testing.T and is only ever called from test files.
//
// Unlike net/http/httptest, this package does import "testing" itself (for
// t.Helper/t.Cleanup/t.TempDir). Do not import it from non-test code --
// doing so would pull testing's flag registration into a production binary.
package transporttest

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHD is a throwaway OpenSSH server. It authenticates only the one client
// key it generates, as the current OS user -- there is no way to create a
// fresh system user without root, and nothing about this fixture is meant
// to outlive the test process.
type SSHD struct {
	Addr         string
	HostKey      ssh.PublicKey
	ClientKeyPEM []byte
}

func (s *SSHD) User() string {
	me, err := user.Current()
	if err != nil {
		panic(err) // user.Current() failing mid-test means the environment is broken beyond recovery
	}
	return me.Username
}

// Start starts sshd in the foreground under t.TempDir() and registers a
// cleanup that kills it. It skips the test if no sshd binary is installed,
// and fails it if sshd exits or times out before reporting readiness.
func Start(t *testing.T) *SSHD {
	t.Helper()
	sshdPath := findSSHD(t)
	dir := t.TempDir()

	hostKeyPath := filepath.Join(dir, "host_key")
	runKeygen(t, "-t", "ed25519", "-f", hostKeyPath, "-N", "", "-q")
	hostKeyPub := readPublicKey(t, hostKeyPath+".pub")

	clientKeyPath := filepath.Join(dir, "client_key")
	runKeygen(t, "-t", "ed25519", "-f", clientKeyPath, "-N", "", "-q")
	clientKeyPEM, err := os.ReadFile(clientKeyPath)
	if err != nil {
		t.Fatalf("reading generated client key: %v", err)
	}

	authorizedKeys := filepath.Join(dir, "authorized_keys")
	copyFile(t, clientKeyPath+".pub", authorizedKeys)

	port := freeTCPPort(t)
	me, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}

	args := []string{
		"-D", "-e",
		"-f", "/dev/null",
		"-p", strconv.Itoa(port),
		"-h", hostKeyPath,
		"-o", "PidFile=" + filepath.Join(dir, "sshd.pid"),
		"-o", "StrictModes=no",
		"-o", "UsePAM=no",
		"-o", "PasswordAuthentication=no",
		"-o", "PubkeyAuthentication=yes",
		"-o", "AuthorizedKeysFile=" + authorizedKeys,
		"-o", "AllowUsers=" + me.Username,
	}
	if sftpServer, ok := SFTPServerPath(); ok {
		args = append(args, "-o", "Subsystem=sftp "+sftpServer)
	}
	cmd := exec.Command(sshdPath, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("sshd stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sshd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	if err := waitForListening(t, stderr, 5*time.Second); err != nil {
		t.Fatalf("sshd did not become ready: %v", err)
	}

	return &SSHD{
		Addr:         fmt.Sprintf("127.0.0.1:%d", port),
		HostKey:      hostKeyPub,
		ClientKeyPEM: clientKeyPEM,
	}
}

// SFTPServerPath looks for the sftp-server helper binary sshd needs to
// serve the SFTP subsystem. It is bundled with the openssh-server package
// on every distribution this project targets, but under a different path
// each time, so several candidates are tried. It never fails a test itself
// -- callers that need SFTP check ok and skip if it is missing.
func SFTPServerPath() (string, bool) {
	for _, candidate := range []string{
		"/usr/lib/openssh/sftp-server",
		"/usr/libexec/openssh/sftp-server",
		"/usr/lib/ssh/sftp-server",
		"/usr/libexec/sftp-server",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

func waitForListening(t *testing.T, stderr io.Reader, timeout time.Duration) error {
	t.Helper()
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	deadline := time.After(timeout)
	var seen []string
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return fmt.Errorf("sshd exited before reporting readiness; output:\n%s", strings.Join(seen, "\n"))
			}
			t.Logf("sshd: %s", line)
			seen = append(seen, line)
			if strings.Contains(line, "Server listening") {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("timed out after %s waiting for readiness; output so far:\n%s", timeout, strings.Join(seen, "\n"))
		}
	}
}

func findSSHD(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/usr/sbin/sshd", "/usr/bin/sshd"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("sshd"); err == nil {
		return path
	}
	t.Skip("sshd not found on PATH; install openssh-server to run this test")
	return ""
}

func runKeygen(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("ssh-keygen", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen %v: %v\n%s", args, err, out)
	}
}

func readPublicKey(t *testing.T, path string) ssh.PublicKey {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(raw)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return key
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
