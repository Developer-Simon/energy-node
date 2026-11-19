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
// opens the rule.
type Fix struct {
	Code   string `json:"code"`
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
// set: events, scale or links.
type Evidence struct {
	Shape     string     `json:"shape,omitempty"`
	From      int64      `json:"from,omitempty"`
	To        int64      `json:"to,omitempty"`
	Ist       []int64    `json:"ist,omitempty"`
	SollSpans [][2]int64 `json:"soll_spans,omitempty"`
	Soll      []int64    `json:"soll,omitempty"`
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
