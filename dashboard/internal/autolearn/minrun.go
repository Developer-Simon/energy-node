package autolearn

import "github.com/Developer-Simon/energy-node-dashboard/internal/energy"

// minRunForRole is the minimum run time in seconds a device tolerates before it
// may switch off (spec 15.4 "Takten", as a table in Go). A heat pump takes
// 600 s, a wallbox 180 s, a light nothing. Every other switchable load, a heater
// included, takes 300 s, because the device has no heater role in the energy
// model. The longer value is the safe side for a heater.
func minRunForRole(info DeviceInfo) float64 {
	switch {
	case info.Light:
		return 0
	case info.Role == energy.RoleHeatPump:
		return 600
	case info.Role == energy.RoleWallbox:
		return 180
	default:
		return 300
	}
}
