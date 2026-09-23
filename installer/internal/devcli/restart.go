package devcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// dashboardUnit is the unit step 60 installs. It is not in the manifest:
// step 60 is a core step and carries no Unit field the way service steps do.
const dashboardUnit = "energy-node-dashboard.service"

// restartUnits is a package-level seam like the ones in deploy.go, so
// deploy_test.go can prove RunDeploy's --only restart without an sshd.
var restartUnits = defaultRestartUnits

// RestartArgs configures one call to RunRestart. RemoteBundleDir is a field
// for the same reason as in DiagnoseArgs: tests point it at a throwaway path.
type RestartArgs struct {
	Client          *transport.Client
	RemoteBundleDir string
	Only            string // "" restarts the dashboard and every service unit
	Stdout          io.Writer
}

// RunRestart restarts units on the node without deploying anything -- the
// replacement for the old deploy script's --restart-only. The installed
// bundle's manifest names the units, so a node restarts exactly what it has.
//
// With --only the one unit is restarted unconditionally. Without it every
// unit gets try-restart: a service the operator deselected stays stopped.
func RunRestart(ctx context.Context, args RestartArgs) error {
	manifest, err := readInstalledManifest(ctx, args.Client, args.RemoteBundleDir)
	if err != nil {
		return err
	}

	if args.Only == "" {
		units := []string{dashboardUnit}
		for _, entry := range manifest.Steps {
			if entry.Unit != "" {
				units = append(units, entry.Unit)
			}
		}
		return restartUnits(ctx, args.Client, "try-restart", units, args.Stdout)
	}

	entry, err := ResolveStepTarget(manifest, args.Only)
	if err != nil {
		return err
	}
	unit, ok := unitForStep(entry)
	if !ok {
		return fmt.Errorf("--only %s has no unit of its own to restart; run restart without --only", args.Only)
	}
	return restartUnits(ctx, args.Client, "restart", []string{unit}, args.Stdout)
}

// unitForStep names the unit that runs the code a step installs: the
// dashboard for step 60, the service's own unit for a service step. Other
// steps (wheels, packages, ...) have none.
func unitForStep(entry bundle.StepEntry) (string, bool) {
	if entry.ID == fixedOnlyTargets["dashboard"] {
		return dashboardUnit, true
	}
	return entry.Unit, entry.Unit != ""
}

// readInstalledManifest reads manifest.json of the bundle currently deployed
// on the node.
func readInstalledManifest(ctx context.Context, client *transport.Client, remoteBundleDir string) (*bundle.Manifest, error) {
	manifestPath := path.Join(remoteBundleDir, "manifest.json")
	var stdout, stderr bytes.Buffer
	cmd := "cat " + transport.ShellQuote(manifestPath)
	if err := client.Run(ctx, cmd, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("reading %s (is anything installed on this node yet?): %w (stderr: %s)", manifestPath, err, stderr.String())
	}

	var manifest bundle.Manifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", manifestPath, err)
	}
	return &manifest, nil
}

func defaultRestartUnits(ctx context.Context, client *transport.Client, verb string, units []string, stdout io.Writer) error {
	quoted := make([]string, len(units))
	for i, unit := range units {
		quoted[i] = transport.ShellQuote(unit)
	}
	cmd := "sudo systemctl " + verb + " " + strings.Join(quoted, " ")
	var stderr bytes.Buffer
	if err := client.Run(ctx, cmd, io.Discard, &stderr); err != nil {
		return fmt.Errorf("systemctl %s %s: %w (stderr: %s)", verb, strings.Join(units, " "), err, stderr.String())
	}
	fmt.Fprintf(stdout, "systemctl %s: %s\n", verb, strings.Join(units, ", "))
	return nil
}
