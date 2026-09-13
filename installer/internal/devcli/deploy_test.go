package devcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// swapDeployCollaborators overrides every package-level seam this task
// introduces and restores the originals when the test ends, so tests never
// leak a fake into another test.
func swapDeployCollaborators(t *testing.T) {
	t.Helper()
	origBuild, origExtract := buildViaRepo, extractArchive
	origVerifyLocal, origDeploy := verifyBundleLocal, deployBundle
	origVerifyRemote, origPreview := verifyBundleRemote, previewRun
	origRun, origClear := runSteps, clearRemoteConfigAndStamp
	origPubKey, origPubKeyPEM := embeddedPublicKey, embeddedPublicKeyPEM
	t.Cleanup(func() {
		buildViaRepo, extractArchive = origBuild, origExtract
		verifyBundleLocal, deployBundle = origVerifyLocal, origDeploy
		verifyBundleRemote, previewRun = origVerifyRemote, origPreview
		runSteps, clearRemoteConfigAndStamp = origRun, origClear
		embeddedPublicKey, embeddedPublicKeyPEM = origPubKey, origPubKeyPEM
	})

	buildViaRepo = func(context.Context, bundle.BuildArgs) (string, error) { return "/fake/archive.tar.gz", nil }
	extractArchive = func(string, string) error { return nil }
	embeddedPublicKey = func() (ed25519.PublicKey, error) { return ed25519.PublicKey{}, nil }
	embeddedPublicKeyPEM = func() []byte { return nil }
}

func fakeManifest() *bundle.Manifest {
	return &bundle.Manifest{
		Version: "v0.3.0",
		Steps: []bundle.StepEntry{
			{ID: "50", Optional: false},
			{ID: "60", Optional: false},
			{ID: "81", Optional: true, ServiceID: "apsystems"},
		},
	}
}

func TestRunDeployFullRunUsesEveryManifestStep(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployCalled, verifyRemoteCalled := false, false
	deployBundle = func(context.Context, *transport.Client, string, string) error { deployCalled = true; return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { verifyRemoteCalled = true; return nil }

	var gotOpts steps.RunOptions
	runSteps = func(_ context.Context, opts steps.RunOptions) error { gotOpts = opts; return nil }

	err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if !deployCalled || !verifyRemoteCalled {
		t.Fatalf("expected Deploy and VerifyRemote to run for a full deploy")
	}
	if len(gotOpts.Steps) != 3 {
		t.Fatalf("expected all three manifest steps, got %+v", gotOpts.Steps)
	}
	if gotOpts.BundleVersion != "v0.3.0" {
		t.Fatalf("unexpected bundle version passed to Run: %q", gotOpts.BundleVersion)
	}
}

func TestRunDeployOnlyFiltersToASingleStep(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }

	var gotOpts steps.RunOptions
	runSteps = func(_ context.Context, opts steps.RunOptions) error { gotOpts = opts; return nil }

	err := RunDeploy(context.Background(), DeployArgs{Only: "dashboard", Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if len(gotOpts.Steps) != 1 || gotOpts.Steps[0].ID != "60" {
		t.Fatalf("expected exactly step 60, got %+v", gotOpts.Steps)
	}
}

func TestRunDeployDryRunSkipsDeployAndRun(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }

	deployCalled, runCalled := false, false
	deployBundle = func(context.Context, *transport.Client, string, string) error { deployCalled = true; return nil }
	runSteps = func(context.Context, steps.RunOptions) error { runCalled = true; return nil }

	var previewedVersion string
	previewRun = func(_ context.Context, _ *transport.Client, _, _, bundleVersion string) (*steps.Plan, error) {
		previewedVersion = bundleVersion
		return &steps.Plan{BundleVersion: bundleVersion}, nil
	}

	var out bytes.Buffer
	if err := RunDeploy(context.Background(), DeployArgs{DryRun: true, Stdout: &out}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if deployCalled || runCalled {
		t.Fatalf("--dry-run must not call Deploy or Run")
	}
	if previewedVersion != "v0.3.0" {
		t.Fatalf("expected Preview to be called with the freshly built version, got %q", previewedVersion)
	}
	if out.Len() == 0 {
		t.Fatalf("expected --dry-run to print something")
	}
}

func TestRunDeployForceConfigRequiresStep60InTheRunList(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error {
		t.Fatalf("must not deploy before the --force-config validation error")
		return nil
	}

	err := RunDeploy(context.Background(), DeployArgs{Only: "wheels", ForceConfig: true, Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatalf("expected an error: --only wheels never touches step 60")
	}
}

func TestRunDeployForceConfigDeclinedAbortsBeforeRunning(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error {
		t.Fatalf("must not run any step when --force-config is declined")
		return nil
	}

	err := RunDeploy(context.Background(), DeployArgs{
		Only: "dashboard", ForceConfig: true, Stdout: &bytes.Buffer{},
		Confirm: func(string) (bool, error) { return false, nil },
	})
	if err == nil {
		t.Fatalf("expected an error when --force-config is declined")
	}
}

func TestRunDeployForceConfigConfirmedClearsRemoteStateBeforeRunning(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }

	var order []string
	clearRemoteConfigAndStamp = func(context.Context, *transport.Client) error {
		order = append(order, "clear")
		return nil
	}
	runSteps = func(context.Context, steps.RunOptions) error {
		order = append(order, "run")
		return nil
	}

	err := RunDeploy(context.Background(), DeployArgs{
		Only: "dashboard", ForceConfig: true, Stdout: &bytes.Buffer{},
		Confirm: func(string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if len(order) != 2 || order[0] != "clear" || order[1] != "run" {
		t.Fatalf("expected clear before run, got %v", order)
	}
}

func TestRunDeployTranslatesAStepFailureThroughFaults(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error {
		return &steps.StepFailure{StepID: "50", Code: "PIP_EXTERNALLY_MANAGED"}
	}

	err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got := err.Error(); !containsAll(got, "50", "break-system-packages") {
		t.Fatalf("expected the translated fault text in the error, got: %s", got)
	}
}

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}
