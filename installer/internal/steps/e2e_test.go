package steps_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// realVerifyBundlePath locates Plan A-II's actual verify_bundle.sh relative
// to this test file. This is a deliberate cross-plan dependency: this test
// proves the Go and Bash halves of the installer actually fit together,
// not just that each side matches its own idea of the other.
func realVerifyBundlePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "bootstrap", "verify_bundle.sh"))
	if err != nil {
		t.Fatalf("resolving verify_bundle.sh path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("scripts/bootstrap/verify_bundle.sh not found (Plan A not present in this checkout yet): %v", err)
	}
	return path
}

func hostUnameMachine(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		t.Fatalf("uname -m: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func hostPythonABI(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("python3", "-c", `import sys; print("cp%d%d" % sys.version_info[:2])`).Output()
	if err != nil {
		t.Skip("python3 not available; skipping a test that needs the real verify_bundle.sh")
	}
	return strings.TrimSpace(string(out))
}

// requireOpenSSL skips the test if openssl is missing. The real
// verify_bundle.sh shells out to `openssl pkeyutl -verify` for the
// signature check regardless of which scenario a test is exercising.
func requireOpenSSL(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not available; skipping a test that needs the real verify_bundle.sh")
	}
}

const idempotentStepTemplate = `#!/bin/sh
id="__STEP_ID__"
mkdir -p "$EN_STATE_DIR/steps"
stamp="$EN_STATE_DIR/steps/$id"
printf '##STEP %s begin\n' "$id"
if [ -f "$stamp" ] && [ "$(cat "$stamp")" = "bundle=$EN_BUNDLE_VERSION" ]; then
  printf '##STEP %s skip already done\n' "$id"
  exit 0
fi
printf 'doing the work for %s\n' "$id"
printf 'bundle=%s\n' "$EN_BUNDLE_VERSION" > "$stamp"
printf '##STEP %s ok\n' "$id"
`

func idempotentStep(id string) string {
	return strings.ReplaceAll(idempotentStepTemplate, "__STEP_ID__", id)
}

func encodePublicKeyPEM(t *testing.T, pub ed25519.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshaling public key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// buildEndToEndBundle lays out a bundle directory with the real
// verify_bundle.sh plus two idempotent fake steps, and writes a correctly
// hashed, correctly signed manifest.json for it.
func buildEndToEndBundle(t *testing.T, dir, arch string, unameMachine []string, pythonABI, version string, priv ed25519.PrivateKey) {
	t.Helper()
	verifyContent, err := os.ReadFile(realVerifyBundlePath(t))
	if err != nil {
		t.Fatalf("reading real verify_bundle.sh: %v", err)
	}

	files := map[string][]byte{
		"bootstrap/verify_bundle.sh":  verifyContent,
		"bootstrap/10-apt.sh":         []byte(idempotentStep("10")),
		"bootstrap/50-python-deps.sh": []byte(idempotentStep("50")),
	}
	hashes := map[string]string{}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, content, 0o755); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		sum := sha256.Sum256(content)
		hashes[rel] = hex.EncodeToString(sum[:])
	}

	manifest := map[string]any{
		"version":       version,
		"arch":          arch,
		"uname_machine": unameMachine,
		"python_minor":  "irrelevant-for-this-test",
		"python_abi":    pythonABI,
		"files":         hashes,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encoding manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatalf("writing manifest.json: %v", err)
	}
	sig := ed25519.Sign(priv, raw)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), sig, 0o644); err != nil {
		t.Fatalf("writing manifest.json.sig: %v", err)
	}
}

func deployPrebuiltBundle(t *testing.T, client *transport.Client, srcDir string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if out, err := exec.Command("tar", "-czf", archive, "-C", srcDir, ".").CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	remoteDir := "/tmp/energy-node-installer-e2e-test/" + t.Name() + "/bundle"
	if err := bundle.Deploy(context.Background(), client, archive, remoteDir); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf /tmp/energy-node-installer-e2e-test/"+t.Name(), &bytes.Buffer{}, &bytes.Buffer{})
	})
	return remoteDir
}

