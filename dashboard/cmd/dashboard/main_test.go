package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestValidateNodeDeviceID(t *testing.T) {
	if err := validateNodeDeviceID("energy_node", "/etc/energy-node/config.json"); err != nil {
		t.Fatalf("energy_node must be accepted, got %v", err)
	}
	for _, bad := range []string{"energy-node", "", "node", "Energy Node"} {
		err := validateNodeDeviceID(bad, "/etc/energy-node/config.json")
		if err == nil {
			t.Fatalf("validateNodeDeviceID(%q) = nil, want an error", bad)
		}
		if !strings.Contains(err.Error(), "energy_node") || !strings.Contains(err.Error(), "/etc/energy-node/config.json") {
			t.Fatalf("error for %q lacks the id or the config path: %v", bad, err)
		}
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
	payload, _, err := buildBalancePayload(reg, resolver, time.Unix(1754730000, 0))
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

func TestBuildBalancePayloadContainsCategories(t *testing.T) {
	reg := registry.New()
	resolver := energy.NewResolver(nil)
	payload, _, err := buildBalancePayload(reg, resolver, time.Unix(1754730000, 0))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Categories map[string]float64 `json:"categories"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Categories == nil {
		t.Fatal("categories missing from balance payload")
	}
}

func TestDesiredOptionalSensorsFollowsSourcesAndCategories(t *testing.T) {
	snapshot := energy.Snapshot{
		Sources: map[energy.Role][]string{
			energy.RoleWallbox:          {"sensor.wallbox"},
			energy.Role("custom:sauna"): {"sensor.sauna"},
		},
		Categories: map[string]energy.Category{
			"sauna": {Label: "Sauna", Base: energy.CategoryConsumer},
			"leer":  {Label: "Leer", Base: energy.CategoryConsumer},
		},
	}
	var keys []string
	for _, s := range desiredOptionalSensors(snapshot) {
		keys = append(keys, s.Key)
	}
	if strings.Join(keys, ",") != "wallbox,custom_sauna" {
		t.Fatalf("keys = %v", keys)
	}
}
