package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/monitoring"
)

const (
	monitoringPath    = "/api/v1/monitoring"
	monitoringAckPath = "/api/v1/monitoring/ack"
)

// MonitoringDeps verbindet die Routen mit Sammeln, Speicher und Lernwerten.
// Nil heißt 501.
type MonitoringDeps struct {
	// Notices sammelt und wendet den Speicher an, now ist die Zeit der Runde.
	Notices func(now time.Time) []monitoring.Notice
	// Ack quittiert oder nimmt zurück und speichert.
	Ack func(id string, ack bool, user string, now time.Time) error
	// Learned liefert die Lernwerte wie GET /api/v1/automations/learned,
	// ergänzt um Gerätename und Icon.
	Learned func() MonitoringLearned
}

// MonitoringLearned sind die Lernwerte der Überwachung.
type MonitoringLearned struct {
	Enabled bool                     `json:"enabled"`
	Values  []MonitoringLearnedValue `json:"values"`
}

// MonitoringLearnedValue ist ein Lernwert mit Gerätename und Icon.
type MonitoringLearnedValue struct {
	learnedValueView
	DeviceName string `json:"device_name,omitempty"`
	DeviceIcon string `json:"device_icon,omitempty"`
}

// monitoringBody is the answer of GET /api/v1/monitoring and of the ack POST.
// Notices is never null.
type monitoringBody struct {
	Notices  []monitoring.Notice `json:"notices"`
	NewCount int                 `json:"new_count"`
	Learned  MonitoringLearned   `json:"learned"`
}

// monitoringAckBody is the body of POST /api/v1/monitoring/ack.
type monitoringAckBody struct {
	ID  string `json:"id"`
	Ack bool   `json:"ack"`
}

// handleMonitoring serves GET /api/v1/monitoring. Without deps the answer is 501.
func handleMonitoring(deps *MonitoringDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if deps == nil {
			writeError(w, http.StatusNotImplemented, "monitoring_unavailable", "Monitoring ist auf dieser Instanz nicht verfügbar.")
			return
		}
		writeJSON(w, monitoringAnswer(deps, time.Now()))
	}
}

// handleMonitoringAck serves POST /api/v1/monitoring/ack. It needs the edit
// setup role, and answers the monitoring page like GET after the change.
func handleMonitoringAck(manager *auth.Manager, deps *MonitoringDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if deps == nil || deps.Ack == nil {
			writeError(w, http.StatusNotImplemented, "monitoring_unavailable", "Monitoring ist auf dieser Instanz nicht verfügbar.")
			return
		}
		if !requireRole(w, r, manager, auth.RoleEditSetup, "monitoring_ack_forbidden", "error.monitoring_ack_forbidden", "Quittieren braucht das Recht Einrichten") {
			return
		}
		if !requireCSRF(w, r, manager) {
			return
		}
		var body monitoringAckBody
		if err := decodeBody(r, &body); err != nil || body.ID == "" {
			writeErrorKey(w, http.StatusBadRequest, "monitoring_ack_invalid", "error.monitoring_ack_invalid", nil, "Die Kennung der Meldung ist ungültig.")
			return
		}
		user := ""
		if current, ok := auth.UserFromContext(r.Context()); ok {
			user = current.Username
		}
		now := time.Now()
		if err := deps.Ack(body.ID, body.Ack, user, now); err != nil {
			if errors.Is(err, monitoring.ErrUnknown) {
				writeErrorKey(w, http.StatusNotFound, "monitoring_ack_unknown", "error.monitoring_ack_unknown", nil, "Diese Meldung gibt es nicht mehr.")
				return
			}
			writeErrorKey(w, http.StatusInternalServerError, "monitoring_save_failed", "error.monitoring_save_failed", nil, "Die Quittung konnte nicht gespeichert werden.")
			return
		}
		writeJSON(w, monitoringAnswer(deps, now))
	}
}

// monitoringAnswer collects the notices of the round and counts the ones
// without an acknowledgement. Empty lists stay lists, never null.
func monitoringAnswer(deps *MonitoringDeps, now time.Time) monitoringBody {
	notices := []monitoring.Notice{}
	if deps.Notices != nil {
		if collected := deps.Notices(now); collected != nil {
			notices = collected
		}
	}
	newCount := 0
	for _, notice := range notices {
		if notice.Ack == nil {
			newCount++
		}
	}
	learned := MonitoringLearned{Values: []MonitoringLearnedValue{}}
	if deps.Learned != nil {
		learned = deps.Learned()
	}
	if learned.Values == nil {
		learned.Values = []MonitoringLearnedValue{}
	}
	return monitoringBody{Notices: notices, NewCount: newCount, Learned: learned}
}
