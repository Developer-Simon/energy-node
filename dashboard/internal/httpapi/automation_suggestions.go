package httpapi

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

const (
	automationSuggestionsPath = "/api/v1/automations/suggestions"
	// automationSuggestionActionPrefix serves POST .../{id}/dismiss, /later and /undo.
	automationSuggestionActionPrefix = "/api/v1/automations/suggestions/"
	secondsPerDay                    = 24 * 60 * 60
)

// automationSuggestionSource is the part of the suggestion book the handlers use.
// The book of package main implements it.
type automationSuggestionSource interface {
	// Visible returns the suggestions to show now. exists reports whether a
	// device is known and not hidden, nil keeps every device.
	Visible(now time.Time, exists func(device string) bool) []autolearn.Finding
	// Known reports whether the ID belongs to a current or hidden suggestion.
	Known(id string) bool
	Dismiss(id string, now time.Time) error
	Later(id string, now time.Time) error
	Undo(id string) error
}

// automationSuggestionsBody is the answer of GET /api/v1/automations/suggestions.
// Suggestions is never null.
type automationSuggestionsBody struct {
	Enabled     bool                          `json:"enabled"`
	Learning    automationSuggestionsLearning `json:"learning"`
	Suggestions []autolearn.Finding           `json:"suggestions"`
}

// automationSuggestionsLearning tells since when and on how many devices the
// patterns were learned, and for how many days.
type automationSuggestionsLearning struct {
	Since   int64 `json:"since"`
	Devices int   `json:"devices"`
	Days    int   `json:"days"`
}

// handleAutomationSuggestions serves GET /api/v1/automations/suggestions. With
// "Muster lernen" off the answer is enabled false and an empty list. Without a
// source the answer is 501.
func handleAutomationSuggestions(learning *LearningDeps, source automationSuggestionSource, exists func(device string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if source == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		now := time.Now()
		body := automationSuggestionsBody{Enabled: true, Suggestions: []autolearn.Finding{}}
		if learning != nil {
			// Without readable settings everything stays on, as for the other switches.
			if learning.Settings != nil {
				if value, err := learning.Settings(); err == nil {
					body.Enabled = !value.AutomationPatternsDisabled
				}
			}
			if learning.Usage != nil {
				usage := learning.Usage()
				body.Learning = automationSuggestionsLearning{Since: usage.Since, Devices: usage.Devices, Days: learningDays(usage.Since, now)}
			}
		}
		if body.Enabled {
			if visible := source.Visible(now, exists); visible != nil {
				body.Suggestions = visible
			}
		}
		writeJSON(w, body)
	}
}

// learningDays counts the whole days from since to now. Without a start it is 0.
func learningDays(since int64, now time.Time) int {
	if since <= 0 {
		return 0
	}
	days := (now.Unix() - since) / secondsPerDay
	if days < 0 {
		return 0
	}
	return int(days)
}

// handleAutomationSuggestionAction serves POST
// /api/v1/automations/suggestions/{id}/{dismiss|later|undo}. It needs the
// automations role and a CSRF token, answers {"ok":true}, and 404 for an ID the
// source does not know.
func handleAutomationSuggestionAction(manager *auth.Manager, source automationSuggestionSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if source == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		if !requireAutomationSuggestionMutation(w, r, manager) {
			return
		}
		id, action, ok := parseAutomationSuggestionPath(r.URL.Path)
		if !ok || !source.Known(id) {
			writeErrorKey(w, http.StatusNotFound, "suggestion_unknown", "error.suggestion_unknown", nil, "Der Vorschlag ist nicht bekannt")
			return
		}
		now := time.Now()
		var err error
		switch action {
		case "dismiss":
			err = source.Dismiss(id, now)
		case "later":
			err = source.Later(id, now)
		case "undo":
			err = source.Undo(id)
		}
		if err != nil {
			log.Printf("httpapi: suggestion %s: %v", action, err)
			writeErrorKey(w, http.StatusInternalServerError, "suggestion_save_failed", "error.suggestion_save_failed", nil, "Der Vorschlag konnte nicht gespeichert werden.")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// parseAutomationSuggestionPath splits .../{id}/{action}. The action is the last
// segment, so an ID may contain colons. Other actions are not known.
func parseAutomationSuggestionPath(path string) (id, action string, ok bool) {
	rest := strings.TrimPrefix(path, automationSuggestionActionPrefix)
	idx := strings.LastIndex(rest, "/")
	if idx <= 0 {
		return "", "", false
	}
	id, action = rest[:idx], rest[idx+1:]
	switch action {
	case "dismiss", "later", "undo":
		return id, action, true
	}
	return "", "", false
}

// requireAutomationSuggestionMutation gates the suggestion actions. Without an
// auth manager everything passes, otherwise the caller needs the automations
// role plus a valid CSRF token.
func requireAutomationSuggestionMutation(w http.ResponseWriter, r *http.Request, manager *auth.Manager) bool {
	if manager == nil {
		return true
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return false
	}
	if !auth.HasRole(user, auth.RoleAutomations) {
		writeErrorKey(w, http.StatusForbidden, "suggestion_forbidden", "error.suggestion_forbidden", nil, "Für die Vorschläge fehlt die Berechtigung")
		return false
	}
	return requireAutomationFindingsCSRF(w, r, manager)
}
