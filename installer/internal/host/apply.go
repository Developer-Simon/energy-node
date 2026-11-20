package host

import (
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/selection"
)

// applyAlwaysStep is the step every apply-selection run executes, so that the
// installed services get their configuration rewritten.
const applyAlwaysStep = "65"

// effectiveSelected reports whether step id runs under sel. A step that is not
// optional always runs. An optional step runs when it is selected, it is not
// a first-install-only default (an installed state always exists in this
// mode), and every step named in its Requires runs as well. A step without a
// manifest entry is not optional, so it counts as running.
func effectiveSelected(m *bundle.Manifest, sel *selection.Selection, id string) bool {
	entry, ok := findStepEntry(m, id)
	if !ok || !entry.Optional {
		return true
	}
	if entry.FirstInstallOnly || !sel.Selected(id) {
		return false
	}
	for _, req := range strings.Split(entry.Requires, ",") {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		if !effectiveSelected(m, sel, req) {
			return false
		}
	}
	return true
}

// ApplySteps returns the IDs of the steps an apply-selection run has to
// execute when the selection changes from before to after, in manifest order.
// Those are the steps whose effective state changed, plus step 65. It returns
// nil when the effective selection does not change.
func ApplySteps(m *bundle.Manifest, before, after *selection.Selection) []string {
	changed := make(map[string]bool)
	for _, step := range m.Steps {
		if effectiveSelected(m, before, step.ID) != effectiveSelected(m, after, step.ID) {
			changed[step.ID] = true
		}
	}
	if len(changed) == 0 {
		return nil
	}
	changed[applyAlwaysStep] = true

	var ids []string
	seen := make(map[string]bool)
	for _, step := range m.Steps {
		if changed[step.ID] && !seen[step.ID] {
			seen[step.ID] = true
			ids = append(ids, step.ID)
		}
	}
	return ids
}

// findStepEntry returns the manifest entry with the given step ID.
func findStepEntry(m *bundle.Manifest, id string) (bundle.StepEntry, bool) {
	for _, step := range m.Steps {
		if step.ID == id {
			return step, true
		}
	}
	return bundle.StepEntry{}, false
}
