package diagnostics

import (
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type chainRuleKind struct {
	Kind   string
	RuleID string
	Key    string
}

// ChainRule reports the learned model's findings of one kind as warnings.
// Findings returns every kind, the rule keeps only its own.
type ChainRule struct {
	Kind     string
	Findings func() []energy.ChainFinding
}

func (rule ChainRule) entry() (chainRuleKind, bool) {
	for _, entry := range chainRuleKinds {
		if entry.Kind == rule.Kind {
			return entry, true
		}
	}
	return chainRuleKind{}, false
}

func (rule ChainRule) RuleID() string {
	entry, _ := rule.entry()
	return entry.RuleID
}

func (rule ChainRule) Evaluate(snapshot registry.DiagnosticsSnapshot, ctx SnapshotContext) []Warning {
	entry, ok := rule.entry()
	if !ok || rule.Findings == nil {
		return nil
	}
	entities := make(map[string]registry.EntityView)
	deviceOf := make(map[string]string)
	for _, device := range snapshot.Devices {
		for _, entity := range device.Entities {
			if _, seen := entities[entity.UniqueID]; seen {
				continue
			}
			entities[entity.UniqueID] = entity
			deviceOf[entity.UniqueID] = device.ID
		}
	}
	text := ruleTexts[entry.Key]
	var result []Warning
	for _, finding := range rule.Findings() {
		if finding.Kind != rule.Kind {
			continue
		}
		entityID := chainFindingEntity(finding)
		topic := entities[entityID].StateTopic
		result = append(result, Warning{
			RuleID:   entry.RuleID,
			Key:      entry.Key,
			Severity: SeverityWarning,
			DeviceID: deviceOf[entityID],
			EntityID: entityID,
			Topic:    topic,
			Message:  text.Message,
			Hint:     text.Hint,
			Details: map[string]any{
				"chain":    finding.ChainLabel,
				"chain_id": finding.ChainID,
				"from":     finding.From,
				"to":       finding.To,
				"since":    finding.Since,
			},
			At: finding.Since,
		})
	}
	return result
}

// chainFindingEntity names the measuring point a finding is about: the
// suggestion's entity when there is one, otherwise the point the finding
// flags. Frozen names its frozen point in From, the other kinds the
// downstream point in To, falling back to From.
func chainFindingEntity(finding energy.ChainFinding) string {
	if finding.Suggestion != nil && finding.Suggestion.EntityID != "" {
		return finding.Suggestion.EntityID
	}
	switch finding.Kind {
	case energy.FindingDrift, energy.FindingContradiction:
		if finding.To != "" {
			return finding.To
		}
	}
	return finding.From
}
