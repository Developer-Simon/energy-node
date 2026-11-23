package autolearn

import (
	"encoding/json"
	"time"
)

// Intent is what a rule or a suggestion switches: the entity, the direction and
// the kind of condition that triggers the switch. An active rule with the same
// entity and trigger as a suggestion already covers it, or contradicts it.
type Intent struct {
	Entity  string
	On      bool
	Trigger string
}

// nightEnd is the clock time before which the end of a midnight window counts
// as night, 08:00 in minutes of the day.
const nightEnd = 8 * 60

// TemplateIntent returns the intent of a suggestion fix built from a known
// template. An unknown template gives false.
func TemplateIntent(fix Fix) (Intent, bool) {
	p := fix.Prefill
	switch fix.Template {
	case "time_point":
		return Intent{Entity: prefillString(p, "entity"), On: prefillBool(p, "on"), Trigger: "time"}, true
	case "runtime_off":
		return Intent{Entity: prefillString(p, "entity"), Trigger: "runtime"}, true
	case "quiet_off":
		return Intent{Entity: prefillString(p, "entity"), Trigger: "quiet"}, true
	case "night_off":
		return Intent{Entity: prefillString(p, "entity"), Trigger: "night"}, true
	case "after_battery_full":
		return Intent{Entity: prefillString(p, "entity"), On: true, Trigger: "battery_full"}, true
	case "follow_switch":
		return Intent{Entity: prefillString(p, "target_entity"), On: prefillBool(p, "target_on"), Trigger: "follow"}, true
	case "shift_start":
		return Intent{Entity: prefillString(p, "entity"), On: true, Trigger: "surplus"}, true
	}
	return Intent{}, false
}

// RuleIntents returns the intents of a rule, one per target entity and trigger.
// Triggers come from the conditions: a time window is "time", or "night" when it
// crosses midnight and ends before 08:00, a sun window is "time", a learned
// quiet state is "quiet", a balance threshold on battery_soc is "battery_full"
// and on grid_export "surplus". An entity_value holding the target itself is
// "runtime" when it has a hold, and an entity_value on another entity is "follow".
// A target without a known payload counts as off, since the direction does not
// decide the gate. Without targets the rule has no intents.
func RuleIntents(rule Rule, targets []Target) []Intent {
	if len(targets) == 0 {
		return nil
	}
	own := make(map[string]bool, len(targets))
	for _, t := range targets {
		own[t.Entity] = true
	}
	var triggers []string
	seen := map[string]bool{}
	for _, c := range rule.Conditions {
		trigger := conditionTrigger(c, own)
		if trigger == "" || seen[trigger] {
			continue
		}
		seen[trigger] = true
		triggers = append(triggers, trigger)
	}
	var out []Intent
	for _, t := range targets {
		if t.Entity == "" {
			continue
		}
		for _, trigger := range triggers {
			out = append(out, Intent{Entity: t.Entity, On: t.On != nil && *t.On, Trigger: trigger})
		}
	}
	return out
}

// conditionTrigger names the trigger a condition gives its rule, or "" for a
// condition that triggers nothing. own holds the entities the rule switches.
func conditionTrigger(c Condition, own map[string]bool) string {
	switch c.Type {
	case "time_window":
		if isNightWindow(c) {
			return "night"
		}
		return "time"
	case "sun_window":
		return "time"
	case "learned_state":
		if rawString(c.Raw, "equals") == "quiet" {
			return "quiet"
		}
	case "balance_threshold":
		switch c.Field {
		case "battery_soc":
			return "battery_full"
		case "grid_export":
			return "surplus"
		}
	case "entity_value":
		if c.EntityID == "" {
			return ""
		}
		if !own[c.EntityID] {
			return "follow"
		}
		_, learnedHold := c.Learned["hold_seconds"]
		if c.HoldSeconds != nil || learnedHold {
			return "runtime"
		}
	}
	return ""
}

// isNightWindow reports a window that crosses midnight and ends before 08:00.
// A bound that is a learned reference counts with its fallback, and a window
// whose bounds cannot be read is no night window.
func isNightWindow(c Condition) bool {
	start, okStart := windowClock(c, "start")
	end, okEnd := windowClock(c, "end")
	return okStart && okEnd && start > end && end < nightEnd
}

// windowClock reads a bound of a time window in minutes of the day.
func windowClock(c Condition, name string) (int, bool) {
	if ref, ok := c.Learned[name]; ok {
		text, _ := ref.Fallback.(string)
		return clockOfText(text)
	}
	var text string
	if err := json.Unmarshal(rawField(c.Raw, name), &text); err != nil {
		return 0, false
	}
	return clockOfText(text)
}

// clockOfText reads "HH:MM" in minutes of the day.
func clockOfText(text string) (int, bool) {
	t, err := time.Parse("15:04", text)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// rawField returns the field name of a raw JSON object, nil when it is absent.
func rawField(raw json.RawMessage, name string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields[name]
}

// rawString returns the text of a field of a raw JSON object, "" when it is not text.
func rawString(raw json.RawMessage, name string) string {
	var text string
	if err := json.Unmarshal(rawField(raw, name), &text); err != nil {
		return ""
	}
	return text
}

func prefillString(prefill map[string]any, key string) string {
	text, _ := prefill[key].(string)
	return text
}

func prefillBool(prefill map[string]any, key string) bool {
	on, _ := prefill[key].(bool)
	return on
}
