package main

import (
	"path/filepath"
	"reflect"
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

func TestMonitoringForecastCollects(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	season := autolearn.LoadSeasonStore(filepath.Join(t.TempDir(), "season.json"), t.Logf)
	season.Put(autolearn.Week{Key: "2024-W01", Source: "days", Days: 7})
	season.Put(autolearn.Week{Key: "2022-W10", Source: "days", Days: 7})
	check := autolearn.LoadForecastCheck(filepath.Join(t.TempDir(), "forecast.json"), t.Logf)

	doc := func() (autolearn.Doc, bool) {
		return autolearn.Doc{Rules: []autolearn.Rule{{
			ID:         "a",
			Name:       "Wallbox",
			Conditions: []autolearn.Condition{{Learned: map[string]autolearn.LearnedRef{"threshold": {Key: "site/surplus_left_wh"}}}},
		}}}, true
	}
	got := monitoringForecast(check, season, doc, now)

	if got.Forecast.Needed != autolearn.ForecastWindow {
		t.Errorf("Needed = %d, want %d", got.Forecast.Needed, autolearn.ForecastWindow)
	}
	if got.Season.Year != 2026 {
		t.Errorf("Season.Year = %d, want 2026", got.Season.Year)
	}
	if len(got.Season.Weeks) != 1 || got.Season.Weeks[0].Key != "2024-W01" {
		t.Fatalf("Season.Weeks = %+v, want only 2024-W01", got.Season.Weeks)
	}
	want := []autolearn.RuleRef{{ID: "a", Name: "Wallbox"}}
	if !reflect.DeepEqual(got.Forecast.Readers, want) {
		t.Errorf("Readers = %+v, want %+v", got.Forecast.Readers, want)
	}

	none := func() (autolearn.Doc, bool) { return autolearn.Doc{}, false }
	got = monitoringForecast(check, season, none, now)
	if got.Forecast.Readers == nil || len(got.Forecast.Readers) != 0 {
		t.Errorf("without document Readers = %#v, want empty", got.Forecast.Readers)
	}
}
