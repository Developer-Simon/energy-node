package nodeagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServiceManifestsReadsFloor(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"shelly.json":     `{"service_id":"shelly","unit":"shelly-rpc.service","poll_floor_s":12}`,
		"automation.json": `{"service_id":"automation","unit":"automation.service"}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadServiceManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d manifests, want 2: %+v", len(got), got)
	}
	if got[0] != (ServiceManifest{ServiceID: "automation", Unit: "automation.service", PollFloorS: 0}) {
		t.Fatalf("automation = %+v", got[0])
	}
	if got[1] != (ServiceManifest{ServiceID: "shelly", Unit: "shelly-rpc.service", PollFloorS: 12}) {
		t.Fatalf("shelly = %+v, want floor 12", got[1])
	}
}
