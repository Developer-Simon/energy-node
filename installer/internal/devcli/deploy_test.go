package devcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// recordCalls counts how often RunDeploy recorded the installed manifest;
// swapDeployCollaborators resets it and installs the counting fake.
var recordCalls int

// clearedStamps and restarted record what RunDeploy asked the node to do
// around an --only run; swapDeployCollaborators resets both.
var (
	clearedStamps []string
	restarted     []string
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
	origVerifyLocalDev, origVerifyRemoteDev := verifyBundleLocalDev, verifyBundleRemoteDev
	origProvision := provisionRemoteStateDir
	origRecord := recordInstalled
	origClearStamp, origRestart := clearRemoteStepStamp, restartUnits
	t.Cleanup(func() {
		clearRemoteStepStamp, restartUnits = origClearStamp, origRestart
		buildViaRepo, extractArchive = origBuild, origExtract
		verifyBundleLocal, deployBundle = origVerifyLocal, origDeploy
		verifyBundleRemote, previewRun = origVerifyRemote, origPreview
		runSteps, clearRemoteConfigAndStamp = origRun, origClear
		embeddedPublicKey, embeddedPublicKeyPEM = origPubKey, origPubKeyPEM
		verifyBundleLocalDev, verifyBundleRemoteDev = origVerifyLocalDev, origVerifyRemoteDev
		provisionRemoteStateDir = origProvision
		recordInstalled = origRecord
	})

	buildViaRepo = func(context.Context, bundle.BuildArgs) (string, error) { return "/fake/archive.tar.gz", nil }
	extractArchive = func(string, string) error { return nil }
	embeddedPublicKey = func() (ed25519.PublicKey, error) { return ed25519.PublicKey{}, nil }
	embeddedPublicKeyPEM = func() []byte { return nil }
	provisionRemoteStateDir = func(context.Context, *transport.Client) error { return nil }
	recordCalls = 0
	recordInstalled = func(context.Context, *transport.Client, string, string) error { recordCalls++; return nil }
	clearedStamps, restarted = nil, nil
	clearRemoteStepStamp = func(_ context.Context, _ *transport.Client, _, stepID string) error {
		clearedStamps = append(clearedStamps, stepID)
		return nil
	}
	restartUnits = func(_ context.Context, _ *transport.Client, verb string, units []string, _ io.Writer) error {
		for _, u := range units {
			restarted = append(restarted, verb+" "+u)
		}
		return nil
	}
}

