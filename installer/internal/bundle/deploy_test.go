package bundle_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

// realVerifyBundlePath locates the actual verify_bundle.sh relative to this
// test file, mirroring steps.e2e_test's realVerifyBundlePath: this proves
// VerifyRemoteDev's --target-only invocation actually matches what the real
// script accepts, not just a fake script that ignores its own arguments.
func realVerifyBundlePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "bootstrap", "verify_bundle.sh"))
	if err != nil {
		t.Fatalf("resolving verify_bundle.sh path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("scripts/bootstrap/verify_bundle.sh not found: %v", err)
	}
	return path
}

func TestVerifyRemoteDevAcceptsAnUnsignedBundleAgainstTheRealScript(t *testing.T) {
	requireSFTPServerForBundle(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; the real verify_bundle.sh needs it")
	}
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)

	verifyScript, err := os.ReadFile(realVerifyBundlePath(t))
	if err != nil {
		t.Fatalf("reading real verify_bundle.sh: %v", err)
	}
	uname, err := exec.Command("uname", "-m").Output()
	if err != nil {
		t.Fatalf("uname -m: %v", err)
	}
	machine := strings.TrimSpace(string(uname))
	manifest := fmt.Sprintf(`{"version":"v0.1.0","uname_machine":["%s"]}`, machine)
	archive := buildTestArchive(t, map[string]string{
		"bootstrap/verify_bundle.sh": string(verifyScript),
		"manifest.json":              manifest,
	})
	remoteDir := "/tmp/energy-node-installer-verify-dev-test/" + t.Name()
	if err := bundle.Deploy(context.Background(), client, archive, remoteDir); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// No manifest.json.sig was written above: the real script must accept
	// this bundle under --target-only precisely because it never asks for a
	// signature or a key.
	if err := bundle.VerifyRemoteDev(ctx, client, remoteDir); err != nil {
		t.Fatalf("VerifyRemoteDev against the real script: %v", err)
	}
}

func TestVerifyRemoteDevReportsTheFaultCode(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := deployFakeVerifyScript(t, client, "FEHLER ARCH_MISMATCH", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := bundle.VerifyRemoteDev(ctx, client, remoteDir)
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) {
		t.Fatalf("expected a *bundle.Error, got %v", err)
	}
	if bundleErr.Code != bundle.FaultArchMismatch {
		t.Fatalf("expected FaultArchMismatch, got %s", bundleErr.Code)
	}
}

