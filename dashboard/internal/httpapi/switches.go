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
// switchState for the details. Pending commands do not change the state.
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
					State:    switchState(entity),
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

// switchState returns "on", "off" or "unknown" for a switch entity. The
// comparison uses the exact payloads of the entity. Value is the last
// confirmed state, so Pending is ignored, and a stale retained value still
// counts, because for a switch the retained state is the real one.
//
// The state_on / state_off fields of Home Assistant discovery are not kept in
// EntityInfo yet. Until they are, an entity that uses other payloads reports
// "unknown" here. When such a device shows up, add those fields to the
// registry and compare against them.
func switchState(entity registry.EntityView) string {
	if entity.HasAvailability && !entity.Available {
		return "unknown"
	}
	if !entity.HasValue {
		return "unknown"
	}
	if entity.PayloadOn != "" && entity.Value == entity.PayloadOn {
		return "on"
	}
	if entity.PayloadOff != "" && entity.Value == entity.PayloadOff {
		return "off"
	}
	return "unknown"
}
