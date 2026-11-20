package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// OfflineDetector flags a rule whose time window starts while its device is
// usually unreachable (spec 15.4 "Offline-Fenster"). The fix moves the start of
// the time window 5 minutes before the window of the device.
var OfflineDetector = Detector{
	Name:      "offline_window",
	Group:     "reliability",
	Light:     false,
	PerDevice: true,
	Run:       offlineRun,
}

const (
	// offlineMinDays is the number of days with data the device needs.
	offlineMinDays = 14
	// offlineLimit is the lower Wilson bound of the unreachable share a quarter
	// needs to belong to a window.
	offlineLimit = 0.6
	// offlineLeadMin is the lead in minutes the fix gives the time window.
	offlineLeadMin = 5.0
	// offlineWeekdays is the number of weekdays of the bins.
	offlineWeekdays = 7
)

// offlineBin counts the days of one bin: n with data, k unreachable.
type offlineBin struct{ n, k int }

// offlineWindow is an unreachable span of the day in minutes, from inclusive
// to exclusive. lower is the lower Wilson bound of its unreachable share.
type offlineWindow struct {
	from, to int
	lower    float64
}

// offlineRun finds the rules whose time window starts in an unreachable window
// of the device, over at least offlineMinDays days with data.
func offlineRun(ctx context.Context, in *Input, device string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	var bins [offlineWeekdays][dayQuarters]offlineBin
	days := 0
	for i := range in.Days {
		f := &in.Days[i]
		dd := f.Devices[device]
		if dd == nil {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		wd := mondayIndex(t.Weekday())
		observed := false
		for q := 0; q < dayQuarters; q++ {
			if dd.Seen[q] == 0 {
				continue
			}
			observed = true
			bins[wd][q].n++
			if unreachable(dd, q) {
				bins[wd][q].k++
			}
		}
		if observed {
			days++
		}
	}
	if days < offlineMinDays {
		return nil
	}
	windows := offlineWindows(&bins)
	if len(windows) == 0 {
		return nil
	}
	actions := offlineActionMinutes(in.Days, device, loc)

	info, hasInfo := in.Devices[device]
	if !hasInfo {
		info = DeviceInfo{ID: device}
	}
	rules := ruleByID(in.Doc)
	var out []Candidate
	for _, rule := range in.Doc.Rules {
		if ctx.Err() != nil {
			return nil
		}
		idx, start, ok := offlineTimeWindow(rule)
		if !ok {
			continue
		}
		mins := actions[rule.ID]
		for _, w := range windows {
			if !offlineAffected(start, mins, w) {
				continue
			}
			out = append(out, offlineCandidate(device, info, rules, rule.ID, idx, start, w, days))
			break
		}
	}
	return out
}

// offlineBins sums the bins of the weekdays into one bin per quarter.
func offlineBins(bins *[offlineWeekdays][dayQuarters]offlineBin) [dayQuarters]offlineBin {
	var pooled [dayQuarters]offlineBin
	for w := range bins {
		for q := range bins[w] {
			pooled[q].n += bins[w][q].n
			pooled[q].k += bins[w][q].k
		}
	}
	return pooled
}

// offlineWindows joins the quarters whose unreachable share is sure enough to
// windows. A window is a day window only when it holds on every weekday with
// data; the others are dropped.
//
// The quarters are pooled over the weekdays. With the minimum of 14 days a
// single weekday bin holds only about two days, and no bin reaches the Wilson
// bound of 0.6 with fewer than five.
func offlineWindows(bins *[offlineWeekdays][dayQuarters]offlineBin) []offlineWindow {
	pooled := offlineBins(bins)
	var out []offlineWindow
	start := -1
	for q := 0; q <= dayQuarters; q++ {
		flagged := q < dayQuarters && WilsonLower(pooled[q].k, pooled[q].n) >= offlineLimit
		if flagged && start < 0 {
			start = q
		}
		if flagged || start < 0 {
			continue
		}
		if w, ok := offlineDayWindow(bins, start, q); ok {
			out = append(out, w)
		}
		start = -1
	}
	return out
}

// offlineDayWindow checks the quarters from to to (exclusive) on every weekday
// with data. It returns the window in minutes when all weekdays hold.
func offlineDayWindow(bins *[offlineWeekdays][dayQuarters]offlineBin, from, to int) (offlineWindow, bool) {
	var all offlineBin
	for w := range bins {
		var sum offlineBin
		for q := from; q < to; q++ {
			sum.n += bins[w][q].n
			sum.k += bins[w][q].k
		}
		all.n += sum.n
		all.k += sum.k
		if sum.n == 0 {
			continue
		}
		if WilsonLower(sum.k, sum.n) < offlineLimit {
			return offlineWindow{}, false
		}
	}
	return offlineWindow{from: from * 15, to: to * 15, lower: WilsonLower(all.k, all.n)}, true
}

// offlineActionMinutes returns per rule the minutes of the day of its actions on
// the device.
func offlineActionMinutes(days []DayFile, device string, loc *time.Location) map[string][]float64 {
	out := map[string][]float64{}
	for i := range days {
		for _, a := range days[i].Actions {
			if a.Device != device || a.Rule == "" {
				continue
			}
			out[a.Rule] = append(out[a.Rule], minuteOfDay(a.At, loc))
		}
	}
	return out
}

// offlineTimeWindow returns the index and the start in minutes of the first
// time window of the rule. It reports false when there is none.
func offlineTimeWindow(rule Rule) (int, float64, bool) {
	for i, c := range rule.Conditions {
		if c.Type != "time_window" {
			continue
		}
		var raw struct {
			Start string `json:"start"`
		}
		if err := json.Unmarshal(c.Raw, &raw); err != nil {
			return 0, 0, false
		}
		var h, m int
		if _, err := fmt.Sscanf(raw.Start, "%d:%d", &h, &m); err != nil {
			return 0, 0, false
		}
		return i, float64(h*60 + m), true
	}
	return 0, 0, false
}

// offlineAffected reports whether the rule is affected by the window: its time
// window starts inside it, or at least a third of its actions fall in it.
func offlineAffected(start float64, mins []float64, w offlineWindow) bool {
	if start >= float64(w.from) && start < float64(w.to) {
		return true
	}
	in := 0
	for _, m := range mins {
		if m >= float64(w.from) && m < float64(w.to) {
			in++
		}
	}
	return len(mins) > 0 && in*3 >= len(mins)
}

// offlineCandidate builds the finding of a rule that starts in an offline window.
func offlineCandidate(device string, info DeviceInfo, rules map[string]Rule, ruleID string, idx int,
	start float64, w offlineWindow, days int) Candidate {
	proposed := math.Mod(float64(w.from)-offlineLeadMin+minutesPerDay, minutesPerDay)
	finding := Finding{
		ID:         FindingID("offline_window", ruleID) + ":" + device,
		Kind:       "offline_window",
		Group:      "check",
		RuleIDs:    []string{ruleID},
		Severity:   "warn",
		Confidence: "high",
		Summary: Text{Code: "check.offline_window", Params: map[string]any{
			"rule":        ruleName(rules, ruleID),
			"device":      deviceName(info, device),
			"window_from": w.from,
			"window_to":   w.to,
		}},
		Evidence: Evidence{Shape: "day", From: 0, To: int64(minutesPerDay),
			Ist: []int64{int64(start)}, IstSpans: [][2]int64{{int64(w.from), int64(w.to)}},
			Soll: []int64{int64(proposed)}},
		Apply: []Fix{{Code: "move_before", RuleID: ruleID,
			Ops: []Op{{Path: []any{"conditions", idx, "start"}, Value: switchTimeClock(proposed)}}}},
		Device: device,
	}
	return Candidate{
		Finding:  finding,
		Detector: "offline_window",
		NEff:     float64(days),
		MinNEff:  offlineMinDays,
		Lower:    w.lower,
		Limit:    offlineLimit,
	}
}
