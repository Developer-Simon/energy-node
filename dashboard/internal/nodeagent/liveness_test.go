package nodeagent

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
)

func livenessAgent(t *testing.T, now time.Time) *Agent {
	a := New(Options{NodeID: "energy_node"})
	a.now = func() time.Time { return now }
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
