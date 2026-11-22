package monitoring

import (
	"reflect"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestCollectEnergyNotices(t *testing.T) {
	in := Input{
		Snapshot: energy.Snapshot{
			UnassignedCount: 6,
			Unassigned: []energy.UnassignedEntity{
				{EntityID: "a", Name: "Limit", Value: 800.4, Unit: "W"},
				{EntityID: "b", Name: "Total", Value: 878, Unit: "W"},
			},
			BatterySoCWithoutCapacity: 1,
		},
		Config: settings.EnergyConfig{
			Interpretation: energy.Interpretation{LoadMode: energy.LoadModeMeasured},
			RoleNeeded:     []string{"z", "x"},
		},
	}
	got := Collect(in)
	ids := []string{}
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	want := []string{"energy:measured_no_meter", "energy:soc_capacity", "energy:unassigned", "energy:role_needed"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	u := got[2]
	if u.Params["count"] != 6 || u.Params["more"] != 4 {
		t.Errorf("unassigned params = %v", u.Params)
	}
	if names := u.Params["names"].([]string); names[0] != "Limit: 800 W" {
		t.Errorf("names = %v", names)
	}
	if u.Steps[0].Target != "energy.focus" || !reflect.DeepEqual(u.Steps[0].IDs, []string{"a", "b"}) {
		t.Errorf("assign step = %+v", u.Steps[0])
	}
	if rn := got[3]; !reflect.DeepEqual(rn.Params["ids"], []string{"x", "z"}) || rn.Steps[1].Target != "action.role_keep" {
		t.Errorf("role_needed = %+v", rn)
	}
}

func TestCollectMeasuredWithLoadRoleIsQuiet(t *testing.T) {
	in := Input{Config: settings.EnergyConfig{
		Interpretation: energy.Interpretation{LoadMode: energy.LoadModeMeasured},
		Assignments:    map[string]energy.Assignment{"haus": {Role: energy.RoleLoad}},
	}}
	if got := Collect(in); len(got) != 0 {
		t.Fatalf("got %v, want no notice", got)
	}
}

func TestCollectChainFindings(t *testing.T) {
	since := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	in := Input{Snapshot: energy.Snapshot{Chains: []energy.ChainState{{
		ID: "balkon", Label: "Balkon",
		Findings: []energy.ChainFinding{{Kind: "drift", ChainID: "balkon", ChainLabel: "Balkon", Since: since, From: "Plug", To: "Umrichter"}},
	}}}}
	got := Collect(in)
	if len(got) != 1 || got[0].ID != "chain:balkon:drift" || !got[0].Since.Equal(since) {
		t.Fatalf("got %+v", got)
	}
	if got[0].Steps[0].Target != "energy.chain" || got[0].Steps[0].Chain != "balkon" {
		t.Errorf("first step = %+v", got[0].Steps[0])
	}
}

func TestCollectLearnOnlyRulelessWithoutApply(t *testing.T) {
	in := Input{Findings: []autolearn.Finding{
		{ID: "shade:225:15", Kind: "shade", Group: "check", Severity: "info",
			Summary:  autolearn.Text{Code: "check.shade", Params: map[string]any{"from": "14:35"}},
			Evidence: autolearn.Evidence{Shape: "curve"}},
		{ID: "duplicate:a:b", Kind: "duplicate", RuleIDs: []string{"a", "b"}},
		{ID: "baseload:2026-11-01", Kind: "baseload", Apply: []autolearn.Fix{{}}},
	}}
	got := Collect(in)
	if len(got) != 1 || got[0].ID != "learn:shade:225:15" || got[0].Code != "check.shade" || got[0].Evidence == nil {
		t.Fatalf("got %+v", got)
	}
	codes := []string{}
	for _, s := range got[0].Steps {
		codes = append(codes, s.Code)
	}
	if !reflect.DeepEqual(codes, []string{"shade_look", "shade_accept", "learned_open"}) {
		t.Errorf("steps = %v", codes)
	}
}
