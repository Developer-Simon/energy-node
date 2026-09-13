package devcli_test

import (
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
)

func targetTestManifest() *bundle.Manifest {
	return &bundle.Manifest{
		Version: "v0.2.0",
		Steps: []bundle.StepEntry{
			{ID: "50", Optional: false},
			{ID: "60", Optional: false},
			{ID: "81", Optional: true, ServiceID: "apsystems"},
			{ID: "88", Optional: true, ServiceID: "automation"},
		},
	}
}

func TestResolveStepTargetMapsDashboardToStep60(t *testing.T) {
	entry, err := devcli.ResolveStepTarget(targetTestManifest(), "dashboard")
	if err != nil {
		t.Fatalf("ResolveStepTarget: %v", err)
	}
	if entry.ID != "60" {
		t.Fatalf("expected step 60, got %q", entry.ID)
	}
}

func TestResolveStepTargetMapsWheelsToStep50(t *testing.T) {
	entry, err := devcli.ResolveStepTarget(targetTestManifest(), "wheels")
	if err != nil {
		t.Fatalf("ResolveStepTarget: %v", err)
	}
	if entry.ID != "50" {
		t.Fatalf("expected step 50, got %q", entry.ID)
	}
}

func TestResolveStepTargetMatchesAServiceID(t *testing.T) {
	entry, err := devcli.ResolveStepTarget(targetTestManifest(), "automation")
	if err != nil {
		t.Fatalf("ResolveStepTarget: %v", err)
	}
	if entry.ID != "88" {
		t.Fatalf("expected step 88, got %q", entry.ID)
	}
}

func TestResolveStepTargetRejectsAnUnknownTarget(t *testing.T) {
	if _, err := devcli.ResolveStepTarget(targetTestManifest(), "shelly"); err == nil {
		t.Fatalf("expected an error: this manifest has no shelly step")
	}
}
