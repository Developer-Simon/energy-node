package httpapi

import (
	"net/http"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// switchView is one commandable switch entity with its resolved on/off state.
// The device map reads this list to show which outputs are switched on.
type switchView struct {
	DeviceID string `json:"device_id"`
	EntityID string `json:"entity_id"`
	Name     string `json:"name"`
	State    string `json:"state"`
}

// handleDeviceSwitches serves GET /api/v1/devices/switches. Only switches that
// can be commanded and are not diagnostic or config entities are listed. The
// state follows the rules in the device map spec, section "state"; see
// registry.EntityView.SwitchState for the details. Pending commands do not
// change the state.
func handleDeviceSwitches(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		switches := []switchView{}
		for _, device := range reg.Snapshot() {
			for _, entity := range device.Entities {
				if entity.Component != "switch" || !entity.Commandable || entity.EntityCategory != "" {
					continue
				}
				switches = append(switches, switchView{
					DeviceID: device.ID,
					EntityID: entity.UniqueID,
					Name:     entity.Name,
					State:    entity.SwitchState(),
				})
			}
		}
		sort.SliceStable(switches, func(i, j int) bool {
			if switches[i].DeviceID != switches[j].DeviceID {
				return switches[i].DeviceID < switches[j].DeviceID
			}
			return switches[i].EntityID < switches[j].EntityID
		})
		writeJSON(w, switches)
	}
}
