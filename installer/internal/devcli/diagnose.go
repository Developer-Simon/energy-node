package devcli

import (
	"context"
	"fmt"
	"io"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/diag"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// DiagnoseArgs configures one call to RunDiagnose. RemoteBundleDir and
// RemoteStateDir are fields rather than the DefaultRemoteBundleDir/
// DefaultRemoteStateDir constants directly so tests can point them at a
// throwaway path on the fixture sshd instead of needing write access under
// /var/lib -- see this task's rationale.
type DiagnoseArgs struct {
	Client          *transport.Client
	RemoteBundleDir string
	RemoteStateDir  string
	Stdout          io.Writer
}

// RunDiagnose reads the installed bundle's manifest.json, runs diagnose.sh
// (via internal/diag) and prints a checklist with a retry hint for every
// failing check.
func RunDiagnose(ctx context.Context, args DiagnoseArgs) error {
	manifest, err := readInstalledManifest(ctx, args.Client, args.RemoteBundleDir)
	if err != nil {
		return err
	}

	report, err := diag.Run(ctx, args.Client, args.RemoteBundleDir, args.RemoteStateDir, manifest.Version)
	if err != nil {
		return fmt.Errorf("running diagnose.sh: %w", err)
	}

	fmt.Fprintf(args.Stdout, "Installed version: %s\n\n", manifest.Version)

	checks := report.Checklist(manifest.Steps)
	failed, warned := 0, 0
	for _, c := range checks {
		status := "OK"
		switch {
		case c.OK:
		case c.Severity == "warn":
			// Ein Hinweis blockiert nichts und hat keinen Reparaturschritt.
			status = "WARN"
			warned++
		default:
			status = "FAIL"
			failed++
		}
		fmt.Fprintf(args.Stdout, "[%s] %-24s %s\n", status, c.Name, c.Detail)
		if status == "FAIL" {
			fmt.Fprintf(args.Stdout, "       -> retry: %s\n", retryHint(manifest.Steps, c.RetryStepID))
		}
	}
	fmt.Fprintf(args.Stdout, "\n%d/%d checks OK\n", len(checks)-failed-warned, len(checks))
	if warned > 0 {
		fmt.Fprintf(args.Stdout, "%d hint(s), nothing blocking\n", warned)
	}
	return nil
}

// retryHint turns a failing check's step id into a copy-pasteable command.
// Core infrastructure steps (10-40, 70) have no --only target of their own
// (E12 only names "dashboard", "wheels" and device services) -- a full
// `installer deploy` is the correct fix there, since core steps that already
// succeeded report "skip" and cost nothing to repeat.
func retryHint(steps []bundle.StepEntry, stepID string) string {
	if stepID == "" {
		return "(no specific step maps to this check)"
	}
	switch stepID {
	case "60":
		return "installer deploy --only dashboard"
	case "50":
		return "installer deploy --only wheels"
	}
	for _, s := range steps {
		if s.ID == stepID && s.ServiceID != "" {
			return "installer deploy --only " + s.ServiceID
		}
	}
	return fmt.Sprintf("installer deploy (full run; step %s has no --only target)", stepID)
}
