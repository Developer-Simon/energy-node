package devcli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/faults"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// Package-level seams: production code always sees these bound to Plan B-I's
// real functions; deploy_test.go (a white-box test in this same package)
// substitutes fakes to prove RunDeploy's own sequencing without a real
// signed bundle, tar archive or sshd -- those are already proven where each
// function itself is defined.
var (
	buildViaRepo              = bundle.BuildViaRepo
	extractArchive            = bundle.ExtractArchive
	verifyBundleLocal         = bundle.Verify
	deployBundle              = bundle.Deploy
	verifyBundleRemote        = bundle.VerifyRemote
	previewRun                = steps.Preview
	runSteps                  = steps.Run
	embeddedPublicKey         = bundle.EmbeddedPublicKey
	embeddedPublicKeyPEM      = bundle.EmbeddedPublicKeyPEM
	clearRemoteConfigAndStamp = defaultClearRemoteConfigAndStamp
)

// DeployArgs configures one call to RunDeploy: which node, which repo
// checkout to build a bundle from, and which of the developer-CLI's flags
// (E12) apply.
type DeployArgs struct {
	Target      Target
	Client      *transport.Client
	RepoRoot    string
	Arch        string
	PythonMinor string
	ABI         string
	SignKeyPath string
	Only        string // "" for a full deploy
	DryRun      bool
	ForceConfig bool
	Confirm     func(prompt string) (bool, error) // nil uses a real terminal prompt
	Stdout      io.Writer
}

// RunDeploy builds a bundle from args.RepoRoot (E3's repo mode), verifies it
// locally, and either previews (--dry-run) or deploys and runs it against
// args.Client. See this task's rationale for why --dry-run runs before any
// node-changing call, and why --force-config only clears a stamp instead of
// re-implementing 60-node-install.sh's own idempotency in Go.
func RunDeploy(ctx context.Context, args DeployArgs) error {
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
		RepoRoot:    args.RepoRoot,
		Arch:        args.Arch,
		PythonMinor: args.PythonMinor,
		ABI:         args.ABI,
		User:        args.Target.User,
		Base:        args.Target.Base,
		OutDir:      outDir,
		SignKeyPath: args.SignKeyPath,
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

	runList := manifest.Steps
	if args.Only != "" {
		entry, err := ResolveStepTarget(manifest, args.Only)
		if err != nil {
			return err
		}
		runList = []bundle.StepEntry{entry}
	}

	if args.ForceConfig && !containsStep(runList, "60") {
		return fmt.Errorf("--force-config only applies when step 60 (dashboard/config) runs; use --only dashboard or a full deploy")
	}

	if args.DryRun {
		plan, err := previewRun(ctx, args.Client, DefaultRemoteBundleDir, DefaultRemoteStateDir, manifest.Version)
		if err != nil {
			return fmt.Errorf("previewing the run (does this node have an existing installation to compare against?): %w", err)
		}
		printPlan(args.Stdout, plan)
		return nil
	}

	if err := deployBundle(ctx, args.Client, archivePath, DefaultRemoteBundleDir); err != nil {
		return fmt.Errorf("uploading bundle: %w", err)
	}
	if err := verifyBundleRemote(ctx, args.Client, DefaultRemoteBundleDir, embeddedPublicKeyPEM()); err != nil {
		return fmt.Errorf("verifying bundle on the node: %w", err)
	}

	if args.ForceConfig {
		confirm := args.Confirm
		if confirm == nil {
			confirm = defaultConfirm
		}
		ok, err := confirm("This overwrites the node's existing config.json and discards any changes made through the dashboard. Continue?")
		if err != nil {
			return fmt.Errorf("confirming --force-config: %w", err)
		}
		if !ok {
			return fmt.Errorf("aborted: --force-config was not confirmed")
		}
		if err := clearRemoteConfigAndStamp(ctx, args.Client); err != nil {
			return fmt.Errorf("clearing existing config for --force-config: %w", err)
		}
	}

	err = runSteps(ctx, steps.RunOptions{
		Client:          args.Client,
		RemoteBundleDir: DefaultRemoteBundleDir,
		RemoteStateDir:  DefaultRemoteStateDir,
		BundleVersion:   manifest.Version,
		TargetUser:      args.Target.User,
		TargetBase:      args.Target.Base,
		Steps:           runList,
		OnMarker:        func(m steps.Marker) { printMarker(args.Stdout, m) },
		OnLog:           func(stepID, line string) { fmt.Fprintf(args.Stdout, "[%s] %s\n", stepID, line) },
	})
	return translateStepFailure(err)
}

// translateStepFailure turns a *steps.StepFailure into operator-facing text
// via internal/faults, right where the failure originates -- cmd/installer
// (Task 14) never needs to know about fault codes at all.
func translateStepFailure(err error) error {
	var failure *steps.StepFailure
	if !errors.As(err, &failure) {
		return err
	}
	entry, ok := faults.Lookup(failure.Code)
	if !ok {
		entry = faults.Unknown(failure.Code)
	}
	return fmt.Errorf("step %s failed: %s\n  -> %s", failure.StepID, entry.Message, entry.Remediation)
}

func containsStep(entries []bundle.StepEntry, id string) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

func printPlan(w io.Writer, plan *steps.Plan) {
	fmt.Fprintf(w, "bundle version: %s\n", plan.BundleVersion)
	for _, s := range plan.Steps {
		fmt.Fprintf(w, "  step %-4s %-10s selected=%v\n", s.ID, s.State, s.Selected)
	}
	for name, v := range plan.Components {
		from := "(none installed yet)"
		if v.From != nil {
			from = *v.From
		}
		fmt.Fprintf(w, "  %s: %s -> %s\n", name, from, v.To)
	}
}

func markerKindName(k steps.Kind) string {
	switch k {
	case steps.Begin:
		return "begin"
	case steps.OK:
		return "ok"
	case steps.Skip:
		return "skip"
	case steps.Fail:
		return "fail"
	default:
		return "?"
	}
}

func printMarker(w io.Writer, m steps.Marker) {
	fmt.Fprintf(w, "##STEP %s %s %s\n", m.StepID, markerKindName(m.Kind), m.Detail)
}

func defaultConfirm(prompt string) (bool, error) {
	fmt.Fprintf(os.Stderr, "%s [yes/NO] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line)) == "yes", nil
}

// defaultClearRemoteConfigAndStamp removes the node's existing config.json
// and step 60's stamp via sudo -- both are root-owned (60-node-install.sh
// installs config.json as root:<user> 0664 in a 0755 directory the
// connecting user cannot write to directly). Passwordless sudo is already a
// hard precondition of this whole installer (the Vorprüfung step checks
// `sudo -n true` before anything runs), so this does not introduce a new
// requirement.
func defaultClearRemoteConfigAndStamp(ctx context.Context, client *transport.Client) error {
	cmd := fmt.Sprintf("sudo rm -f %s %s",
		transport.ShellQuote("/etc/energy-node/config.json"),
		transport.ShellQuote(path.Join(DefaultRemoteStateDir, "steps", "60")))
	var stderr bytes.Buffer
	if err := client.Run(ctx, cmd, io.Discard, &stderr); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return nil
}
