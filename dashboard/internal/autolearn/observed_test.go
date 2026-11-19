package autolearn

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// fakeSeason is a SeasonalSource with fixed answers.
type fakeSeason struct {
	maxW   float64
	months []int
	days   int
	ok     bool
}

func (f fakeSeason) MaxW(field string, from, to time.Time) (float64, []int, int, bool) {
	return f.maxW, f.months, f.days, f.ok
}

func observedDoc(t *testing.T, threshold, hysteresis string) Doc {
	t.Helper()
	return staticDoc(t, `{"rules": [{"id": "heat", "name": "Heater", "enabled": true,
	  "cooldown_seconds": 0,
	  "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
	    "threshold": `+threshold+`, "hysteresis": `+hysteresis+`}],
	  "actions": [{"type": "publish", "topic": "heater/set", "payload_source": "constant", "payload": "1"}]}]}`)
}

func findKind(findings []Finding, kind string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// frequentState puts n triggers into the hour 3 h before testNow, one event
// per minute from the start of that hour.
func frequentState(n int) ObserverState {
	hourStart := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	var events []int64
	for i := 0; i < n; i++ {
		events = append(events, hourStart.Add(time.Duration(i)*time.Minute).Unix())
	}
	return ObserverState{Rules: map[string]RuleStats{"heat": {
		FirstSeen: testNow.Add(-30 * 24 * time.Hour),
		Hours:     map[int64]int{hourStart.Unix() / 3600: n},
		Events:    events,
	}}}
}

func TestObservedFrequentWithBothFixes(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	findings := ObservedFindings(doc, frequentState(15), nil, testNow)

	got := findKind(findings, "frequent")
	if len(got) != 1 {
		t.Fatalf("frequent findings = %d, want 1: %+v", len(got), findings)
	}
	f := got[0]
	if f.ID != "frequent:heat" || f.Group != "check" || f.Severity != "warn" {
		t.Errorf("finding = %s/%s/%s, want frequent:heat/check/warn", f.ID, f.Group, f.Severity)
	}
	if f.Confidence != "high" {
		t.Errorf("Confidence = %q, want high", f.Confidence)
	}
	if f.Evidence.Shape != "events" {
		t.Errorf("Shape = %q, want events", f.Evidence.Shape)
	}
	if len(f.Evidence.Ist) != 15 {
		t.Errorf("Ist = %d events, want 15", len(f.Evidence.Ist))
	}
	if len(f.Evidence.SollSpans) != 1 || f.Evidence.SollSpans[0][1]-f.Evidence.SollSpans[0][0] != 14*60 {
		t.Errorf("SollSpans = %v, want one arc over 14 minutes", f.Evidence.SollSpans)
	}
	if len(f.Apply) != 2 {
		t.Fatalf("fixes = %d, want 2", len(f.Apply))
	}

	hyst := f.Apply[0]
	if hyst.Code != "set_hysteresis" || hyst.RuleID != "heat" || len(hyst.Ops) != 1 {
		t.Fatalf("first fix = %+v, want set_hysteresis", hyst)
	}
	if hyst.Ops[0].Path[0] != "conditions" || hyst.Ops[0].Path[1] != 0 || hyst.Ops[0].Path[2] != "hysteresis" {
		t.Errorf("hysteresis path = %v", hyst.Ops[0].Path)
	}
	if hyst.Ops[0].Value != 300 {
		t.Errorf("hysteresis = %v, want 300", hyst.Ops[0].Value)
	}

	cool := f.Apply[1]
	if cool.Code != "set_cooldown" || len(cool.Ops) != 1 || cool.Ops[0].Path[0] != "cooldown_seconds" || cool.Ops[0].Value != 600 {
		t.Errorf("second fix = %+v, want set_cooldown to 600", cool)
	}

	if len(f.Facts) != 1 || f.Facts[0].Code != "per_hour_max" || f.Facts[0].Value != 15 {
		t.Errorf("Facts = %+v, want per_hour_max 15", f.Facts)
	}
}

func TestObservedFrequentBelowTwelveIsNothing(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	if findings := ObservedFindings(doc, frequentState(11), nil, testNow); len(findKind(findings, "frequent")) != 0 {
		t.Fatalf("11 triggers flagged: %+v", findings)
	}
}

func TestObservedNeverFiredWarnWithThresholdFix(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	st := neverFiredState(testNow.Add(-40*24*time.Hour), 1850)

	findings := ObservedFindings(doc, st, fakeSeason{ok: false}, testNow)
	got := findKind(findings, "never_fired")
	if len(got) != 1 {
		t.Fatalf("never_fired findings = %d, want 1: %+v", len(got), findings)
	}
	f := got[0]
	if f.Group != "check" || f.Confidence != "estimate" {
		t.Errorf("Group/Confidence = %q/%q, want check/estimate", f.Group, f.Confidence)
	}
	if f.Severity != "warn" || f.Summary.Code != "check.never_fired" {
		t.Errorf("severity/code = %s/%s, want warn/check.never_fired", f.Severity, f.Summary.Code)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "set_threshold" {
		t.Fatalf("fixes = %+v, want one set_threshold", f.Apply)
	}
	op := f.Apply[0].Ops[0]
	if op.Path[0] != "conditions" || op.Path[1] != 0 || op.Path[2] != "threshold" || op.Value != 1350 {
		t.Errorf("op = %v -> %v, want conditions/0/threshold -> 1350", op.Path, op.Value)
	}
	if f.Evidence.Shape != "scale" || f.Evidence.Unit != "W" {
		t.Errorf("Evidence shape/unit = %q/%q, want scale/W", f.Evidence.Shape, f.Evidence.Unit)
	}
	if f.Evidence.Seen != [2]float64{100, 1850} || *f.Evidence.IstValue != 3000 || *f.Evidence.SollValue != 1350 {
		t.Errorf("Evidence seen/ist/soll = %v/%v/%v", f.Evidence.Seen, *f.Evidence.IstValue, *f.Evidence.SollValue)
	}
	if f.Evidence.SeasonMax != nil {
		t.Errorf("SeasonMax = %v, want nil without season value", *f.Evidence.SeasonMax)
	}
	if f.Evidence.Max != 3600 {
		t.Errorf("Max = %v, want 3600", f.Evidence.Max)
	}
}

func TestObservedNeverFiredSeasonalRestIsInfo(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	st := neverFiredState(testNow.Add(-40*24*time.Hour), 1850)

	findings := ObservedFindings(doc, st, fakeSeason{maxW: 3400, months: []int{6}, days: 300, ok: true}, testNow)
	got := findKind(findings, "never_fired")
	if len(got) != 1 {
		t.Fatalf("never_fired findings = %d, want 1: %+v", len(got), findings)
	}
	f := got[0]
	if f.Group != "check" || f.Confidence != "estimate" {
		t.Errorf("Group/Confidence = %q/%q, want check/estimate", f.Group, f.Confidence)
	}
	if f.Severity != "info" || f.Summary.Code != "check.seasonal_rest" {
		t.Errorf("severity/code = %s/%s, want info/check.seasonal_rest", f.Severity, f.Summary.Code)
	}
	if len(f.Apply) != 0 {
		t.Errorf("fixes = %+v, want none", f.Apply)
	}
	if len(f.Facts) != 1 || f.Facts[0].Code != "season_months" {
		t.Fatalf("Facts = %+v, want season_months", f.Facts)
	}
	if months, ok := f.Facts[0].Value.([]int); !ok || len(months) != 1 || months[0] != 6 {
		t.Errorf("season_months = %v, want [6]", f.Facts[0].Value)
	}
	if f.Evidence.SeasonMax == nil || *f.Evidence.SeasonMax != 3400 {
		t.Errorf("SeasonMax = %v, want 3400", f.Evidence.SeasonMax)
	}
}

func TestObservedNeverFiredNeedsFourteenDays(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	st := neverFiredState(testNow.Add(-10*24*time.Hour), 1850)
	if findings := ObservedFindings(doc, st, nil, testNow); len(findKind(findings, "never_fired")) != 0 {
		t.Fatalf("rule known for 10 days flagged: %+v", findings)
	}
}

// neverFiredState is a rule without triggers since firstSeen, with 8 days of
// balance values from 100 W up to max W.
func neverFiredState(firstSeen time.Time, max float64) ObserverState {
	var ranges []DayRange
	day := testNow.Unix() / 86400
	for i := 0; i < 8; i++ {
		ranges = append(ranges, DayRange{Day: day - int64(i), Min: 100, Max: 500})
	}
	ranges[0].Max = max
	return ObserverState{
		Rules:  map[string]RuleStats{"heat": {FirstSeen: firstSeen, Hours: map[int64]int{}}},
		Values: map[string][]DayRange{"balance:grid_export": ranges},
	}
}

func TestThresholdsFromEnv(t *testing.T) {
	none := func(string) string { return "" }
	if got := ThresholdsFromEnv(none); got != DefaultThresholds() {
		t.Errorf("default = %+v, want %+v", got, DefaultThresholds())
	}
	fast := func(key string) string {
		if key == "ENERGY_NODE_AUTOLEARN_FAST" {
			return "1"
		}
		return ""
	}
	want := Thresholds{FrequentMinCount: 3, NeverFiredWindow: 0, NeverFiredMinAge: 0, MinRangeDays: 0}
	if got := ThresholdsFromEnv(fast); got != want {
		t.Errorf("FAST=1 = %+v, want %+v", got, want)
	}
}

func TestObservedFindingsWithFastThresholds(t *testing.T) {
	doc := observedDoc(t, "3000", "0")
	st := frequentState(3)

	if findings := ObservedFindings(doc, st, nil, testNow); len(findKind(findings, "frequent")) != 0 {
		t.Fatalf("defaults flagged 3 triggers: %+v", findings)
	}
	fast := ThresholdsFromEnv(func(key string) string {
		if key == "ENERGY_NODE_AUTOLEARN_FAST" {
			return "1"
		}
		return ""
	})
	findings := ObservedFindingsWith(doc, st, nil, testNow, fast)
	if got := findKind(findings, "frequent"); len(got) != 1 {
		t.Fatalf("fast frequent findings = %d, want 1: %+v", len(got), findings)
	}
}
