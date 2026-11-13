package host

import (
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/diag"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// TestDiagnoseViewMatchesDiagnose prueft, dass diagnoseView dieselben Felder
// setzt, die Diagnose vorher gesetzt hat. Diagnose und der Strom teilen sich
// diese Abbildung.
func TestDiagnoseViewMatchesDiagnose(t *testing.T) {
	report := &diag.Report{
		BundleVersion:  "v1.4.2",
		Units:          map[string]string{"energy-node.service": "active", "energy-dev.service": "unbekannt"},
		Ports:          map[string]bool{"8080": true, "1883": false},
		RebootRequired: true,
		SystemUpdates:  &steps.SystemUpdates{Count: 1, CheckedAt: "2026-11-13", Packages: []steps.SystemPackage{{Name: "curl", From: "1", To: "2"}}},
		Versions: diag.VersionsReport{
			Components: map[string]string{"dashboard": "0.11.7"},
			Services:   map[string]string{"energy-node.service": "0.11.7"},
		},
		Devices: map[string][]diag.Device{
			"energy-dev.service": {{ID: "dev-1", Name: "Wallbox"}},
			"energy-bad.service": nil,
		},
	}
	stepList := []bundle.StepEntry{{ID: "40", Unit: "energy-dev.service"}}

	view := diagnoseView(report, stepList)

	if view.BundleVersion != "v1.4.2" {
		t.Errorf("BundleVersion = %q", view.BundleVersion)
	}
	if !reflect.DeepEqual(view.Units, report.Units) {
		t.Errorf("Units = %v, want %v", view.Units, report.Units)
	}
	if !reflect.DeepEqual(view.Ports, report.Ports) {
		t.Errorf("Ports = %v, want %v", view.Ports, report.Ports)
	}
	if !view.RebootRequired {
		t.Errorf("RebootRequired = false, want true")
	}
	if view.SystemUpdates == nil || view.SystemUpdates.Count != 1 || len(view.SystemUpdates.Packages) != 1 || view.SystemUpdates.Packages[0].To != "2" {
		t.Errorf("SystemUpdates = %+v", view.SystemUpdates)
	}
	wantVersions := &hostapi.DiagnoseVersions{Components: report.Versions.Components, Services: report.Versions.Services}
	if !reflect.DeepEqual(view.Versions, wantVersions) {
		t.Errorf("Versions = %+v, want %+v", view.Versions, wantVersions)
	}
	wantDevices := map[string][]hostapi.DeviceEntry{
		"energy-dev.service": {{ID: "dev-1", Name: "Wallbox"}},
		"energy-bad.service": nil,
	}
	if !reflect.DeepEqual(view.Devices, wantDevices) {
		t.Errorf("Devices = %+v, want %+v", view.Devices, wantDevices)
	}

	checks := report.Checklist(stepList)
	if len(view.Checks) != len(checks) || len(checks) == 0 {
		t.Fatalf("got %d checks, want %d (and at least one)", len(view.Checks), len(checks))
	}
	for i, check := range checks {
		got := view.Checks[i]
		if got.Name != check.Name || got.OK != check.OK || got.Detail != check.Detail || got.RetryStepID != check.RetryStepID ||
			got.Group != check.Group || got.Subject != check.Subject || got.Severity != check.Severity || got.Needs != check.Needs {
			t.Errorf("check %d = %+v, want %+v", i, got, check)
		}
	}
}
