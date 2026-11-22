package autolearn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unsatisfiableDoc has one rule that can never be true: above 500 and below 300.
const unsatisfiableDoc = `{"rules":[{"id":"r1","name":"Heizung","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid","comparison":"above","threshold":500},{"type":"balance_threshold","field":"grid","comparison":"below","threshold":300}],"actions":[]}]}`

const emptyRuleDoc = `{"rules":[]}`

func staticRead(raw string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(raw), nil }
}

func TestCheckerIgnoredFindingIsLeftOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, path, t.Logf)

	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(got), got)
	}
	if err := c.Ignore("unsatisfiable:r1"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}
	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("ignored finding still listed: %+v", got)
	}
}

func TestCheckerUnignoreBringsFindingBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, path, t.Logf)

	if err := c.Ignore("unsatisfiable:r1"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}
	if err := c.Unignore("unsatisfiable:r1"); err != nil {
		t.Fatalf("Unignore: %v", err)
	}
	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings after Unignore = %d, want 1", len(got))
	}
}

func TestCheckerDropsIgnoredIDOfDeletedRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	doc := unsatisfiableDoc
	read := func() ([]byte, error) { return []byte(doc), nil }
	c := NewChecker(NewObserver(time.UTC), nil, read, nil, path, t.Logf)

	c.Findings(testBase)
	if err := c.Ignore("unsatisfiable:r1"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}

	// The rule is deleted. The document is read again after 60 s.
	doc = emptyRuleDoc
	c.Findings(testBase.Add(61 * time.Second))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ignored file: %v", err)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		t.Fatalf("ignored file is not a list: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ignored file = %v, want empty list", ids)
	}
}

func TestCheckerKeepsIgnoredIDAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	first := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, path, t.Logf)
	if err := first.Ignore("unsatisfiable:r1"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}

	second := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, path, t.Logf)
	if got := second.Findings(testBase); len(got) != 0 {
		t.Fatalf("ignored finding after restart: %+v", got)
	}
}

func TestCheckerBrokenIgnoredFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	if err := os.WriteFile(path, []byte("kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, path, t.Logf)
	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings = %d, want 1 with a broken ignored file", len(got))
	}
}

func TestCheckerFindingsOrderedBySeverityThenID(t *testing.T) {
	// r1 has an entity that does not exist (warn), r2 can never be true (bad).
	doc := `{"rules":[` +
		`{"id":"r1","name":"Lampe","enabled":true,"conditions":[{"type":"entity_value","entity_id":"sensor.gone","comparison":"above","value":5}],"actions":[]},` +
		`{"id":"r2","name":"Heizung","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid","comparison":"above","threshold":500},{"type":"balance_threshold","field":"grid","comparison":"below","threshold":300}],"actions":[]}]}`
	exists := func(string) bool { return false }
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(doc), exists, "", t.Logf)

	got := c.Findings(testBase)
	if len(got) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "unsatisfiable:r2" || got[1].ID != "orphan:r1:sensor.gone" {
		t.Fatalf("order = %s, %s; want unsatisfiable:r2, orphan:r1:sensor.gone", got[0].ID, got[1].ID)
	}
}

func TestCheckerFindingsCappedAtTwenty(t *testing.T) {
	var rules []string
	for i := 0; i < 25; i++ {
		rules = append(rules, fmt.Sprintf(`{"id":"r%02d","name":"Regel %d","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid","comparison":"above","threshold":500},{"type":"balance_threshold","field":"grid","comparison":"below","threshold":300}],"actions":[]}`, i, i))
	}
	doc := `{"rules":[` + strings.Join(rules, ",") + `]}`
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(doc), nil, "", t.Logf)

	if got := c.Findings(testBase); len(got) != 20 {
		t.Fatalf("findings = %d, want 20", len(got))
	}
}

func TestCheckerReadsRuleDocumentAtMostEveryMinute(t *testing.T) {
	reads := 0
	read := func() ([]byte, error) {
		reads++
		return []byte(emptyRuleDoc), nil
	}
	c := NewChecker(NewObserver(time.UTC), nil, read, nil, "", t.Logf)

	c.ObserveTick(testBase, "", nil, nil)
	c.ObserveTick(testBase.Add(30*time.Second), "", nil, nil)
	c.ObserveTick(testBase.Add(61*time.Second), "", nil, nil)
	if reads != 2 {
		t.Fatalf("rule document read %d times, want 2", reads)
	}
}

