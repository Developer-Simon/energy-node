package autolearn

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// replayBalanceKeys are the balance keys the replay reads, in the order the
// stretched noise is drawn. battery_soc has no minute key of its own before
// the minute record holds it, so a missing key is simply left out of the tick.
var replayBalanceKeys = []string{"load_total", "grid_import", "grid_export", "pv",
	"battery_charge", "battery_discharge", "battery_soc"}

// ReplayDays returns the minutes of the last days complete days before today,
// oldest first. A day with minutes in in.Minutes is replayed minute by minute.
// Any other day with a day file is stretched from its quarter hours, with a
// noise trace drawn from a seed that depends on the day only, so the same day
// always replays the same way. estimate is true when a stretched day is part
// of the result. Learned values are included in a minute only when they were
// learned at or before that minute.
func ReplayDays(in *Input, days int) (minutes []ReplayMinute, estimate bool) {
	if in == nil || days <= 0 {
		return nil, false
	}
	loc := replayLoc(in)
	today := in.Now.In(loc)
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	byDay := make(map[string]*DayFile, len(in.Days))
	for i := range in.Days {
		byDay[in.Days[i].Day] = &in.Days[i]
	}
	learned := numericLearned(in.Learned)
	for k := days; k >= 1; k-- {
		start := first.AddDate(0, 0, -k)
		end := start.AddDate(0, 0, 1)
		if in.Minutes != nil && in.Minutes.Coverage(start, end) >= 1 {
			minutes = append(minutes, minuteDay(in.Minutes, start, learned)...)
			continue
		}
		f := byDay[start.Format(dayLayout)]
		if f == nil {
			continue
		}
		estimate = true
		minutes = append(minutes, stretchedDay(f, start, learned)...)
	}
	return minutes, estimate
}

// replayLoc is the location of the input, local time when none is set.
func replayLoc(in *Input) *time.Location {
	if in.Loc != nil {
		return in.Loc
	}
	return time.Local
}

// learnedEntry is one numeric learned value with the time it was learned.
type learnedEntry struct {
	key string
	val float64
	at  time.Time
}

// numericLearned keeps the learned values that hold a number, in key order.
func numericLearned(in map[string]LearnedEntry) []learnedEntry {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []learnedEntry
	for _, k := range keys {
		e := in[k]
		var v float64
		switch x := e.Value.(type) {
		case float64:
			v = x
		case float32:
			v = float64(x)
		case int:
			v = float64(x)
		case int64:
			v = float64(x)
		default:
			continue
		}
		out = append(out, learnedEntry{key: k, val: v, at: time.Unix(e.At, 0)})
	}
	return out
}

// learnedAt returns the learned values that were known at t, nil when none.
func learnedAt(entries []learnedEntry, t time.Time) map[string]float64 {
	var out map[string]float64
	for _, e := range entries {
		if e.at.After(t) {
			continue
		}
		if out == nil {
			out = make(map[string]float64, len(entries))
		}
		out[e.key] = e.val
	}
	return out
}

