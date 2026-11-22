package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// ShiftDetector suggests starting a switchable load in the surplus of the day
// when it runs from the grid (spec 15.6 "Lastverschiebung"). A run counts when
// more than half of its energy came from the grid. It is movable when the
// surplus of the same day held its power for its whole run, after the runs
// that already took surplus. Surplus is shared by the switchable devices in
// the order of their grid energy, so a stretch counts once. The device is only
// proposed when its start times spread, a device that always starts at the same
// minute is a habit and not a shiftable load.
var ShiftDetector = Detector{
	Name:      "load_shift",
	Group:     "energy",
	Light:     false,
	PerDevice: true,
	Run:       shiftRunFor,
}

const (
	// shiftMinRuns is the number of grid runs from which a suggestion is safe.
	shiftMinRuns = 8
	// shiftLimit is the Wilson lower bound of the movable share.
	shiftLimit = 0.5
	// shiftGridShare is the share of a run's energy from the grid that makes it a grid run.
	shiftGridShare = 0.5
	// shiftSpreadMin is the spread of the start times in minutes above which a device is movable.
	shiftSpreadMin = 60.0
	// shiftLatestPct is the percentile of the start times that sets the latest start.
	// The plan gives the value 75, read as the percentile.
	shiftLatestPct = 0.75
	// shiftLatestStepMin rounds the latest start.
	shiftLatestStepMin = 15
	// shiftRoundW rounds the surplus threshold.
	shiftRoundW = 100.0
	// shiftHoldS is the hold of the surplus condition in seconds, as the template sets it.
	shiftHoldS = 120
	// shiftQuarterS is the length of a quarter hour in seconds.
	shiftQuarterS = 900
	// shiftReplayDays is the number of days the replay check reads.
	shiftReplayDays = 14
)

// shiftScopeDay is one day the detector reads: the surplus per quarter hour and
// the quarters the day has a measurement for.
type shiftScopeDay struct {
	date    string
	seen    [dayQuarters]bool
	surplus [dayQuarters]float64
}

// shiftRun is one finished cycle of a device on a day of the scope.
type shiftRun struct {
	device   string
	day      int
	q0, dur  int
	startMin float64
	durS     int
	grid     float64
	heavy    bool
	claimant bool
	opW      float64
	claimed  bool
}