func TestCheckerObserveTickCollectsOnlyRuleValueKeys(t *testing.T) {
	doc := `{"rules":[{"id":"r1","name":"Heizung","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid","comparison":"above","threshold":100},{"type":"entity_value","entity_id":"sensor.temp","comparison":"below","value":20}],"actions":[]}]}`
	obs := NewObserver(time.UTC)
	c := NewChecker(obs, nil, staticRead(doc), nil, "", t.Logf)

	var asked []string
	entityValues := func(id string) (float64, bool) {
		asked = append(asked, id)
		return 18, true
	}
	c.ObserveTick(testBase, "", map[string]float64{"grid": 300, "pv": 5}, entityValues)

	values := obs.Snapshot().Values
	if _, ok := values["balance:pv"]; ok {
		t.Fatal("balance:pv collected although no rule reads it")
	}
	if got := values["balance:grid"]; len(got) != 1 || got[0].Max != 300 {
		t.Fatalf("balance:grid = %+v, want max 300", got)
	}
	if got := values["entity:sensor.temp"]; len(got) != 1 || got[0].Max != 18 {
		t.Fatalf("entity:sensor.temp = %+v, want max 18", got)
	}
	if len(asked) != 1 || asked[0] != "sensor.temp" {
		t.Fatalf("entityValues asked for %v, want [sensor.temp]", asked)
	}
}

func TestCheckerObserveTickCountsTriggersAndPrunesDeletedRules(t *testing.T) {
	doc := unsatisfiableDoc
	read := func() ([]byte, error) { return []byte(doc), nil }
	obs := NewObserver(time.UTC)
	c := NewChecker(obs, nil, read, nil, "", t.Logf)

	c.ObserveTick(testBase, `{"rules":{"r1":{"trigger_count":2}}}`, nil, nil)
	c.ObserveTick(testBase.Add(10*time.Second), `{"rules":{"r1":{"trigger_count":5}}}`, nil, nil)
	st, ok := obs.Snapshot().Rules["r1"]
	if !ok {
		t.Fatal("rule r1 not observed")
	}
	if total := sumHours(st.Hours); total != 3 {
		t.Fatalf("triggers = %d, want 3", total)
	}

	// The rule is deleted, so its observations are pruned on the next re-read.
	doc = emptyRuleDoc
	c.ObserveTick(testBase.Add(61*time.Second), `{"rules":{}}`, nil, nil)
	if _, ok := obs.Snapshot().Rules["r1"]; ok {
		t.Fatal("observations of deleted rule r1 still kept")
	}
}

func TestCheckerFindingsFollowASavedRuleAtOnce(t *testing.T) {
	doc := unsatisfiableDoc
	read := func() ([]byte, error) { return []byte(doc), nil }
	c := NewChecker(NewObserver(time.UTC), nil, read, nil, "", t.Logf)

	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
	// The user turns the rule off and saves, the next request is a second later.
	doc = strings.Replace(doc, `"enabled":true`, `"enabled":false`, 1)
	if got := c.Findings(testBase.Add(time.Second)); len(got) != 0 {
		t.Fatalf("finding of a disabled rule still listed: %+v", got)
	}
}

// duplicateDoc has two enabled rules with the same trigger and action.
const duplicateDoc = `{"rules":[` +
	`{"id":"first","name":"Pump first","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":500}],"actions":[{"type":"publish","topic":"pump/set","payload_source":"constant","payload":"on"}]},` +
	`{"id":"second","name":"Pump second","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid_export","comparison":"above","threshold":500}],"actions":[{"type":"publish","topic":"pump/set","payload_source":"constant","payload":"on"}]}]}`

func TestCheckerPreviewFindsDraftDuplicate(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(emptyRuleDoc), nil, "", t.Logf)

	got, err := c.Preview([]byte(duplicateDoc))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(got) != 1 || got[0].ID != "duplicate:first:second" {
		t.Fatalf("Preview = %+v, want duplicate:first:second", got)
	}
}

