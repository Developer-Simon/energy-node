package main

import (
	"context"
	"testing"
	"time"

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
