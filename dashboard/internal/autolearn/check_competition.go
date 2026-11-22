package autolearn

import (
	"context"
	"math"
	"sort"
	"time"
)

// CompetitionDetector finds rules that switch on from the same balance value.
// When two or more of them fire in one quarter hour and the grid import stays
// above the smallest of their loads, the loads compete for the surplus (spec
// 15.4 "Konkurrenz"). The proposal is a staircase of thresholds, one step per
// load. It runs once a day and needs the operating powers of the loads.
var CompetitionDetector = Detector{
	Name:      "competition",
	Group:     "reliability",
	Light:     false,
	PerDevice: false,
	Run:       competitionRun,
}

const (
	// competitionWindowDays is the number of the latest day files the check counts.
	competitionWindowDays = 14
	// competitionMinDays is the number of days with a competing quarter hour a finding needs.
	competitionMinDays = 3
	// competitionLimit is the lower Wilson bound of the share of days a finding needs.
	competitionLimit = 0.2
	// competitionMinReserveW is the smallest reserve above the staircase, in W.
	competitionMinReserveW = 100.0
)

// competitionRun lists the competition findings as candidates.
func competitionRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	return competitionCandidates(in)
}

// competitionCandidates checks every balance field that rules switch on from.
// The reserve is twice the reading noise of the export, at least 100 W.
func competitionCandidates(in *Input) []Candidate {
	if in == nil || len(in.Doc.Rules) == 0 {
		return nil
	}
	days := competitionWindow(in.Days)
	reserve := math.Max(2*feedbackNoise(in.Days), competitionMinReserveW)
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	var out []Candidate
	for _, field := range competitionFields(in.Doc) {
		if c, ok := competitionField(in, days, field, reserve, loc); ok {
			out = append(out, c)
		}
	}
	return out
}

// competitionWindow returns the latest competitionWindowDays day files in order.
func competitionWindow(days []DayFile) []DayFile {
	sorted := append([]DayFile(nil), days...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Day < sorted[j].Day })
	if len(sorted) > competitionWindowDays {
		sorted = sorted[len(sorted)-competitionWindowDays:]
	}
	return sorted
}

// competitionFields lists the balance fields of the enabled rules that switch on
// above a value, sorted.
func competitionFields(doc Doc) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range doc.Rules {
		if !r.Enabled {
			continue
		}
		for _, c := range r.Conditions {
			if c.Type == "balance_threshold" && c.Comparison == "above" && c.Field != "" && !seen[c.Field] {
				seen[c.Field] = true
				out = append(out, c.Field)
			}
		}
	}
	sort.Strings(out)
	return out
}

// compAcc collects what the competing quarter hours of the window hold.
type compAcc struct {
	hits    int
	rules   map[string]bool
	devices map[string]bool
}

// competitionField builds the finding of one balance field, or reports false
// when the rules do not compete in the window.
func competitionField(in *Input, days []DayFile, field string, reserve float64, loc *time.Location) (Candidate, bool) {
	members := map[string]bool{}
	for _, r := range in.Doc.Rules {
		if _, _, ok := compThreshold(r, field); ok && r.Enabled {
			members[r.ID] = true
		}
	}
	if len(members) < 2 {
		return Candidate{}, false
	}
	acc := compAcc{rules: map[string]bool{}, devices: map[string]bool{}}
	for i := range days {
		if competitionDay(in, &days[i], field, members, loc, &acc) {
			acc.hits++
		}
	}
	n := len(days)
	lower := WilsonLower(acc.hits, n)
	if acc.hits < competitionMinDays || lower < competitionLimit {
		return Candidate{}, false
	}
	stages, ok := competitionStaircase(in, days, field, acc, reserve)
	if !ok {
		return Candidate{}, false
	}
	finding, ok := competitionFinding(in, field, acc, stages)
	if !ok {
		return Candidate{}, false
	}
	return Candidate{
		Finding:  finding,
		Detector: "competition",
		NEff:     float64(n),
		MinNEff:  competitionMinDays,
		Lower:    lower,
		Limit:    competitionLimit,
	}, true
}

// competitionDay reports whether the day has a quarter hour in which two or more
// rules on the field switch on loads and the grid import in the next two quarter
// hours is above the smallest of these loads. The rules, devices and loads of
// such a quarter hour go into acc.
func competitionDay(in *Input, f *DayFile, field string, members map[string]bool, loc *time.Location, acc *compAcc) bool {
	fires := map[int]map[string]string{}
	for _, a := range f.Actions {
		if !members[a.Rule] || a.Want == nil || !*a.Want {
			continue
		}
		q := quarterOf(time.Unix(a.At, 0).In(loc))
		if fires[q] == nil {
			fires[q] = map[string]string{}
		}
		fires[q][a.Rule] = a.Device
	}
	quarters := make([]int, 0, len(fires))
	for q := range fires {
		quarters = append(quarters, q)
	}
	sort.Ints(quarters)
	hit := false
	for _, q := range quarters {
		byRule := fires[q]
		if len(byRule) < 2 || q+2 >= dayQuarters {
			continue
		}
		powers := map[string]float64{}
		for _, dev := range byRule {
			if p, ok := learnedOperatingPower(in.Learned, dev); ok {
				powers[dev] = p
			}
		}
		if len(powers) < 2 {
			continue
		}
		minP := math.Inf(1)
		for _, p := range powers {
			minP = math.Min(minP, p)
		}
		if !compImportAbove(f, q+1, minP) || !compImportAbove(f, q+2, minP) {
			continue
		}
		hit = true
		for rule, dev := range byRule {
			acc.rules[rule] = true
			if _, ok := powers[dev]; ok {
				acc.devices[dev] = true
			}
		}
	}
	return hit
}

