package autolearn

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// BatteryDetector watches the charge of the battery. A battery that is full
// before solar noon on most days is a finding. A battery that is empty before
// sunrise on most days is a finding too, unless the same weeks of the year
// before were empty on as many days. The learned site/battery_full_at and
// site/night_wh follow the days of today's day type. It is daily and not per
// device.
var BatteryDetector = Detector{
	Name:      "battery",
	Group:     "energy",
	Light:     false,
	PerDevice: false,
	Run:       batteryRun,
}

const (
	// batteryFullPct is the charge in percent from which a battery counts as full.
	batteryFullPct = 97.0
	// batteryEmptyMargin is the margin in points above the lowest charge of the
	// last batteryEmptyWindow days, a charge at or below counts as empty.
	batteryEmptyMargin = 3.0
	batteryEmptyWindow = 14
	// batteryMinDays is the number of days a finding or a learned value needs.
	// The spec gives no number, the value is an assumption.
	batteryMinDays = 10
	// batteryShare is the lower Wilson bound of the share of days, and the
	// share of last year, that decide a finding.
	batteryShare = 0.5
	// batteryMaxApply is the number of devices that get a night off fix.
	batteryMaxApply = 2
	// batteryWeekDays turns the nightly kWh into the weekly benefit.
	batteryWeekDays = 7.0
	// batteryLearnRoundWh rounds the learned night energy.
	batteryLearnRoundWh = 100.0
)

// batteryDay is one day file with a charge. The sun values are local minutes
// of the day and are only set when sunOK is true. fullQ and emptyQ are the
// first quarter at or above batteryFullPct and the first morning quarter at or
// below the empty threshold, -1 for none.
type batteryDay struct {
	date    string
	week    int
	group   int
	f       *DayFile
	sunOK   bool
	rise    float64
	set     float64
	fullQ   int
	thr     float64
	hasThr  bool
	morning bool
	emptyQ  int
	nightWh float64
	nightOK bool
}

// batteryRun returns the full and empty findings and the learned values.
func batteryRun(ctx context.Context, in *Input, _ string) []Candidate {
	if ctx.Err() != nil {
		return nil
	}
	days := batteryDays(in)
	if len(days) == 0 {
		return nil
	}
	var out []Candidate
	out = append(out, batteryFull(days)...)
	out = append(out, batteryEmpty(in, days)...)
	out = append(out, batteryLearned(in, days)...)
	return out
}

// batteryDays reads the day files with a charge in date order, today left out,
// and scans them with the site of the rule document.
func batteryDays(in *Input) []*batteryDay {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	today := in.Now.In(loc).Format(dayLayout)
	files := make([]*DayFile, 0, len(in.Days))
	for i := range in.Days {
		if in.Days[i].Day != today {
			files = append(files, &in.Days[i])
		}
	}
	out := batteryScan(files, loc, in.Doc.Location)
	for _, d := range out {
		if g, ok := in.Base.DayType[d.date]; ok {
			d.group = g
		} else {
			d.group = -1
		}
	}
	return out
}

// batteryScan reads the day files with a charge in date order. Each day gets
// its sun times, its first full quarter and its empty threshold from the lowest
// charge of the 14 days before it in the list. Then the morning before sunrise
// and the night after sunset of each day are read. This is the one definition of
// empty and full that the detector and the season record share.
func batteryScan(files []*DayFile, loc *time.Location, site *sun.Location) []*batteryDay {
	sort.SliceStable(files, func(a, b int) bool { return files[a].Day < files[b].Day })
	var out []*batteryDay
	for _, f := range files {
		if !batteryHasSoC(f) {
			continue
		}
		t, err := time.ParseInLocation(dayLayout, f.Day, loc)
		if err != nil {
			continue
		}
		_, week := t.ISOWeek()
		d := &batteryDay{date: f.Day, week: week, f: f, fullQ: -1, emptyQ: -1}
		if site != nil {
			rise, set, ok := sun.Times(t, site.Lat, site.Lon)
			if ok {
				d.sunOK = true
				d.rise = batteryMinute(rise, loc)
				d.set = batteryMinute(set, loc)
			}
		}
		for q := 0; q < dayQuarters; q++ {
			if v := f.Site.SoC[q]; v != nil && float64(*v) >= batteryFullPct {
				d.fullQ = q
				break
			}
		}
		out = append(out, d)
	}
	for i, d := range out {
		lo := math.Inf(1)
		for k := i - batteryEmptyWindow; k < i; k++ {
			if k < 0 {
				continue
			}
			lo = math.Min(lo, batteryMinSoC(out[k].f))
		}
		if !math.IsInf(lo, 1) {
			d.thr = lo + batteryEmptyMargin
			d.hasThr = true
		}
	}
	for _, d := range out {
		if !d.sunOK || !d.hasThr {
			continue
		}
		for q := 0; q < dayQuarters && float64(q)*baseQuarterMin < d.rise; q++ {
			v := d.f.Site.SoC[q]
			if v == nil {
				continue
			}
			d.morning = true
			if d.emptyQ < 0 && float64(*v) <= d.thr {
				d.emptyQ = q
			}
		}
	}
	for i, d := range out {
		if i+1 < len(out) && batteryNextDay(d.date, out[i+1].date) {
			d.nightWh, d.nightOK = batteryNight(d, out[i+1])
		}
	}
	return out
}

