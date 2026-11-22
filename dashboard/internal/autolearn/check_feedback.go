package autolearn

import (
	"context"
	"math"
	"sort"
	"time"
)

// FeedbackDetector checks a rule pair that switches one load on and off by the
// same balance value. The load itself moves that value, so the rule can switch
// itself off again (spec 15.4 "Rueckkopplung"). It runs with the light rounds
// and needs no history for the static part.
var FeedbackDetector = Detector{
	Name:      "feedback",
	Group:     "reliability",
	Light:     true,
	PerDevice: false,
	Run:       feedbackRun,
}

const (
	// feedbackField is the balance value the pair switches on: the export, which is the surplus.
	feedbackField = "grid_export"
	// feedbackRoundW is the step the proposed thresholds are rounded up to.
	feedbackRoundW = 10.0
	// feedbackConfirmShare is how far the observed step may be from the operating power.
	feedbackConfirmShare = 0.3
	// feedbackMinChanges is the number of switch-ons needed to observe the step.
	feedbackMinChanges = 3
)

// feedbackRun lists the feedback findings as candidates. The check is static, so
// one case is enough and the candidate always passes the gate.
func feedbackRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	var out []Candidate
	for _, f := range FeedbackFindings(in) {
		out = append(out, Candidate{Finding: f, Detector: "feedback", NEff: 1, MinNEff: 1, Lower: 1, Limit: 1})
	}
	return out
}

// FeedbackFindings returns the rules that switch themselves off again. For each
// device with an operating power, a rule with "above X" and a rule with
// "below Xaus" on the export that both act on it form a pair. The pair is
// unstable when X - P_Anlauf - Rauschen <= Xaus. A learned threshold counts
// with its smallest value for the switch-on rule and its largest for the
// switch-off rule, since that is the worst case. The finding is confirmed, and
// its confidence high, when the median export step after the switch-ons is the
// operating power within 30 %.
func FeedbackFindings(in *Input) []Finding {
	if in == nil || len(in.Doc.Rules) == 0 {
		return nil
	}
	noise := feedbackNoise(in.Days)
	reserve := 2 * noise
	rules := ruleByID(in.Doc)
	var out []Finding
	seen := map[string]bool{}
	for _, device := range sortedDeviceIDs(in.Devices) {
		info := in.Devices[device]
		p, ok := learnedOperatingPower(in.Learned, device)
		if !ok {
			continue
		}
		pAnlauf := feedbackInrush(in.Days, device, p)
		for _, on := range in.Doc.Rules {
			if !on.Enabled || seen[on.ID] || !actsOnDevice(on, info) {
				continue
			}
			onIdx, x, ok := balanceSide(on, "above", true)
			if !ok {
				continue
			}
			off, offIdx, xAus, ok := feedbackOffRule(in.Doc, info, on.ID, rules)
			if !ok || x-pAnlauf-noise > xAus {
				continue
			}
			seen[on.ID] = true
			newOn := feedbackCeil(pAnlauf + reserve)
			newOff := 0 - feedbackCeil(reserve)
			confidence := "estimate"
			if feedbackConfirmed(in, device, p) {
				confidence = "high"
			}
			out = append(out, Finding{
				ID:         FindingID("feedback", on.ID),
				Kind:       "feedback",
				Group:      "check",
				RuleIDs:    []string{on.ID, off.ID},
				Severity:   "warn",
				Confidence: confidence,
				Summary: Text{Code: "check.feedback", Params: map[string]any{
					"rule": ruleName(rules, on.ID), "device": feedbackDeviceName(info, device)}},
				Evidence: Evidence{Shape: "scale", IstValue: &xAus, SollValue: &newOn},
				Facts: []Fact{
					{Code: "op_w", Value: roundTenth(p)},
					{Code: "peak_w", Value: roundTenth(pAnlauf)},
					{Code: "noise_w", Value: roundTenth(noise)},
				},
				Apply: []Fix{{Code: "feedback_thresholds", RuleID: on.ID, Rules: []RuleOps{
					{RuleID: on.ID, Ops: []Op{{Path: []any{"conditions", onIdx, "threshold"}, Value: newOn}}},
					{RuleID: off.ID, Ops: []Op{{Path: []any{"conditions", offIdx, "threshold"}, Value: newOff}}},
				}}},
				Device: device,
			})
		}
	}
	return out
}

