package autolearn

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func TestMinRunForRole(t *testing.T) {
	cases := []struct {
		name string
		info DeviceInfo
		want float64
	}{
		{"heat pump", DeviceInfo{Role: energy.RoleHeatPump}, 600},
		{"wallbox", DeviceInfo{Role: energy.RoleWallbox}, 180},
		{"light", DeviceInfo{Light: true}, 0},
		{"heater without role", DeviceInfo{Switchable: true}, 300},
		{"other role", DeviceInfo{Role: energy.RoleLoad}, 300},
	}
	for _, c := range cases {
		if got := minRunForRole(c.info); got != c.want {
			t.Errorf("%s: minRunForRole = %v, want %v", c.name, got, c.want)
		}
	}
}
