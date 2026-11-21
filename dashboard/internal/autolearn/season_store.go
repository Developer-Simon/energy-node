package autolearn

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	seasonFileVersion = 1
	// seasonMinDays is the number of day files a week needs to be kept.
	seasonMinDays = 4
	// seasonYears is how long weeks are kept.
	seasonYears = 3
	// seasonSourceDays marks a week built from day files, seasonSourceStats one
	// refilled from the energy statistics. Day files win over statistics.
	seasonSourceDays  = "days"
	seasonSourceStats = "stats"
	// seasonNightHours is the night window 00:00 to 05:00 local in hours, and
	// seasonNightQuarters the same window in quarter hours of a day file.
	seasonNightHours    = 5
	seasonNightQuarters = seasonNightHours * 4
)

// Week is the summary of one calendar week (ISO week). Key is YYYY-Www. Energy
// values are kWh of the week. BaseW is the night base load in W. TempC is nil
// until the outdoor temperature is known. RunMin holds the minutes each device
// ran. Triggers counts the rule actions per rule. Learned holds the usual clock
// times as minutes of the day.
type Week struct {
	Key       string             `json:"key"`
	Source    string             `json:"source"`
	Days      int                `json:"days"`
	ExportKWh float64            `json:"export_kwh"`
	PVKWh     float64            `json:"pv_kwh"`
	LoadKWh   float64            `json:"load_kwh"`
	BaseW     float64            `json:"base_w"`
	TempC     *float64           `json:"temp_c,omitempty"`
	RunMin    map[string]float64 `json:"run_min,omitempty"`
	Triggers  map[string]float64 `json:"triggers,omitempty"`
	Learned   map[string]float64 `json:"learned,omitempty"`
}

// WeekOf summarises the day files of one week. It returns false when there are
// fewer than seasonMinDays day files with a valid date. The week is the ISO
// week of the earliest day. BaseW is the median over the days of the 10th
// percentile of each day's load in the night window (00:00 to 05:00 local).
// learned maps a key to its clock times in minutes of the day, the median of
// each key is stored.
func WeekOf(days []DayFile, learned map[string][]float64, loc *time.Location) (Week, bool) {
	if loc == nil {
		loc = time.Local
	}
	var used []DayFile
	var first time.Time
	for _, d := range days {
		t, err := time.ParseInLocation(dayLayout, d.Day, loc)
		if err != nil {
			continue
		}
		if len(used) == 0 || t.Before(first) {
			first = t
		}
		used = append(used, d)
	}
	if len(used) < seasonMinDays {
		return Week{}, false
	}

	w := Week{
		Key:    isoWeekKey(first),
		Source: seasonSourceDays,
		Days:   len(used),
	}
	var dayBase []float64
	for _, d := range used {
		var night []float64
		for q := 0; q < dayQuarters; q++ {
			w.ExportKWh += float64(d.Site.GridExport[q]) * baseWattQuarterKWh
			w.PVKWh += float64(d.Site.PV[q]) * baseWattQuarterKWh
			w.LoadKWh += float64(d.Site.LoadTotal[q]) * baseWattQuarterKWh
			if d.Site.Seen[q] != 0 && q < seasonNightQuarters {
				night = append(night, float64(d.Site.LoadTotal[q]))
			}
		}
		if len(night) > 0 {
			dayBase = append(dayBase, basePercentile10(night))
		}
		for id, dev := range d.Devices {
			if dev == nil {
				continue
			}
			var mins float64
			for q := 0; q < dayQuarters; q++ {
				mins += float64(dev.OnMin[q])
			}
			if mins > 0 {
				if w.RunMin == nil {
					w.RunMin = map[string]float64{}
				}
				w.RunMin[id] += mins
			}
		}
		for _, a := range d.Actions {
			if a.Rule == "" {
				continue
			}
			if w.Triggers == nil {
				w.Triggers = map[string]float64{}
			}
			w.Triggers[a.Rule]++
		}
	}
	if len(dayBase) > 0 {
		w.BaseW = seasonMedian(dayBase)
	}
	for key, minutes := range learned {
		if len(minutes) == 0 {
			continue
		}
		if w.Learned == nil {
			w.Learned = map[string]float64{}
		}
		w.Learned[key] = seasonMedian(minutes)
	}
	return w, true
}

