package host

import (
	"encoding/json"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/diag"
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