// shiftRunFor returns the shift suggestion of one device, if it is movable and
// safe. The other switchable devices decide on the same surplus, so the run is
// allocated for all of them first.
func shiftRunFor(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	info, ok := in.Devices[device]
	if !ok || !info.Switchable || len(info.Entities) == 0 {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	days, runs := shiftAllocate(in, loc)

	var starts, opW, runDur, claimedDur []float64
	n, k := 0, 0
	heavyDay := make([]int, len(days))
	claimedDay := make([]int, len(days))
	for i := range runs {
		r := &runs[i]
		if r.device != device {
			continue
		}
		starts = append(starts, r.startMin)
		opW = append(opW, r.opW)
		runDur = append(runDur, float64(r.durS))
		if r.heavy {
			n++
			heavyDay[r.day]++
		}
		if r.claimed {
			k++
			claimedDay[r.day]++
			claimedDur = append(claimedDur, float64(r.durS))
		}
	}
	if n < shiftMinRuns {
		return nil
	}
	center := CircMedian(starts)
	spread := CircSpread(starts, center)
	if spread <= shiftSpreadMin {
		return nil
	}
	lower := WilsonLower(k, n)
	if lower < shiftLimit {
		return nil
	}
	agree := shiftHalvesAgree(heavyDay, claimedDay)

	var claimedWh float64
	for i := range runs {
		if runs[i].device == device && runs[i].claimed {
			claimedWh += runs[i].grid
		}
	}
	weeks := math.Max(1, float64(len(days))/7)
	benefit := Benefit{Unit: "kwh_week", Value: claimedWh / 1000 / weeks}

	p := Median(opW)
	if p <= 0 {
		return nil
	}
	onW := math.Max(shiftRoundW, math.Round(p/shiftRoundW)*shiftRoundW)
	runS := int(Median(claimedDur))
	if len(claimedDur) == 0 {
		runS = int(Median(runDur))
	}
	latest := shiftLatest(starts)
	latestClock := shiftClock(latest)
	peak := Median(shiftPeaks(in, device))
	if peak <= 0 {
		peak = p
	}
	cycleWhMed := Median(shiftWh(in, device))

	conf := "estimate"
	if agree {
		conf = "high"
	}
	entity := info.Entities[0]
	id := "shift:" + device
	finding := Finding{
		ID:         id,
		Kind:       "load_shift",
		Group:      "energy",
		Severity:   "info",
		Confidence: conf,
		Summary: Text{Code: "shift.load", Params: map[string]any{
			"device": device, "kwh": math.Round(benefit.Value*10) / 10, "latest": latestClock, "runs": n}},
		Evidence: Evidence{Shape: "curve", From: 0, To: int64(minutesPerDay), Quarters: shiftTypicalDay(days)},
		Facts: []Fact{
			{Code: "spread_min", Value: math.Round(spread)},
			{Code: "hit_rate", Value: float64(k) / float64(n)},
		},
		Apply: []Fix{{Code: "shift_start", Template: "shift_start", Prefill: map[string]any{
			"device": device, "entity": entity, "on_w": onW, "latest": latestClock, "min_run_s": runS}}},
		Device:  device,
		Benefit: &benefit,
	}
	cand := Candidate{
		Finding:     finding,
		Detector:    "load_shift",
		NEff:        float64(n),
		MinNEff:     shiftMinRuns,
		Lower:       lower,
		Limit:       shiftLimit,
		HalvesAgree: agree,
		Benefit:     benefit,
		Values:      map[string]float64{"hit_rate": float64(k) / float64(n), "latest": float64(latest)},
	}
	if verdict, err := shiftReplay(ctx, in, device, id, onW, latest, p, peak, cycleWhMed, runS); err == nil && verdict != nil {
		cand.Replay = verdict
	}
	return []Candidate{cand}
}

// shiftAllocate reads the scope days and the cycles of all devices. Runs that
// took the surplus on their own are taken off the surplus first. Then the grid
// runs of the switchable devices claim a stretch of their length in the day, by
// grid energy in descending order. A claimed stretch counts once.
func shiftAllocate(in *Input, loc *time.Location) ([]shiftScopeDay, []shiftRun) {
	files := make([]*DayFile, 0, len(in.Days))
	for i := range in.Days {
		if !in.Base.Away[in.Days[i].Day] {
			files = append(files, &in.Days[i])
		}
	}
	sort.SliceStable(files, func(a, b int) bool { return files[a].Day < files[b].Day })

	opByDevice := map[string][]float64{}
	for _, f := range files {
		for _, c := range f.Cycles {
			if c.Wh > 0 && c.OpW > 0 {
				opByDevice[c.Device] = append(opByDevice[c.Device], float64(c.OpW))
			}
		}
	}
	var days []shiftScopeDay
	var runs []shiftRun
	for _, f := range files {
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		d := shiftScopeDay{date: f.Day}
		_, without := surWinSeries(in, f, t)
		for q := 0; q < dayQuarters; q++ {
			d.seen[q] = f.Site.Seen[q] != 0
			if d.seen[q] {
				d.surplus[q] = without[q]
			}
		}
		days = append(days, d)
		dayIdx := len(days) - 1

		var taken [dayQuarters]float64
		var claimants []int
		for _, c := range f.Cycles {
			if c.Wh <= 0 || c.DurS <= 0 {
				continue
			}
			at := time.Unix(c.Start, 0).In(loc)
			r := shiftRun{
				device:   c.Device,
				day:      dayIdx,
				q0:       at.Hour()*4 + at.Minute()/15,
				dur:      max(1, int(math.Ceil(float64(c.DurS)/shiftQuarterS))),
				startMin: float64(at.Hour()*60 + at.Minute()),
				durS:     c.DurS,
				grid:     float64(c.Wh) * float64(c.Grid),
				heavy:    float64(c.Grid) > shiftGridShare,
				opW:      float64(c.OpW),
			}
			r.claimant = r.heavy && in.Devices[c.Device].Switchable
			runs = append(runs, r)
			idx := len(runs) - 1
			if r.claimant {
				claimants = append(claimants, idx)
			} else {
				shiftMark(&taken, r.q0, r.dur, r.opW)
			}
		}

		sort.SliceStable(claimants, func(a, b int) bool {
			ra, rb := runs[claimants[a]], runs[claimants[b]]
			if ra.grid != rb.grid {
				return ra.grid > rb.grid
			}
			if ra.startMin != rb.startMin {
				return ra.startMin < rb.startMin
			}
			return ra.device < rb.device
		})
		for _, idx := range claimants {
			r := &runs[idx]
			pw := Median(opByDevice[r.device])
			if pw <= 0 {
				continue
			}
			for s := 0; s+r.dur <= dayQuarters; s++ {
				if !shiftFree(&d.surplus, &taken, s, r.dur, pw) {
					continue
				}
				shiftMark(&taken, s, r.dur, pw)
				r.claimed = true
				break
			}
		}
	}
	return days, runs
}

// shiftFree reports whether the surplus minus what is taken holds pw for the
// quarters from s up to s + dur.
func shiftFree(surplus, taken *[dayQuarters]float64, s, dur int, pw float64) bool {
	for q := s; q < s+dur; q++ {
		if surplus[q]-taken[q] < pw {
			return false
		}
	}
	return true
}

// shiftMark adds w to the taken power of the quarters from q0 up to q0 + dur.
func shiftMark(taken *[dayQuarters]float64, q0, dur int, w float64) {
	for q := q0; q < q0+dur && q < dayQuarters; q++ {
		taken[q] += w
	}
}

// shiftHalvesAgree reports whether the first and the second half of the days
// both have grid runs, and in each the claimed share is at least one half.
func shiftHalvesAgree(heavy, claimed []int) bool {
	half := len(heavy) / 2
	agree := true
	for _, part := range [][2]int{{0, half}, {half, len(heavy)}} {
		n, k := 0, 0
		for d := part[0]; d < part[1]; d++ {
			n += heavy[d]
			k += claimed[d]
		}
		if n == 0 || 2*k < n {
			agree = false
		}
	}
	return agree
}

// shiftLatest returns the latest start in minutes of the day: the percentile of
// the start times, rounded to shiftLatestStepMin.
func shiftLatest(starts []float64) int {
	s := append([]float64(nil), starts...)
	sort.Float64s(s)
	idx := int(math.Ceil(shiftLatestPct*float64(len(s)))) - 1
	idx = min(max(idx, 0), len(s)-1)
	m := int(math.Round(s[idx]/shiftLatestStepMin)) * shiftLatestStepMin
	return min(max(m, 0), dayQuarters*15-shiftLatestStepMin)
}

// shiftClock formats minutes of the day as HH:MM.
func shiftClock(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60%24, m%60)
}