// EmptyShareOf returns the share of the days of week in which the charge fell
// to the empty threshold before sunrise, the same definition as the battery
// detector. prior are the day files before the week, the threshold reads the
// last 14 of them. ok is false without sun, without a charge or with fewer than
// seasonMinDays evaluated days.
func EmptyShareOf(prior, week []DayFile, site *sun.Location, loc *time.Location) (float64, bool) {
	if loc == nil {
		loc = time.Local
	}
	files := make([]*DayFile, 0, len(prior)+len(week))
	for i := range prior {
		files = append(files, &prior[i])
	}
	inWeek := map[string]bool{}
	for i := range week {
		files = append(files, &week[i])
		inWeek[week[i].Day] = true
	}
	n, hits := 0, 0
	for _, d := range batteryScan(files, loc, site) {
		if !inWeek[d.date] || !d.sunOK || !d.hasThr || !d.morning {
			continue
		}
		n++
		if d.emptyQ >= 0 {
			hits++
		}
	}
	if n < seasonMinDays {
		return 0, false
	}
	return float64(hits) / float64(n), true
}

// batteryNextDay reports whether the date next is the day after date.
func batteryNextDay(date, next string) bool {
	t, err := time.Parse(dayLayout, date)
	if err != nil {
		return false
	}
	return t.AddDate(0, 0, 1).Format(dayLayout) == next
}

// batteryNight returns the house energy in Wh from the sunset of d to the
// sunrise of the next day, or to the quarter where the battery of the next day
// is empty before that.
func batteryNight(d, next *batteryDay) (float64, bool) {
	if !d.sunOK || !next.sunOK {
		return 0, false
	}
	var wh float64
	for q := 0; q < dayQuarters; q++ {
		if float64(q)*baseQuarterMin < d.set {
			continue
		}
		if d.f.Site.Seen[q] == 0 {
			return 0, false
		}
		wh += float64(d.f.Site.LoadTotal[q]) * batteryQuarterWh
	}
	for q := 0; q < dayQuarters && float64(q)*baseQuarterMin < next.rise; q++ {
		if next.emptyQ >= 0 && q >= next.emptyQ {
			break
		}
		if next.f.Site.Seen[q] == 0 {
			return 0, false
		}
		wh += float64(next.f.Site.LoadTotal[q]) * batteryQuarterWh
	}
	return wh, true
}

// batteryQuarterWh turns the mean power of a quarter in W into Wh.
const batteryQuarterWh = 0.25

// batteryHasSoC reports whether the day file has a charge in any quarter.
func batteryHasSoC(f *DayFile) bool {
	for q := 0; q < dayQuarters; q++ {
		if f.Site.SoC[q] != nil {
			return true
		}
	}
	return false
}

// batteryMinSoC returns the lowest charge of the day file.
func batteryMinSoC(f *DayFile) float64 {
	lo := math.Inf(1)
	for q := 0; q < dayQuarters; q++ {
		if v := f.Site.SoC[q]; v != nil {
			lo = math.Min(lo, float64(*v))
		}
	}
	return lo
}

