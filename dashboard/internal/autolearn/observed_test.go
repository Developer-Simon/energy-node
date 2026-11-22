package autolearn

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
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
	if cool.Code != "set_cooldown" || len(cool.Ops) != 1 || cool.Ops[0].Path[0] != "cooldown_seconds" || cool.Ops[0].Value != 300 {
		t.Errorf("second fix = %+v, want set_cooldown to 300", cool)
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
	return neverFiredStateFor("grid_export", firstSeen, max)
}

// neverFiredStateFor is neverFiredState for the balance field, with the stats
// under the ID of the rule pvRule or observedDoc builds.
func neverFiredStateFor(field string, firstSeen time.Time, max float64) ObserverState {
	id := "heat"
	if field == "pv" {
		id = "sun"
	}
	var ranges []DayRange
	day := testNow.Unix() / 86400
	for i := 0; i < 8; i++ {
		ranges = append(ranges, DayRange{Day: day - int64(i), Min: 100, Max: 500})
	}
	ranges[0].Max = max
	return ObserverState{
		Rules:  map[string]RuleStats{id: {FirstSeen: firstSeen, Hours: map[int64]int{}}},
		Values: map[string][]DayRange{"balance:" + field: ranges},
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

// A learned threshold has no fixed number, so no hysteresis fix is proposed
// for it, only the cooldown fix.
func TestObservedFrequentWithLearnedThresholdHasNoHysteresisFix(t *testing.T) {
	doc := staticDoc(t, `{"rules": [{"id": "heat", "name": "Heater", "enabled": true,
	  "cooldown_seconds": 0,
	  "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above",
	    "threshold": {"learned": "device/wallbox_1/usual_runtime_s", "min": 60, "max": 14400, "fallback": 3600}, "hysteresis": 0}],
	  "actions": [{"type": "publish", "topic": "heater/set", "payload_source": "constant", "payload": "1"}]}]}`)
	got := findKind(ObservedFindings(doc, frequentState(15), nil, testNow), "frequent")
	if len(got) != 1 {
		t.Fatalf("frequent findings = %d, want 1", len(got))
	}
	if len(got[0].Apply) != 1 || got[0].Apply[0].Code != "set_cooldown" {
		t.Errorf("fixes = %+v, want only set_cooldown", got[0].Apply)
	}
}

// pvRoof is the 5 kW south roof of the plan, tilt 30 degrees, with the capped
// peak at 5 % above its PMax.
func pvRoof() PVModel {
	return PVModel{Surfaces: []Surface{{PMax: 5000, TiltDeg: 30, AzimuthDeg: 180}}, CapW: 5250, Safe: true}
}

var berlin = sun.Location{Lat: 52.5, Lon: 13.4}

// pvRule is a rule on the PV power above threshold, with the site location.
func pvRule(t *testing.T, threshold string) Doc {
	t.Helper()
	return staticDoc(t, `{"rules": [{"id": "sun", "name": "Sun", "enabled": true,
	  "cooldown_seconds": 0,
	  "conditions": [{"type": "balance_threshold", "field": "pv", "comparison": "above",
	    "threshold": `+threshold+`, "hysteresis": 0}],
	  "actions": [{"type": "publish", "topic": "sun/set", "payload_source": "constant", "payload": "1"}]}],
	  "settings": {"latitude": 52.5, "longitude": 13.4}}`)
}

func pvSource(m PVModel, ok bool) func() (PVModel, *Raster, bool) {
	return func() (PVModel, *Raster, bool) { return m, nil, ok }
}

// pvSourceWith hands out the model with a raster.
func pvSourceWith(m PVModel, r *Raster) func() (PVModel, *Raster, bool) {
	return func() (PVModel, *Raster, bool) { return m, r, true }
}

// rasterAt is a raster with two samples at az/el, enough for a cell value.
func rasterAt(az, el, w float64) *Raster {
	r := NewRaster(5, 5)
	r.Add(az, el, w, "2026-06-20")
	r.Add(az, el, w, "2026-06-21")
	return r
}

// Focus 3: 6000 W on a 5 kW roof never fires, so it is not a seasonal rest.
// 4000 W is above the modelled roof as well.
func TestSeasonWeekForAboveTheRoofIsNever(t *testing.T) {
	now := time.Date(2026, 12, 10, 12, 0, 0, 0, time.UTC)
	for _, th := range []float64{4000, 6000} {
		if week, ok := SeasonWeekFor(th, pvRoof(), nil, berlin, now); ok {
			t.Errorf("threshold %v: week %d, want never", th, week)
		}
	}
}

func TestSeasonWeekForReachedInSpring(t *testing.T) {
	now := time.Date(2026, 12, 10, 12, 0, 0, 0, time.UTC)
	week, ok := SeasonWeekFor(3000, pvRoof(), nil, berlin, now)
	if !ok || week != 13 {
		t.Fatalf("SeasonWeekFor(3000) = %d, %v, want 13, true", week, ok)
	}
}

func TestObservedNeverFiredPVSeasonalWeekIsInfo(t *testing.T) {
	doc := pvRule(t, "3000")
	st := neverFiredStateFor("pv", testNow.Add(-40*24*time.Hour), 1850)
	week, ok := SeasonWeekFor(3000, pvRoof(), nil, berlin, testNow)
	if !ok {
		t.Fatal("SeasonWeekFor(3000) from testNow: not ok, want a week")
	}

	findings := ObservedFindingsWithPV(doc, st, fakeSeason{ok: false}, pvSource(pvRoof(), true), nil, testNow, DefaultThresholds())
	got := findKind(findings, "never_fired")
	if len(got) != 1 {
		t.Fatalf("never_fired findings = %d, want 1: %+v", len(got), findings)
	}
	f := got[0]
	if f.Severity != "info" || f.Summary.Code != "check.never_fired_season" {
		t.Errorf("severity/code = %s/%s, want info/check.never_fired_season", f.Severity, f.Summary.Code)
	}
	if len(f.Apply) != 0 {
		t.Errorf("fixes = %+v, want none", f.Apply)
	}
	if len(f.Facts) != 1 || f.Facts[0].Code != "season_week" || f.Facts[0].Value != week {
		t.Errorf("Facts = %+v, want season_week %d", f.Facts, week)
	}
}

func TestObservedNeverFiredPVAboveRoofIsWarnWithFix(t *testing.T) {
	doc := pvRule(t, "6000")
	st := neverFiredStateFor("pv", testNow.Add(-40*24*time.Hour), 1850)

	findings := ObservedFindingsWithPV(doc, st, fakeSeason{maxW: 5000, months: []int{6}, days: 300, ok: true},
		pvSourceWith(pvRoof(), rasterAt(180, 61, 5000)), nil, testNow, DefaultThresholds())
	got := findKind(findings, "never_fired")
	if len(got) != 1 {
		t.Fatalf("never_fired findings = %d, want 1: %+v", len(got), findings)
	}
	f := got[0]
	if f.Severity != "warn" || f.Summary.Code != "check.never_fired" {
		t.Errorf("severity/code = %s/%s, want warn/check.never_fired", f.Severity, f.Summary.Code)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "set_threshold" {
		t.Fatalf("fixes = %+v, want one set_threshold", f.Apply)
	}
}

func TestObservedNeverFiredPVWithoutModelUsesSeasonMax(t *testing.T) {
	doc := pvRule(t, "3000")
	st := neverFiredStateFor("pv", testNow.Add(-40*24*time.Hour), 1850)
	season := fakeSeason{maxW: 3400, months: []int{6}, days: 300, ok: true}

	for name, src := range map[string]func() (PVModel, *Raster, bool){
		"no source": nil,
		"no model":  pvSource(PVModel{}, false),
		"not safe":  pvSource(PVModel{Surfaces: pvRoof().Surfaces, CapW: 5250}, true),
	} {
		findings := ObservedFindingsWithPV(doc, st, season, src, nil, testNow, DefaultThresholds())
		got := findKind(findings, "never_fired")
		if len(got) != 1 || got[0].Summary.Code != "check.seasonal_rest" {
			t.Errorf("%s: findings = %+v, want check.seasonal_rest", name, got)
		}
	}
}

// A winter raster does not know the summer sun: a threshold the curve does not
// reach is not "can never be met" and gets no fix.
func TestObservedNeverFiredPVWinterRasterFallsBack(t *testing.T) {
	doc := pvRule(t, "6000")
	st := neverFiredStateFor("pv", testNow.Add(-40*24*time.Hour), 1850)
	winter := rasterAt(180, 14, 1800)

	findings := ObservedFindingsWithPV(doc, st, fakeSeason{maxW: 5000, months: []int{6}, days: 300, ok: true},
		pvSourceWith(pvRoof(), winter), nil, testNow, DefaultThresholds())
	if got := findKind(findings, "never_fired"); len(got) != 0 {
		t.Fatalf("never_fired with a winter raster = %+v, want none", got)
	}

	findings = ObservedFindingsWithPV(doc, st, fakeSeason{maxW: 6500, months: []int{6}, days: 300, ok: true},
		pvSourceWith(pvRoof(), winter), nil, testNow, DefaultThresholds())
	got := findKind(findings, "never_fired")
	if len(got) != 1 || got[0].Summary.Code != "check.seasonal_rest" {
		t.Fatalf("findings = %+v, want check.seasonal_rest from the seasonal maximum", got)
	}
}

// spikeDays returns the balance keys of the given days: the export is 1000 W
// with every tenth minute at 1080 W, the load total is 1000 W.
func spikeDays(days ...string) map[string]map[string][]float32 {
	out := make(map[string]map[string][]float32, len(days))
	for _, d := range days {
		grid := make([]float32, 1440)
		load := make([]float32, 1440)
		for m := range grid {
			grid[m], load[m] = 1000, 1000
			if m%10 == 5 {
				grid[m] = 1080
			}
		}
		out[d] = map[string][]float32{"b:grid_export": grid, "b:load_total": load}
	}
	return out
}

// frequentInputFor is an input with a 1000 W heater on switch.heater and the
// given minute days.
func frequentInputFor(t *testing.T, now time.Time, days map[string]map[string][]float32) *Input {
	t.Helper()
	doc := staticDoc(t, `{"rules": [{"id": "heat", "name": "Heater", "enabled": true,
	  "conditions": [{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": 1000}],
	  "actions": [{"type": "publish", "topic": "heater/set", "payload": "ON", "entity_id": "switch.heater"}]}]}`)
	in := &Input{
		Now:     now,
		Loc:     time.UTC,
		Doc:     doc,
		Devices: map[string]DeviceInfo{"heater": {ID: "heater", Switchable: true, Entities: []string{"switch.heater"}}},
		Learned: map[string]LearnedEntry{"device/heater/op_power_w": {Key: "device/heater/op_power_w", Value: 1000.0}},
	}
	if days != nil {
		in.Minutes = fakeMinutes{loc: time.UTC, days: days}
	}
	return in
}

func TestObservedFrequentHysteresisFromMinuteNoise(t *testing.T) {
	in := frequentInputFor(t, testNow, spikeDays("2026-11-18", "2026-11-17", "2026-11-16"))
	doc := in.Doc
	findings := ObservedFindingsWithPV(doc, frequentState(15), nil, nil, in, testNow, DefaultThresholds())
	got := findKind(findings, "frequent")
	if len(got) != 1 {
		t.Fatalf("frequent findings = %d, want 1", len(got))
	}
	hyst := got[0].Apply[0]
	if hyst.Code != "set_hysteresis" || hyst.Ops[0].Value != 1200 {
		t.Fatalf("hysteresis fix = %+v, want 1200 (2 x 80 W noise + 1000 W load, up to 50 W)", hyst)
	}
}

func TestObservedFrequentHysteresisFallsBackWithoutMinutes(t *testing.T) {
	in := frequentInputFor(t, testNow, nil)
	findings := ObservedFindingsWithPV(in.Doc, frequentState(15), nil, nil, in, testNow, DefaultThresholds())
	got := findKind(findings, "frequent")
	if len(got) != 1 {
		t.Fatalf("frequent findings = %d, want 1", len(got))
	}
	if v := got[0].Apply[0].Ops[0].Value; v != hysteresisFor(1000) {
		t.Fatalf("hysteresis = %v, want the fallback %d", v, hysteresisFor(1000))
	}
}

func TestObservedFrequentEvidenceSollFromReplay(t *testing.T) {
	// The burst sits on 19 Nov at 09:00, the replay runs on the days before 20 Nov.
	now := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	in := frequentInputFor(t, now, spikeDays("2026-11-19", "2026-11-18"))
	findings := ObservedFindingsWithPV(in.Doc, frequentState(15), nil, nil, in, now, DefaultThresholds())
	got := findKind(findings, "frequent")
	if len(got) != 1 {
		t.Fatalf("frequent findings = %d, want 1", len(got))
	}
	if got[0].Evidence.Soll == nil {
		t.Fatalf("Soll = nil, want the replayed triggers with the hysteresis")
	}
	for _, s := range got[0].Evidence.Soll {
		if s < got[0].Evidence.From || s >= got[0].Evidence.To {
			t.Errorf("Soll %d outside the evidence window", s)
		}
	}
}

func TestObservedFrequentCooldownFromBurstGap(t *testing.T) {
	if got := frequentCooldown([]int64{0, 60, 120}); got != 300 {
		t.Errorf("cooldown for 60 s gaps = %d, want the floor 300", got)
	}
	if got := frequentCooldown([]int64{0, 200, 400}); got != 600 {
		t.Errorf("cooldown for 200 s gaps = %d, want 600", got)
	}
}
