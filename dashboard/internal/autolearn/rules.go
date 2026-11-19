// Package autolearn reads the automation rule document and defines the
// findings that the rule check and the learning steps report on it.
package autolearn

import (
	"encoding/json"
	"fmt"
)

// Doc is the parsed automation rule document: the rules and the scenarios.
type Doc struct {
	Rules     []Rule
	Scenarios []Scenario
}

// Rule is one automation rule with its conditions and actions.
type Rule struct {
	ID              string
	Name            string
	Enabled         bool
	Conditions      []Condition
	Actions         []Action
	CooldownSeconds float64
	RepeatSeconds   float64
}

// Condition is one condition of a rule. Threshold is read from threshold of a
// balance_threshold, Value from value of a topic_value or entity_value. Raw
// keeps the original JSON of the condition.
type Condition struct {
	Type       string
	Field      string
	Comparison string
	Topic      string
	EntityID   string
	Threshold  float64
	Hysteresis float64
	Value      *float64
	Raw        json.RawMessage
}

// Action is one action of a rule, a publish or a notification.
type Action struct {
	Type          string
	Topic         string
	PayloadSource string
	Payload       string
	EntityID      string
}

// Scenario is a regulator scenario: its topics are the topics of the
// on_start, while and on_end actions, RegulatorTopic is the regulator output.
type Scenario struct {
	ID             string
	Name           string
	Enabled        bool
	RegulatorTopic string
	ActionTopics   []string
}

// wireDoc mirrors the JSON of the rule document. Unknown fields are ignored.
type wireDoc struct {
	Rules     []wireRule     `json:"rules"`
	Scenarios []wireScenario `json:"scenarios"`
}

type wireRule struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	CooldownSeconds float64           `json:"cooldown_seconds"`
	RepeatSeconds   float64           `json:"repeat_seconds"`
	Conditions      []json.RawMessage `json:"conditions"`
	Actions         []wireAction      `json:"actions"`
}

type wireCondition struct {
	Type       string   `json:"type"`
	Field      string   `json:"field"`
	Comparison string   `json:"comparison"`
	Topic      string   `json:"topic"`
	EntityID   string   `json:"entity_id"`
	Threshold  float64  `json:"threshold"`
	Hysteresis float64  `json:"hysteresis"`
	Value      *float64 `json:"value"`
}

type wireAction struct {
	Type          string `json:"type"`
	Topic         string `json:"topic"`
	PayloadSource string `json:"payload_source"`
	Payload       string `json:"payload"`
	EntityID      string `json:"entity_id"`
}

type wireScenario struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	OnStart wirePhase `json:"on_start"`
	While   wireWhile `json:"while"`
	OnEnd   wirePhase `json:"on_end"`
}

type wirePhase struct {
	Actions []wireAction `json:"actions"`
}

type wireWhile struct {
	Actions   []wireAction `json:"actions"`
	Regulator *struct {
		Output struct {
			Topic string `json:"topic"`
		} `json:"output"`
	} `json:"regulator"`
}

// ParseDoc parses the rule document. Unknown fields are ignored.
func ParseDoc(raw []byte) (Doc, error) {
	var wire wireDoc
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Doc{}, fmt.Errorf("parse rule document: %w", err)
	}
	var doc Doc
	for _, wr := range wire.Rules {
		rule := Rule{ID: wr.ID, Name: wr.Name, Enabled: wr.Enabled,
			CooldownSeconds: wr.CooldownSeconds, RepeatSeconds: wr.RepeatSeconds}
		for _, rawCond := range wr.Conditions {
			var wc wireCondition
			if err := json.Unmarshal(rawCond, &wc); err != nil {
				return Doc{}, fmt.Errorf("rule %s condition: %w", wr.ID, err)
			}
			rule.Conditions = append(rule.Conditions, Condition{
				Type: wc.Type, Field: wc.Field, Comparison: wc.Comparison, Topic: wc.Topic,
				EntityID: wc.EntityID, Threshold: wc.Threshold, Hysteresis: wc.Hysteresis,
				Value: wc.Value, Raw: append(json.RawMessage(nil), rawCond...),
			})
		}
		for _, wa := range wr.Actions {
			rule.Actions = append(rule.Actions, toAction(wa))
		}
		doc.Rules = append(doc.Rules, rule)
	}
	for _, ws := range wire.Scenarios {
		scenario := Scenario{ID: ws.ID, Name: ws.Name, Enabled: ws.Enabled}
		if ws.While.Regulator != nil {
			scenario.RegulatorTopic = ws.While.Regulator.Output.Topic
		}
		var actions []wireAction
		actions = append(actions, ws.OnStart.Actions...)
		actions = append(actions, ws.While.Actions...)
		actions = append(actions, ws.OnEnd.Actions...)
		for _, wa := range actions {
			if wa.Topic != "" {
				scenario.ActionTopics = append(scenario.ActionTopics, wa.Topic)
			}
		}
		doc.Scenarios = append(doc.Scenarios, scenario)
	}
	return doc, nil
}

func toAction(wa wireAction) Action {
	return Action{Type: wa.Type, Topic: wa.Topic, PayloadSource: wa.PayloadSource,
		Payload: wa.Payload, EntityID: wa.EntityID}
}

// ValueKey names the value a condition reads, so that conditions on the same
// value can be compared. It is empty for conditions without a number.
func (c Condition) ValueKey() string {
	switch {
	case c.Type == "balance_threshold":
		return "balance:" + c.Field
	case (c.Type == "entity_value" || c.Type == "topic_value") && c.EntityID != "":
		return "entity:" + c.EntityID
	case c.Topic != "":
		return "topic:" + c.Topic
	default:
		return ""
	}
}
