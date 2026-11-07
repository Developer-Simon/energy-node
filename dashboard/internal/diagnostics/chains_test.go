package diagnostics

import (
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func chainTestRegistry() *registry.Registry {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "plug", Name: "Plug"}, Entity: registry.EntityInfo{UniqueID: "plug_power", Component: "sensor", StateTopic: "state/plug_power", UnitOfMeasurement: "W"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "plug", Name: "Plug"}, Entity: registry.EntityInfo{UniqueID: "plug_relay", Component: "switch", StateTopic: "state/plug_relay"}})
	reg.UpsertEntity(registry.Discovery{Device: registry.DeviceInfo{ID: "other", Name: "Other"}, Entity: registry.EntityInfo{UniqueID: "other_power", Component: "sensor", StateTopic: "state/other_power", UnitOfMeasurement: "W"}})
	return reg
}

func chainTestEngine(findings ...energy.ChainFinding) *Engine {
	engine := NewEngine(chainTestRegistry(), nil)
	engine.SetChainFindings(func() []energy.ChainFinding { return findings })
	return engine
}

func chainWarnings(warnings []Warning) []Warning {
	var result []Warning
	for _, item := range warnings {
		if strings.HasPrefix(item.RuleID, "Chain") {
			result = append(result, item)
		}
	}
	return result
}

func TestChainWarningsPerKind(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		finding energy.ChainFinding
		ruleID  string
		key     string
		entity  string
		device  string
	}{
		{"drift", energy.ChainFinding{Kind: energy.FindingDrift, From: "meter", To: "plug_power"}, "ChainDrift", "chain_drift", "plug_power", "plug"},
		{"frozen", energy.ChainFinding{Kind: energy.FindingFrozen, From: "plug_power", To: "other_power"}, "ChainFrozen", "chain_frozen", "plug_power", "plug"},
		{"contradiction", energy.ChainFinding{Kind: energy.FindingContradiction, From: "meter", To: "plug_power"}, "ChainContradiction", "chain_contradiction", "plug_power", "plug"},
		{"sign with suggestion", energy.ChainFinding{Kind: energy.FindingSign, From: "meter", To: "other_power", Suggestion: &energy.ChainSuggestion{EntityID: "plug_power", Invert: true}}, "ChainSign", "chain_sign", "plug_power", "plug"},
		{"unit without suggestion", energy.ChainFinding{Kind: energy.FindingUnit, From: "other_power", To: "meter"}, "ChainUnit", "chain_unit", "other_power", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			finding := tc.finding
			finding.ChainID = "chain-1"
			finding.ChainLabel = "Plug chain"
			finding.Since = since
			warnings := chainWarnings(chainTestEngine(finding).Evaluate(since.Add(time.Hour)))
			if len(warnings) != 1 {
				t.Fatalf("got %d chain warnings, want 1: %#v", len(warnings), warnings)
			}
			got := warnings[0]
			if got.RuleID != tc.ruleID || got.Key != tc.key || got.Severity != SeverityWarning {
				t.Fatalf("got rule %q key %q severity %q", got.RuleID, got.Key, got.Severity)
			}
			if got.DeviceID != tc.device || got.EntityID != tc.entity {
				t.Fatalf("got device %q entity %q, want %q %q", got.DeviceID, got.EntityID, tc.device, tc.entity)
			}
			if got.Message != ruleTexts[tc.key].Message || got.Hint != ruleTexts[tc.key].Hint {
				t.Fatalf("texts %q %q do not come from ruleTexts", got.Message, got.Hint)
			}
			if got.Details["chain"] != "Plug chain" || got.Details["chain_id"] != "chain-1" || got.Details["from"] != tc.finding.From || got.Details["to"] != tc.finding.To {
				t.Fatalf("details %#v", got.Details)
			}
			if since, ok := got.Details["since"].(time.Time); !ok || !since.Equal(finding.Since) {
				t.Fatalf("details since %#v", got.Details["since"])
			}
			if !got.At.Equal(finding.Since) {
				t.Fatalf("At = %v, want %v", got.At, finding.Since)
			}
		})
	}
}

