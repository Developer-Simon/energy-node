package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// handleEnergyStats liefert die Energiestatistik eines Tages, einer Woche,
// eines Monats oder Jahres: GET /api/v1/energy/stats?period=day|week|month|year&at=<datum>.
func handleEnergyStats(service *energystats.Service) http.HandlerFunc {
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
		response, err := service.Stats(query.Get("period"), query.Get("at"))
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

// handleEnergyStatsHours liefert Stundenmittel in W fuer die Verlaufsseite:
// GET /api/v1/energy/stats/hours?from=<ms>&to=<ms>.
func handleEnergyStatsHours(service *energystats.Service) http.HandlerFunc {
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
		from, errFrom := strconv.ParseInt(query.Get("from"), 10, 64)
		to, errTo := strconv.ParseInt(query.Get("to"), 10, 64)
		if errFrom != nil || errTo != nil {
			writeError(w, http.StatusBadRequest, "energy_stats_invalid_range", "Zeitraum ist ungültig")
			return
		}
		rows, err := service.HistoryRows(time.UnixMilli(from), time.UnixMilli(to))
		switch {
		case errors.Is(err, energystats.ErrInvalidRange):
			writeError(w, http.StatusBadRequest, "energy_stats_invalid_range", "Zeitraum ist ungültig")
			return
		case err != nil:
			writeErrorDetail(w, http.StatusInternalServerError, "energy_stats_failed", err)
			return
		}
		writeJSON(w, map[string]any{"rows": rows})
	}
}
