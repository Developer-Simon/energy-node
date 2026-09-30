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
	verifyBundleLocalDev      = bundle.VerifyDev
	verifyBundleRemoteDev     = bundle.VerifyRemoteDev
	provisionRemoteStateDir   = defaultProvisionRemoteStateDir
	recordInstalled           = steps.RecordInstalled
	readInstalledManifest     = bundle.ReadInstalledManifest
	deployDelta               = bundle.DeployDelta
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
	DevUnsigned bool   // skip signature verification (local and remote); there is no private key matching the embedded release public key outside CI
	Only        string // "" for a full deploy
	DryRun      bool
	ForceFull   bool // skip the incremental diff, always replace the whole bundle directory
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
	if args.DevUnsigned {
		fmt.Fprintln(args.Stdout, "WARNING: --dev-unsigned: skipping bundle signature verification (local and remote). Never use this against a node you do not control.")
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
		DevVersion:  true,
	})
	if err != nil {
		return fmt.Errorf("building bundle: %w", err)
	}

	extractDir := filepath.Join(outDir, "extracted")
	if err := extractArchive(archivePath, extractDir); err != nil {
		return fmt.Errorf("extracting built bundle: %w", err)
	}

	manifest, err := verifyLocal(extractDir, args.DevUnsigned)
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

	if err := provisionRemoteStateDir(ctx, args.Client); err != nil {
		return fmt.Errorf("provisioning %s on the node: %w", DefaultRemoteStateDir, err)
	}

	usedDelta, err := stageForDeploy(ctx, args, archivePath, extractDir, manifest)
	if err != nil {
		return fmt.Errorf("uploading bundle: %w", err)
	}
	if err := verifyRemote(ctx, args.Client, args.DevUnsigned); err != nil {
		if !usedDelta {
			return fmt.Errorf("verifying bundle on the node: %w", err)
		}
		fmt.Fprintf(args.Stdout, "warning: the incremental transfer could not be verified on the node: %v\n", err)
		confirm := args.Confirm
		if confirm == nil {
			confirm = defaultConfirm
		}
		ok, confirmErr := confirm("Recommendation: retransfer the full bundle. Continue?")
		if confirmErr != nil {
			return fmt.Errorf("confirming the full retransfer: %w", confirmErr)
		}
		if !ok {
			return fmt.Errorf("verifying bundle on the node: %w", err)
		}
		if err := deployBundle(ctx, args.Client, archivePath, DefaultRemoteBundleDir); err != nil {
			return fmt.Errorf("retransferring bundle: %w", err)
		}
		if err := verifyRemote(ctx, args.Client, args.DevUnsigned); err != nil {
			return fmt.Errorf("verifying bundle on the node after a full retransfer: %w", err)
		}
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

	// --only is the developer's "ship this one part now": the step runs even
	// if its stamp says this bundle version already did it (a dirty tree keeps
	// the same dev version across edits), and its unit restarts afterwards
	// even when the restart rule sees no version change -- step 60 on its own
	// never restarts a running dashboard at all.
	if args.Only != "" {
		if err := clearRemoteStepStamp(ctx, args.Client, DefaultRemoteStateDir, runList[0].ID); err != nil {
			return fmt.Errorf("clearing step %s's stamp: %w", runList[0].ID, err)
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
		// Code from a working tree changes without its service's VERSION
		// changing, so the restart rule would leave the old code running.
		// --only restarts its one unit itself below.
		RestartAll: args.Only == "",
		OnMarker:   func(m steps.Marker) { printMarker(args.Stdout, m) },
		OnLog:      func(stepID, line string) { fmt.Fprintf(args.Stdout, "[%s] %s\n", stepID, line) },
	})
	if err != nil {
		return translateStepFailure(err)
	}
	if args.Only != "" {
		unit, ok := unitForStep(runList[0])
		if !ok {
			fmt.Fprintf(args.Stdout, "--only %s has no unit of its own; run `installer restart` if the services should pick the change up.\n", args.Only)
			return nil
		}
		return restartUnits(ctx, args.Client, "restart", []string{unit}, args.Stdout)
	}
	// Only a full run makes the node "at this bundle version"; --only leaves
	// the other steps untouched. See steps.RecordInstalled.
	if err := recordInstalled(ctx, args.Client, DefaultRemoteBundleDir, DefaultRemoteStateDir); err != nil {
		fmt.Fprintf(args.Stdout, "warning: could not record installed-manifest.json: %v\n", err)
	}
	return nil
}

// stageForDeploy stages the built bundle on the node: an incremental delta
// against installed-manifest.json by default, or a full replace when
// args.ForceFull was asked for or there is no installed manifest to diff
// against at all (a first-ever deploy). It reports whether it took the
// delta path -- RunDeploy needs that to decide how to react if
// verifyRemote then fails: a full transfer's own verify failure is
// reported as-is, but a delta transfer's asks before retrying as a full
// one (Global Constraints: never silently switch).
func stageForDeploy(ctx context.Context, args DeployArgs, archivePath, bundleDir string, manifest *bundle.Manifest) (usedDelta bool, err error) {
	if !args.ForceFull {
		if installed := readInstalledManifest(ctx, args.Client, DefaultRemoteStateDir); installed != nil {
			changed, removed := bundle.DiffManifest(installed, manifest)
			if err := deployDelta(ctx, args.Client, bundleDir, changed, removed, DefaultRemoteBundleDir, nil); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := deployBundle(ctx, args.Client, archivePath, DefaultRemoteBundleDir); err != nil {
		return false, err
	}
	return false, nil
}

// verifyLocal picks the signed or unsigned local verifier: the default path
// unchanged from before --dev-unsigned existed, or (only when the caller
// asked for it) the hash-only check a bundle built without --sign-key can
// actually pass, since no developer checkout ever holds the private key
// matching the embedded release public key.
func verifyLocal(extractDir string, devUnsigned bool) (*bundle.Manifest, error) {
	if devUnsigned {
		return verifyBundleLocalDev(extractDir)
	}
	pubKey, err := embeddedPublicKey()
	if err != nil {
		return nil, fmt.Errorf("loading embedded signing key: %w", err)
	}
	return verifyBundleLocal(extractDir, pubKey)
}

// verifyRemote mirrors verifyLocal's choice for the node-side check.
func verifyRemote(ctx context.Context, client *transport.Client, devUnsigned bool) error {
	if devUnsigned {
		return verifyBundleRemoteDev(ctx, client, DefaultRemoteBundleDir)
	}
	return verifyBundleRemote(ctx, client, DefaultRemoteBundleDir, embeddedPublicKeyPEM())
}

// defaultProvisionRemoteStateDir grants the connecting user ownership of
// DefaultRemoteStateDir before anything writes under it. /var/lib is
// root-owned 0755, so mkdir -p by the unprivileged connecting user (which is
// what Deploy and step.sh's own stamp writes both do) fails with EACCES on
// a node that has never had this installer run before. install -d is
// idempotent, so running this once per connection is safe even when the
// directory already exists and is already owned correctly. Passwordless
// sudo is already a hard precondition of this whole installer (the
// Vorprüfung step checks `sudo -n true` before anything runs), so this does
// not introduce a new requirement.
func defaultProvisionRemoteStateDir(ctx context.Context, client *transport.Client) error {
	cmd := fmt.Sprintf(`sudo install -d -o "$(id -un)" -g "$(id -un)" -m 0755 %s`, transport.ShellQuote(DefaultRemoteStateDir))
	var stderr bytes.Buffer
	if err := client.Run(ctx, cmd, io.Discard, &stderr); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, stderr.String())
	}
	return nil
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