// compImportAbove reports whether quarter q of the day has a reading and a grid
// import above w.
func compImportAbove(f *DayFile, q int, w float64) bool {
	return f.Site.Seen[q] != 0 && float64(f.Site.GridImport[q]) > w
}

// compStage is one step of the staircase: the load that is switched on at it,
// its threshold and whether the season reaches it on too few days.
type compStage struct {
	device    string
	threshold float64
	reached   int
	dormant   bool
}

// competitionStaircase orders the loads and sums their operating powers. A load
// with a fixed cycle energy comes first, then the one the user starts by hand
// most often, then the higher operating power. Step k is the sum of the first k
// powers plus the reserve. It reports false without two loads with a rule.
func competitionStaircase(in *Input, days []DayFile, field string, acc compAcc, reserve float64) ([]compStage, bool) {
	type load struct {
		id    string
		p     float64
		fixed bool
		hand  float64
	}
	var loads []load
	for dev := range acc.devices {
		p, _ := learnedOperatingPower(in.Learned, dev)
		loads = append(loads, load{id: dev, p: p, fixed: hasCycleEnergy(days, dev), hand: handShare(days, dev)})
	}
	sort.Slice(loads, func(i, j int) bool {
		a, b := loads[i], loads[j]
		if a.fixed != b.fixed {
			return a.fixed
		}
		if a.hand != b.hand {
			return a.hand > b.hand
		}
		if a.p != b.p {
			return a.p > b.p
		}
		return a.id < b.id
	})
	if len(loads) < 2 {
		return nil, false
	}
	var stages []compStage
	sum := 0.0
	for _, l := range loads {
		sum += l.p
		th := feedbackCeil(sum + reserve)
		reached := daysReaching(days, th)
		stages = append(stages, compStage{device: l.id, threshold: th, reached: reached,
			dormant: reached < competitionMinDays})
	}
	return stages, true
}

// hasCycleEnergy reports whether the window holds a finished cycle of the device with energy.
func hasCycleEnergy(days []DayFile, device string) bool {
	for i := range days {
		for _, c := range days[i].Cycles {
			if c.Device == device && c.Wh > 0 {
				return true
			}
		}
	}
	return false
}

// handShare is the share of the device's starts in the window that were made by
// hand, 0 when there were no starts.
func handShare(days []DayFile, device string) float64 {
	var hand, rule float64
	for i := range days {
		dd := days[i].Devices[device]
		if dd == nil {
			continue
		}
		for q := 0; q < dayQuarters; q++ {
			hand += float64(dd.Hand[q])
			rule += float64(dd.Rule[q])
		}
	}
	if hand+rule == 0 {
		return 0
	}
	return hand / (hand + rule)
}

// daysReaching counts the days of the window whose export reaches th in some quarter hour.
func daysReaching(days []DayFile, th float64) int {
	n := 0
	for i := range days {
		for q := 0; q < dayQuarters; q++ {
			if days[i].Site.Seen[q] != 0 && float64(days[i].Site.GridExport[q]) >= th {
				n++
				break
			}
		}
	}
	return n
}

// competitionFinding builds the finding with one threshold change per competing
// rule, the rule of the first step first, and an open rule fix on that rule.
// It reports false when no competing rule has a threshold on the field.
func competitionFinding(in *Input, field string, acc compAcc, stages []compStage) (Finding, bool) {
	rules := ruleByID(in.Doc)
	var ids []string
	for id := range acc.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	type mapped struct {
		rule  Rule
		step  int
		index int
		cur   float64
	}
	var ms []mapped
	for _, id := range ids {
		r, ok := rules[id]
		if !ok {
			continue
		}
		for k, st := range stages {
			info, ok := in.Devices[st.device]
			if !ok || !actsOnDevice(r, info) {
				continue
			}
			idx, cur, ok := compThreshold(r, field)
			if ok {
				ms = append(ms, mapped{rule: r, step: k, index: idx, cur: cur})
			}
			break
		}
	}
	sort.SliceStable(ms, func(a, b int) bool { return ms[a].step < ms[b].step })

	if len(ms) == 0 {
		return Finding{}, false
	}
	var ops []RuleOps
	var marks []Mark
	for _, st := range stages {
		marks = append(marks, Mark{Value: st.threshold, Dormant: st.dormant})
	}
	istValue := math.Inf(1)
	for _, m := range ms {
		ops = append(ops, RuleOps{RuleID: m.rule.ID, Ops: []Op{{
			Path: []any{"conditions", m.index, "threshold"}, Value: stages[m.step].threshold}}})
		istValue = math.Min(istValue, m.cur)
	}
	sollValue := stages[0].threshold
	first := ms[0].rule.ID
	return Finding{
		ID:         "competition:" + field,
		Kind:       "competition",
		Group:      "check",
		RuleIDs:    ids,
		Severity:   "info",
		Confidence: "estimate",
		Summary:    Text{Code: "check.competition", Params: map[string]any{"rules": len(ids), "field": field}},
		Evidence:   Evidence{Shape: "scale", IstValue: &istValue, SollValue: &sollValue, Marks: marks},
		Apply: []Fix{
			{Code: "stagger", RuleID: first, Rules: ops},
			{Code: "open_rule", RuleID: first},
		},
	}, true
}

// compThreshold returns the index and the value of the first condition of the
// rule on the balance field that switches on above a value. A learned threshold
// counts with its minimum, the value the rule can reach first.
func compThreshold(r Rule, field string) (int, float64, bool) {
	for i, c := range r.Conditions {
		if c.Type != "balance_threshold" || c.Field != field || c.Comparison != "above" {
			continue
		}
		if ref, learned := c.Learned["threshold"]; learned {
			v, ok := asFloat(ref.Min)
			return i, v, ok
		}
		return i, c.Threshold, true
	}
	return 0, 0, false
}