// batteryFull looks for a battery that is full before solar noon on most days.
// Days without sun are left out. The finding carries the median time of the
// first full quarter and the export after it.
func batteryFull(days []*batteryDay) []Candidate {
	var flags []bool
	var hits []*batteryDay
	for _, d := range days {
		if !d.sunOK {
			continue
		}
		hit := d.fullQ >= 0 && float64(d.fullQ)*baseQuarterMin < (d.rise+d.set)/2
		flags = append(flags, hit)
		if hit {
			hits = append(hits, d)
		}
	}
	n := len(flags)
	if n < batteryMinDays {
		return nil
	}
	lower := WilsonLower(len(hits), n)
	if lower < batteryShare {
		return nil
	}
	var at, kwh []float64
	for _, d := range hits {
		at = append(at, float64(d.fullQ)*baseQuarterMin)
		var wh float64
		for q := d.fullQ; q < dayQuarters; q++ {
			wh += float64(d.f.Site.GridExport[q]) * batteryQuarterWh
		}
		kwh = append(kwh, wh/1000)
	}
	// The finding has no rule and no fix. In the group check it reaches the
	// rule check and from there the Monitoring page as a notice, like shade.
	finding := Finding{
		ID:         "battery:full",
		Kind:       "battery",
		Group:      "check",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "battery.full_early", Params: map[string]any{
			"time":      batteryClock(Median(at)),
			"kwh_after": round1(Median(kwh)),
		}},
		Evidence: batteryEvidence(days),
	}
	return []Candidate{{
		Finding:     finding,
		Detector:    "battery",
		NEff:        float64(n),
		MinNEff:     batteryMinDays,
		Lower:       lower,
		Limit:       batteryShare,
		HalvesAgree: batteryHalves(flags),
	}}
}

// batteryEmpty looks for a battery that is empty before sunrise on most days,
// when the same weeks of the year before were empty on less than half of their
// days. Without a record of the year before no finding is made. The fix is a
// night off for the devices that take together over half of the missing night
// energy, switchable ones only, at most batteryMaxApply.
func batteryEmpty(in *Input, days []*batteryDay) []Candidate {
	if in.PriorEmptyShare == nil {
		return nil
	}
	var evaluated []*batteryDay
	var flags []bool
	var hits []*batteryDay
	for _, d := range days {
		if !d.sunOK || !d.hasThr || !d.morning {
			continue
		}
		evaluated = append(evaluated, d)
		hit := d.emptyQ >= 0
		flags = append(flags, hit)
		if hit {
			hits = append(hits, d)
		}
	}
	n := len(evaluated)
	if n < batteryMinDays {
		return nil
	}
	lower := WilsonLower(len(hits), n)
	if lower < batteryShare || !batteryHalves(flags) {
		return nil
	}
	var prior float64
	for _, d := range evaluated {
		share, ok := in.PriorEmptyShare(d.week)
		if !ok {
			return nil
		}
		prior += share
	}
	if prior/float64(n) >= batteryShare {
		return nil
	}
	var at, missing []float64
	devSum := map[string]float64{}
	devDays := map[string]int{}
	var loadSum float64
	for _, d := range hits {
		at = append(at, float64(d.emptyQ)*baseQuarterMin)
		wh, load, dev := batteryMorningLoad(d)
		missing = append(missing, wh/1000)
		loadSum += load
		for id, w := range dev {
			devSum[id] += w
			devDays[id]++
		}
	}
	meanLoad := loadSum / float64(len(hits))
	candidates, apply := batteryCandidates(in, devSum, devDays, meanLoad)
	kwh := Median(missing)
	names := make([]string, 0, len(candidates))
	for _, id := range candidates {
		names = append(names, deviceName(in.Devices[id], id))
	}
	finding := Finding{
		ID:         "battery:empty",
		Kind:       "battery",
		Group:      "energy",
		Severity:   "info",
		Confidence: "high",
		Summary: Text{Code: "battery.empty_early", Params: map[string]any{
			"time":        batteryClock(Median(at)),
			"kwh_missing": round1(kwh),
			"candidates":  strings.Join(names, ", "),
		}},
		Evidence: batteryEvidence(days),
		Apply:    apply,
		Benefit:  &Benefit{Unit: "kwh_week", Value: kwh * batteryWeekDays},
	}
	return []Candidate{{
		Finding:     finding,
		Detector:    "battery",
		NEff:        float64(n),
		MinNEff:     batteryMinDays,
		Lower:       lower,
		Limit:       batteryShare,
		HalvesAgree: true,
		Benefit:     *finding.Benefit,
	}}
}

// batteryMorningLoad returns the house energy in Wh and the mean house power in
// W of the morning quarters from the empty quarter to sunrise, and the mean
// power of each device with a record in them.
func batteryMorningLoad(d *batteryDay) (float64, float64, map[string]float64) {
	var wh, sum float64
	n := 0
	devSum := map[string]float64{}
	devN := map[string]int{}
	for q := d.emptyQ; q < dayQuarters && float64(q)*baseQuarterMin < d.rise; q++ {
		if d.f.Site.Seen[q] == 0 {
			continue
		}
		wh += float64(d.f.Site.LoadTotal[q]) * batteryQuarterWh
		sum += float64(d.f.Site.LoadTotal[q])
		n++
		for id, dd := range d.f.Devices {
			if dd == nil || dd.Seen[q] == 0 {
				continue
			}
			devSum[id] += float64(dd.AvgW[q])
			devN[id]++
		}
	}
	dev := map[string]float64{}
	for id, s := range devSum {
		dev[id] = s / float64(devN[id])
	}
	if n == 0 {
		return 0, 0, dev
	}
	return wh, sum / float64(n), dev
}

