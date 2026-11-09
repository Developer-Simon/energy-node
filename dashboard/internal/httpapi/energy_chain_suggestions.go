package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/chainsuggest"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const (
	energyChainSuggestionsPath        = "/api/v1/energy/chain-suggestions"
	energyChainSuggestionsDismissPath = "/api/v1/energy/chain-suggestions/dismiss"
)

// chainSuggester is the part of the chain suggestions the handlers use. The
// collector of package chainsuggest implements it.
type chainSuggester interface {
	Enabled() bool
	Suggestions(model energy.Model, edge func(deviceA, deviceB string) bool) []chainsuggest.Suggestion
	Dismiss(a, b string) error
}

// chainSuggestionsBody is the answer of GET /api/v1/energy/chain-suggestions.
// Suggestions is never null.
type chainSuggestionsBody struct {
	Suggestions []chainsuggest.Suggestion `json:"suggestions"`
	// Disabled ist true, wenn die Vorschlaege abgeschaltet sind. Dann
	// kommt keine Liste.
	Disabled bool `json:"disabled"`
}

// chainSuggestionDismissBody is the body of POST
// /api/v1/energy/chain-suggestions/dismiss.
type chainSuggestionDismissBody struct {
	A string `json:"a"`
	B string `json:"b"`
}

// energyEdgeFunc reports whether an energy connection in the device map links
// the two devices, in either order. Without a readable device map no pair is
// connected.
func energyEdgeFunc(store *settings.Store) func(deviceA, deviceB string) bool {
	if store == nil {
		return func(string, string) bool { return false }
	}
	deviceMap, err := store.LoadDeviceMap()
	if err != nil {
		return func(string, string) bool { return false }
	}
	return func(deviceA, deviceB string) bool {
		for _, prop := range deviceMap.EdgeProps {
			if prop.Type == "energy" && pairMatches(prop.ParentID, prop.ChildID, deviceA, deviceB) {
				return true
			}
		}
		return false
	}
}

// handleEnergyChainSuggestions serves GET /api/v1/energy/chain-suggestions.
// The suggester may be nil (no data directory), then the answer is 501.
func handleEnergyChainSuggestions(store *settings.Store, resolver *energy.Resolver, suggester chainSuggester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if suggester == nil {
			writeError(w, http.StatusNotImplemented, "energy_roles_unavailable", "Energie-Rollen sind ohne Datenverzeichnis nicht verfügbar")
			return
		}
		if !suggester.Enabled() {
			writeJSON(w, chainSuggestionsBody{Suggestions: []chainsuggest.Suggestion{}, Disabled: true})
			return
		}
		suggestions := suggester.Suggestions(resolver.Model(), energyEdgeFunc(store))
		if suggestions == nil {
			suggestions = []chainsuggest.Suggestion{}
		}
		writeJSON(w, chainSuggestionsBody{Suggestions: suggestions})
	}
}

// handleEnergyChainSuggestionDismiss serves POST
// /api/v1/energy/chain-suggestions/dismiss. It needs edit_energy and a CSRF
// token, like the chain writes, and answers 204.
func handleEnergyChainSuggestionDismiss(manager *auth.Manager, suggester chainSuggester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if suggester == nil {
			writeError(w, http.StatusNotImplemented, "energy_roles_unavailable", "Energie-Rollen sind ohne Datenverzeichnis nicht verfügbar")
			return
		}
		if !requireEnergyMutation(w, r, manager) {
			return
		}
		defer r.Body.Close()
		var body chainSuggestionDismissBody
		if err := decodeBody(r, &body); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "chain_suggestion_invalid", err)
			return
		}
		if body.A == "" || body.B == "" || body.A == body.B {
			writeErrorKey(w, http.StatusBadRequest, "chain_suggestion_invalid", "error.chain_suggestion_invalid", nil, "Das Paar des Messketten-Vorschlags ist ungültig.")
			return
		}
		if err := suggester.Dismiss(body.A, body.B); err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "chain_suggestion_failed", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