func TestCheckerPreviewLeavesIgnoredIDOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.json")
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(emptyRuleDoc), nil, path, t.Logf)
	if err := c.Ignore("duplicate:first:second"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}

	got, err := c.Preview([]byte(duplicateDoc))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ignored finding in preview: %+v", got)
	}

	// A preview is a pure read: an ignored ID of a rule the draft does not
	// mention stays in the ignored file.
	if _, err := c.Preview([]byte(emptyRuleDoc)); err != nil {
		t.Fatalf("Preview empty: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ignored file: %v", err)
	}
	if !strings.Contains(string(data), "duplicate:first:second") {
		t.Fatalf("preview changed the ignored file: %s", data)
	}
}

func TestCheckerPreviewBrokenDocumentIsError(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(emptyRuleDoc), nil, "", t.Logf)

	if _, err := c.Preview([]byte(`{"rules":`)); err == nil {
		t.Fatal("Preview of broken JSON returned no error")
	}
}

func TestCheckerPreviewKeepsStoredDocument(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(emptyRuleDoc), nil, "", t.Logf)

	if got, err := c.Preview([]byte(duplicateDoc)); err != nil || len(got) != 1 {
		t.Fatalf("Preview = %+v, %v", got, err)
	}
	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("stored document changed by a preview: %+v", got)
	}
}

// reliabilityDoc has one enabled rule that is satisfiable and has no static finding.
const reliabilityDoc = `{"rules":[{"id":"r1","name":"Lampe","enabled":true,"conditions":[{"type":"balance_threshold","field":"grid","comparison":"above","threshold":500}],"actions":[]}]}`

// reliabilityCandidate is a candidate of a reliability detector as the engine hands it over.
func reliabilityCandidate(kind string, ruleIDs ...string) Candidate {
	return Candidate{
		Finding: Finding{
			ID:         FindingID(kind, ruleIDs...),
			Kind:       kind,
			Group:      "check",
			RuleIDs:    ruleIDs,
			Severity:   "warn",
			Confidence: "high",
			Summary:    Text{Code: "check." + kind, Params: map[string]any{"rule": ruleIDs[0]}},
			Evidence:   Evidence{Shape: "events"},
		},
		Detector: kind,
		NEff:     10,
		MinNEff:  3,
	}
}

func checkerWithEngine(t *testing.T, doc string, cands []Candidate, on *bool, path string) *Checker {
	t.Helper()
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(doc), nil, path, t.Logf)
	c.SetEngineSource(func() []Candidate { return cands }, func() bool { return *on })
	return c
}

func TestCheckerReliabilityFindingCanBeIgnoredLikeStatic(t *testing.T) {
	on := true
	path := filepath.Join(t.TempDir(), "ignored.json")
	c := checkerWithEngine(t, reliabilityDoc, []Candidate{reliabilityCandidate("flapping", "r1")}, &on, path)

	got := c.Findings(testBase)
	if len(got) != 1 || got[0].ID != "flapping:r1" {
		t.Fatalf("findings = %+v, want one flapping:r1", got)
	}
	if err := c.Ignore("flapping:r1"); err != nil {
		t.Fatalf("Ignore: %v", err)
	}
	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("ignored reliability finding still listed: %+v", got)
	}
	// The ignore survives a restart like a static one.
	second := checkerWithEngine(t, reliabilityDoc, []Candidate{reliabilityCandidate("flapping", "r1")}, &on, path)
	if got := second.Findings(testBase); len(got) != 0 {
		t.Fatalf("ignored reliability finding after restart: %+v", got)
	}
}

func TestCheckerReliabilityFindingLeavesWithItsGroup(t *testing.T) {
	on := true
	c := checkerWithEngine(t, reliabilityDoc, []Candidate{reliabilityCandidate("no_effect", "r1")}, &on, "")

	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings with group on = %d, want 1", len(got))
	}
	on = false
	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("findings with group off = %+v, want none", got)
	}
	on = true
	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("findings after switching the group on again = %d, want 1", len(got))
	}
}

