package autolearn

import (
	"context"
	"math"
	"sort"
	"time"
)

// NoEffectDetector flags a rule whose actions are sent but do not change the
// device in the reaction time (spec 15.4 "Aktion ohne Wirkung"). The cause is
// read from the failures: unreachable quarters point to the offline window,
// clustered failures to the rule, scattered ones get a repeat.
var NoEffectDetector = Detector{
	Name:      "no_effect",
	Group:     "reliability",
	Light:     true,
	PerDevice: true,
	Run:       noEffectRun,
}

const (
	// noEffectMinFailures is the number of failures a finding needs.
	noEffectMinFailures = 3
	// noEffectLimit is the lower Wilson bound of the failure share a finding needs.
	noEffectLimit = 0.2
	// noEffectRepeatSeconds is the repeat interval of the repeat fix.
	noEffectRepeatSeconds = 60
	// noEffectMaxEvidence caps the failure times kept as evidence.
	noEffectMaxEvidence = 50
)

// noEffectTally counts the actions of one rule on the device.
type noEffectTally struct {
	considered int
	failures   int
	// offline counts the failures in quarters the device could not be reached.
	offline int
	// reachable holds the minute of the day of the other failures.
	reachable []float64
	failAt    []int64
}

// noEffectRun counts, per rule, the actions on the device that were not already
// in the target state and did not take effect, and suggests a fix for each rule
// whose failure share is sure enough.
func noEffectRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	tallies := map[string]*noEffectTally{}
	for i := range in.Days {
		f := &in.Days[i]
		dd := f.Devices[device]
		for _, a := range f.Actions {
			if a.Device != device || a.There || a.Rule == "" {
				continue
			}
			t := tallies[a.Rule]
			if t == nil {
				t = &noEffectTally{}
				tallies[a.Rule] = t
			}
			t.considered++
			if a.OK {
				continue
			}
			t.failures++
			t.failAt = append(t.failAt, a.At)
			at := time.Unix(a.At, 0).In(loc)
			if unreachable(dd, quarterOf(at)) {
				t.offline++
				continue
			}
			t.reachable = append(t.reachable, float64(at.Hour()*60+at.Minute()))
		}
	}

	rules := ruleByID(in.Doc)
	ids := make([]string, 0, len(tallies))
	for id := range tallies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Candidate
	for _, id := range ids {
		rule, ok := rules[id]
		if !ok {
			continue
		}
		t := tallies[id]
		if t.failures < noEffectMinFailures {
			continue
		}
		lower := WilsonLower(t.failures, t.considered)
		if lower < noEffectLimit {
			continue
		}
		out = append(out, noEffectCandidate(device, in.Devices[device], rule, t, lower))
	}
	return out
}

// noEffectCandidate builds the finding of one rule from its failures.
func noEffectCandidate(device string, info DeviceInfo, rule Rule, t *noEffectTally, lower float64) Candidate {
	share := math.Round(float64(t.failures)/float64(t.considered)*100) / 100
	params := map[string]any{
		"rule":     rule.Name,
		"device":   deviceName(info, device),
		"failures": t.failures,
		"share":    share,
	}
	var apply []Fix
	var facts []Fact
	switch {
	case t.offline == t.failures:
		params["feature"] = "offline_window"
		facts = []Fact{{Code: "cause", Value: "offline_window"}}
		apply = []Fix{{Code: "open_rule", RuleID: rule.ID}}
	default:
		if center, clustered := reliabilityCluster(t.reachable); clustered {
			params["feature"] = "clustered"
			params["window_from"] = int(wrapMinutes(center - reliabilityWindowMin))
			params["window_to"] = int(wrapMinutes(center + reliabilityWindowMin))
			facts = []Fact{{Code: "cause", Value: "clustered"}}
			apply = []Fix{{Code: "open_rule", RuleID: rule.ID}}
		} else {
			params["feature"] = "scattered"
			facts = []Fact{{Code: "cause", Value: "scattered"}}
			apply = []Fix{{Code: "repeat", RuleID: rule.ID,
				Ops: []Op{{Path: []any{"repeat_seconds"}, Value: noEffectRepeatSeconds}}}}
		}
	}

	at := t.failAt
	if len(at) > noEffectMaxEvidence {
		at = at[len(at)-noEffectMaxEvidence:]
	}
	from, to := t.failAt[0], t.failAt[0]
	for _, v := range t.failAt {
		from = min(from, v)
		to = max(to, v)
	}
	finding := Finding{
		ID:         FindingID("no_effect", rule.ID) + ":" + device,
		Kind:       "no_effect",
		Group:      "check",
		RuleIDs:    []string{rule.ID},
		Severity:   "warn",
		Confidence: "high",
		Summary:    Text{Code: "check.no_effect", Params: params},
		Evidence:   Evidence{Shape: "events", From: from, To: to, Ist: append([]int64(nil), at...)},
		Facts:      facts,
		Apply:      apply,
		Device:     device,
	}
	return Candidate{
		Finding:  finding,
		Detector: "no_effect",
		NEff:     float64(t.considered),
		MinNEff:  noEffectMinFailures,
		Lower:    lower,
		Limit:    noEffectLimit,
		Values:   map[string]float64{"share": share},
		Spread:   map[string]float64{"share": 1 / float64(t.considered)},
	}
}

// deviceName returns the name of the device, or its ID when the name is empty.
func deviceName(info DeviceInfo, id string) string {
	if info.Name != "" {
		return info.Name
	}
	return id
}
