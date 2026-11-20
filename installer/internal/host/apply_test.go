package host

import (
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
)

// applyTestManifest mirrors the step layout the apply-selection rules are
// written against, in numeric step order. 15 and 65 are core steps, 35 needs
// the Shelly service 83, 81, 83 and 88 are optional services and 89 is a
// first-install default.
func applyTestManifest() *bundle.Manifest {
	return &bundle.Manifest{Steps: []bundle.StepEntry{
		{ID: "15"},
		{ID: "35", Optional: true, Requires: "83"},
		{ID: "65"},
		{ID: "81", Optional: true},
		{ID: "83", Optional: true},
		{ID: "88", Optional: true},
		{ID: "89", Optional: true, FirstInstallOnly: true, Requires: "81,83,88"},
	}}
}

func selectionOf(steps map[string]bool) *selection.Selection {
	return &selection.Selection{Steps: steps}
}

func TestApplyStepsTurnsOnOneService(t *testing.T) {
	m := applyTestManifest()
	before := selectionOf(map[string]bool{"81": false})
	after := selectionOf(map[string]bool{"81": true})

	got := ApplySteps(m, before, after)
	want := []string{"65", "81"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplySteps = %v, want %v", got, want)
	}
}

func TestApplyStepsTurnsOffWithDependent(t *testing.T) {
	m := applyTestManifest()
	before := selectionOf(map[string]bool{"83": true, "35": true})
	after := selectionOf(map[string]bool{"83": false, "35": true})

	got := ApplySteps(m, before, after)
	want := []string{"35", "65", "83"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplySteps = %v, want %v", got, want)
	}
}

func TestApplyStepsIgnoresFirstInstallOnly(t *testing.T) {
	m := applyTestManifest()
	before := selectionOf(map[string]bool{"89": false})
	after := selectionOf(map[string]bool{"89": true})

	if got := ApplySteps(m, before, after); got != nil {
		t.Fatalf("ApplySteps = %v, want nil", got)
	}
}

func TestApplyStepsNilWithoutChange(t *testing.T) {
	m := applyTestManifest()
	sel := selectionOf(map[string]bool{"81": true, "83": false})
	same := selectionOf(map[string]bool{"81": true, "83": false})

	if got := ApplySteps(m, sel, same); got != nil {
		t.Fatalf("ApplySteps = %v, want nil", got)
	}
}

func TestApplyStepsMissingEntryCountsAsSelected(t *testing.T) {
	m := applyTestManifest()
	before := selectionOf(map[string]bool{})
	after := selectionOf(map[string]bool{"81": false})

	got := ApplySteps(m, before, after)
	want := []string{"65", "81"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplySteps = %v, want %v", got, want)
	}
}
