package httpapi

import (
	"errors"
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// handleEnergyChainStats serves the measurement chain losses per bucket:
// GET /api/v1/energy/stats/chains?period=day|week|month|year&at=<datum>.
func handleEnergyChainStats(service *energystats.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if service == nil {
			writeError(w, http.StatusNotImplemented, "energy_stats_unavailable", "Die Energiestatistik ist ohne Datenverzeichnis nicht verfügbar")
			return
		}
		query := r.URL.Query()
		response, err := service.ChainStats(query.Get("period"), query.Get("at"))
		switch {
		case errors.Is(err, energystats.ErrInvalidPeriod):
			writeError(w, http.StatusBadRequest, "energy_stats_invalid_period", "Zeitraum oder Datum ist ungültig")
			return
		case err != nil:
			writeErrorDetail(w, http.StatusInternalServerError, "energy_stats_failed", err)
			return
		}
		writeJSON(w, response)
	}
}

// handleEnergyEfficiency serves the round trip, standby and loss figures of
// every measurement chain: GET /api/v1/energy/efficiency.
// efficiencyBody is the answer of GET /api/v1/energy/efficiency.
// LearningDisabled is true while the learning of the chain monitor is off.
type efficiencyBody struct {
	energystats.EfficiencyResponse
	LearningDisabled bool `json:"learning_disabled"`
}

// learningOn may be nil, then learning counts as on.
func handleEnergyEfficiency(service *energystats.Service, learningOn func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if service == nil {
			writeError(w, http.StatusNotImplemented, "energy_stats_unavailable", "Die Energiestatistik ist ohne Datenverzeichnis nicht verfügbar")
			return
		}
		response, err := service.Efficiency()
		if err != nil {
			writeErrorDetail(w, http.StatusInternalServerError, "energy_stats_failed", err)
			return
		}
		body := efficiencyBody{EfficiencyResponse: response}
		if learningOn != nil {
			body.LearningDisabled = !learningOn()
		}
		writeJSON(w, body)
	}
}

// learningOnFunc returns the learning switch of the chain monitor, nil without one.
func learningOnFunc(monitor interface{ LearningOn() bool }) func() bool {
	if monitor == nil {
		return nil
	}
	return monitor.LearningOn
}