func fakeManifest() *bundle.Manifest {
	return &bundle.Manifest{
		Version: "v0.3.0",
		Steps: []bundle.StepEntry{
			{ID: "50", Optional: false},
			{ID: "60", Optional: false},
			{ID: "81", Optional: true, ServiceID: "apsystems", Unit: "apsystems-ez1.service"},
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

func TestRunDeployFullRunRecordsTheInstalledManifest(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	if err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if recordCalls != 1 {
		t.Fatalf("recordInstalled called %d times after a full run, want 1", recordCalls)
	}
}

func TestRunDeployRecordFailureIsOnlyAWarning(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }
	recordInstalled = func(context.Context, *transport.Client, string, string) error { return errors.New("disk full") }

	var out bytes.Buffer
	if err := RunDeploy(context.Background(), DeployArgs{Stdout: &out}); err != nil {
		t.Fatalf("a failed record must not fail the run: %v", err)
	}
	if !containsAll(out.String(), "warning", "disk full") {
		t.Fatalf("expected a warning naming the cause, got: %q", out.String())
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

func TestRunDeployBuildsWithADevVersion(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }
	var gotBuild bundle.BuildArgs
	buildViaRepo = func(_ context.Context, a bundle.BuildArgs) (string, error) {
		gotBuild = a
		return "/fake/archive.tar.gz", nil
	}

	if err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if !gotBuild.DevVersion {
		t.Fatalf("a deploy from a checkout must build with DevVersion")
	}
}

func TestRunDeployFullRunRestartsEveryServiceThroughTheSteps(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	var gotOpts steps.RunOptions
	runSteps = func(_ context.Context, opts steps.RunOptions) error { gotOpts = opts; return nil }

	if err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if !gotOpts.RestartAll {
		t.Fatalf("a full developer deploy must restart every service (RestartAll)")
	}
	if len(clearedStamps) != 0 || len(restarted) != 0 {
		t.Fatalf("a full run must neither clear stamps nor restart on its own: %v %v", clearedStamps, restarted)
	}
}

func TestRunDeployOnlyRedoesTheStepAndRestartsItsUnit(t *testing.T) {
	for _, tc := range []struct{ only, step, unit string }{
		{"dashboard", "60", "restart energy-node-dashboard.service"},
		{"apsystems", "81", "restart apsystems-ez1.service"},
	} {
		t.Run(tc.only, func(t *testing.T) {
			swapDeployCollaborators(t)
			verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
			deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
			verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
			var gotOpts steps.RunOptions
			runSteps = func(_ context.Context, opts steps.RunOptions) error {
				if len(clearedStamps) != 1 || clearedStamps[0] != tc.step {
					t.Fatalf("stamp of step %s must be cleared before the run, got %v", tc.step, clearedStamps)
				}
				gotOpts = opts
				return nil
			}

			if err := RunDeploy(context.Background(), DeployArgs{Only: tc.only, Stdout: &bytes.Buffer{}}); err != nil {
				t.Fatalf("RunDeploy: %v", err)
			}
			if gotOpts.RestartAll {
				t.Fatalf("--only restarts its own unit; RestartAll must stay off")
			}
			if len(restarted) != 1 || restarted[0] != tc.unit {
				t.Fatalf("restarted = %v, want [%s]", restarted, tc.unit)
			}
		})
	}
}

func TestRunDeployOnlyWheelsRestartsNothingAndSaysSo(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	var out bytes.Buffer
	if err := RunDeploy(context.Background(), DeployArgs{Only: "wheels", Stdout: &out}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if len(restarted) != 0 {
		t.Fatalf("--only wheels has no unit, restarted = %v", restarted)
	}
	if !containsAll(out.String(), "installer restart") {
		t.Fatalf("expected a hint to run installer restart, got %q", out.String())
	}
}

func TestRunDeployOnlyDryRunClearsNothing(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	previewRun = func(_ context.Context, _ *transport.Client, _, _, v string) (*steps.Plan, error) {
		return &steps.Plan{BundleVersion: v}, nil
	}

	if err := RunDeploy(context.Background(), DeployArgs{Only: "dashboard", DryRun: true, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if len(clearedStamps) != 0 || len(restarted) != 0 {
		t.Fatalf("--dry-run must not touch the node: %v %v", clearedStamps, restarted)
	}
}

func TestRunDeployOnlyDoesNotRecordTheInstalledManifest(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	if err := RunDeploy(context.Background(), DeployArgs{Only: "dashboard", Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if recordCalls != 0 {
		t.Fatalf("a single-step run must not record, calls = %d", recordCalls)
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
	if recordCalls != 0 {
		t.Fatalf("a failed run must not record, calls = %d", recordCalls)
	}
}

func TestRunDeployProvisionsTheRemoteStateDirBeforeDeployingTheBundle(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	var order []string
	provisionRemoteStateDir = func(context.Context, *transport.Client) error {
		order = append(order, "provision")
		return nil
	}
	deployBundle = func(context.Context, *transport.Client, string, string) error {
		order = append(order, "deploy")
		return nil
	}

	err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if len(order) != 2 || order[0] != "provision" || order[1] != "deploy" {
		t.Fatalf("expected the remote state dir to be provisioned before the bundle is deployed, got %v", order)
	}
}

func TestRunDeployAbortsBeforeDeployingWhenProvisioningFails(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	provisionRemoteStateDir = func(context.Context, *transport.Client) error { return errors.New("no sudo") }
	deployBundle = func(context.Context, *transport.Client, string, string) error {
		t.Fatalf("must not deploy when provisioning the remote state dir fails")
		return nil
	}

	err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRunDeployDryRunDoesNotProvisionTheRemoteStateDir(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	previewRun = func(context.Context, *transport.Client, string, string, string) (*steps.Plan, error) {
		return &steps.Plan{}, nil
	}
	provisionRemoteStateDir = func(context.Context, *transport.Client) error {
		t.Fatalf("--dry-run must not touch the node at all")
		return nil
	}

	if err := RunDeploy(context.Background(), DeployArgs{DryRun: true, Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
}

func TestRunDeployDevUnsignedSkipsSignatureVerificationLocallyAndRemotely(t *testing.T) {
	swapDeployCollaborators(t)
	embeddedPublicKey = func() (ed25519.PublicKey, error) {
		t.Fatalf("--dev-unsigned must never need the embedded signing key")
		return nil, nil
	}
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) {
		t.Fatalf("--dev-unsigned must use the unsigned local verifier, not the signed one")
		return nil, nil
	}
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error {
		t.Fatalf("--dev-unsigned must use the unsigned remote verifier, not the signed one")
		return nil
	}
	localCalled, remoteCalled := false, false
	verifyBundleLocalDev = func(string) (*bundle.Manifest, error) { localCalled = true; return fakeManifest(), nil }
	verifyBundleRemoteDev = func(context.Context, *transport.Client, string) error { remoteCalled = true; return nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	var out bytes.Buffer
	err := RunDeploy(context.Background(), DeployArgs{DevUnsigned: true, Stdout: &out})
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if !localCalled || !remoteCalled {
		t.Fatalf("expected both the local and remote unsigned verifiers to run")
	}
	if !containsAll(out.String(), "dev-unsigned") {
		t.Fatalf("expected a loud warning that signature verification was skipped, got: %s", out.String())
	}
}

func TestRunDeployWithoutDevUnsignedUsesTheSignedVerifiers(t *testing.T) {
	swapDeployCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	runSteps = func(context.Context, steps.RunOptions) error { return nil }
	verifyBundleLocalDev = func(string) (*bundle.Manifest, error) {
		t.Fatalf("must not use the unsigned local verifier by default")
		return nil, nil
	}
	verifyBundleRemoteDev = func(context.Context, *transport.Client, string) error {
		t.Fatalf("must not use the unsigned remote verifier by default")
		return nil
	}

	if err := RunDeploy(context.Background(), DeployArgs{Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("RunDeploy: %v", err)
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
