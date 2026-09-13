package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// StepPreview mirrors one entry of plan.sh's "steps" array (Plan A-II,
// Task 14).
type StepPreview struct {
	ID        string `json:"id"`
	Optional  bool   `json:"optional"`
	Selected  bool   `json:"selected"`
	State     string `json:"state"` // "done", "deselected" or "pending"
	ServiceID string `json:"service_id,omitempty"`
	Dir       string `json:"dir,omitempty"`
	Unit      string `json:"unit,omitempty"`
}

// ComponentVersions mirrors one entry of plan.sh's "components" map. From
// is nil when no installed-manifest.json copy exists yet -- the very first
// run against a node -- distinguishing "unknown" from an empty string the
// same way JSON null differs from "".
type ComponentVersions struct {
	From *string `json:"von"`
	To   string  `json:"nach"`
}

// Plan mirrors plan.sh's JSON report exactly (Plan A-II, Task 14): what a
// run would do, computed without changing anything on the node.
type Plan struct {
	BundleVersion string                       `json:"bundle_version"`
	Steps         []StepPreview                `json:"steps"`
	Components    map[string]ComponentVersions `json:"components"`
}

// Preview runs plan.sh on the node and parses its report. It changes
// nothing -- the same guarantee plan.sh itself gives -- so it is safe to
// call at any time, including before Deploy/VerifyRemote against a node
// that already has a bundle from a previous run. Plan C's Re-Deploy
// "Vorschau" screen (AK8) calls this before the operator confirms anything.
func Preview(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir, bundleVersion string) (*Plan, error) {
	env := map[string]string{
		"EN_STATE_DIR":      remoteStateDir,
		"EN_BUNDLE_DIR":     remoteBundleDir,
		"EN_BUNDLE_VERSION": bundleVersion,
		"EN_SELECTION":      path.Join(remoteStateDir, "selection.json"),
	}
	scriptPath := path.Join(remoteBundleDir, "bootstrap", "plan.sh")
	command := transport.BuildCommand(env, "bash "+transport.ShellQuote(scriptPath))

	var stdout, stderr bytes.Buffer
	if err := client.Run(ctx, command, &stdout, &stderr); err != nil {
		if code, ok := strings.CutPrefix(strings.TrimSpace(stdout.String()), "FEHLER "); ok {
			return nil, fmt.Errorf("plan.sh reported %s", code)
		}
		return nil, fmt.Errorf("plan.sh failed: %w (stderr: %s)", err, stderr.String())
	}

	var plan Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		return nil, fmt.Errorf("parsing plan.sh output: %w (stdout: %q)", err, stdout.String())
	}
	return &plan, nil
}
