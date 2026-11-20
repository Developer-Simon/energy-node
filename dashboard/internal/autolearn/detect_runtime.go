package autolearn

import (
	"context"
	"math"
	"time"
)

// RuntimeDetector suggests switching a device off after its usual runtime when
// some runs went on far longer and no rule ended them. It follows spec 15.5
// "Übliche Laufzeit, Vergessen" and works on the logarithm of the durations.
var RuntimeDetector = Detector{
	Name:      "runtime",
	Group:     "habit",
	Light:     false,
	PerDevice: true,
	Run:       runtimeRun,
}

const (
	// runtimeMinRatio is the factor the medians of two clusters must lie apart.
	runtimeMinRatio = 3.0
	// runtimeOtsuBins is the number of histogram bins for the split of the log
	// durations. The spec gives no number, the value is an assumption.
	runtimeOtsuBins = 16
	// runtimeForgetSigma is the spread above the median after which a run is forgotten.
	runtimeForgetSigma = 3.0
	// runtimeSuggestSigma is the spread above the median of the suggested switch-off.
	runtimeSuggestSigma = 2.0
	// runtimeRoundMin rounds the suggested switch-off up to whole steps of minutes.
	runtimeRoundMin = 5.0
	// runtimeMinOrdinary and runtimeMinForgotten are the counts for a sure finding.
	runtimeMinOrdinary  = 8
	runtimeMinForgotten = 2
	// runtimeMinExtraKWh is the extra energy of the forgotten runs for a sure finding.
	runtimeMinExtraKWh = 0.5
)

// runtimeStat is the median and the spread of the log durations of one cluster.
type runtimeStat struct {
	m, s float64
}

// runtimeTally counts the runs of one cluster. extraWh is the energy above the
// usual runtime of the forgotten runs, longest the longest forgotten run in minutes.
type runtimeTally struct {
	ordinary, forgotten, overnight int
	extraWh, longest               float64
}

// runtimeRun finds the forgotten runs of one device and suggests a switch-off
// time for the cluster with the most extra energy, if the finding is sure.
func runtimeRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	var runs []Run
	for _, r := range runsIn(in.Days, loc) {
		if r.Device == device && r.End.After(r.Start) {
			runs = append(runs, r)
		}
	}
	if len(runs) < runtimeMinOrdinary+runtimeMinForgotten {
		return nil
	}

	logs := make([]float64, len(runs))
	first, last := runs[0].Start, runs[0].End
	for i, r := range runs {
		logs[i] = math.Log(runtimeMinutes(r))
		first = minTime(first, r.Start)
		last = maxTime(last, r.End)
	}
	member, stats := runtimeClusters(logs)

	tallies := make([]runtimeTally, len(stats))
	for i, r := range runs {
		g := member[i]
		dur := runtimeMinutes(r)
		st := stats[g]
		t := &tallies[g]
		if r.EndOrigin == OriginRule || dur <= math.Exp(st.m+runtimeForgetSigma*st.s) {
			t.ordinary++
			continue
		}
		t.forgotten++
		t.extraWh += runtimeExtraWh(r.Wh, dur, math.Exp(st.m))
		t.longest = math.Max(t.longest, dur)
		if runtimeOvernight(r, in.Base, loc) {
			t.overnight++
		}
	}

	weeks := math.Max(last.Sub(first).Hours()/24/7, 1)
	best := -1
	for g := range tallies {
		if !runtimeSure(tallies[g]) {
			continue
		}
		if best < 0 || tallies[g].extraWh > tallies[best].extraWh {
			best = g
		}
	}
	if best < 0 {
		return nil
	}
	return []Candidate{runtimeCandidate(device, stats[best], tallies[best], weeks)}
}

// runtimeMinutes is the length of a run in minutes.
func runtimeMinutes(r Run) float64 {
	return r.End.Sub(r.Start).Minutes()
}

