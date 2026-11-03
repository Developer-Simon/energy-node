package onboarding

import (
	"slices"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func stepIDs(steps []Step) []string {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	return ids
}

func TestCatalogDropsTailscaleWhenNotInstalled(t *testing.T) {
	if slices.Contains(stepIDs(Catalog(map[string]bool{})), "tailscale") {
		t.Fatal("tailscale step present although the service is not installed")
	}
	steps := Catalog(map[string]bool{"tailscale": true})
	var found bool
	for _, step := range steps {
		if step.ID == "tailscale" {
			found = true
			if step.Needs != "tailscale" {
				t.Fatalf("tailscale Needs = %q, want tailscale", step.Needs)
			}
		}
	}
	if !found {
		t.Fatal("tailscale step missing although the service is installed")
	}
}

func TestCatalogAutomationsNeedsService(t *testing.T) {
	if slices.Contains(stepIDs(Catalog(map[string]bool{})), "automations") {
		t.Fatal("automations step present although the service is not installed")
	}
	steps := Catalog(map[string]bool{"automation": true})
	last := steps[len(steps)-1]
	if last.ID != "automations" {
		t.Fatalf("last step = %q, want automations", last.ID)
	}
	if last.Area != "devices" || last.Installer || last.Needs != "automation" {
		t.Fatalf("automations step = %+v, want devices, not installer, Needs automation", last)
	}
}

func TestCatalogOrderAndAreas(t *testing.T) {
	steps := Catalog(map[string]bool{"tailscale": true, "automation": true})
	wantIDs := []string{
		"users", "guest", "tailscale", "companion",
		"theme", "title", "statusbar", "tabwidth", "tiles",
		"updates", "metrics",
		"configservice", "config", "devices", "roles", "devicemap", "automations",
	}
	if got := stepIDs(steps); !slices.Equal(got, wantIDs) {
		t.Fatalf("step IDs = %v, want %v", got, wantIDs)
	}
	wantAreas := map[string]string{
		"users": "access", "guest": "access", "tailscale": "access", "companion": "access",
		"theme": "display", "title": "display",
		"statusbar": "display", "tabwidth": "display", "tiles": "display",
		"updates": "system", "metrics": "system",
		"configservice": "devices", "config": "devices", "devices": "devices", "roles": "devices", "devicemap": "devices",
		"automations": "devices",
	}
	wantInstaller := map[string]bool{
		"users": true, "guest": true, "theme": true, "title": true,
		"statusbar": true, "tabwidth": true, "updates": true, "metrics": true,
	}
	for _, step := range steps {
		if step.Area != wantAreas[step.ID] {
			t.Errorf("%s Area = %q, want %q", step.ID, step.Area, wantAreas[step.ID])
		}
		if step.Installer != wantInstaller[step.ID] {
			t.Errorf("%s Installer = %v, want %v", step.ID, step.Installer, wantInstaller[step.ID])
		}
	}
}

func defaultInput() DetectInput {
	return DetectInput{Settings: settings.Default(), GuestAccess: true}
}

func TestDetectDefaultSettingsAreOpen(t *testing.T) {
	got := Detect(defaultInput())
	for id, state := range got {
		if state == StepDone {
			t.Errorf("%s detected as done on default settings", id)
		}
	}
}

func TestDetectChangedThemeIsDone(t *testing.T) {
	in := defaultInput()
	in.Settings.Theme = settings.ThemeStromblau
	got := Detect(in)
	if got["theme"] != StepDone {
		t.Fatalf("theme = %q, want %q", got["theme"], StepDone)
	}
}

func TestDetectRolesNeedsOneAssignment(t *testing.T) {
	in := defaultInput()
	in.Energy = settings.EnergyConfig{Assignments: map[string]energy.Assignment{"sensor.x": {}}}
	if got := Detect(in); got["roles"] == StepDone {
		t.Fatal("roles done without any assigned role")
	}
	in.Energy = settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.x": {}, "sensor.pv": {Role: energy.RolePV},
	}}
	if got := Detect(in); got["roles"] != StepDone {
		t.Fatalf("roles = %q, want %q", got["roles"], StepDone)
	}
}

func TestDetectMetricsOffIsDone(t *testing.T) {
	in := defaultInput()
	in.MQTT.Metrics = map[string]bool{nodeagent.Metrics[0]: false}
	if got := Detect(in); got["metrics"] != StepDone {
		t.Fatalf("metrics = %q, want %q", got["metrics"], StepDone)
	}
}

func TestDetectMetricsAllExplicitlyOnStaysOpen(t *testing.T) {
	in := defaultInput()
	in.MQTT.Metrics = map[string]bool{}
	for _, metric := range nodeagent.Metrics {
		in.MQTT.Metrics[metric] = true
	}
	if got := Detect(in); got["metrics"] == StepDone {
		t.Fatal("metrics done although every metric is on")
	}
}

func TestDetectGuestAccessOffIsDone(t *testing.T) {
	in := defaultInput()
	in.GuestAccess = false
	if got := Detect(in); got["guest"] != StepDone {
		t.Fatalf("guest = %q, want %q", got["guest"], StepDone)
	}
}

func TestDetectRuntimeStatusOffIsDone(t *testing.T) {
	in := defaultInput()
	in.Settings.ShowRuntimeStatus = false
	if got := Detect(in); got["statusbar"] != StepDone {
		t.Fatalf("statusbar = %q, want %q", got["statusbar"], StepDone)
	}
}

func TestDetectPartiallyConfiguredInstall(t *testing.T) {
	in := defaultInput()
	in.Settings.Theme = settings.ThemeSignalgelb
	in.Energy = settings.EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.pv": {Role: energy.RolePV},
	}}
	in.Users = []auth.User{
		{Username: "admin", Roles: []string{auth.RoleUserManagement}},
	}
	got := Detect(in)
	if got["theme"] != StepDone {
		t.Errorf("theme = %q, want %q", got["theme"], StepDone)
	}
	if got["roles"] != StepDone {
		t.Errorf("roles = %q, want %q", got["roles"], StepDone)
	}
	for _, id := range []string{"users", "title", "statusbar", "tabwidth", "updates", "config", "devices", "devicemap", "tiles"} {
		if got[id] == StepDone {
			t.Errorf("%s detected as done, want open", id)
		}
	}
}

func TestMergeStoredWins(t *testing.T) {
	detected := map[string]StepState{"theme": StepDone, "roles": StepDone}
	stored := map[string]StepState{"theme": StepLater, "users": StepInstaller, "roles": StepDone}
	got := Merge(detected, stored)
	want := map[string]StepState{"theme": StepLater, "roles": StepDone, "users": StepInstaller}
	if len(got) != len(want) {
		t.Fatalf("Merge = %v, want %v", got, want)
	}
	for id, state := range want {
		if got[id] != state {
			t.Errorf("%s = %q, want %q", id, got[id], state)
		}
	}
	// Erkannt füllt Lücken, gespeichert überschreibt nur, was gespeichert ist.
	if fill := Merge(map[string]StepState{"title": StepDone}, nil); fill["title"] != StepDone {
		t.Errorf("detected title lost without stored state: %v", fill)
	}
}