// feedbackOffRule returns the first enabled rule that switches the device off
// on the export below a threshold, with its condition index and the threshold.
func feedbackOffRule(doc Doc, info DeviceInfo, onID string, rules map[string]Rule) (Rule, int, float64, bool) {
	for _, r := range doc.Rules {
		if r.ID == onID || !r.Enabled || !actsOnDevice(r, info) {
			continue
		}
		idx, xAus, ok := balanceSide(r, "below", false)
		if ok {
			return r, idx, xAus, true
		}
	}
	return Rule{}, 0, 0, false
}

// balanceSide returns the index and the number of the export condition with the
// given comparison. A learned threshold gives its minimum when low is true and
// its maximum otherwise. It reports false without such a condition or without a
// number.
func balanceSide(r Rule, comparison string, low bool) (int, float64, bool) {
	for i, c := range r.Conditions {
		if c.Type != "balance_threshold" || c.Field != feedbackField || c.Comparison != comparison {
			continue
		}
		if ref, learned := c.Learned["threshold"]; learned {
			bound := ref.Max
			if low {
				bound = ref.Min
			}
			v, ok := asFloat(bound)
			return i, v, ok
		}
		return i, c.Threshold, true
	}
	return 0, 0, false
}

// actsOnDevice reports whether one action of the rule targets an entity of the device.
func actsOnDevice(r Rule, info DeviceInfo) bool {
	for _, a := range r.Actions {
		if a.EntityID == "" {
			continue
		}
		for _, e := range info.Entities {
			if a.EntityID == e {
				return true
			}
		}
	}
	return false
}

// feedbackConfirmed reports whether the median export step around the switch-ons
// of the device, from the quarter before to the quarter after, is the operating
// power within feedbackConfirmShare. The step is negative when the load draws.
func feedbackConfirmed(in *Input, device string, p float64) bool {
	if p <= 0 {
		return false
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	days := make(map[string]*DayFile, len(in.Days))
	for i := range in.Days {
		days[in.Days[i].Day] = &in.Days[i]
	}
	var steps []float64
	for _, e := range ruleEventsOf(in.Days, device) {
		if !e.on {
			continue
		}
		t := time.Unix(e.at, 0).In(loc)
		f := days[t.Format(dayLayout)]
		q := quarterOf(t)
		if f == nil || q < 1 || q+1 >= dayQuarters {
			continue
		}
		if f.Site.Seen[q-1] == 0 || f.Site.Seen[q+1] == 0 {
			continue
		}
		steps = append(steps, float64(f.Site.GridExport[q+1]-f.Site.GridExport[q-1]))
	}
	if len(steps) < feedbackMinChanges {
		return false
	}
	med := Median(steps)
	return med < 0 && math.Abs(-med-p) <= feedbackConfirmShare*p
}

// feedbackNoise is QuarterNoise of the export over all days in order. Quarters
// without a reading are gaps and do not count as steps.
func feedbackNoise(days []DayFile) float64 {
	sorted := append([]DayFile(nil), days...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Day < sorted[j].Day })
	var series []float32
	for i := range sorted {
		for q := 0; q < dayQuarters; q++ {
			if sorted[i].Site.Seen[q] == 0 {
				series = append(series, float32(math.NaN()))
				continue
			}
			series = append(series, sorted[i].Site.GridExport[q])
		}
	}
	return QuarterNoise(series)
}

// feedbackInrush is the median start peak of the device over its cycles, or the
// operating power when no cycle has a peak.
func feedbackInrush(days []DayFile, device string, p float64) float64 {
	var peaks []float64
	for i := range days {
		for _, c := range days[i].Cycles {
			if c.Device == device && c.PeakW > 0 {
				peaks = append(peaks, float64(c.PeakW))
			}
		}
	}
	if len(peaks) == 0 {
		return p
	}
	return Median(peaks)
}

// learnedOperatingPower reads device/<id>/op_power_w, false when it is missing or not positive.
func learnedOperatingPower(learned map[string]LearnedEntry, device string) (float64, bool) {
	e, ok := learned["device/"+device+"/op_power_w"]
	if !ok {
		return 0, false
	}
	v, ok := asFloat(e.Value)
	return v, ok && v > 0
}

// asFloat reads a number of the types a learned value or a rule can carry.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// feedbackCeil rounds up to feedbackRoundW.
func feedbackCeil(v float64) float64 {
	return math.Ceil(v/feedbackRoundW) * feedbackRoundW
}

// feedbackDeviceName is the device name, or its ID when the name is empty.
func feedbackDeviceName(info DeviceInfo, id string) string {
	if info.Name != "" {
		return info.Name
	}
	return id
}

// roundTenth rounds to one decimal, for the facts.
func roundTenth(v float64) float64 {
	return math.Round(v*10) / 10
}
