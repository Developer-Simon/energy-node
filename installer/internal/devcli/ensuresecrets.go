package devcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/term"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// clearRemoteStepStamp is the ensure-secrets-only counterpart to Task 10's
// clearRemoteConfigAndStamp: it removes only a step's stamp, never
// config.json, so ensure-secrets can never touch a dashboard-edited
// configuration.
var clearRemoteStepStamp = defaultClearRemoteStepStamp

// EnsureSecretsArgs configures one call to RunEnsureSecrets.
type EnsureSecretsArgs struct {
	Target          Target
	Client          *transport.Client
	RepoRoot        string
	Arch            string
	PythonMinor     string
	ABI             string
	SignKeyPath     string
	MQTTSecretPath  string                             // e.g. secrets/mqtt.pw
	AdminSecretPath string                             // e.g. secrets/dashboard-admin.pw
	PromptSecret    func(label string) (string, error) // nil uses a real terminal prompt
	Stdout          io.Writer
}

// RunEnsureSecrets supplies a missing MQTT or dashboard admin password to an
// existing installation by clearing step 60's stamp and rerunning it -- see
// this task's rationale for why that reuses 60-node-install.sh's own
// idempotency instead of a second implementation in Go.
func RunEnsureSecrets(ctx context.Context, args EnsureSecretsArgs) error {
	pubKey, err := embeddedPublicKey()
	if err != nil {
		return fmt.Errorf("loading embedded signing key: %w", err)
	}

	outDir, err := os.MkdirTemp("", "energy-node-installer-build-*")
	if err != nil {
		return fmt.Errorf("creating a build directory: %w", err)
	}
	defer os.RemoveAll(outDir)

	archivePath, err := buildViaRepo(ctx, bundle.BuildArgs{
		RepoRoot: args.RepoRoot, Arch: args.Arch, PythonMinor: args.PythonMinor,
		ABI: args.ABI, User: args.Target.User, Base: args.Target.Base,
		OutDir: outDir, SignKeyPath: args.SignKeyPath,
	})
	if err != nil {
		return fmt.Errorf("building bundle: %w", err)
	}

	extractDir := filepath.Join(outDir, "extracted")
	if err := extractArchive(archivePath, extractDir); err != nil {
		return fmt.Errorf("extracting built bundle: %w", err)
	}

	manifest, err := verifyBundleLocal(extractDir, pubKey)
	if err != nil {
		return fmt.Errorf("verifying built bundle: %w", err)
	}
	step60, ok := manifest.StepByID("60")
	if !ok {
		return fmt.Errorf("bundle manifest has no step 60 (dashboard/config); cannot supply secrets")
	}

	prompt := args.PromptSecret
	if prompt == nil {
		prompt = defaultSecretPrompt
	}
	mqttPassword, err := LoadOrPromptSecret(args.MQTTSecretPath, "MQTT password", prompt)
	if err != nil {
		return fmt.Errorf("getting the MQTT password: %w", err)
	}
	adminPassword, err := LoadOrPromptSecret(args.AdminSecretPath, "Dashboard admin password", prompt)
	if err != nil {
		return fmt.Errorf("getting the dashboard admin password: %w", err)
	}

	if err := deployBundle(ctx, args.Client, archivePath, DefaultRemoteBundleDir); err != nil {
		return fmt.Errorf("uploading bundle: %w", err)
	}
	if err := verifyBundleRemote(ctx, args.Client, DefaultRemoteBundleDir, embeddedPublicKeyPEM()); err != nil {
		return fmt.Errorf("verifying bundle on the node: %w", err)
	}

	if err := clearRemoteStepStamp(ctx, args.Client, DefaultRemoteStateDir, step60.ID); err != nil {
		return fmt.Errorf("clearing step 60's stamp: %w", err)
	}

	err = runSteps(ctx, steps.RunOptions{
		Client:          args.Client,
		RemoteBundleDir: DefaultRemoteBundleDir,
		RemoteStateDir:  DefaultRemoteStateDir,
		BundleVersion:   manifest.Version,
		TargetUser:      args.Target.User,
		TargetBase:      args.Target.Base,
		Steps:           []bundle.StepEntry{step60},
		Secrets:         &steps.Secrets{MQTTPassword: mqttPassword, AdminPassword: adminPassword},
		OnMarker:        func(m steps.Marker) { printMarker(args.Stdout, m) },
		OnLog:           func(stepID, line string) { fmt.Fprintf(args.Stdout, "[%s] %s\n", stepID, line) },
	})
	return translateStepFailure(err)
}

func defaultSecretPrompt(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", label, err)
	}
	return string(raw), nil
}

func defaultClearRemoteStepStamp(ctx context.Context, client *transport.Client, remoteStateDir, stepID string) error {
	cmd := "sudo rm -f " + transport.ShellQuote(path.Join(remoteStateDir, "steps", stepID))
	var stderr bytes.Buffer
	if err := client.Run(ctx, cmd, io.Discard, &stderr); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return nil
}