// shiftTypicalDay is the median surplus per quarter over the days that have the
// quarter, the curve of a typical day.
func shiftTypicalDay(days []shiftScopeDay) []float32 {
	out := make([]float32, dayQuarters)
	for q := 0; q < dayQuarters; q++ {
		var xs []float64
		for i := range days {
			if days[i].seen[q] {
				xs = append(xs, days[i].surplus[q])
			}
		}
		out[q] = float32(Median(xs))
	}
	return out
}

// shiftPeaks returns the start peaks of the device cycles in the scope.
func shiftPeaks(in *Input, device string) []float64 {
	var out []float64
	for i := range in.Days {
		for _, c := range in.Days[i].Cycles {
			if c.Device == device && c.PeakW > 0 {
				out = append(out, float64(c.PeakW))
			}
		}
	}
	return out
}

// shiftWh returns the energy of the device cycles in Wh.
func shiftWh(in *Input, device string) []float64 {
	var out []float64
	for i := range in.Days {
		for _, c := range in.Days[i].Cycles {
			if c.Device == device && c.Wh > 0 {
				out = append(out, float64(c.Wh))
			}
		}
	}
	return out
}

// shiftReplay checks the proposed rule by replay: the surplus starts the device
// before the latest start. A rule that cannot be replayed, or a replay without
// minutes, leaves the candidate without a replay.
func shiftReplay(ctx context.Context, in *Input, device, id string, onW float64, latest int, p, peak, cycleWh float64, runS int) (*ReplayVerdict, error) {
	if in.Doc.Location == nil {
		return nil, nil
	}
	rule := map[string]any{"id": id, "enabled": true, "conditions": []map[string]any{
		{"type": "balance_threshold", "field": "grid_export", "comparison": "above", "threshold": onW, "hold_seconds": shiftHoldS},
		{"type": "time_window", "start": "00:00", "end": shiftClock(latest), "weekdays": []int{}},
	}}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, err
	}
	chk := ReplayCheck{
		Rule:   json.RawMessage(raw),
		Target: ReplayTarget{RuleID: id, Device: device, OpW: p, PeakW: peak, CycleWh: cycleWh, MinRunS: float64(runS)},
	}
	verdict, _, err := EvaluateReplay(ctx, in, chk, shiftReplayDays, loc, *in.Doc.Location)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return verdict, err
}
