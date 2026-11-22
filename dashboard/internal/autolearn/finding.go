package autolearn

import (
	"sort"
	"strings"
)

// Finding is one result of the rule check: what was found, on which rules,
// the evidence behind it and the fixes that can be applied.
type Finding struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Group      string   `json:"group"`
	RuleIDs    []string `json:"rule_ids"`
	Severity   string   `json:"severity"`
	Confidence string   `json:"confidence"`
	Summary    Text     `json:"summary"`
	Evidence   Evidence `json:"evidence"`
	Facts      []Fact   `json:"facts"`
	Apply      []Fix    `json:"apply"`
	Device     string   `json:"device"`
	Season     *Season  `json:"season"`
	Benefit    *Benefit `json:"benefit"`
}

// Season is the period of the year in ISO weeks, from FromWeek to ToWeek. A
// seasonal finding holds only in it.
type Season struct {
	FromWeek int `json:"from_week"`
	ToWeek   int `json:"to_week"`
	// HeatingBelowC is the outdoor temperature below which the heating season
	// holds. Nil when the finding does not depend on the temperature.
	HeatingBelowC *float64 `json:"heating_below_c,omitempty"`
}

// Text is a translatable text: a code plus the parameters of the text.
type Text struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

// Fact is one fact that supports a finding.
type Fact struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// Fix is one change that can be applied to a rule. An empty Ops list only
// opens the rule. Template and Prefill name a template and its parameters for
// a new rule.
type Fix struct {
	Code     string         `json:"code"`
	RuleID   string         `json:"rule_id"`
	Ops      []Op           `json:"ops"`
	Template string         `json:"template,omitempty"`
	Prefill  map[string]any `json:"prefill,omitempty"`
	Rules    []RuleOps      `json:"rules,omitempty"`
}

// RuleOps is the ops of one rule. A fix that changes several rules at once
// carries one RuleOps per rule in Fix.Rules. The browser applies all of them
// or none.
type RuleOps struct {
	RuleID string `json:"rule_id"`
	Ops    []Op   `json:"ops"`
}

// Op sets Value at Path, relative to the rule, for example
// ["conditions", 0, "threshold"].
type Op struct {
	Path  []any `json:"path"`
	Value any   `json:"value"`
}

// Evidence holds the numbers behind a finding. Shape tells which fields are
// set: events, scale, links, day or curve. The day shape uses minutes of the day from
// From (0) to To (1440), Ist and IstSpans are the observed times, Soll and
// SollSpans the proposed times or windows. The curve shape holds the power per
// quarter of the day in W: Quarters is the measured curve, SollQ the clear sky
// curve and BestQ the best curve with the sky raster (JSON ist_q, soll_q, best_q).
type Evidence struct {
	Shape     string     `json:"shape,omitempty"`
	From      int64      `json:"from,omitempty"`
	To        int64      `json:"to,omitempty"`
	Ist       []int64    `json:"ist,omitempty"`
	IstSpans  [][2]int64 `json:"ist_spans,omitempty"`
	SollSpans [][2]int64 `json:"soll_spans,omitempty"`
	Soll      []int64    `json:"soll,omitempty"`
	Quarters  []float32  `json:"ist_q,omitempty"`
	SollQ     []float32  `json:"soll_q,omitempty"`
	BestQ     []float32  `json:"best_q,omitempty"`
	Unit      string     `json:"unit,omitempty"`
	Max       float64    `json:"max,omitempty"`
	Seen      [2]float64 `json:"seen,omitempty"`
	IstValue  *float64   `json:"ist_value,omitempty"`
	SollValue *float64   `json:"soll_value,omitempty"`
	SeasonMax *float64   `json:"season_max,omitempty"`
	Target    string     `json:"target,omitempty"`
	IstNames  []string   `json:"ist_names,omitempty"`
	SollNames []string   `json:"soll_names,omitempty"`
	Missing   bool       `json:"missing,omitempty"`
}

// FindingID builds the stable ID of a finding from its kind and rule IDs. The
// rule IDs are sorted, so the order of the arguments does not matter.
func FindingID(kind string, ruleIDs ...string) string {
	sorted := append([]string(nil), ruleIDs...)
	sort.Strings(sorted)
	return kind + ":" + strings.Join(sorted, ":")
}
