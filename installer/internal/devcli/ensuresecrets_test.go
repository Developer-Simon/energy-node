package devcli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

func swapEnsureSecretsCollaborators(t *testing.T) {
	t.Helper()
	swapDeployCollaborators(t)
	orig := clearRemoteStepStamp
	t.Cleanup(func() { clearRemoteStepStamp = orig })
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) { return fakeManifest(), nil }
	deployBundle = func(context.Context, *transport.Client, string, string) error { return nil }
	verifyBundleRemote = func(context.Context, *transport.Client, string, []byte) error { return nil }
	clearRemoteStepStamp = func(context.Context, *transport.Client, string, string) error { return nil }
}

func TestRunEnsureSecretsUsesCachedSecretsWithoutPrompting(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	dir := t.TempDir()
	mqttPath := filepath.Join(dir, "mqtt.pw")
	adminPath := filepath.Join(dir, "dashboard-admin.pw")
	if err := os.WriteFile(mqttPath, []byte("mqtt-geheim\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if err := os.WriteFile(adminPath, []byte("admin-geheim\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	var gotOpts steps.RunOptions
	runSteps = func(_ context.Context, opts steps.RunOptions) error { gotOpts = opts; return nil }

	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		MQTTSecretPath:  mqttPath,
		AdminSecretPath: adminPath,
		PromptSecret: func(label string) (string, error) {
			t.Fatalf("must not prompt when both secrets are already cached, label=%q", label)
			return "", nil
		},
		Stdout: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunEnsureSecrets: %v", err)
	}
	if gotOpts.Secrets == nil || gotOpts.Secrets.MQTTPassword != "mqtt-geheim" || gotOpts.Secrets.AdminPassword != "admin-geheim" {
		t.Fatalf("unexpected secrets passed to Run: %+v", gotOpts.Secrets)
	}
	if len(gotOpts.Steps) != 1 || gotOpts.Steps[0].ID != "60" {
		t.Fatalf("expected exactly step 60, got %+v", gotOpts.Steps)
	}
}

func TestRunEnsureSecretsPromptsAndCachesMissingSecrets(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	dir := t.TempDir()

	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		MQTTSecretPath:  filepath.Join(dir, "mqtt.pw"),
		AdminSecretPath: filepath.Join(dir, "dashboard-admin.pw"),
		PromptSecret: func(label string) (string, error) {
			if label == "MQTT password" {
				return "frisch-mqtt", nil
			}
			return "frisch-admin", nil
		},
		Stdout: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunEnsureSecrets: %v", err)
	}
	cached, _ := os.ReadFile(filepath.Join(dir, "mqtt.pw"))
	if string(cached) != "frisch-mqtt" {
		t.Fatalf("expected the prompted MQTT password to be cached, got %q", cached)
	}
}

func TestRunEnsureSecretsClearsTheStampBeforeRunning(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	dir := t.TempDir()

	var order []string
	clearRemoteStepStamp = func(_ context.Context, _ *transport.Client, remoteStateDir, stepID string) error {
		if stepID != "60" {
			t.Errorf("expected to clear step 60's stamp, got %q", stepID)
		}
		order = append(order, "clear")
		return nil
	}
	runSteps = func(context.Context, steps.RunOptions) error { order = append(order, "run"); return nil }

	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		MQTTSecretPath:  filepath.Join(dir, "mqtt.pw"),
		AdminSecretPath: filepath.Join(dir, "dashboard-admin.pw"),
		PromptSecret:    func(string) (string, error) { return "x", nil },
		Stdout:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunEnsureSecrets: %v", err)
	}
	if len(order) != 2 || order[0] != "clear" || order[1] != "run" {
		t.Fatalf("expected clear before run, got %v", order)
	}
}

func TestRunEnsureSecretsProvisionsTheRemoteStateDirBeforeDeployingTheBundle(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	dir := t.TempDir()
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

	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		MQTTSecretPath:  filepath.Join(dir, "mqtt.pw"),
		AdminSecretPath: filepath.Join(dir, "dashboard-admin.pw"),
		PromptSecret:    func(string) (string, error) { return "x", nil },
		Stdout:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("RunEnsureSecrets: %v", err)
	}
	if len(order) != 2 || order[0] != "provision" || order[1] != "deploy" {
		t.Fatalf("expected the remote state dir to be provisioned before the bundle is deployed, got %v", order)
	}
}

func TestRunEnsureSecretsDevUnsignedSkipsSignatureVerificationLocallyAndRemotely(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	dir := t.TempDir()
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
	runSteps = func(context.Context, steps.RunOptions) error { return nil }

	var out bytes.Buffer
	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		DevUnsigned:     true,
		MQTTSecretPath:  filepath.Join(dir, "mqtt.pw"),
		AdminSecretPath: filepath.Join(dir, "dashboard-admin.pw"),
		PromptSecret:    func(string) (string, error) { return "x", nil },
		Stdout:          &out,
	})
	if err != nil {
		t.Fatalf("RunEnsureSecrets: %v", err)
	}
	if !localCalled || !remoteCalled {
		t.Fatalf("expected both the local and remote unsigned verifiers to run")
	}
	if !containsAll(out.String(), "dev-unsigned") {
		t.Fatalf("expected a loud warning that signature verification was skipped, got: %s", out.String())
	}
}

func TestRunEnsureSecretsFailsWhenManifestHasNoStep60(t *testing.T) {
	swapEnsureSecretsCollaborators(t)
	verifyBundleLocal = func(string, ed25519.PublicKey) (*bundle.Manifest, error) {
		return &bundle.Manifest{Version: "v0.3.0", Steps: []bundle.StepEntry{{ID: "50"}}}, nil
	}
	runSteps = func(context.Context, steps.RunOptions) error {
		t.Fatalf("must not run anything when the bundle has no step 60")
		return nil
	}

	dir := t.TempDir()
	err := RunEnsureSecrets(context.Background(), EnsureSecretsArgs{
		MQTTSecretPath:  filepath.Join(dir, "mqtt.pw"),
		AdminSecretPath: filepath.Join(dir, "dashboard-admin.pw"),
		PromptSecret:    func(string) (string, error) { return "x", nil },
		Stdout:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatalf("expected an error")
	}
}
