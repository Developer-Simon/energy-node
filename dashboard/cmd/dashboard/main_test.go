package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
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

func TestNodeAgentOptionsFromConfig(t *testing.T) {
	cfg, err := appconfig.Load(filepath.Join("..", "..", "..", "services", "energy-node.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := nodeagent.New(nodeagent.Options{
		NodeID:                   cfg.Dashboard.NodeDeviceID,
		NodeName:                 cfg.Dashboard.NodeDeviceName,
		DiscoveryPrefix:          "homeassistant",
		PollIntervalS:            cfg.Dashboard.NodePollIntervalS,
		DiagnosticPollMultiplier: cfg.Dashboard.NodeDiagnosticPollMultiplier,
	})
	if a.PollInterval() != 60*time.Second {
		t.Fatalf("PollInterval = %v, want 60s", a.PollInterval())
	}
	if a.DiagnosticInterval() != 600*time.Second {
		t.Fatalf("DiagnosticInterval = %v, want 600s", a.DiagnosticInterval())
	}
	msgs := a.DiscoveryMessages(nil)
	if len(msgs) == 0 || msgs[0].Topic[:14] != "homeassistant/" {
		t.Fatalf("unexpected discovery messages: %+v", msgs)
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
