package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestEngineRunnerDropsTickWhenChannelIsFull(t *testing.T) {
	r := newEngineRunner(nil, func() map[string]bool { return nil }, func() {})
	done := make(chan struct{})
	go func() {
		defer close(done)
		start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 3; i++ {
			r.ObserveMinute(learnrec.Frame{Minute: start.Add(time.Duration(i) * time.Minute)})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveMinute blocked on a full channel")
	}
	if got := len(r.ticks); got != 1 {
		t.Fatalf("queued ticks = %d, want 1", got)
	}
	first := <-r.ticks
	if !first.Equal(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("kept tick = %s, want the first minute", first)
	}
}

func TestEngineRunnerStopsWithContext(t *testing.T) {
	r := newEngineRunner(nil, func() map[string]bool { return nil }, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the context ended")
	}
}

func TestLearnDevicesCarriesEnergyRole(t *testing.T) {
	heatPump := registry.EntityView{UniqueID: "sensor.waermepumpe_power", Component: "sensor",
		ObjectID: "waermepumpe_power", Name: "Wärmepumpe Leistung", UnitOfMeasurement: "W"}
	plug := registry.EntityView{UniqueID: "switch.plug", Component: "switch", ObjectID: "plug", Name: "Steckdose"}
	wallboxSwitch := registry.EntityView{UniqueID: "switch.wallbox", Component: "switch", ObjectID: "wallbox", Name: "Ladebox"}
	devices := []registry.DeviceView{
		{ID: "pump", Name: "Pumpe", Entities: []registry.EntityView{heatPump, plug}},
		{ID: "plug", Name: "Steckdose", Entities: []registry.EntityView{plug}},
		{ID: "box", Name: "Ladebox", Entities: []registry.EntityView{wallboxSwitch}},
	}
	resolver := energy.NewResolver(map[string]energy.Assignment{
		"switch.wallbox": {Role: energy.RoleWallbox},
	})

	got := learnDevices(devices, resolver)
	if role := got["pump"].Role; role != energy.RoleHeatPump {
		t.Errorf("pump role = %q, want heat_pump", role)
	}
	if role := got["plug"].Role; role != "" {
		t.Errorf("plug role = %q, want empty", role)
	}
	if role := got["box"].Role; role != energy.RoleWallbox {
		t.Errorf("box role = %q, want wallbox", role)
	}
	if got["box"].Name != "Ladebox" || !got["plug"].Switchable {
		t.Errorf("name or switchable lost: %+v", got)
	}

	if role := learnDevices(devices, nil)["pump"].Role; role != "" {
		t.Errorf("without resolver role = %q, want empty", role)
	}
}

func TestEngineGroupsFollowTheSwitches(t *testing.T) {
	cases := []struct {
		name  string
		value settings.Settings
		want  map[string]bool
	}{
		{"defaults", settings.Default(), map[string]bool{"habit": true, "reliability": true}},
		{"patterns off", settings.Settings{AutomationPatternsDisabled: true}, map[string]bool{"habit": false, "reliability": false}},
		{"habits off", settings.Settings{AutomationHabitsDisabled: true}, map[string]bool{"habit": false, "reliability": true}},
		{"reliability off", settings.Settings{AutomationReliabilityDisabled: true}, map[string]bool{"habit": true, "reliability": false}},
		{"both off", settings.Settings{AutomationHabitsDisabled: true, AutomationReliabilityDisabled: true}, map[string]bool{"habit": false, "reliability": false}},
		{"energy off", settings.Settings{AutomationEnergyDisabled: true}, map[string]bool{"habit": true, "reliability": true, "energy": false}},
		{"energy on by default", settings.Default(), map[string]bool{"habit": true, "reliability": true, "energy": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := engineGroupsFor(tc.value)
			for group, want := range tc.want {
				if got[group] != want {
					t.Fatalf("%s = %v, want %v (groups %v)", group, got[group], want, got)
				}
			}
		})
	}
}

// TestPVSiteRegulatorCountsEveryRegulator checks that a regulator with an entity
// base, as the zero feed template writes it, counts as present. The automation
// service regulates every regulator on the grid net.
func TestPVSiteRegulatorCountsEveryRegulator(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "demo", "fixtures", "devices", "demo", "automation_rules.json"))
	if err != nil {
		t.Fatalf("read demo rules: %v", err)
	}
	c := &pvSiteCache{readDoc: func() ([]byte, error) { return raw, nil }, logf: t.Logf}
	c.refresh()
	reg := c.regulator()
	if reg == nil || !reg.Present || reg.MaxW != 800 || reg.DeadbandW != 20 {
		t.Fatalf("regulator = %+v, want present with max 800 W and deadband 20 W", reg)
	}
	off := []byte(`{"version":1,"scenarios":[{"id":"a","enabled":false,"while":{"mode":"regulate","regulator":{"output":{"max":800},"base":{"source":"balance","field":"pv"}}}}]}`)
	c = &pvSiteCache{readDoc: func() ([]byte, error) { return off, nil }, logf: t.Logf}
	c.refresh()
	if reg := c.regulator(); reg != nil {
		t.Errorf("disabled scenario gives regulator %+v, want none", reg)
	}
}

func TestPVKeepDaysFollowsTheSeasonSwitch(t *testing.T) {
	if got := pvKeepDays(settings.Settings{}); got != autolearn.PVRasterYearDays {
		t.Fatalf("default = %d, want a year", got)
	}
	if got := pvKeepDays(settings.Settings{LearningSeasonYearDisabled: true, LearningProfileDays: 90}); got != 90 {
		t.Fatalf("switch off = %d, want the profile days 90", got)
	}
	if got := pvKeepDays(settings.Settings{LearningSeasonYearDisabled: true}); got != settings.LearningProfileDaysDefault {
		t.Fatalf("switch off without profile days = %d, want the default", got)
	}
}
