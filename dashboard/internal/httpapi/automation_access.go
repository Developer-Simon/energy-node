package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
)

// automationToggleKeys are the arrays of automation_rules whose entries may be
// switched on or off with operate_automations alone.
var automationToggleKeys = []string{"rules", "scenarios"}

// isAutomationRulesRecovery reports whether parts names a restore or reload of
// automation_rules. Those need automations, not edit_config.
func isAutomationRulesRecovery(r *http.Request, parts []string) bool {
	return len(parts) == 2 && parts[0] == "automation_rules" && r.Method == http.MethodPost && (parts[1] == "restore" || parts[1] == "reload")
}

// onlyEnabledDiffers reports whether incoming differs from stored only in the
// enabled flag of the entries of rules and scenarios. Entries are compared by
// position, so the arrays must have the same length and order. Invalid JSON
// yields false.
func onlyEnabledDiffers(stored, incoming []byte) bool {
	var before, after any
	if json.Unmarshal(stored, &before) != nil || json.Unmarshal(incoming, &after) != nil {
		return false
	}
	beforeMap, ok := before.(map[string]any)
	if !ok {
		return false
	}
	afterMap, ok := after.(map[string]any)
	if !ok || len(beforeMap) != len(afterMap) {
		return false
	}
	for key, value := range beforeMap {
		other, ok := afterMap[key]
		if !ok {
			return false
		}
		if slices.Contains(automationToggleKeys, key) {
			if !sameExceptEnabled(value, other) {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(value, other) {
			return false
		}
	}
	return true
}

// sameExceptEnabled compares two JSON arrays element by element, ignoring the
// enabled key of objects.
func sameExceptEnabled(stored, incoming any) bool {
	storedList, ok := stored.([]any)
	if !ok {
		return false
	}
	incomingList, ok := incoming.([]any)
	if !ok || len(storedList) != len(incomingList) {
		return false
	}
	for i := range storedList {
		if !reflect.DeepEqual(withoutEnabled(storedList[i]), withoutEnabled(incomingList[i])) {
			return false
		}
	}
	return true
}

// withoutEnabled returns a copy of value without the enabled key when value is
// an object, and value unchanged otherwise.
func withoutEnabled(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	copied := make(map[string]any, len(object))
	for key, entry := range object {
		if key != "enabled" {
			copied[key] = entry
		}
	}
	return copied
}
