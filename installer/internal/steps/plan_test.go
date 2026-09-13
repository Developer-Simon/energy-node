package steps_test

import (
	"context"
	"testing"

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
    {"id": "81", "optional": true, "selected": true, "state": "pending", "service_id": "apsystems", "dir": "apsystems_ez1", "unit": "apsystems-ez1.service"}
  ],
  "components": {
    "dashboard": {"von": "v0.6.0", "nach": "v0.6.1"},
    "services":  {"von": null,     "nach": "v1.4.0"}
  }
}
JSON
`

func TestPreviewParsesThePlanReport(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": fakePlanScript})

	plan, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0")
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

	dashboard, ok := plan.Components["dashboard"]
	if !ok || dashboard.From == nil || *dashboard.From != "v0.6.0" || dashboard.To != "v0.6.1" {
		t.Fatalf("unexpected dashboard component: %+v", dashboard)
	}
	services, ok := plan.Components["services"]
	if !ok || services.From != nil || services.To != "v1.4.0" {
		t.Fatalf(`unexpected services component ("von" must be nil, not ""): %+v`, services)
	}
}

func TestPreviewReportsAMissingManifest(t *testing.T) {
	requireSFTPServerForSteps(t)
	sshd := transporttest.Start(t)
	client := dialForStepsTest(t, sshd)

	const failScript = "#!/bin/sh\nprintf 'FEHLER BUNDLE_MANIFEST_MISSING\\n'\nexit 1\n"
	bundleDir, stateDir := deployBootstrapScripts(t, client, map[string]string{"plan.sh": failScript})

	if _, err := steps.Preview(context.Background(), client, bundleDir, stateDir, "v0.2.0"); err == nil {
		t.Fatalf("expected an error")
	}
}
