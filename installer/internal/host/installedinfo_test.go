package host

import (
	"encoding/json"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/diag"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
)

func TestInstalledInfoKeepsAnUnreadableDeviceFileApart(t *testing.T) {
	report := &diag.Report{
		Versions: diag.VersionsReport{Services: map[string]string{"shelly-rpc.service": "v0.4.2"}},
		Devices: map[string][]diag.Device{
			"shelly-rpc.service":  {{ID: "plug", Name: "Plug"}},
			"trucki-http.service": {},
			"tuya.service":        nil,
		},
	}
	versions, devices := installedInfo(report)
	if versions == nil || versions.Services["shelly-rpc.service"] != "v0.4.2" {
		t.Fatalf("unexpected versions: %+v", versions)
	}
	raw, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"shelly-rpc.service":[{"id":"plug","name":"Plug"}],"trucki-http.service":[],"tuya.service":null}`
	if string(raw) != want {
		t.Errorf("devices JSON = %s, want %s", raw, want)
	}
}

func TestInstalledInfoWithoutAnInstalledManifest(t *testing.T) {
	versions, devices := installedInfo(&diag.Report{})
	if versions != nil || devices != nil {
		t.Errorf("expected nothing without an installed manifest, got %+v %+v", versions, devices)
	}
}

func TestSystemUpdatesViewCopiesTheReportAndKeepsUnknownApart(t *testing.T) {
	if systemUpdatesView(nil) != nil {
		t.Fatalf("an unknown state must stay nil")
	}
	view := systemUpdatesView(&steps.SystemUpdates{
		Count: 1, CheckedAt: "2026-10-04T06:12:00+00:00",
		Packages: []steps.SystemPackage{{Name: "libssl3", From: "3.0.11", To: "3.0.13"}},
	})
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"count":1,"checked_at":"2026-10-04T06:12:00+00:00","packages":[{"name":"libssl3","from":"3.0.11","to":"3.0.13"}]}`
	if string(raw) != want {
		t.Errorf("view JSON = %s, want %s", raw, want)
	}
}
