package webui

import "testing"

func TestTourTargetsKnowsTheMarkupAndTheSetupSteps(t *testing.T) {
	targets := TourTargets()
	// theme is a literal data-tour in settings.html, guest only exists as a
	// data('guest') call in onboarding-steps.js and an x-bind in users.html.
	for _, id := range []string{"theme", "users", "guest", "devices"} {
		if !targets[id] {
			t.Errorf("target %q missing from %d known targets", id, len(targets))
		}
	}
	if targets["favorites"] || targets["nosuchtarget"] {
		t.Errorf("unknown target counted as known")
	}
}