func TestCheckerDropsReliabilityFindingOfVanishedRule(t *testing.T) {
	on := true
	c := checkerWithEngine(t, emptyRuleDoc, []Candidate{reliabilityCandidate("overridden", "r1")}, &on, "")

	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("finding of a deleted rule listed: %+v", got)
	}
}

func TestCheckerDropsReliabilityCandidateBelowMinNEff(t *testing.T) {
	on := true
	weak := reliabilityCandidate("flapping", "r1")
	weak.NEff = 2
	c := checkerWithEngine(t, reliabilityDoc, []Candidate{weak}, &on, "")

	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("candidate below MinNEff listed: %+v", got)
	}
}

func TestCheckerIgnoresHabitCandidates(t *testing.T) {
	on := true
	habit := reliabilityCandidate("coupling", "r1")
	habit.Finding.Group = "habit"
	c := checkerWithEngine(t, reliabilityDoc, []Candidate{habit}, &on, "")

	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("habit candidate listed in the rule check: %+v", got)
	}
}

func TestCheckerDropsReliabilityFindingWhoseFixIsSaved(t *testing.T) {
	on := true
	saved := reliabilityCandidate("flapping", "r1")
	saved.Finding.Apply = []Fix{{Code: "set_threshold", RuleID: "r1", Ops: []Op{{Path: []any{"conditions", 0, "threshold"}, Value: 500}}}}
	c := checkerWithEngine(t, reliabilityDoc, []Candidate{saved}, &on, "")
	if got := c.Findings(testBase); len(got) != 0 {
		t.Fatalf("finding with a saved fix listed: %+v", got)
	}

	open := reliabilityCandidate("flapping", "r1")
	open.Finding.Apply = []Fix{{Code: "set_threshold", RuleID: "r1", Ops: []Op{{Path: []any{"conditions", 0, "threshold"}, Value: 600}}}}
	c = checkerWithEngine(t, reliabilityDoc, []Candidate{open}, &on, "")
	if got := c.Findings(testBase); len(got) != 1 {
		t.Fatalf("finding with an open fix = %d findings, want 1", len(got))
	}
}

func TestCheckerReadsReplaySourceOnFindings(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, "", t.Logf)
	calls := 0
	c.SetReplaySource(func() *Input {
		calls++
		return &Input{Now: testBase}
	})
	c.Findings(testBase)
	if calls != 1 {
		t.Fatalf("replay source calls = %d, want 1", calls)
	}
}

// regulatorScenarioDoc has one scenario with a regulator and no rule.
const regulatorScenarioDoc = `{"rules":[],"scenarios":[{"id":"heater","name":"Heizstab","enabled":true,"while":{"regulator":{"output":{"topic":"heater/power/set"}}}}]}`

func TestCheckerKeepsRegulatorFindingOfScenario(t *testing.T) {
	on := true
	reg := reliabilityCandidate("regulator", "heater")
	c := checkerWithEngine(t, regulatorScenarioDoc, []Candidate{reg}, &on, "")
	got := c.Findings(testBase)
	if len(got) != 1 || got[0].ID != "regulator:heater" {
		t.Fatalf("findings = %+v, want the regulator:heater finding of the scenario", got)
	}
}

func TestCheckerReplayInputIsKeptTenMinutes(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(unsatisfiableDoc), nil, "", t.Logf)
	calls := 0
	c.SetReplaySource(func() *Input {
		calls++
		return &Input{Now: testBase}
	})
	c.Findings(testBase)
	c.Findings(testBase.Add(5 * time.Minute))
	if calls != 1 {
		t.Fatalf("source calls after 5 minutes = %d, want 1", calls)
	}
	c.Findings(testBase.Add(11 * time.Minute))
	if calls != 2 {
		t.Fatalf("source calls after 11 minutes = %d, want 2", calls)
	}
}

func TestCheckerReplayInputNotBuiltWithoutEnabledRule(t *testing.T) {
	c := NewChecker(NewObserver(time.UTC), nil, staticRead(emptyRuleDoc), nil, "", t.Logf)
	calls := 0
	c.SetReplaySource(func() *Input {
		calls++
		return &Input{Now: testBase}
	})
	c.Findings(testBase)
	if calls != 0 {
		t.Fatalf("source calls without rules = %d, want 0", calls)
	}
}
