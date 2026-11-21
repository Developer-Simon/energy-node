package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// learnedValueSource is the part of the learned value store the handler uses.
// The store of package autolearn implements it.
type learnedValueSource interface {
	Entries() []autolearn.LearnedEntry
}

// automationLearnedBody is the answer of GET /api/v1/automations/learned.
// Values is never null.
type automationLearnedBody struct {
	Enabled bool                     `json:"enabled"`
	Values  []autolearn.LearnedEntry `json:"values"`
}

// handleAutomationLearned serves GET /api/v1/automations/learned. Enabled is
// false when "Muster lernen" or the habit group is off. The values stay in the
// list then, because rules keep using them until they age out. Values of hidden
// or missing devices are left out, site values always stay. Without a source the
// answer is 501.
func handleAutomationLearned(learning *LearningDeps, source learnedValueSource, exists func(device string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if source == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		body := automationLearnedBody{Enabled: true, Values: []autolearn.LearnedEntry{}}
		if learning != nil && learning.Settings != nil {
			// Without readable settings everything stays on, as for the suggestions.
			if value, err := learning.Settings(); err == nil {
				body.Enabled = !value.AutomationPatternsDisabled && !value.AutomationHabitsDisabled
			}
		}
		for _, entry := range source.Entries() {
			if entry.Device != "" && exists != nil && !exists(entry.Device) {
				continue
			}
			body.Values = append(body.Values, entry)
		}
		writeJSON(w, body)
	}
}
