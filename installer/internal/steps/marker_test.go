package steps_test

import (
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
)

func TestParseMarkerBegin(t *testing.T) {
	m, ok := steps.ParseMarker("##STEP 10 begin")
	if !ok {
		t.Fatalf("expected a marker")
	}
	if m.StepID != "10" || m.Kind != steps.Begin || m.Detail != "" {
		t.Fatalf("unexpected marker: %+v", m)
	}
}

func TestParseMarkerOK(t *testing.T) {
	m, ok := steps.ParseMarker("##STEP 10 ok")
	if !ok || m.Kind != steps.OK {
		t.Fatalf("unexpected result: %+v, %v", m, ok)
	}
}

func TestParseMarkerSkipCarriesTheReason(t *testing.T) {
	m, ok := steps.ParseMarker("##STEP 40 skip login ausstehend")
	if !ok {
		t.Fatalf("expected a marker")
	}
	if m.StepID != "40" || m.Kind != steps.Skip || m.Detail != "login ausstehend" {
		t.Fatalf("unexpected marker: %+v", m)
	}
}

func TestParseMarkerFailCarriesTheCode(t *testing.T) {
	m, ok := steps.ParseMarker("##STEP 50 fail PIP_EXTERNALLY_MANAGED")
	if !ok {
		t.Fatalf("expected a marker")
	}
	if m.StepID != "50" || m.Kind != steps.Fail || m.Detail != "PIP_EXTERNALLY_MANAGED" {
		t.Fatalf("unexpected marker: %+v", m)
	}
}

func TestParseMarkerRejectsHumanText(t *testing.T) {
	for _, line := range []string{
		"",
		"installing packages...",
		"a line that merely mentions ##STEP somewhere",
		"##STEPX 10 begin",
		"##STEP 10 sideways",
	} {
		if _, ok := steps.ParseMarker(line); ok {
			t.Fatalf("expected %q to be rejected as human text", line)
		}
	}
}

func TestParseMarkerRejectsAMarkerWithoutAKind(t *testing.T) {
	if _, ok := steps.ParseMarker("##STEP 10"); ok {
		t.Fatalf("a marker without a kind must be rejected")
	}
}
