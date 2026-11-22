package autolearn

import (
	"testing"
	"time"
)

// fbDay is the local midnight of the November day 10+d.
func fbDay(loc *time.Location, d int) time.Time {
	return time.Date(2026, 11, d, 0, 0, 0, 0, loc)
}

// fbDevice is a switchable device with one switch entity.
func fbDevice() map[string]DeviceInfo {
	return map[string]DeviceInfo{"plug.kettle": {ID: "plug.kettle", Name: "Kessel", Switchable: true,
		Entities: []string{"switch.plug.kettle"}}}
}

// fbRule is a rule that switches the kettle on the balance grid_export with a
// fixed threshold. cmp is "above" for the switching-on rule, "below" for the
// switching-off rule.
func fbRule(id, cmp string, x float64) Rule {
	return Rule{ID: id, Name: id, Enabled: true,
		Conditions: []Condition{{Type: "balance_threshold", Field: "grid_export", Comparison: cmp, Threshold: x}},
		Actions:    []Action{{Type: "publish", EntityID: "switch.plug.kettle"}}}
}

// fbLearned gives the kettle an operating power of p watt.
func fbLearned(p float64) map[string]LearnedEntry {
	return map[string]LearnedEntry{"device/plug.kettle/op_power_w": {
		Key: "device/plug.kettle/op_power_w", Device: "plug.kettle", Kind: ValueNumber, Unit: "W", Value: p, N: 10}}
}

func TestFeedbackFlagsSelfSwitchingRule(t *testing.T) {
	loc := berlinLoc(t)
	cases := []struct {
		name    string
		on, off float64
		flagged bool
	}{
		{"on 3000 off 500, stable", 3000, 500, false},
		{"on 1500 off 500, unstable", 1500, 500, true},
		// Exactly on the boundary X - P = X_aus: the spec counts it as unstable.
		{"on 2500 off 500, on the boundary", 2500, 500, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := relInput(loc, nil, fbDevice(), fbRule("on", "above", c.on), fbRule("off", "below", c.off))
			in.Learned = fbLearned(2000)
			got := FeedbackFindings(in)
			if (len(got) == 1) != c.flagged {
				t.Fatalf("findings = %+v, want flagged %v", got, c.flagged)
			}
		})
	}
}

func TestFeedbackFindingShape(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, nil, fbDevice(), fbRule("on", "above", 1500), fbRule("off", "below", 500))
	in.Learned = fbLearned(2000)
	got := FeedbackFindings(in)
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
	f := got[0]
	if f.ID != "feedback:on" || f.Kind != "feedback" || f.Group != "check" || f.Severity != "warn" {
		t.Errorf("finding = id %q kind %q group %q severity %q", f.ID, f.Kind, f.Group, f.Severity)
	}
	if f.Device != "plug.kettle" || f.Summary.Code != "check.feedback" {
		t.Errorf("device %q summary %q", f.Device, f.Summary.Code)
	}
	if f.Confidence != "estimate" {
		t.Errorf("confidence = %q, want estimate without observation", f.Confidence)
	}
	if f.Evidence.IstValue == nil || *f.Evidence.IstValue != 500 {
		t.Errorf("ist value = %v, want the off threshold 500", f.Evidence.IstValue)
	}
	if f.Evidence.SollValue == nil || *f.Evidence.SollValue != 2000 {
		// Noise is 0 without days, so the reserve is 0 and the new threshold is P_Anlauf = 2000.
		t.Errorf("soll value = %v, want 2000", f.Evidence.SollValue)
	}
	if len(f.Apply) != 1 || f.Apply[0].Code != "feedback_thresholds" || f.Apply[0].RuleID != "on" {
		t.Fatalf("apply = %+v, want feedback_thresholds on the on rule", f.Apply)
	}
	rules := f.Apply[0].Rules
	if len(rules) != 2 || rules[0].RuleID != "on" || rules[1].RuleID != "off" {
		t.Fatalf("rules = %+v, want one RuleOps for on and one for off", rules)
	}
	if rules[0].Ops[0].Value != 2000.0 || rules[1].Ops[0].Value != 0.0 {
		t.Errorf("values = %v and %v, want 2000 and 0", rules[0].Ops[0].Value, rules[1].Ops[0].Value)
	}
}

func TestFeedbackNeedsOperatingPower(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, nil, fbDevice(), fbRule("on", "above", 1500), fbRule("off", "below", 500))
	if got := FeedbackFindings(in); len(got) != 0 {
		t.Fatalf("findings without op_power_w = %+v, want none", got)
	}
}

