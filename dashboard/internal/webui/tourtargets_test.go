package webui

import "testing"

func TestTourTargetsKnowsTheMarkupAndTheSetupSteps(t *testing.T) {
	targets := TourTargets()
	// theme is a literal data-tour in settings.html, guest only exists as a
	// data('guest') call in onboarding-steps.js and an x-bind in users.html.
	for _, id := range []string{"theme", "users", "guest", "devices",
		"language", "numberformat", "history-exchange", "settings-nav", "settings-picker",
		"language-pill", "versions-highlights", "versions-history", "versions-components", "versions-changes",
		"users-tabs", "checklist-explore", "checklist-catch-up", "checklist-more", "mqtt-sensors", "mqtt-bridge",
		"diagnostics", "service-logs", "config-status",
		"devicemap-connect", "devicemap-layer-energy", "devicemap-layer-data", "devicemap-layer-balance",
		"devicemap-view", "devicemap-filter", "energy-plant", "energy-interpretation", "energy-roles-head",
		"energy-groups", "stats-nav", "stats-kpis", "stats-table-bar", "history-status", "history-resolution",
		"automations-tabs", "automations-rules", "@time-rule"} {
		if !targets[id] {
			t.Errorf("target %q missing from %d known targets", id, len(targets))
		}
	}
	if targets["favorites"] || targets["nosuchtarget"] {
		t.Errorf("unknown target counted as known")
	}
}

func TestTourTargetsReadQuotedIDsFromXBind(t *testing.T) {
	got := tourTargetsIn([]byte(`<b x-bind:data-tour="isSelf(cardId) ? 'own-account' : null"></b><i x-bind:data-tour="a ? 'x-one' : 'x-two'"></i>`))
	for _, id := range []string{"own-account", "x-one", "x-two"} {
		if !got[id] {
			t.Errorf("%q missing in %v", id, got)
		}
	}
}