// runtimeClusters splits the log durations at the Otsu cut when the medians of
// the two sides lie at least a factor 3 apart. It returns the cluster of each
// run and the median and spread of each cluster.
func runtimeClusters(logs []float64) (member []int, stats []runtimeStat) {
	member = make([]int, len(logs))
	k := 1
	if cut, ok := OtsuSplit(logs, runtimeOtsuBins); ok {
		var lo, hi []float64
		for _, v := range logs {
			if v < cut {
				lo = append(lo, v)
			} else {
				hi = append(hi, v)
			}
		}
		if Median(hi)-Median(lo) >= math.Log(runtimeMinRatio) {
			k = 2
			for i, v := range logs {
				if v >= cut {
					member[i] = 1
				}
			}
		}
	}
	vals := make([][]float64, k)
	for i, v := range logs {
		vals[member[i]] = append(vals[member[i]], v)
	}
	stats = make([]runtimeStat, k)
	for g := range vals {
		stats[g] = runtimeStat{m: Median(vals[g]), s: MADScale(vals[g])}
	}
	return member, stats
}

// runtimeExtraWh is the energy a run drew beyond the usual runtime. The mean
// power of the run times the minutes beyond usual, so it is wh * (dur-usual)/dur.
func runtimeExtraWh(wh, dur, usual float64) float64 {
	if dur <= 0 {
		return 0
	}
	return math.Max(wh*(dur-usual)/dur, 0)
}

// runtimeOvernight reports whether the run lasted through the night rest of
// its day type, or through a day marked absent. It is a mark on the finding and
// has no threshold of its own.
func runtimeOvernight(r Run, base Base, loc *time.Location) bool {
	ls := r.Start.In(loc)
	le := r.End.In(loc)
	d := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, loc)
	for d.Before(le) {
		key := d.Format(dayLayout)
		if base.Away[key] {
			return true
		}
		if g, ok := base.DayType[key]; ok {
			if nr, ok := base.NightRest[g]; ok {
				point := d.Add(time.Duration(nr * float64(time.Minute)))
				if r.Start.Before(point) && point.Before(r.End) {
					return true
				}
			}
		}
		d = d.AddDate(0, 0, 1)
	}
	return false
}

// runtimeSure is the gate of a sure finding: enough ordinary and forgotten runs,
// and more than 0.5 kWh of extra energy in the forgotten runs.
func runtimeSure(t runtimeTally) bool {
	return t.ordinary >= runtimeMinOrdinary && t.forgotten >= runtimeMinForgotten &&
		t.extraWh/1000 > runtimeMinExtraKWh
}

// runtimeCandidate builds the candidate of one cluster. The suggested switch-off
// is exp(m + 2s) rounded up to 5 minutes, the benefit is the extra energy per week.
func runtimeCandidate(device string, st runtimeStat, t runtimeTally, weeks float64) Candidate {
	total := t.ordinary + t.forgotten
	extraKWh := t.extraWh / 1000
	usual := math.Exp(st.m)
	suggest := math.Ceil(math.Exp(st.m+runtimeSuggestSigma*st.s)/runtimeRoundMin) * runtimeRoundMin
	longest := t.longest
	benefit := Benefit{Unit: "kwh_week", Value: extraKWh / weeks}
	finding := Finding{
		ID:         "runtime:" + device,
		Kind:       "runtime",
		Group:      "habit",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "runtime.forgotten", Params: map[string]any{
			"runs":      total,
			"forgotten": t.forgotten,
			"usual_min": math.Round(usual),
			"extra_kwh": math.Round(extraKWh*10) / 10,
		}},
		Evidence: Evidence{Shape: "scale", IstValue: &longest, SollValue: &suggest},
		Facts:    []Fact{{Code: "overnight", Value: t.overnight}},
		Apply: []Fix{{Code: "runtime_off", Template: "runtime_off",
			Prefill: map[string]any{"device": device, "minutes": int(suggest)}}},
		Device:  device,
		Benefit: &benefit,
	}
	return Candidate{
		Finding:  finding,
		Detector: "runtime",
		NEff:     float64(total),
		MinNEff:  runtimeMinOrdinary + runtimeMinForgotten,
		Benefit:  benefit,
		Values:   map[string]float64{"extra_kwh": extraKWh},
		Spread:   map[string]float64{"log_sigma": st.s},
	}
}

// minTime and maxTime return the earlier and the later of two times.
func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
