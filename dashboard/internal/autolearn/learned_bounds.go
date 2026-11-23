package autolearn

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// learnedBoundsDocument is the name of the rule document in the configuration.
const learnedBoundsDocument = "automation_rules"

// boundsDayMinutes is the length of a day in minutes, the circle of a clock.
const boundsDayMinutes = 1440

var boundsClockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// CheckLearnedBoundsOnSave refuses a rule document whose learned bounds the
// automation service would reject. Other documents pass unchecked.
func CheckLearnedBoundsOnSave(name string, data []byte) error {
	if name != learnedBoundsDocument {
		return nil
	}
	if errs := ValidateLearnedBounds(data); len(errs) > 0 {
		return fmt.Errorf("learned bounds: %s", strings.Join(errs, ", "))
	}
	return nil
}

// ValidateLearnedBounds finds every learned reference in the rule document raw
// and reports where min is above max or where fallback lies outside min and
// max, with the same texts as the service. Time bounds run over midnight, so
// min to max is an arc of the clock. A reference is any object with a learned
// key. A reference whose bounds have the wrong type is left to the service.
func ValidateLearnedBounds(raw []byte) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	var errs []string
	walkLearnedBounds(doc, "", &errs)
	return errs
}

func walkLearnedBounds(node any, path string, errs *[]string) {
	switch typed := node.(type) {
	case map[string]any:
		if _, ok := typed["learned"]; ok {
			checkLearnedBounds(typed, path, errs)
			return
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := key
			if path != "" {
				child = path + "." + key
			}
			walkLearnedBounds(typed[key], child, errs)
		}
	case []any:
		for i, item := range typed {
			walkLearnedBounds(item, fmt.Sprintf("%s[%d]", path, i), errs)
		}
	}
}

// checkLearnedBounds checks one reference. Text bounds are clock times, number
// bounds are numbers, and mixed types are not checked here.
func checkLearnedBounds(ref map[string]any, path string, errs *[]string) {
	add := func(msg string) { *errs = append(*errs, path+": "+msg) }
	if _, isText := ref["min"].(string); isText {
		low, okLow := clockMinutes(ref["min"])
		high, okHigh := clockMinutes(ref["max"])
		fallback, okFallback := clockMinutes(ref["fallback"])
		if okLow && okHigh && okFallback && dayMod(fallback-low) > dayMod(high-low) {
			add("fallback must lie between min and max")
		}
		return
	}
	low, okLow := ref["min"].(float64)
	high, okHigh := ref["max"].(float64)
	fallback, okFallback := ref["fallback"].(float64)
	if !okLow || !okHigh || !okFallback {
		return
	}
	switch {
	case low > high:
		add("min must not be above max")
	case fallback < low || fallback > high:
		add("fallback must lie between min and max")
	}
}

// clockMinutes reads an HH:MM text as minutes after midnight.
func clockMinutes(value any) (int, bool) {
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	match := boundsClockRE.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	return hours*60 + minutes, true
}

// dayMod maps minutes onto the clock circle, 0 to 1439.
func dayMod(minutes int) int {
	return ((minutes % boundsDayMinutes) + boundsDayMinutes) % boundsDayMinutes
}