// WeeksFromStats builds one week per ISO week from the hourly energy statistics,
// oldest first. Weeks without hours are not listed. BaseW is the median over the
// days of the 10th percentile of each day's hourly load (W) for the hours
// 00:00 to 05:00 local.
func WeeksFromStats(hours []energystats.HourRecord, loc *time.Location) []Week {
	if loc == nil {
		loc = time.Local
	}
	type acc struct {
		week  Week
		days  map[string]bool
		night map[string][]float64
	}
	byKey := map[string]*acc{}
	for _, h := range hours {
		local := h.Hour.In(loc)
		key := isoWeekKey(local)
		a := byKey[key]
		if a == nil {
			a = &acc{week: Week{Key: key, Source: seasonSourceStats}, days: map[string]bool{}, night: map[string][]float64{}}
			byKey[key] = a
		}
		day := local.Format(dayLayout)
		a.days[day] = true
		a.week.ExportKWh += h.KWh["grid_export"]
		a.week.PVKWh += h.KWh["pv"]
		if load, ok := h.KWh["load_total"]; ok {
			a.week.LoadKWh += load
			if local.Hour() < seasonNightHours {
				a.night[day] = append(a.night[day], load*1000)
			}
		}
	}
	out := make([]Week, 0, len(byKey))
	for _, a := range byKey {
		a.week.Days = len(a.days)
		var dayBase []float64
		for _, xs := range a.night {
			dayBase = append(dayBase, basePercentile10(xs))
		}
		if len(dayBase) > 0 {
			a.week.BaseW = seasonMedian(dayBase)
		}
		out = append(out, a.week)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// seasonFile is the content of season.json.
type seasonFile struct {
	Version int    `json:"version"`
	Weeks   []Week `json:"weeks"`
}

// SeasonStore keeps the weekly figures. It is safe for concurrent use.
type SeasonStore struct {
	mu    sync.Mutex
	path  string
	logf  func(string, ...any)
	weeks map[string]Week
}

// LoadSeasonStore reads the store from path. A missing file means an empty
// store. A damaged file is logged and starts empty.
func LoadSeasonStore(path string, logf func(string, ...any)) *SeasonStore {
	if logf == nil {
		logf = log.Printf
	}
	s := &SeasonStore{path: path, logf: logf, weeks: map[string]Week{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("season store: read %s: %v", path, err)
		}
		return s
	}
	var f seasonFile
	if err := json.Unmarshal(data, &f); err != nil {
		logf("season store: %s is damaged, starting empty: %v", path, err)
		return s
	}
	for _, w := range f.Weeks {
		s.weeks[w.Key] = w
	}
	return s
}

// Put stores a week. Day files replace statistics, statistics never replace day
// files.
func (s *SeasonStore) Put(w Week) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.weeks[w.Key]; ok && old.Source == seasonSourceDays && w.Source != seasonSourceDays {
		return
	}
	s.weeks[w.Key] = w
}

// Weeks lists the weeks with from <= Key <= to, oldest first.
func (s *SeasonStore) Weeks(from, to string) []Week {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Week
	for key, w := range s.weeks {
		if key >= from && key <= to {
			out = append(out, w)
		}
	}
	sortWeeks(out)
	return out
}

// Same lists the weeks of the calendar week and its neighbours (one week before
// and after) in the given number of years before the current year, oldest first.
func (s *SeasonStore) Same(week int, years int) []Week {
	s.mu.Lock()
	defer s.mu.Unlock()
	year := time.Now().Year()
	var out []Week
	for k := 1; k <= years; k++ {
		for w := week - 1; w <= week+1; w++ {
			if w < 1 || w > 53 {
				continue
			}
			if rec, ok := s.weeks[fmt.Sprintf("%d-W%02d", year-k, w)]; ok {
				out = append(out, rec)
			}
		}
	}
	sortWeeks(out)
	return out
}

// Prune drops the weeks that start more than seasonYears before now.
func (s *SeasonStore) Prune(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := now.UTC().AddDate(-seasonYears, 0, 0)
	cutoff := time.Date(c.Year(), c.Month(), c.Day(), 0, 0, 0, 0, time.UTC)
	for key := range s.weeks {
		monday, ok := isoWeekMonday(key)
		if ok && monday.Before(cutoff) {
			delete(s.weeks, key)
		}
	}
}

// Save writes the store atomically through a temporary file and a rename.
// Without a path nothing is written.
func (s *SeasonStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	weeks := make([]Week, 0, len(s.weeks))
	for _, w := range s.weeks {
		weeks = append(weeks, w)
	}
	sortWeeks(weeks)
	data, err := json.Marshal(seasonFile{Version: seasonFileVersion, Weeks: weeks})
	if err != nil {
		return err
	}
	return energystats.WriteFileAtomic(filepath.Dir(s.path), s.path, data)
}

// isoWeekKey is the ISO week of t as YYYY-Www.
func isoWeekKey(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// isoWeekMonday is the Monday (UTC) of a YYYY-Www key.
func isoWeekMonday(key string) (time.Time, bool) {
	var y, w int
	if _, err := fmt.Sscanf(key, "%d-W%d", &y, &w); err != nil {
		return time.Time{}, false
	}
	jan4 := time.Date(y, time.January, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7
	return jan4.AddDate(0, 0, -offset+(w-1)*7), true
}

func sortWeeks(ws []Week) {
	sort.Slice(ws, func(i, j int) bool { return ws[i].Key < ws[j].Key })
}

// seasonMedian is the median of xs, the mean of the two middle values for an
// even count. xs is not changed. It returns 0 for no values.
func seasonMedian(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}