// batteryCandidates takes the devices with the largest mean power in the empty
// nights, until together they reach over half of the mean house power. It
// returns the candidates and the night off fixes of the switchable ones.
func batteryCandidates(in *Input, devSum map[string]float64, devDays map[string]int, meanLoad float64) ([]string, []Fix) {
	type ranked struct {
		id string
		w  float64
	}
	var list []ranked
	for id, s := range devSum {
		if devDays[id] == 0 {
			continue
		}
		list = append(list, ranked{id: id, w: s / float64(devDays[id])})
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].w != list[b].w {
			return list[a].w > list[b].w
		}
		return list[a].id < list[b].id
	})
	var ids []string
	var apply []Fix
	acc := 0.0
	for _, r := range list {
		if acc > meanLoad/2 || r.w <= 0 {
			break
		}
		acc += r.w
		ids = append(ids, r.id)
		if info, ok := in.Devices[r.id]; ok && info.Switchable && len(apply) < batteryMaxApply {
			apply = append(apply, Fix{Code: "night_off", Template: "night_off",
				Prefill: withEntity(map[string]any{"device": r.id}, "entity", switchEntityOf(in, r.id))})
		}
	}
	return ids, apply
}

// batteryLearned returns site/battery_full_at and site/night_wh over the days
// of today's day type, when ten days of it have the value.
func batteryLearned(in *Input, days []*batteryDay) []Candidate {
	g, ok := learnTodayGroup(in)
	if !ok {
		return nil
	}
	var at, nights []float64
	for _, d := range days {
		if d.group != g {
			continue
		}
		if d.fullQ >= 0 {
			at = append(at, float64(d.fullQ)*baseQuarterMin)
		}
		if d.nightOK {
			nights = append(nights, d.nightWh)
		}
	}
	var out []Candidate
	if len(at) >= batteryMinDays {
		out = append(out, Candidate{
			Detector: "battery",
			NEff:     float64(len(at)),
			MinNEff:  batteryMinDays,
			Value: &Value{Key: "site/battery_full_at", Kind: ValueTime, Group: "energy",
				Text: batteryClock(Median(at)), N: len(at)},
		})
	}
	if len(nights) >= batteryMinDays {
		out = append(out, Candidate{
			Detector: "battery",
			NEff:     float64(len(nights)),
			MinNEff:  batteryMinDays,
			Value: &Value{Key: "site/night_wh", Kind: ValueNumber, Unit: "Wh", Group: "energy",
				Num: math.Round(Median(nights)/batteryLearnRoundWh) * batteryLearnRoundWh, N: len(nights)},
		})
	}
	return out
}

// batteryEvidence shows the charge of the last day as the curve, in percent.
func batteryEvidence(days []*batteryDay) Evidence {
	last := days[len(days)-1].f
	curve := make([]float32, dayQuarters)
	for q := 0; q < dayQuarters; q++ {
		if v := last.Site.SoC[q]; v != nil {
			curve[q] = *v
		}
	}
	return Evidence{Shape: "curve", Quarters: curve, Unit: "%"}
}

// batteryHalves reports whether the first and the second half of the days both
// reach the share of batteryShare.
func batteryHalves(flags []bool) bool {
	h := len(flags) / 2
	return batteryShareOf(flags[:h]) >= batteryShare && batteryShareOf(flags[h:]) >= batteryShare
}

// batteryShareOf returns the share of true values, 0 for no values.
func batteryShareOf(flags []bool) float64 {
	if len(flags) == 0 {
		return 0
	}
	hits := 0
	for _, f := range flags {
		if f {
			hits++
		}
	}
	return float64(hits) / float64(len(flags))
}

// batteryClock returns the clock time of minutes of the day, rounded to 5 minutes.
func batteryClock(m float64) string {
	return switchTimeClock(learnRoundClock(m))
}

// batteryMinute returns the local minute of the day of t.
func batteryMinute(t time.Time, loc *time.Location) float64 {
	lt := t.In(loc)
	return float64(lt.Hour()*60+lt.Minute()) + float64(lt.Second())/60
}