// minuteDay reads one day of minutes from r. A minute without load total is
// a stale tick, the other keys are left out when they have no value.
func minuteDay(r learnrec.Reader, start time.Time, learned []learnedEntry) []ReplayMinute {
	series := make(map[string][]float32, len(replayBalanceKeys))
	for _, k := range replayBalanceKeys {
		series[k] = r.Range("b:"+k, start, start.Add(24*time.Hour)).Values
	}
	out := make([]ReplayMinute, 0, 1440)
	for i := 0; i < 1440; i++ {
		at := start.Add(time.Duration(i) * time.Minute)
		m := ReplayMinute{At: at, Learned: learnedAt(learned, at)}
		if v := series["load_total"]; i < len(v) && !math.IsNaN(float64(v[i])) {
			m.Balance = make(map[string]float64, len(replayBalanceKeys))
			for _, k := range replayBalanceKeys {
				if v := series[k]; i < len(v) && !math.IsNaN(float64(v[i])) {
					m.Balance[k] = float64(v[i])
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// stretchedDay turns the quarter hours of a day file into 1440 minutes. Each
// quarter hour keeps its mean: the noise of the quarter is drawn around zero
// and centred, so the 15 minutes average to the quarter value exactly.
func stretchedDay(f *DayFile, start time.Time, learned []learnedEntry) []ReplayMinute {
	rng := rand.New(rand.NewSource(dayNumber(start)))
	quarters := make([][dayQuarters]float64, len(replayBalanceKeys))
	noise := make([][dayQuarters][15]float64, len(replayBalanceKeys))
	for ki, k := range replayBalanceKeys {
		series := make([]float32, dayQuarters)
		for q := 0; q < dayQuarters; q++ {
			quarters[ki][q] = quarterValue(f, k, q)
			series[q] = float32(quarters[ki][q])
		}
		amp := QuarterNoise(series)
		for q := 0; q < dayQuarters; q++ {
			sum := 0.0
			for m := 0; m < 15; m++ {
				noise[ki][q][m] = amp * (2*rng.Float64() - 1)
				sum += noise[ki][q][m]
			}
			for m := 0; m < 15; m++ {
				noise[ki][q][m] -= sum / 15
			}
		}
	}

	out := make([]ReplayMinute, 0, 1440)
	for i := 0; i < 1440; i++ {
		q, mi := i/15, i%15
		at := start.Add(time.Duration(i) * time.Minute)
		m := ReplayMinute{At: at, Learned: learnedAt(learned, at)}
		if !math.IsNaN(quarters[0][q]) {
			m.Balance = make(map[string]float64, len(replayBalanceKeys))
			for ki, k := range replayBalanceKeys {
				if v := quarters[ki][q]; !math.IsNaN(v) {
					m.Balance[k] = v + noise[ki][q][mi]
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// quarterValue is the value of a balance key in quarter q of a day file, NaN
// when the quarter was not seen.
func quarterValue(f *DayFile, key string, q int) float64 {
	if f.Site.Seen[q] == 0 {
		return math.NaN()
	}
	switch key {
	case "load_total":
		return float64(f.Site.LoadTotal[q])
	case "grid_import":
		return float64(f.Site.GridImport[q])
	case "grid_export":
		return float64(f.Site.GridExport[q])
	case "pv":
		return float64(f.Site.PV[q])
	case "battery_charge":
		return float64(f.Site.BatteryCharge[q])
	case "battery_discharge":
		return float64(f.Site.BatteryDischarge[q])
	case "battery_soc":
		if p := f.Site.SoC[q]; p != nil {
			return float64(*p)
		}
	}
	return math.NaN()
}

// dayNumber is the date as a number, YYYYMMDD, the seed of the noise.
func dayNumber(t time.Time) int64 {
	return int64(t.Year()*10000 + int(t.Month())*100 + t.Day())
}

// RealState returns, per minute, whether the device was on in reality. The last
// switch event of the device at or before the minute decides. Before the first
// event the on minutes of the quarter hour decide (OnMin of the day file).
func RealState(in *Input, device string, minutes []ReplayMinute) []bool {
	var events []EventRec
	for _, f := range in.Days {
		for _, e := range f.Events {
			if e.Device == device {
				events = append(events, e)
			}
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })
	loc := replayLoc(in)
	byDay := make(map[string]*DayFile, len(in.Days))
	for i := range in.Days {
		byDay[in.Days[i].Day] = &in.Days[i]
	}

	out := make([]bool, len(minutes))
	ei, have, state := 0, false, false
	for i, m := range minutes {
		t := m.At.Unix()
		for ei < len(events) && events[ei].At <= t {
			state, have = events[ei].On, true
			ei++
		}
		if have {
			out[i] = state
			continue
		}
		out[i] = onMinute(byDay[m.At.In(loc).Format(dayLayout)], device, m.At.In(loc))
	}
	return out
}

// onMinute tells from the on minutes of the quarter hour whether the device
// was on at t: the first OnMin minutes of the quarter are on.
func onMinute(f *DayFile, device string, t time.Time) bool {
	if f == nil {
		return false
	}
	dd := f.Devices[device]
	if dd == nil {
		return false
	}
	minute := t.Hour()*60 + t.Minute()
	return minute%15 < int(dd.OnMin[minute/15])
}

// ReplayLastYear returns the minutes of the same calendar weeks one year back,
// for the weeks last years, oldest first. Each hour of the energy statistics
// is spread over its 60 minutes at its mean power, so the result is an
// estimate. ok is false when the statistics hold no hour in those weeks.
func ReplayLastYear(in *Input, stats *energystats.Store, weeks int) (minutes []ReplayMinute, ok bool) {
	if in == nil || stats == nil || weeks <= 0 {
		return nil, false
	}
	loc := replayLoc(in)
	now := in.Now.In(loc)
	for w := weeks - 1; w >= 0; w-- {
		monday := isoMonday(now.AddDate(-1, 0, -7*w), loc)
		err := stats.Each(monday, monday.AddDate(0, 0, 7), func(hour time.Time, m *energystats.Month, i int) {
			if m == nil {
				return
			}
			idx, inRange := m.Index(hour)
			if !inRange || m.Covered[idx] == 0 {
				minutes = appendGapHour(minutes, hour)
				return
			}
			bal := map[string]float64{}
			for _, k := range siteKeys {
				s := m.Roles[k]
				if s == nil || idx >= len(s.KWh) || math.IsNaN(s.KWh[idx]) {
					continue
				}
				bal[k] = s.KWh[idx] * 1000
			}
			if len(bal) == 0 {
				minutes = appendGapHour(minutes, hour)
				return
			}
			ok = true
			for j := 0; j < 60; j++ {
				minutes = append(minutes, ReplayMinute{At: hour.Add(time.Duration(j) * time.Minute), Balance: bal})
			}
		})
		if err != nil {
			return nil, false
		}
	}
	return minutes, ok
}

// appendGapHour adds the 60 minutes of an hour without values as stale ticks.
func appendGapHour(minutes []ReplayMinute, hour time.Time) []ReplayMinute {
	for j := 0; j < 60; j++ {
		minutes = append(minutes, ReplayMinute{At: hour.Add(time.Duration(j) * time.Minute)})
	}
	return minutes
}

// isoMonday is the Monday of the week of t, at midnight in loc.
func isoMonday(t time.Time, loc *time.Location) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return time.Date(t.Year(), t.Month(), t.Day()-(wd-1), 0, 0, 0, 0, loc)
}

// MinuteNoise estimates the noise of the signal key of r between from and to.
// It is the 95th percentile of |signal − moving median over 5 minutes|, taken
// only in the minutes where the signal lies within 20 % of threshold. key is
// the full reader key, such as "b:load_total". ok is false without such minutes.
func MinuteNoise(r learnrec.Reader, key string, threshold float64, from, to time.Time) (float64, bool) {
	if r == nil {
		return 0, false
	}
	vals := r.Range(key, from, to).Values
	band := 0.2 * math.Abs(threshold)
	var devs []float64
	for i, v := range vals {
		x := float64(v)
		if math.IsNaN(x) || math.Abs(x-threshold) >= band {
			continue
		}
		var win []float64
		for j := i - 2; j <= i+2; j++ {
			if j < 0 || j >= len(vals) || math.IsNaN(float64(vals[j])) {
				continue
			}
			win = append(win, float64(vals[j]))
		}
		if len(win) < 3 {
			continue
		}
		sort.Float64s(win)
		devs = append(devs, math.Abs(x-win[len(win)/2]))
	}
	if len(devs) == 0 {
		return 0, false
	}
	sort.Float64s(devs)
	rank := int(math.Ceil(0.95*float64(len(devs)))) - 1
	return devs[rank], true
}
