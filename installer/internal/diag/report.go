// Package diag runs the node's diagnose.sh (Plan A-II, Task 15) over an
// existing SSH connection and parses its report.
package diag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// ConfigReport mirrors diagnose.sh's "config" object.
type ConfigReport struct {
	ConfigJSON bool     `json:"config.json"`
	Manifests  []string `json:"manifests"`
}

// TailscaleReport mirrors diagnose.sh's "tailscale" object. Angemeldet keeps
// diagnose.sh's own JSON key ("logged in") -- a stable wire contract with
// Plan A-II, not operator-facing text.
type TailscaleReport struct {
	Angemeldet bool `json:"angemeldet"`
}

// Report mirrors diagnose.sh's JSON report exactly (Plan A-II, Task 15).
type Report struct {
	BundleVersion string            `json:"bundle_version"`
	Steps         map[string]string `json:"steps"`
	Units         map[string]string `json:"units"`
	Ports         map[string]bool   `json:"ports"`
	Config        ConfigReport      `json:"config"`
	Tailscale     TailscaleReport   `json:"tailscale"`
}

// Run executes diagnose.sh on the node and parses its output. diagnose.sh
// always exits 0 and never emits a "##STEP" marker (Plan A-II, Task 15) --
// a non-nil error here means the command itself could not be run (a missing
// script, a dead connection), not a bad node state, which the Report itself
// describes instead.
func Run(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir, bundleVersion string) (*Report, error) {
	env := map[string]string{
		"EN_STATE_DIR":      remoteStateDir,
		"EN_BUNDLE_DIR":     remoteBundleDir,
		"EN_BUNDLE_VERSION": bundleVersion,
	}
	scriptPath := path.Join(remoteBundleDir, "bootstrap", "diagnose.sh")
	command := transport.BuildCommand(env, "bash "+transport.ShellQuote(scriptPath))

	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("running diagnose.sh: %w (stderr: %s)", err, stderr.String())
	}

	var report Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return nil, fmt.Errorf("parsing diagnose.sh output: %w (stdout: %q)", err, stdout.String())
	}
	return &report, nil
}
