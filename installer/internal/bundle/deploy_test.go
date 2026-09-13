package bundle_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// testStagingTag mirrors bundle's unexported stagingTag: Deploy and
// VerifyRemote namespace their /tmp staging filenames by the target
// remoteDir precisely so that a cleanup check can scope its glob to its own
// remoteDir instead of matching another concurrently-running test binary's
// unrelated staged file on the same shared /tmp.
func testStagingTag(remoteDir string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return replacer.Replace(strings.Trim(remoteDir, "/"))
}

func requireSFTPServerForBundle(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-sftp-server to run this test")
	}
}

func dialForBundleTest(t *testing.T, sshd *transporttest.SSHD) *transport.Client {
	t.Helper()
	store := transport.NewHostKeyStore(filepath.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := transport.Dial(ctx, transport.Config{
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

// buildTestArchive lays out files under a temp dir with mode 0755 (so
// scripts stay executable through tar) and packs it into a .tar.gz whose
// entries are rooted at ".", exactly like make_bundle.sh's own output.
func buildTestArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	src := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if out, err := exec.Command("tar", "-czf", archive, "-C", src, ".").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	return archive
}

func fakeVerifyBundleScript(output string, exitCode int) string {
	return fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\nexit %d\n", output, exitCode)
}

func remoteFileState(t *testing.T, ctx context.Context, client *transport.Client, remotePath string) string {
	t.Helper()
	var out bytes.Buffer
	_ = client.Run(ctx, "test -e "+remotePath+" && echo present || echo gone", &out, &bytes.Buffer{})
	s := out.String()
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// remoteGlobCount counts files in /tmp matching pattern. Deploy and
// VerifyRemote stage their upload under a randomly suffixed name (so
// concurrent runs never collide, see randomSuffix in deploy.go), so a
// cleanup check cannot look for one fixed path -- it looks for "nothing
// matching the shape of a staged file remains".
func remoteGlobCount(t *testing.T, ctx context.Context, client *transport.Client, pattern string) int {
	t.Helper()
	var out bytes.Buffer
	_ = client.Run(ctx, "find /tmp -maxdepth 1 -name "+transport.ShellQuote(pattern)+" | wc -l", &out, &bytes.Buffer{})
	n, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("parsing glob count for %q: %v (%q)", pattern, err, out.String())
	}
	return n
}

func TestDeployUploadsAndExtractsTheArchive(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)

	archive := buildTestArchive(t, map[string]string{
		"manifest.json":       `{"version":"v0.1.0"}`,
		"bootstrap/10-apt.sh": "#!/bin/sh\necho hallo\n",
	})
	remoteDir := "/tmp/energy-node-installer-deploy-test/" + t.Name()

	if err := bundle.Deploy(context.Background(), client, archive, remoteDir); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remoteDir+"/manifest.json", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading extracted manifest.json: %v", err)
	}
	if stdout.String() != `{"version":"v0.1.0"}` {
		t.Fatalf("unexpected content: %q", stdout.String())
	}

	// The staged archive played no role beyond getting the bytes there and
	// must not linger on the node afterwards. Scoped to this test's own
	// remoteDir tag so a concurrently-running package (steps also calls
	// Deploy) staging its own archive under the same /tmp cannot make this
	// assertion flaky.
	if n := remoteGlobCount(t, ctx, client, "energy-node-installer-bundle-"+testStagingTag(remoteDir)+"-*.tar.gz"); n != 0 {
		t.Fatalf("staged archive was not cleaned up: %d matching files remain", n)
	}
}

func deployFakeVerifyScript(t *testing.T, client *transport.Client, output string, exitCode int) string {
	t.Helper()
	archive := buildTestArchive(t, map[string]string{
		"bootstrap/verify_bundle.sh": fakeVerifyBundleScript(output, exitCode),
	})
	remoteDir := "/tmp/energy-node-installer-verify-test/" + t.Name()
	if err := bundle.Deploy(context.Background(), client, archive, remoteDir); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })
	return remoteDir
}

func TestVerifyRemoteAcceptsAnOKResult(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := deployFakeVerifyScript(t, client, "OK", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bundle.VerifyRemote(ctx, client, remoteDir, []byte("irrelevant for this fake")); err != nil {
		t.Fatalf("VerifyRemote: %v", err)
	}
}

func TestVerifyRemoteReportsTheFaultCode(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := deployFakeVerifyScript(t, client, "FEHLER ARCH_MISMATCH", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := bundle.VerifyRemote(ctx, client, remoteDir, []byte("irrelevant for this fake"))
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) {
		t.Fatalf("expected a *bundle.Error, got %v", err)
	}
	if bundleErr.Code != bundle.FaultArchMismatch {
		t.Fatalf("expected FaultArchMismatch, got %s", bundleErr.Code)
	}
}

func TestVerifyRemoteCleansUpTheUploadedPublicKey(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := deployFakeVerifyScript(t, client, "OK", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bundle.VerifyRemote(ctx, client, remoteDir, []byte("irrelevant")); err != nil {
		t.Fatalf("VerifyRemote: %v", err)
	}

	if n := remoteGlobCount(t, ctx, client, "energy-node-installer-pubkey-"+testStagingTag(remoteDir)+"-*.pem"); n != 0 {
		t.Fatalf("uploaded public key was not cleaned up: %d matching files remain", n)
	}
}
