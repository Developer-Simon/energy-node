package main

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/monitoring"
)

func TestMonitoringDeviceIcon(t *testing.T) {
	cases := []struct {
		role       energy.Role
		switchable bool
		want       string
	}{
		{energy.RoleHeatPump, false, "ico-heatpump"},
		{energy.RoleWallbox, false, "ico-wallbox"},
		{energy.RoleBattery, false, "ico-battery"},
		{energy.RolePV, false, "ico-sun"},
		{energy.RoleSwitch, false, "ico-switch"},
		{energy.RoleLoad, false, "ico-load"},
		{energy.CustomRole("akku"), false, "ico-load"},
		{"", true, "ico-switch"},
		{"", false, "ico-device"},
	}
	for _, c := range cases {
		if got := monitoringDeviceIcon(c.role, c.switchable); got != c.want {
			t.Errorf("monitoringDeviceIcon(%q, %v) = %q, want %q", c.role, c.switchable, got, c.want)
		}
	}
}

func TestMonitoringRunnerKeepsFirstSeen(t *testing.T) {
	store := monitoring.OpenStore("", t.Logf)
	collect := func() monitoring.Input {
		return monitoring.Input{Findings: []autolearn.Finding{{ID: "f1", Summary: autolearn.Text{Code: "x"}}}}
	}
	m := newMonitoringRunner(store, collect)
	t0 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	first := findMonitoringNotice(m.Notices(t0), "learn:f1")
	second := findMonitoringNotice(m.Notices(t0.Add(time.Minute)), "learn:f1")
	if first == nil || second == nil {
		t.Fatalf("notice learn:f1 missing: first=%v second=%v", first, second)
	}
	if first.FirstSeen != t0.Unix() || second.FirstSeen != first.FirstSeen {
		t.Fatalf("FirstSeen first=%d second=%d, want %d", first.FirstSeen, second.FirstSeen, t0.Unix())
	}
}

func findMonitoringNotice(notices []monitoring.Notice, id string) *monitoring.Notice {
	for i := range notices {
		if notices[i].ID == id {
			return &notices[i]
		}
	}
	return nil
}
