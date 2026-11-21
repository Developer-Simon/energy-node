// Package autolearn reads the automation rule document and defines the
// findings that the rule check and the learning steps report on it.
package autolearn

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// Doc is the parsed automation rule document: the rules, the scenarios and the
// site location. Location is nil when the document has no coordinates.
type Doc struct {
	Rules     []Rule
	Scenarios []Scenario
	Location  *sun.Location
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
// balance_threshold, Value from value of a topic_value or entity_value,
// HoldSeconds from hold_seconds. A field that holds a learned value is not a
// number: it stays 0 or nil here and is listed in Learned under its field name.
// Raw keeps the original JSON of the condition.
type Condition struct {
	Type        string
	Field       string
	Comparison  string
	Topic       string
	EntityID    string
	Threshold   float64
	Hysteresis  float64
	Value       *float64
	HoldSeconds *float64
	Learned     map[string]LearnedRef
	Raw         json.RawMessage
}

// LearnedRef is a learned value a condition field reads instead of a number.
// Key is the learned key, such as site/day_start. Min, Max and Fallback are
// numbers, or "HH:MM" texts for clock times. Offset is added to the resolved
// value. MaxAgeS is the age in seconds after which the value counts as missing,
// 0 when the rule does not set it.
type LearnedRef struct {
	Key      string
	Min      any
	Max      any
	Fallback any
	Offset   float64
	MaxAgeS  float64
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
	// Regulator is nil when the scenario has no regulator block.
	Regulator    *RegulatorSpec
	ActionTopics []string
}

// RegulatorSpec is the part of a scenario regulator that the learning steps
// read. Target is the balance field the regulator reads (base.field), empty
// when the base is not a balance field. DeadbandW is the regulator deadband
// (deadband_w, default 20). MaxW is the upper end of the output range (output.max).
type RegulatorSpec struct {
	Target    string
	DeadbandW float64
	MaxW      float64
}

// wireDoc mirrors the JSON of the rule document. Unknown fields are ignored.
type wireDoc struct {
	Rules     []wireRule     `json:"rules"`
	Scenarios []wireScenario `json:"scenarios"`
	Settings  wireSettings   `json:"settings"`
}

// wireSettings holds the site coordinates. A nil field is missing.
type wireSettings struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

// location returns the site location. It is nil when either coordinate is
// missing or when both are zero, the state of an unset location.
func (w wireSettings) location() *sun.Location {
	if w.Latitude == nil || w.Longitude == nil {
		return nil
	}
	if *w.Latitude == 0 && *w.Longitude == 0 {
		return nil
	}
	return &sun.Location{Lat: *w.Latitude, Lon: *w.Longitude}
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

// wireCondition keeps the fields that may hold a learned reference as raw
// JSON, since a reference is an object and not a number.
type wireCondition struct {
	Type        string          `json:"type"`
	Field       string          `json:"field"`
	Comparison  string          `json:"comparison"`
	Topic       string          `json:"topic"`
	EntityID    string          `json:"entity_id"`
	Threshold   json.RawMessage `json:"threshold"`
	Hysteresis  float64         `json:"hysteresis"`
	Value       json.RawMessage `json:"value"`
	Start       json.RawMessage `json:"start"`
	End         json.RawMessage `json:"end"`
	HoldSeconds json.RawMessage `json:"hold_seconds"`
}

// wireLearnedRef mirrors the JSON of a learned reference.
type wireLearnedRef struct {
	Learned  string  `json:"learned"`
	Min      any     `json:"min"`
	Max      any     `json:"max"`
	Fallback any     `json:"fallback"`
	Offset   float64 `json:"offset"`
	MaxAgeS  float64 `json:"max_age_s"`
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
	Actions   []wireAction   `json:"actions"`
	Regulator *wireRegulator `json:"regulator"`
}

// wireRegulator mirrors the regulator block of a while phase. Only the fields
// the learning steps read are listed.
type wireRegulator struct {
	Output struct {
		Topic string  `json:"topic"`
		Max   float64 `json:"max"`
	} `json:"output"`
	DeadbandW *float64 `json:"deadband_w"`
	Base      struct {
		Source string `json:"source"`
		Field  string `json:"field"`
	} `json:"base"`
}

// ParseDoc parses the rule document. Unknown fields are ignored.
func ParseDoc(raw []byte) (Doc, error) {
	var wire wireDoc
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Doc{}, fmt.Errorf("parse rule document: %w", err)
	}
	doc := Doc{Location: wire.Settings.location()}
	for _, wr := range wire.Rules {
		rule := Rule{ID: wr.ID, Name: wr.Name, Enabled: wr.Enabled,
			CooldownSeconds: wr.CooldownSeconds, RepeatSeconds: wr.RepeatSeconds}
		for _, rawCond := range wr.Conditions {
			condition, err := parseCondition(rawCond)
			if err != nil {
				return Doc{}, fmt.Errorf("rule %s condition: %w", wr.ID, err)
			}
			rule.Conditions = append(rule.Conditions, condition)
		}
		for _, wa := range wr.Actions {
			rule.Actions = append(rule.Actions, toAction(wa))
		}
		doc.Rules = append(doc.Rules, rule)
	}
	for _, ws := range wire.Scenarios {
		scenario := Scenario{ID: ws.ID, Name: ws.Name, Enabled: ws.Enabled}
		if wr := ws.While.Regulator; wr != nil {
			scenario.RegulatorTopic = wr.Output.Topic
			scenario.Regulator = wr.spec()
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

// spec returns the learning view of the regulator block. A missing deadband_w
// takes the schema default of 20 W.
func (wr *wireRegulator) spec() *RegulatorSpec {
	spec := &RegulatorSpec{DeadbandW: 20, MaxW: wr.Output.Max}
	if wr.DeadbandW != nil {
		spec.DeadbandW = *wr.DeadbandW
	}
	if wr.Base.Source == "balance" {
		spec.Target = wr.Base.Field
	}
	return spec
}

// parseCondition reads one condition. A numeric field becomes its number, a
// learned reference is recorded in Learned.
func parseCondition(rawCond json.RawMessage) (Condition, error) {
	var wc wireCondition
	if err := json.Unmarshal(rawCond, &wc); err != nil {
		return Condition{}, err
	}
	learned := map[string]LearnedRef{}
	threshold, err := number(wc.Threshold, "threshold", learned)
	if err != nil {
		return Condition{}, err
	}
	value, err := number(wc.Value, "value", learned)
	if err != nil {
		return Condition{}, err
	}
	holdSeconds, err := number(wc.HoldSeconds, "hold_seconds", learned)
	if err != nil {
		return Condition{}, err
	}
	if _, err := learnedRef(wc.Start, "start", learned); err != nil {
		return Condition{}, err
	}
	if _, err := learnedRef(wc.End, "end", learned); err != nil {
		return Condition{}, err
	}
	condition := Condition{
		Type: wc.Type, Field: wc.Field, Comparison: wc.Comparison, Topic: wc.Topic,
		EntityID: wc.EntityID, Hysteresis: wc.Hysteresis, Value: value, HoldSeconds: holdSeconds,
		Raw: append(json.RawMessage(nil), rawCond...),
	}
	if threshold != nil {
		condition.Threshold = *threshold
	}
	if len(learned) > 0 {
		condition.Learned = learned
	}
	return condition, nil
}

// number reads a field that holds a number. It returns nil when the field is
// absent, null or a learned reference, and then records the reference.
func number(raw json.RawMessage, name string, learned map[string]LearnedRef) (*float64, error) {
	if isRef, err := learnedRef(raw, name, learned); err != nil || isRef {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	var n float64
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return nil, fmt.Errorf("%s must be a number or a learned value", name)
	}
	return &n, nil
}

// learnedRef records raw under name when it is a learned reference and reports
// whether it was one.
// An object is a reference only with a non empty text under learned, as the
// service's is_ref reads it. Any other object is not stored, so the numeric
// fields reject it and start and end ignore it.
func learnedRef(raw json.RawMessage, name string, learned map[string]LearnedRef) (bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false, nil
	}
	var ref wireLearnedRef
	if err := json.Unmarshal(trimmed, &ref); err != nil || ref.Learned == "" {
		return false, nil
	}
	learned[name] = LearnedRef{Key: ref.Learned, Min: ref.Min, Max: ref.Max, Fallback: ref.Fallback,
		Offset: ref.Offset, MaxAgeS: ref.MaxAgeS}
	return true, nil
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
