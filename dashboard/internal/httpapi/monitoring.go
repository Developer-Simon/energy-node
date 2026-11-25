package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/monitoring"
)

const (
	monitoringPath         = "/api/v1/monitoring"
	monitoringAckPath      = "/api/v1/monitoring/ack"
	monitoringForecastPath = "/api/v1/monitoring/forecast"
)

// MonitoringForecast is the answer of GET /api/v1/monitoring/forecast.
type MonitoringForecast struct {
	Forecast MonitoringForecastCheck `json:"forecast"`
	Season   MonitoringSeason        `json:"season"`
}

// MonitoringForecastCheck is the forecast check of the monitoring page.
// Days and Readers are never null.
type MonitoringForecastCheck struct {
	Needed    int                     `json:"needed"`
	UseMedian bool                    `json:"use_median"`
	Days      []autolearn.ForecastDay `json:"days"`
	Readers   []autolearn.RuleRef     `json:"readers"`
}

// MonitoringSeason is the season view of the monitoring page.
type MonitoringSeason struct {
	Year  int                    `json:"year"`
	Week  int                    `json:"week"`
	Weeks []MonitoringSeasonWeek `json:"weeks"`
}

// MonitoringSeasonWeek is one week of the season view, without the rule fields.
type MonitoringSeasonWeek struct {
	Key       string   `json:"key"`
	Source    string   `json:"source"`
	Days      int      `json:"days"`
	ExportKWh float64  `json:"export_kwh"`
	PVKWh     float64  `json:"pv_kwh"`
	LoadKWh   float64  `json:"load_kwh"`
	BaseW     float64  `json:"base_w"`
	TempC     *float64 `json:"temp_c,omitempty"`
}

// SeasonView keeps the shown fields of the season weeks. Weeks is never null.
func SeasonView(year, week int, weeks []autolearn.Week) MonitoringSeason {
	view := MonitoringSeason{Year: year, Week: week, Weeks: []MonitoringSeasonWeek{}}
	for _, item := range weeks {
		view.Weeks = append(view.Weeks, MonitoringSeasonWeek{
			Key:       item.Key,
			Source:    item.Source,
			Days:      item.Days,
			ExportKWh: item.ExportKWh,
			PVKWh:     item.PVKWh,
			LoadKWh:   item.LoadKWh,
			BaseW:     item.BaseW,
			TempC:     item.TempC,
		})
	}
	return view
}

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
	// Forecast liefert die Prognoseprüfung und die Saisonwochen, now ist die Zeit der Anfrage.
	Forecast func(now time.Time) MonitoringForecast
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

// handleMonitoringForecast serves GET /api/v1/monitoring/forecast. Without the
// Forecast dep the answer is 501. Days, Readers and Weeks are never null.
func handleMonitoringForecast(deps *MonitoringDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if deps == nil || deps.Forecast == nil {
			writeError(w, http.StatusNotImplemented, "monitoring_unavailable", "Monitoring ist auf dieser Instanz nicht verfügbar.")
			return
		}
		answer := deps.Forecast(time.Now())
		if answer.Forecast.Days == nil {
			answer.Forecast.Days = []autolearn.ForecastDay{}
		}
		if answer.Forecast.Readers == nil {
			answer.Forecast.Readers = []autolearn.RuleRef{}
		}
		if answer.Season.Weeks == nil {
			answer.Season.Weeks = []MonitoringSeasonWeek{}
		}
		writeJSON(w, answer)
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
