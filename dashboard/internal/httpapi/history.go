// dashboard/internal/httpapi/history.go
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// historySample ist ein Messpunkt fuer die Browser-Historie. Value ist ein
// Zeiger, damit ein nicht numerischer oder fehlender Zustand als null
// erscheint statt als 0 - eine 0 waere eine Messwertbehauptung.
type historySample struct {
	EntityID string   `json:"entity_id"`
	Value    *float64 `json:"value"`
	Unit     string   `json:"unit,omitempty"`
	Stale    bool     `json:"stale"`
}

type historyEntitiesResponse struct {
	At      time.Time       `json:"at"`
	Samples []historySample `json:"samples"`
}

// handleHistoryEntities liefert den aktuellen Wert genau der Entitaeten, die
// in den Einstellungen unter history_extra_entities stehen. Die Allowlist
// bleibt bewusst serverseitig: der Browser fragt nicht nach beliebigen IDs,
// sondern holt sich, was konfiguriert ist.
//
// Der Handler ist rein lesend und haelt keinen Zustand - die Historie selbst
// liegt in der IndexedDB des Browsers, nicht hier.
func handleHistoryEntities(reg *registry.Registry, store *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		response := historyEntitiesResponse{At: time.Now().UTC(), Samples: []historySample{}}
		if store == nil {
			writeJSON(w, response)
			return
		}
		value, err := store.LoadSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "settings_invalid", err.Error())
			return
		}
		if len(value.HistoryExtraEntities) == 0 {
			writeJSON(w, response)
			return
		}
		for _, entity := range reg.Values(value.HistoryExtraEntities) {
			sample := historySample{EntityID: entity.UniqueID, Unit: entity.Unit, Stale: entity.Stale}
			if entity.HasValue {
				if parsed, parseErr := strconv.ParseFloat(entity.Value, 64); parseErr == nil {
					sample.Value = &parsed
				}
			}
			response.Samples = append(response.Samples, sample)
		}
		writeJSON(w, response)
	}
}