func TestChainWarningsTwoKindsOnOneEntitySurvive(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	engine := chainTestEngine(
		energy.ChainFinding{Kind: energy.FindingFrozen, ChainID: "c", From: "plug_power", Since: since},
		energy.ChainFinding{Kind: energy.FindingSign, ChainID: "c", From: "plug_power", To: "meter", Since: since},
	)
	warnings := chainWarnings(engine.Evaluate(since))
	if len(warnings) != 2 {
		t.Fatalf("got %d chain warnings, want 2: %#v", len(warnings), warnings)
	}
}

func TestChainWarningsResolveDeviceOfUnknownEntity(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	warnings := chainWarnings(chainTestEngine(energy.ChainFinding{Kind: energy.FindingFrozen, ChainID: "c", From: "ghost", Since: since}).Evaluate(since))
	if len(warnings) != 1 {
		t.Fatalf("got %d chain warnings, want 1", len(warnings))
	}
	if warnings[0].DeviceID != "" || warnings[0].EntityID != "ghost" {
		t.Fatalf("got device %q entity %q", warnings[0].DeviceID, warnings[0].EntityID)
	}
}

func TestEngineWithoutChainSourceHasNoChainRules(t *testing.T) {
	engine := NewEngine(chainTestRegistry(), nil)
	for _, ruleID := range engine.RuleIDs() {
		if strings.HasPrefix(ruleID, "Chain") {
			t.Fatalf("RuleIDs lists %q without a chain source", ruleID)
		}
	}
	if warnings := chainWarnings(engine.Evaluate(time.Now().UTC())); len(warnings) != 0 {
		t.Fatalf("got chain warnings without a source: %#v", warnings)
	}
	if warnings := chainWarnings(engine.Device("plug", time.Now().UTC())); len(warnings) != 0 {
		t.Fatalf("Device got chain warnings without a source: %#v", warnings)
	}
}

func TestRuleIDsListChainRules(t *testing.T) {
	ruleIDs := map[string]bool{}
	for _, ruleID := range chainTestEngine().RuleIDs() {
		ruleIDs[ruleID] = true
	}
	for _, ruleID := range []string{"ChainDrift", "ChainFrozen", "ChainContradiction", "ChainSign", "ChainUnit"} {
		if !ruleIDs[ruleID] {
			t.Fatalf("RuleIDs misses %q", ruleID)
		}
	}
}

func TestDeviceWarningsIncludeChainFindings(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	engine := chainTestEngine(energy.ChainFinding{Kind: energy.FindingDrift, ChainID: "c", From: "meter", To: "plug_power", Since: since})
	if warnings := chainWarnings(engine.Device("plug", since)); len(warnings) != 1 {
		t.Fatalf("Device(plug) got %d chain warnings, want 1", len(warnings))
	}
	if warnings := chainWarnings(engine.Device("other", since)); len(warnings) != 0 {
		t.Fatalf("Device(other) got %d chain warnings, want 0", len(warnings))
	}
}

func TestSummaryCountsChainWarnings(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	reg := chainTestRegistry()
	engine := NewEngine(reg, nil)
	base := Summarize(engine.Evaluate(since), engine.Health(since), reg.Snapshot())
	engine.SetChainFindings(func() []energy.ChainFinding {
		return []energy.ChainFinding{{Kind: energy.FindingDrift, ChainID: "c", From: "meter", To: "plug_power", Since: since}}
	})
	warnings := engine.Evaluate(since)
	withChain := Summarize(warnings, engine.Health(since), reg.Snapshot())
	if withChain.Warning != base.Warning+1 {
		t.Fatalf("warning count %d, want %d", withChain.Warning, base.Warning+1)
	}
}

func TestChainWarningsAreNotOperational(t *testing.T) {
	since := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	warnings := chainWarnings(chainTestEngine(energy.ChainFinding{Kind: energy.FindingUnit, ChainID: "c", From: "plug_power", To: "meter", Since: since}).Evaluate(since))
	if len(warnings) != 1 {
		t.Fatalf("got %d chain warnings, want 1", len(warnings))
	}
	if warnings[0].Operational || IsOperationalRule(warnings[0].RuleID) {
		t.Fatal("chain warning counts as operational")
	}
}
