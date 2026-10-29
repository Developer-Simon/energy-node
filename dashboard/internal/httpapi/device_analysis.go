package httpapi

import (
	"net/http"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// canViewDeviceAnalysis reports whether the request may see the analysis data
// of a device: discovery payloads, the last MQTT message and the command
// history, plus the diagnostics warnings that are not operational. Without a
// user in the context (authentication disabled) everything is visible.
func canViewDeviceAnalysis(r *http.Request) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return true
	}
	return auth.HasRole(user, auth.RoleViewDeviceAnalysis)
}

// stripDeviceAnalysis returns a copy of dev without discovery payloads and
// with each entity's last message reduced to its timestamp. The entity slice
// is copied, so the registry snapshot the caller holds stays untouched.
func stripDeviceAnalysis(dev registry.DeviceView) registry.DeviceView {
	out := dev
	out.DiscoveryJSON = nil
	out.Entities = make([]registry.EntityView, len(dev.Entities))
	copy(out.Entities, dev.Entities)
	for i := range out.Entities {
		entity := &out.Entities[i]
		entity.DiscoveryJSON = ""
		if entity.LastMessage != nil {
			entity.LastMessage = &registry.MQTTMessage{At: entity.LastMessage.At}
		}
	}
	return out
}

// operationalWarnings keeps only the warnings about the running system
// (device offline, no state update). The result is never nil so that it
// serialises as an empty list.
func operationalWarnings(in []diagnostics.Warning) []diagnostics.Warning {
	out := make([]diagnostics.Warning, 0, len(in))
	for _, warning := range in {
		if warning.Operational {
			out = append(out, warning)
		}
	}
	return out
}
