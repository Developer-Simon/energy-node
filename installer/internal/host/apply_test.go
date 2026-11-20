package host

import (
	"context"
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
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

func TestPlanCarriesApplyStepsOnlyForTheInstalledNodePackage(t *testing.T) {
	origPreview, origNode := previewPlan, nodeSelection
	t.Cleanup(func() { previewPlan, nodeSelection = origPreview, origNode })
	previewPlan = func(context.Context, *transport.Client, string, string, string, *selection.Selection) (*steps.Plan, error) {
		return &steps.Plan{}, nil
	}
	nodeSelection = func(*Host) *hostapi.SelectionView {
		return &hostapi.SelectionView{Steps: map[string]bool{"81": false}, Source: "node"}
	}
	m := applyTestManifest()
	pending := selectionOf(map[string]bool{"81": true})

	cases := []struct {
		name     string
		resolved *hostapi.ResolvedInfo
		want     []string
	}{
		{"github package", &hostapi.ResolvedInfo{Kind: "github", Installed: true}, nil},
		{"node package not installed", &hostapi.ResolvedInfo{Kind: "node"}, nil},
		{"installed node package", &hostapi.ResolvedInfo{Kind: "node", Installed: true}, []string{"65", "81"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &Host{client: &transport.Client{}, manifest: m, bundleDir: "/bundle", resolved: c.resolved, pending: pending}
			view, err := h.Plan(context.Background())
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if !reflect.DeepEqual(view.ApplySteps, c.want) {
				t.Fatalf("ApplySteps = %v, want %v", view.ApplySteps, c.want)
			}
		})
	}
}

func TestApplyListNeedsANodeSelection(t *testing.T) {
	// Without a connection the node cannot be asked, so there is no current
	// selection to compare against and the list stays empty.
	h := &Host{
		manifest: applyTestManifest(),
		resolved: &hostapi.ResolvedInfo{Kind: "node", Installed: true},
		pending:  selectionOf(map[string]bool{"81": true}),
	}
	if got := h.applyList(h.manifest); got != nil {
		t.Fatalf("applyList = %v, want nil", got)
	}
}
