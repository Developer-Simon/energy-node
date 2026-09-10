package nodeagent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
)

func livenessAgent(t *testing.T, now time.Time) *Agent {
	a := New(Options{NodeID: "energy_node"})
	a.SetServiceCatalog([]string{"apsystems", "shelly"}, map[string]appconfig.ServicePoll{
		"apsystems": {PollIntervalS: 60, DiagnosticPollMultiplier: 10}, // Fenster 600 + 60
		"shelly":    {PollIntervalS: 20, DiagnosticPollMultiplier: 15}, // Fenster 300 + 60
	})
	return a
}

func TestLivenessConfiguredWithoutOnline(t *testing.T) {
	now := time.Unix(10_000, 0)
	a := livenessAgent(t, now)
	got := a.ServiceLiveness(now)
	for _, s := range got {
		if s.State != "configured" {
			t.Fatalf("%s = %s, want configured", s.ID, s.State)
		}
	}
}

func TestLivenessActiveWithinWindow(t *testing.T) {
	now := time.Unix(10_000, 0)
	a := livenessAgent(t, now)
	a.ObserveLiveness("outstation/apsystems/status/online", []byte("1"))
	a.ObserveLiveness("outstation/apsystems/settings/status", []byte(`{"last_update":9700}`)) // 300s alt, Fenster 660
	got := a.ServiceLiveness(now)
	if got[0].ID != "apsystems" || got[0].State != "active" {
		t.Fatalf("apsystems = %+v, want active", got[0])
	}
}

func TestLivenessStaleLastUpdateIsConfigured(t *testing.T) {
	now := time.Unix(10_000, 0)
	a := livenessAgent(t, now)
	a.ObserveLiveness("outstation/shelly/status/online", []byte("1"))
	a.ObserveLiveness("outstation/shelly/settings/status", []byte(`{"last_update":9000}`)) // 1000s alt, Fenster 360
	got := a.ServiceLiveness(now)
	for _, s := range got {
		if s.ID == "shelly" && s.State != "configured" {
			t.Fatalf("shelly = %s, want configured (stale)", s.State)
		}
	}
}

func TestLoadServiceIDsMissingDirIsEmpty(t *testing.T) {
	ids, err := LoadServiceIDs(t.TempDir() + "/nope")
	if err != nil || ids != nil {
		t.Fatalf("got %v, %v; want nil, nil", ids, err)
	}
}

func TestLoadServiceIDsHappyPathSortsAndSkipsNonManifests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Three real manifests, deliberately out of alphabetical order.
	write("shelly.json", `{"service_id":"shelly"}`)
	write("apsystems.json", `{"service_id":"apsystems"}`)
	write("mqtt-bridge.json", `{"service_id":"mqtt_bridge","other":"ignored"}`)
	// A .json with no service_id must be skipped, not error.
	write("placeholder.json", `{"name":"placeholder"}`)
	write("empty-id.json", `{"service_id":""}`)
	// A non-.json file must be skipped.
	write("README.txt", `not a manifest`)
	// A subdirectory (even one ending in .json) must be skipped.
	if err := os.Mkdir(filepath.Join(dir, "nested.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("nested.json", "deep.json"), `{"service_id":"should_not_be_seen"}`)

	ids, err := LoadServiceIDs(dir)
	if err != nil {
		t.Fatalf("LoadServiceIDs: %v", err)
	}
	want := []string{"apsystems", "mqtt_bridge", "shelly"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("LoadServiceIDs = %v, want %v", ids, want)
	}
}
