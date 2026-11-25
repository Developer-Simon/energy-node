package autolearn

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// Selbstpruefung der Prognose. The surplus forecast of a day is taken at 10:00
// local time, which is the solar time of the site up to the clock offset, and
// compared with the measured rest of the day from the same quarter. When the
// forecast has been worse than the season median over the last days, the
// median is published instead.

const (
	forecastFileVersion = 1
	// forecastDays is the number of finished days the check keeps.
	forecastDays = 14
	// ForecastWindow is the number of finished days the check keeps.
	ForecastWindow = forecastDays
	// forecastHour is the local hour of the forecast.
	forecastHour = 10
	// forecastQuarter is the quarter of the forecast, 10:00 local time.
	forecastQuarter = forecastHour * 60 / shadeQuarterMin
)

// forecastEntry is one finished day: the forecast taken at 10:00, the measured
// rest from 10:00 and the season median of the rest from 10:00.
type forecastEntry struct {
	Day      string  `json:"day"`
	Forecast float64 `json:"forecast_wh"`
	Actual   float64 `json:"actual_wh"`
	Season   float64 `json:"season_wh"`
}

// ForecastDay is one finished day of the check: the forecast of the rest of the
// day from 10:00, the measured rest and the season median of the rest, in Wh.
type ForecastDay struct {
	Day        string  `json:"day"`
	ForecastWh float64 `json:"forecast_wh"`
	ActualWh   float64 `json:"actual_wh"`
	SeasonWh   float64 `json:"season_wh"`
}

// forecastFile is the content of forecast.json.
type forecastFile struct {
	Version int                `json:"version"`
	Pending map[string]float64 `json:"pending,omitempty"`
	Days    []forecastEntry    `json:"days,omitempty"`
}

// ForecastCheck keeps the 10:00 forecast of each day until the day is over, then
// the last forecastDays finished days. It decides whether the season median
// replaces the forecast. It is safe for concurrent use.
type ForecastCheck struct {
	mu      sync.Mutex
	path    string
	logf    func(string, ...any)
	pending map[string]float64
	days    []forecastEntry
	dirty   bool
}

// LoadForecastCheck reads the check from path. A missing file means an empty
// check, a damaged file is logged and starts empty.
func LoadForecastCheck(path string, logf func(string, ...any)) *ForecastCheck {
	if logf == nil {
		logf = log.Printf
	}
	c := &ForecastCheck{path: path, logf: logf, pending: map[string]float64{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c
	}
	var f forecastFile
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
		return c
	}
	for day, v := range f.Pending {
		c.pending[day] = v
	}
	c.days = f.Days
	return c
}

// Recorded tells whether the forecast of day is kept, open or finished.
func (c *ForecastCheck) Recorded(day string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recordedLocked(day)
}

func (c *ForecastCheck) recordedLocked(day string) bool {
	if _, ok := c.pending[day]; ok {
		return true
	}
	for _, e := range c.days {
		if e.Day == day {
			return true
		}
	}
	return false
}

// Record stores the forecast of the rest of day from 10:00 in Wh. The first
// forecast of a day wins, a second call for the same day does nothing.
func (c *ForecastCheck) Record(day string, forecastWh float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recordedLocked(day) {
		return
	}
	c.pending[day] = forecastWh
	c.dirty = true
}

// Pending returns the days whose forecast waits for the measured rest, sorted.
func (c *ForecastCheck) Pending() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.pending))
	for day := range c.pending {
		out = append(out, day)
	}
	sort.Strings(out)
	return out
}

// Close compares the forecast of day with the measured rest and the season
// median. The day is kept with the last forecastDays finished days. A NaN
// median closes the day without keeping it, for a day without any similar
// day to compare with. Close does nothing for a day without a forecast.
func (c *ForecastCheck) Close(day string, actualRestWh float64, seasonMedianRestWh float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fc, ok := c.pending[day]
	if !ok {
		return
	}
	delete(c.pending, day)
	c.dirty = true
	if math.IsNaN(seasonMedianRestWh) {
		return
	}
	c.days = append(c.days, forecastEntry{Day: day, Forecast: fc, Actual: actualRestWh, Season: seasonMedianRestWh})
	sort.SliceStable(c.days, func(a, b int) bool { return c.days[a].Day < c.days[b].Day })
	if n := len(c.days); n > forecastDays {
		c.days = append([]forecastEntry(nil), c.days[n-forecastDays:]...)
	}
}

// Days returns a copy of the finished days, oldest first. It is never nil.
func (c *ForecastCheck) Days() []ForecastDay {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ForecastDay, 0, len(c.days))
	for _, e := range c.days {
		out = append(out, ForecastDay{Day: e.Day, ForecastWh: e.Forecast, ActualWh: e.Actual, SeasonWh: e.Season})
	}
	return out
}

// UseSeasonMedian tells whether the season median should replace the forecast.
// It needs forecastDays finished days, and the mean absolute error of the
// forecast must not be smaller than the mean absolute error of the season
// median.
func (c *ForecastCheck) UseSeasonMedian() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.days) < forecastDays {
		return false
	}
	var fc, sm float64
	for _, e := range c.days {
		fc += math.Abs(e.Forecast - e.Actual)
		sm += math.Abs(e.Season - e.Actual)
	}
	n := float64(len(c.days))
	return fc/n >= sm/n
}

// Save writes the check atomically, only when it changed since the last save.
// Without a path nothing is written.
func (c *ForecastCheck) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty || c.path == "" {
		return nil
	}
	data, err := json.Marshal(forecastFile{Version: forecastFileVersion, Pending: c.pending, Days: c.days})
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(c.path), c.path, data); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// Reset verwirft alle offenen und abgeschlossenen Prognosen und loescht die Datei.
// Die Datei wird beim naechsten Save als leerer Stand neu geschrieben.
func (c *ForecastCheck) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = map[string]float64{}
	c.days = nil
	c.dirty = true
	return removeIfExists(c.path)
}