func TestVerifyRemoteDevDoesNotUploadAPublicKey(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := deployFakeVerifyScript(t, client, "OK", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bundle.VerifyRemoteDev(ctx, client, remoteDir); err != nil {
		t.Fatalf("VerifyRemoteDev: %v", err)
	}
	// Scoped to this test's own stagingTag, like the cleanup test above: /tmp
	// is the real, shared temp dir of the machine running the test, and
	// `go test ./...` runs packages concurrently, so an unscoped glob can
	// catch a pubkey file that a different package's own VerifyRemote (e.g.
	// internal/devcli, internal/steps) happens to have in flight at the same
	// moment.
	if n := remoteGlobCount(t, ctx, client, "energy-node-installer-pubkey-"+testStagingTag(remoteDir)+"-*.pem"); n != 0 {
		t.Fatalf("VerifyRemoteDev must never upload a public key, found %d matching files", n)
	}
}

func TestReadInstalledManifestParsesAnExistingFile(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteStateDir := "/tmp/energy-node-installer-installed-manifest-test/" + t.Name()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Run(ctx, "mkdir -p "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("mkdir remoteStateDir: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{})
	})
	manifestJSON := `{"version":"v1.4.2","files":{"a":"1"}}`
	if err := client.UploadBytes([]byte(manifestJSON), remoteStateDir+"/installed-manifest.json", 0o644); err != nil {
		t.Fatalf("uploading installed-manifest.json: %v", err)
	}

	got := bundle.ReadInstalledManifest(ctx, client, remoteStateDir)
	if got == nil || got.Version != "v1.4.2" || got.Files["a"] != "1" {
		t.Fatalf("ReadInstalledManifest = %+v", got)
	}
}

func TestReadInstalledManifestReturnsNilWhenTheFileIsMissing(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := bundle.ReadInstalledManifest(ctx, client, "/tmp/energy-node-installer-never-installed/"+t.Name())
	if got != nil {
		t.Fatalf("ReadInstalledManifest = %+v, want nil", got)
	}
}

func TestReadInstalledManifestReturnsNilOnCorruptJSON(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteStateDir := "/tmp/energy-node-installer-corrupt-manifest-test/" + t.Name()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Run(ctx, "mkdir -p "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("mkdir remoteStateDir: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{})
	})
	if err := client.UploadBytes([]byte("not json"), remoteStateDir+"/installed-manifest.json", 0o644); err != nil {
		t.Fatalf("uploading corrupt manifest: %v", err)
	}

	got := bundle.ReadInstalledManifest(ctx, client, remoteStateDir)
	if got != nil {
		t.Fatalf("ReadInstalledManifest = %+v, want nil for corrupt JSON", got)
	}
}

func TestDeployDeltaAddsChangesFilesAndRemovesDroppedOnesWithoutTouchingOthers(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := "/tmp/energy-node-installer-delta-test/" + t.Name()

	// Seed remoteDir as if a previous full Deploy had already run.
	seed := buildTestArchive(t, map[string]string{
		"manifest.json":       `{"version":"v1"}`,
		"bootstrap/10-apt.sh": "old content",
		"wheels/drop-me.whl":  "will be removed",
	})
	if err := bundle.Deploy(context.Background(), client, seed, remoteDir); err != nil {
		t.Fatalf("seeding Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	// bundleDir holds the *new* version's own files -- only the changed and
	// brand-new ones need to exist for DeployDelta's purposes, but a real
	// bundle directory holds everything; unrelated extra files must be
	// ignored since they are not named in changed.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "bootstrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bootstrap/10-apt.sh"), []byte("new content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{"version":"v2"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var progressCalled bool
	err := bundle.DeployDelta(context.Background(), client, src,
		[]string{"bootstrap/10-apt.sh"}, []string{"wheels/drop-me.whl"},
		remoteDir, func(done, total int64) { progressCalled = true })
	if err != nil {
		t.Fatalf("DeployDelta: %v", err)
	}
	if !progressCalled {
		t.Errorf("expected onProgress to be called for the changed-files upload")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remoteDir+"/manifest.json", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading manifest.json: %v", err)
	}
	if stdout.String() != `{"version":"v2"}` {
		t.Fatalf("manifest.json was not updated: %q", stdout.String())
	}
	stdout.Reset()
	if err := client.Run(ctx, "cat "+remoteDir+"/bootstrap/10-apt.sh", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading bootstrap/10-apt.sh: %v", err)
	}
	if stdout.String() != "new content" {
		t.Fatalf("bootstrap/10-apt.sh was not updated: %q", stdout.String())
	}
	if got := remoteFileState(t, ctx, client, remoteDir+"/wheels/drop-me.whl"); got != "gone" {
		t.Fatalf("wheels/drop-me.whl = %q, want gone", got)
	}
}

func TestDeployDeltaWithNothingChangedStillRefreshesTheManifest(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := "/tmp/energy-node-installer-delta-noop-test/" + t.Name()
	seed := buildTestArchive(t, map[string]string{
		"manifest.json":     `{"version":"v1"}`,
		"manifest.json.sig": "old-sig",
	})
	if err := bundle.Deploy(context.Background(), client, seed, remoteDir); err != nil {
		t.Fatalf("seeding Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{"version":"v2"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := bundle.DeployDelta(context.Background(), client, src, nil, nil, remoteDir, nil); err != nil {
		t.Fatalf("DeployDelta with nothing to do: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remoteDir+"/manifest.json", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading manifest.json: %v", err)
	}
	if stdout.String() != `{"version":"v2"}` {
		t.Fatalf("manifest.json was not updated: %q", stdout.String())
	}
	if got := remoteFileState(t, ctx, client, remoteDir+"/manifest.json.sig"); got != "gone" {
		t.Fatalf("manifest.json.sig = %q, want gone", got)
	}
}

func TestDeployDeltaShipsTheSignatureWhenTheBundleHasOne(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := "/tmp/energy-node-installer-delta-sig-test/" + t.Name()
	seed := buildTestArchive(t, map[string]string{
		"manifest.json": `{"version":"v1"}`,
	})
	if err := bundle.Deploy(context.Background(), client, seed, remoteDir); err != nil {
		t.Fatalf("seeding Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(`{"version":"v2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json.sig"), []byte("new-sig"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := bundle.DeployDelta(context.Background(), client, src, nil, nil, remoteDir, nil); err != nil {
		t.Fatalf("DeployDelta: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	if err := client.Run(ctx, "cat "+remoteDir+"/manifest.json.sig", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading manifest.json.sig: %v", err)
	}
	if stdout.String() != "new-sig" {
		t.Fatalf("manifest.json.sig content = %q, want 'new-sig'", stdout.String())
	}
}

func TestDeployDeltaRejectsAPathThatEscapesRemoteDir(t *testing.T) {
	requireSFTPServerForBundle(t)
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := "/tmp/energy-node-installer-delta-unsafe-test/" + t.Name()

	err := bundle.DeployDelta(context.Background(), client, t.TempDir(), nil, []string{"../../etc/passwd"}, remoteDir, nil)
	if err == nil {
		t.Fatalf("expected an error for a path escaping remoteDir")
	}
}

func TestDeltaTransferPassesTheSameVerificationAsAFullOne(t *testing.T) {
	requireSFTPServerForBundle(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; the real verify_bundle.sh needs it")
	}
	sshd := transporttest.Start(t)
	client := dialForBundleTest(t, sshd)
	remoteDir := "/tmp/energy-node-installer-delta-e2e-test/" + t.Name()

	verifyScript, err := os.ReadFile(realVerifyBundlePath(t))
	if err != nil {
		t.Fatalf("reading real verify_bundle.sh: %v", err)
	}
	uname, err := exec.Command("uname", "-m").Output()
	if err != nil {
		t.Fatalf("uname -m: %v", err)
	}
	machine := strings.TrimSpace(string(uname))

	// v1: seed remoteDir with a full Deploy, as if an earlier install ran.
	oldContent := map[string]string{
		"bootstrap/verify_bundle.sh": string(verifyScript),
		"bootstrap/10-apt.sh":        "old apt step",
		"wheels/old-only.whl":        "dropped in v2",
	}
	oldManifest := buildManifestFor(t, machine, "v1", oldContent)
	oldFiles := map[string]string{"manifest.json": oldManifest}
	for rel, body := range oldContent {
		oldFiles[rel] = body
	}
	seed := buildTestArchive(t, oldFiles)
	if err := bundle.Deploy(context.Background(), client, seed, remoteDir); err != nil {
		t.Fatalf("seeding v1 via Deploy: %v", err)
	}
	t.Cleanup(func() { _ = client.Run(context.Background(), "rm -rf "+remoteDir, &bytes.Buffer{}, &bytes.Buffer{}) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bundle.VerifyRemoteDev(ctx, client, remoteDir); err != nil {
		t.Fatalf("VerifyRemoteDev on the seeded v1: %v", err)
	}

	// v2: bootstrap/10-apt.sh changes, wheels/old-only.whl is dropped,
	// bootstrap/verify_bundle.sh is unchanged -- DeployDelta must not touch
	// it, only re-send what actually changed.
	newContent := map[string]string{
		"bootstrap/verify_bundle.sh": string(verifyScript),
		"bootstrap/10-apt.sh":        "new apt step",
	}
	newManifestJSON := buildManifestFor(t, machine, "v2", newContent)
	newDir := t.TempDir()
	for rel, body := range newContent {
		full := filepath.Join(newDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(newDir, "manifest.json"), []byte(newManifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var oldM, newM bundle.Manifest
	if err := json.Unmarshal([]byte(oldManifest), &oldM); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(newManifestJSON), &newM); err != nil {
		t.Fatal(err)
	}
	changed, removed := bundle.DiffManifest(&oldM, &newM)
	if !reflect.DeepEqual(changed, []string{"bootstrap/10-apt.sh"}) {
		t.Fatalf("changed = %v, want exactly the file that changed", changed)
	}
	if !reflect.DeepEqual(removed, []string{"wheels/old-only.whl"}) {
		t.Fatalf("removed = %v", removed)
	}

	if err := bundle.DeployDelta(context.Background(), client, newDir, changed, removed, remoteDir, nil); err != nil {
		t.Fatalf("DeployDelta to v2: %v", err)
	}

	// The node now has v2's manifest.json, so VerifyRemoteDev checks v2's
	// own file list -- exactly what a full Deploy of v2 would have left it
	// checking, but bootstrap/verify_bundle.sh itself was never re-sent.
	if err := bundle.VerifyRemoteDev(ctx, client, remoteDir); err != nil {
		t.Fatalf("VerifyRemoteDev after the delta transfer to v2: %v", err)
	}

	// Verify that manifest.json was actually updated on the node.
	var remoteManifest bytes.Buffer
	if err := client.Run(ctx, "cat "+remoteDir+"/manifest.json", &remoteManifest, &bytes.Buffer{}); err != nil {
		t.Fatalf("reading remote manifest.json: %v", err)
	}
	if remoteManifest.String() != newManifestJSON {
		t.Fatalf("remote manifest.json = %q, want %q", remoteManifest.String(), newManifestJSON)
	}

	if got := remoteFileState(t, ctx, client, remoteDir+"/wheels/old-only.whl"); got != "gone" {
		t.Fatalf("wheels/old-only.whl = %q, want gone after the v2 delta", got)
	}
}

// buildManifestFor writes a minimal but real manifest.json for content
// (relpath -> file body): version, uname_machine (so VerifyRemoteDev's own
// arch check passes) and a files map of each entry's actual sha256, exactly
// as scripts/build/lib/manifest.sh computes it for a real bundle.
func buildManifestFor(t *testing.T, machine, version string, content map[string]string) string {
	t.Helper()
	files := make(map[string]string, len(content))
	for rel, body := range content {
		sum := sha256.Sum256([]byte(body))
		files[rel] = hex.EncodeToString(sum[:])
	}
	m := struct {
		Version      string            `json:"version"`
		UnameMachine []string          `json:"uname_machine"`
		Files        map[string]string `json:"files"`
	}{Version: version, UnameMachine: []string{machine}, Files: files}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
