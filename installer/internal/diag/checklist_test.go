package diag_test

import (
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/diag"
)

func testSteps() []bundle.StepEntry {
	return []bundle.StepEntry{
		{ID: "10", Optional: false},
		{ID: "83", Optional: true, ServiceID: "shelly", Unit: "shelly-rpc.service"},
	}
}

func TestChecklistFlagsAnInactiveFixedUnit(t *testing.T) {
	report := &diag.Report{
		Units:     map[string]string{"mosquitto.service": "active", "caddy.service": "inactive"},
		Ports:     map[string]bool{"1883": true},
		Config:    diag.ConfigReport{ConfigJSON: true},
		Tailscale: diag.TailscaleReport{Angemeldet: true},
	}
	checks := report.Checklist(testSteps())

	var caddy *diag.Check
	for i := range checks {
		if checks[i].Name == "unit caddy.service" {
			caddy = &checks[i]
		}
	}
	if caddy == nil {
		t.Fatalf("expected a check for caddy.service, got: %+v", checks)
	}
	if caddy.OK {
		t.Errorf("caddy.service is inactive, expected OK=false")
	}
	if caddy.RetryStepID != "70" {
		t.Errorf("expected caddy's fixed retry step to be 70, got %q", caddy.RetryStepID)
	}
}

func TestChecklistResolvesADeviceServiceUnitViaSteps(t *testing.T) {
	report := &diag.Report{
		Units: map[string]string{"shelly-rpc.service": "inactive"},
	}
	checks := report.Checklist(testSteps())

	if len(checks) == 0 || checks[0].Name != "unit shelly-rpc.service" {
		t.Fatalf("expected a check for shelly-rpc.service, got: %+v", checks)
	}
	if checks[0].RetryStepID != "83" {
		t.Errorf("expected the retry step to resolve to 83 via StepEntry.Unit, got %q", checks[0].RetryStepID)
	}
}

func TestChecklistCoversPortsConfigAndTailscale(t *testing.T) {
	report := &diag.Report{
		Ports:     map[string]bool{"1883": true, "8080": false},
		Config:    diag.ConfigReport{ConfigJSON: false},
		Tailscale: diag.TailscaleReport{Angemeldet: false},
	}
	checks := report.Checklist(nil)

	byName := map[string]diag.Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	if c := byName["port 8080"]; c.OK || c.RetryStepID != "60" {
		t.Errorf("unexpected port 8080 check: %+v", c)
	}
	if c := byName["config.json"]; c.OK || c.RetryStepID != "60" {
		t.Errorf("unexpected config.json check: %+v", c)
	}
	if c := byName["tailscale login"]; c.OK || c.RetryStepID != "40" {
		t.Errorf("unexpected tailscale check: %+v", c)
	}
}

func TestChecklistIsOrderedDeterministically(t *testing.T) {
	report := &diag.Report{
		Units: map[string]string{"z.service": "active", "a.service": "active"},
		Ports: map[string]bool{"443": true, "80": true},
	}
	first := report.Checklist(nil)
	for i := 0; i < 5; i++ {
		if again := report.Checklist(nil); len(again) != len(first) || again[0] != first[0] {
			t.Fatalf("Checklist is not deterministic across calls")
		}
	}
}
