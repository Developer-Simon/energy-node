package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// StepPreview mirrors one entry of plan.sh's "steps" array (Plan A-II,
// Task 14).
type StepPreview struct {
	ID        string  `json:"id"`
	Optional  bool    `json:"optional"`
	Selected  bool    `json:"selected"`
	State     string  `json:"state"` // "done", "deselected" or "pending"
	ServiceID string  `json:"service_id,omitempty"`
	Dir       string  `json:"dir,omitempty"`
	Unit      string  `json:"unit,omitempty"`
	From      *string `json:"von,omitempty"`  // installed version of a service step; nil when unknown
	To        string  `json:"nach,omitempty"` // version in the bundle
	Restart   string  `json:"restart,omitempty"`
}

// ComponentVersions mirrors one entry of plan.sh's "components" map. From
// is nil when no installed-manifest.json copy exists yet -- the very first
// run against a node -- distinguishing "unknown" from an empty string the
// same way JSON null differs from "".
type ComponentVersions struct {
	From *string `json:"von"`
	To   string  `json:"nach"`
}

// SystemUpdates mirrors scripts/bootstrap/lib/apt_pending.py: the packages
// apt-get upgrade would update now, simulated on the node's existing package
// lists (no apt-get update). CheckedAt is the lists' age, "" when unknown.
// plan.sh and diagnose.sh report null when apt-get is missing or fails,
// which callers keep as nil.
type SystemUpdates struct {
	Count     int             `json:"count"`
	CheckedAt string          `json:"checked_at"`
	Packages  []SystemPackage `json:"packages"`
}

// SystemPackage is one pending update. From is "" for a package apt would
// install new.
type SystemPackage struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Plan mirrors plan.sh's JSON report exactly (Plan A-II, Task 14): what a
// run would do, computed without changing anything on the node.
type Plan struct {
	BundleVersion string                       `json:"bundle_version"`
	Steps         []StepPreview                `json:"steps"`
	Components    map[string]ComponentVersions `json:"components"`
	SystemUpdates *SystemUpdates               `json:"system_updates"`
}

// previewSelectionName is the file Preview stages an unsaved selection in.
// It sits next to selection.json, never replaces it: the run stays the only
// writer of selection.json, and a root-owned one from a manual install is
// left alone.
const previewSelectionName = "selection.preview.json"

// Preview runs plan.sh on the node and parses its report. It changes
// nothing -- the same guarantee plan.sh itself gives -- so it is safe to
// call at any time, including before Deploy/VerifyRemote against a node
// that already has a bundle from a previous run. Plan C's Re-Deploy
// "Vorschau" screen (AK8) calls this before the operator confirms anything.
// A non-nil sel is planned against instead of the node's selection.json, so
// the preview matches the selection the next run will upload.
func Preview(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir, bundleVersion string, sel *selection.Selection) (*Plan, error) {
	selectionPath := path.Join(remoteStateDir, "selection.json")
	if sel != nil {
		raw, err := json.Marshal(sel)
		if err != nil {
			return nil, fmt.Errorf("encoding selection: %w", err)
		}
		selectionPath = path.Join(remoteStateDir, previewSelectionName)
		if err := client.UploadBytes(raw, selectionPath, 0o644); err != nil {
			return nil, fmt.Errorf("uploading %s: %w", previewSelectionName, err)
		}
		defer func() { _ = client.RemoveRemote(selectionPath) }()
	}
	env := map[string]string{
		"EN_STATE_DIR":      remoteStateDir,
		"EN_BUNDLE_DIR":     remoteBundleDir,
		"EN_BUNDLE_VERSION": bundleVersion,
		"EN_SELECTION":      selectionPath,
	}
	scriptPath := path.Join(remoteBundleDir, "bootstrap", "plan.sh")
	command := transport.BuildCommand(env, "bash "+transport.ShellQuote(scriptPath))

	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		if code, ok := strings.CutPrefix(bundle.LastLine(stdout.String()), "FEHLER "); ok {
			return nil, &bundle.Error{Code: bundle.FaultCode(code), Message: "plan.sh reported a fault"}
		}
		return nil, fmt.Errorf("plan.sh failed: %w (stderr: %s)", err, stderr.String())
	}

	var plan Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		return nil, fmt.Errorf("parsing plan.sh output: %w (stdout: %q)", err, stdout.String())
	}
	return &plan, nil
}
