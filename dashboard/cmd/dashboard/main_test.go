package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestServiceIDForConfig(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
		wantOK bool
	}{
		{name: "automation_rules", input: "automation_rules", wantID: "automation", wantOK: true},
		{name: "shelly_devices", input: "shelly_devices", wantID: "shelly", wantOK: true},
		{name: "unknown_name", input: "unknown_name", wantID: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := serviceIDForConfig(tt.input)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("serviceIDForConfig(%q) = (%q, %v), want (%q, %v)", tt.input, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestBuildBalancePayloadContainsBalanceAndInterpretation(t *testing.T) {
	reg := registry.New()
	resolver := energy.NewResolver(nil)
	payload, err := buildBalancePayload(reg, resolver, time.Unix(1754730000, 0))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["at"].(float64) != 1754730000 {
		t.Fatalf("at = %v", decoded["at"])
	}
	balance := decoded["balance"].(map[string]any)
	if _, ok := balance["load_total"]; !ok {
		t.Fatal("balance.load_total missing")
	}
	interp := decoded["interpretation"].(map[string]any)
	if interp["gap_mode"] != "unknown_consumer" {
		t.Fatalf("interpretation.gap_mode = %v", interp["gap_mode"])
	}
}