func TestFeedbackUsesLearnedThresholdMinimum(t *testing.T) {
	loc := berlinLoc(t)
	on := fbRule("on", "above", 0)
	on.Conditions[0].Learned = map[string]LearnedRef{"threshold": {Key: "site/grid_on_w", Min: 1200.0}}
	in := relInput(loc, nil, fbDevice(), on, fbRule("off", "below", 500))
	in.Learned = fbLearned(2000)
	got := FeedbackFindings(in)
	if len(got) != 1 || got[0].ID != "feedback:on" {
		t.Fatalf("findings = %+v, want feedback:on from the learned minimum 1200", got)
	}
}

func TestFeedbackConfirmedByObservation(t *testing.T) {
	loc := berlinLoc(t)
	var days []DayFile
	for d := 0; d < 3; d++ {
		day := relDay(fbDay(loc, 10+d))
		relReachable(&day, "plug.kettle")
		// A switch-on at 08:00:30 drops the export by 2000 W from the quarter before to the quarter after.
		q := 32
		day.Site.Seen[q-1], day.Site.Seen[q], day.Site.Seen[q+1] = 1, 1, 1
		day.Site.GridExport[q-1], day.Site.GridExport[q], day.Site.GridExport[q+1] = 2500, 500, 500
		relRuleEvent(&day, relAt(fbDay(loc, 10+d), 8, 0, 30), "plug.kettle", "on", true)
		days = append(days, day)
	}
	in := relInput(loc, days, fbDevice(), fbRule("on", "above", 1500), fbRule("off", "below", 500))
	in.Learned = fbLearned(2000)
	got := FeedbackFindings(in)
	if len(got) != 1 || got[0].Confidence != "high" {
		t.Fatalf("findings = %+v, want one finding with confidence high", got)
	}
}

func TestFeedbackDetectorWrapsFindingsAsCandidates(t *testing.T) {
	loc := berlinLoc(t)
	in := relInput(loc, nil, fbDevice(), fbRule("on", "above", 1500), fbRule("off", "below", 500))
	in.Learned = fbLearned(2000)
	got := relRun(FeedbackDetector.Run, in, "")
	if len(got) != 1 || got[0].Finding.ID != "feedback:on" || got[0].NEff < got[0].MinNEff {
		t.Fatalf("candidates = %+v, want one listed feedback candidate", got)
	}
}

// fbNoiseFact returns the noise_w fact of the first feedback finding, false without one.
func fbNoiseFact(got []Finding) (float64, bool) {
	if len(got) != 1 {
		return 0, false
	}
	for _, f := range got[0].Facts {
		if f.Code == "noise_w" {
			v, ok := f.Value.(float64)
			return v, ok
		}
	}
	return 0, false
}

// TestFeedbackNoiseIsMinuteNoiseWithThreeMinuteDays: with three minute days the
// noise near the switch-on threshold comes from the minutes, with fewer it
// stays the quarter noise of the days.
func TestFeedbackNoiseIsMinuteNoiseWithThreeMinuteDays(t *testing.T) {
	loc := berlinLoc(t)
	spikes := make([]float32, 1440)
	for m := range spikes {
		spikes[m] = 1500
		if m%10 == 5 {
			spikes[m] = 1580
		}
	}
	days := []DayFile{relDay(fbDay(loc, 15)), relDay(fbDay(loc, 16)), relDay(fbDay(loc, 17))}
	minutes := func(n int) fakeMinutes {
		fm := fakeMinutes{loc: loc, days: map[string]map[string][]float32{}}
		for _, d := range days[:n] {
			fm.days[d.Day] = map[string][]float32{"b:grid_export": spikes}
		}
		return fm
	}
	run := func(fm fakeMinutes) []Finding {
		in := relInput(loc, days, fbDevice(), fbRule("on", "above", 1500), fbRule("off", "below", 500))
		in.Learned = fbLearned(2000)
		in.Minutes = fm
		return FeedbackFindings(in)
	}

	noise, ok := fbNoiseFact(run(minutes(3)))
	if !ok || noise != 80 {
		t.Fatalf("noise with three minute days = %v (found %v), want the minute noise 80", noise, ok)
	}
	noise, ok = fbNoiseFact(run(minutes(2)))
	if !ok || noise != 0 {
		t.Fatalf("noise with two minute days = %v (found %v), want the quarter noise 0", noise, ok)
	}
}
