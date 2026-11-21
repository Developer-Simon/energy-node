package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
)

// learnedYearWeeks is the most weeks the yearly course of one value lists, and
// learnedYearMonths the span the course covers back from now.
const (
	learnedYearWeeks  = 53
	learnedYearMonths = 12
)

// learnedValueSource is the part of the learned value store the handler uses.
// The store of package autolearn implements it.
type learnedValueSource interface {
	Entries() []autolearn.LearnedEntry
}

// learnedSeasonSource is the part of the season store the handler uses for the
// yearly course of the learned values.
type learnedSeasonSource interface {
	Weeks(from, to string) []autolearn.Week
}

// learnedYearPoint is one week of the yearly course of a learned value. Value is
// a clock text "HH:MM" for time values and a number otherwise.
type learnedYearPoint struct {
	Week  string `json:"week"`
	Value any    `json:"value"`
}

// learnedValueView is one learned value of GET /api/v1/automations/learned. Year
// lists the weeks of the last twelve months in which the value was learned, and
// is left out when there are none.
type learnedValueView struct {
	autolearn.LearnedEntry
	Year []learnedYearPoint `json:"year,omitempty"`
}

// automationLearnedBody is the answer of GET /api/v1/automations/learned.
// Values is never null.
type automationLearnedBody struct {
	Enabled bool               `json:"enabled"`
	Values  []learnedValueView `json:"values"`
}

// handleAutomationLearned serves GET /api/v1/automations/learned. Enabled is
// false when "Muster lernen" or the habit group is off. The values stay in the
// list then, because rules keep using them until they age out. Values of hidden
// or missing devices are left out, site values always stay. Without a source the
// answer is 501. The season source is optional, without it no value has a year.
func handleAutomationLearned(learning *LearningDeps, source learnedValueSource, exists func(device string) bool, season learnedSeasonSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if source == nil {
			writeErrorKey(w, http.StatusNotImplemented, "learning_unavailable", "error.learning_unavailable", nil, "Die Lernfunktionen sind nicht verfügbar")
			return
		}
		body := automationLearnedBody{Enabled: true, Values: []learnedValueView{}}
		if learning != nil && learning.Settings != nil {
			// Without readable settings everything stays on, as for the suggestions.
			if value, err := learning.Settings(); err == nil {
				body.Enabled = !value.AutomationPatternsDisabled && !value.AutomationHabitsDisabled
			}
		}
		var weeks []autolearn.Week
		if season != nil {
			now := time.Now()
			from := isoWeekKeyOf(now.AddDate(0, -learnedYearMonths, 0))
			weeks = season.Weeks(from, isoWeekKeyOf(now))
		}
		for _, entry := range source.Entries() {
			if entry.Device != "" && exists != nil && !exists(entry.Device) {
				continue
			}
			body.Values = append(body.Values, learnedValueView{LearnedEntry: entry, Year: learnedYear(entry, weeks)})
		}
		writeJSON(w, body)
	}
}

// learnedYear lists the weeks of the value, oldest first, at most the last
// learnedYearWeeks of them. Weeks without the value are skipped.
func learnedYear(entry autolearn.LearnedEntry, weeks []autolearn.Week) []learnedYearPoint {
	var out []learnedYearPoint
	for _, w := range weeks {
		minutes, ok := w.Learned[entry.Key]
		if !ok {
			continue
		}
		out = append(out, learnedYearPoint{Week: w.Key, Value: learnedWeekValue(entry.Kind, minutes)})
	}
	if len(out) > learnedYearWeeks {
		out = out[len(out)-learnedYearWeeks:]
	}
	return out
}

// learnedWeekValue turns the weekly median of a value into its answer form. The
// season store keeps clock times as minutes of the day.
func learnedWeekValue(kind string, v float64) any {
	if kind != autolearn.ValueTime {
		return v
	}
	minutes := int(math.Round(v)) % (24 * 60)
	if minutes < 0 {
		minutes += 24 * 60
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// isoWeekKeyOf is the ISO week of t as YYYY-Www.
func isoWeekKeyOf(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}