func assertMarkers(t *testing.T, got, want []steps.Marker) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d markers, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("marker %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestEndToEndInstallVerifyRunAndResume(t *testing.T) {
	requireSFTPServerForSteps(t)
	requireOpenSSL(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	arch := hostUnameMachine(t)
	abi := hostPythonABI(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	src := t.TempDir()
	buildEndToEndBundle(t, src, "test-arch", []string{arch}, abi, "v1.0.0", priv)
	remoteBundleDir := deployPrebuiltBundle(t, client, src)
	remoteStateDir := "/tmp/energy-node-installer-e2e-test/" + t.Name() + "/state"
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{})
	})

	ctx := context.Background()
	if err := bundle.VerifyRemote(ctx, client, remoteBundleDir, encodePublicKeyPEM(t, pub)); err != nil {
		t.Fatalf("VerifyRemote rejected a genuinely matching bundle: %v", err)
	}

	runOpts := steps.RunOptions{
		Client:          client,
		RemoteBundleDir: remoteBundleDir,
		RemoteStateDir:  remoteStateDir,
		BundleVersion:   "v1.0.0",
		Steps: []bundle.StepEntry{
			{ID: "10", Optional: false},
			{ID: "50", Optional: false},
		},
	}

	var firstRunMarkers []steps.Marker
	runOpts.OnMarker = func(m steps.Marker) { firstRunMarkers = append(firstRunMarkers, m) }
	if err := steps.Run(ctx, runOpts); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	assertMarkers(t, firstRunMarkers, []steps.Marker{
		{StepID: "10", Kind: steps.Begin}, {StepID: "10", Kind: steps.OK},
		{StepID: "50", Kind: steps.Begin}, {StepID: "50", Kind: steps.OK},
	})

	// AK4: a second run against the same node repeats nothing -- each step
	// recognises its own stamp and reports skip, the same way a resumed run
	// after an interrupted first attempt would for the steps that already
	// finished before the interruption.
	var secondRunMarkers []steps.Marker
	runOpts.OnMarker = func(m steps.Marker) { secondRunMarkers = append(secondRunMarkers, m) }
	if err := steps.Run(ctx, runOpts); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	assertMarkers(t, secondRunMarkers, []steps.Marker{
		{StepID: "10", Kind: steps.Begin}, {StepID: "10", Kind: steps.Skip, Detail: "already done"},
		{StepID: "50", Kind: steps.Begin}, {StepID: "50", Kind: steps.Skip, Detail: "already done"},
	})
}

func TestEndToEndRejectsAnArchitectureMismatchBeforeAnyStepRuns(t *testing.T) {
	requireSFTPServerForSteps(t)
	requireOpenSSL(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	abi := hostPythonABI(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	src := t.TempDir()
	// A machine string that cannot possibly match this test host.
	buildEndToEndBundle(t, src, "test-arch", []string{"definitely-not-this-machine"}, abi, "v1.0.0", priv)
	remoteBundleDir := deployPrebuiltBundle(t, client, src)
	remoteStateDir := "/tmp/energy-node-installer-e2e-test/" + t.Name() + "/state"
	t.Cleanup(func() {
		_ = client.Run(context.Background(), "rm -rf "+remoteStateDir, &bytes.Buffer{}, &bytes.Buffer{})
	})

	ctx := context.Background()
	err = bundle.VerifyRemote(ctx, client, remoteBundleDir, encodePublicKeyPEM(t, pub))
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) || bundleErr.Code != bundle.FaultArchMismatch {
		t.Fatalf("expected FaultArchMismatch, got %v", err)
	}

	// The point of AK5: nothing below VerifyRemote may have run.
	var stateOut bytes.Buffer
	_ = client.Run(ctx, "test -d "+remoteStateDir+"/steps && echo present || echo gone", &stateOut, &bytes.Buffer{})
	if trimNewlineForSteps(stateOut.String()) != "gone" {
		t.Fatalf("a step appears to have run despite the architecture mismatch")
	}
}
