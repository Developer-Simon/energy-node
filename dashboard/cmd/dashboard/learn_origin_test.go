package main

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestSwitchOfPayload(t *testing.T) {
	info := registry.CommandInfo{Component: "switch", PayloadOn: "ON", PayloadOff: "OFF"}
	cases := []struct {
		name    string
		info    registry.CommandInfo
		payload string
		on, ok  bool
	}{
		{"configured on", info, "ON", true, true},
		{"configured off", info, "OFF", false, true},
		{"generic on", info, "true", true, true},
		{"generic off", info, "0", false, true},
		{"unknown payload", info, "toggle", false, false},
		{"sensor component", registry.CommandInfo{Component: "number", PayloadOn: "ON"}, "ON", false, false},
	}
	for _, tc := range cases {
		on, ok := switchOfPayload(tc.info, tc.payload)
		if on != tc.on || ok != tc.ok {
			t.Errorf("%s: got on=%v ok=%v, want on=%v ok=%v", tc.name, on, ok, tc.on, tc.ok)
		}
	}
}
