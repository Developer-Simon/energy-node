package steps_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

const fakePlanScript = `#!/bin/sh
cat <<'JSON'
{
  "bundle_version": "v0.2.0",
  "steps": [
    {"id": "10", "optional": false, "selected": true, "state": "done"},
    {"id": "40", "optional": true, "selected": false, "state": "deselected"},
    {"id": "81", "optional": true, "selected": true, "state": "pending", "service_id": "apsystems", "dir": "apsystems_ez1", "unit": "apsystems-ez1.service", "von": "v0.4.0", "nach": "v0.4.1", "restart": "version"}
  ],
  "components": {
    "dashboard": {"von": "v0.6.0", "nach": "v0.6.1"},
    "services":  {"von": null,     "nach": "v1.4.0"}
  },
  "system_updates": {"count": 1, "checked_at": "2026-10-04T06:12:00+00:00",
    "packages": [{"name": "libssl3", "from": "3.0.11", "to": "3.0.13"}]}
}
JSON
`

func TestPreviewParsesThePlanReport(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": fakePlanScript})

	plan, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if plan.BundleVersion != "v0.2.0" {
		t.Fatalf("unexpected bundle version: %+v", plan)
	}
	if len(plan.Steps) != 3 || plan.Steps[0].State != "done" || plan.Steps[1].State != "deselected" {
		t.Fatalf("unexpected steps: %+v", plan.Steps)
	}
	if plan.Steps[2].ServiceID != "apsystems" || plan.Steps[2].Unit != "apsystems-ez1.service" {
		t.Fatalf("unexpected service step: %+v", plan.Steps[2])
	}
	if plan.Steps[2].From == nil || *plan.Steps[2].From != "v0.4.0" || plan.Steps[2].To != "v0.4.1" || plan.Steps[2].Restart != "version" {
		t.Fatalf("unexpected restart info: %+v", plan.Steps[2])
	}

	dashboard, ok := plan.Components["dashboard"]
	if !ok || dashboard.From == nil || *dashboard.From != "v0.6.0" || dashboard.To != "v0.6.1" {
		t.Fatalf("unexpected dashboard component: %+v", dashboard)
	}
	services, ok := plan.Components["services"]
	if !ok || services.From != nil || services.To != "v1.4.0" {
		t.Fatalf(`unexpected services component ("von" must be nil, not ""): %+v`, services)
	}
	updates := plan.SystemUpdates
	if updates == nil || updates.Count != 1 || updates.CheckedAt != "2026-10-04T06:12:00+00:00" ||
		len(updates.Packages) != 1 || updates.Packages[0] != (steps.SystemPackage{Name: "libssl3", From: "3.0.11", To: "3.0.13"}) {
		t.Fatalf("unexpected system updates: %+v", updates)
	}
}

func TestPreviewReportsAMissingManifest(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const failScript = "#!/bin/sh\necho diagnostic noise on its own line\nprintf 'FEHLER BUNDLE_MANIFEST_MISSING\\n'\nexit 1\n"
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": failScript})

	_, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0", nil)
	var bundleErr *bundle.Error
	if !errors.As(err, &bundleErr) {
		t.Fatalf("expected a *bundle.Error, got %v", err)
	}
	if bundleErr.Code != bundle.FaultManifestMissing {
		t.Fatalf("expected FaultManifestMissing, got %s", bundleErr.Code)
	}
}

// selectionEchoScript reports step 83 as pending when the selection file it
// was handed switches 83 on, deselected otherwise. json.Marshal writes the
// map without spaces, so the grep pattern matches exactly.
const selectionEchoScript = `#!/bin/sh
if grep -q '"83":true' "$EN_SELECTION" 2>/dev/null; then
  state=pending; sel=true
else
  state=deselected; sel=false
fi
cat <<JSON
{"bundle_version": "v0.2.0", "steps": [{"id": "83", "optional": true, "selected": $sel, "state": "$state"}], "components": {}}
JSON
`

func TestPreviewRunsAgainstAGivenSelectionAndLeavesTheNodeFileAlone(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": selectionEchoScript})

	nodeSelection := []byte(`{"steps":{"83":false}}`)
	if err := client.UploadBytes(nodeSelection, stateDir+"/selection.json", 0o644); err != nil {
		t.Fatalf("upload node selection: %v", err)
	}

	plan, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0", nil)
	if err != nil {
		t.Fatalf("Preview without selection: %v", err)
	}
	if plan.Steps[0].State != "deselected" {
		t.Fatalf("without a selection Preview must read the node's selection.json, got %+v", plan.Steps[0])
	}

	pending := &selection.Selection{Steps: map[string]bool{"83": true}}
	plan, err = steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0", pending)
	if err != nil {
		t.Fatalf("Preview with selection: %v", err)
	}
	if plan.Steps[0].State != "pending" || !plan.Steps[0].Selected {
		t.Fatalf("with a selection Preview must plan against it, got %+v", plan.Steps[0])
	}

	var out bytes.Buffer
	if err := client.Run(context.Background(), "cat "+stateDir+"/selection.json", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("read node selection: %v", err)
	}
	if out.String() != string(nodeSelection) {
		t.Errorf("node selection.json = %q, Preview must never touch it", out.String())
	}
	if err := client.Run(context.Background(), "test ! -e "+stateDir+"/selection.preview.json", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Errorf("staged selection.preview.json is still on the node")
	}
}

func TestPreviewRemovesTheStagedSelectionEvenWhenPlanFails(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const failScript = "#!/bin/sh\nprintf 'FEHLER BUNDLE_MANIFEST_MISSING\\n'\nexit 1\n"
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": failScript})

	pending := &selection.Selection{Steps: map[string]bool{"83": true}}
	if _, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0", pending); err == nil {
		t.Fatalf("Preview must report the plan.sh fault")
	}
	if err := client.Run(context.Background(), "test ! -e "+stateDir+"/selection.preview.json", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Errorf("staged selection.preview.json survived a failing plan.sh")
	}
}
