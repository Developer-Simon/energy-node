package autolearn

import (
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// SeasonalSource gives the maximum power of a balance field over a period in
// W, the months in which that maximum was reached, the number of days with
// data and whether there was any data at all.
type SeasonalSource interface {
	MaxW(field string, from, to time.Time) (maxW float64, months []int, coveredDays int, ok bool)
}

// energySeason reads the seasonal maximum from the hourly energy statistics.
type energySeason struct {
	store *energystats.Store
}

// EnergyStatsSeason reads the seasonal maximum from the energy statistics.
// The value of an hour is its mean power in W, the kWh of the hour times 1000.
func EnergyStatsSeason(store *energystats.Store) SeasonalSource {
	return energySeason{store: store}
}

// MaxW scans the hours in [from, to). Hours without a value are skipped. Days
// are counted in UTC, the same as the energy statistics store them.
func (s energySeason) MaxW(field string, from, to time.Time) (float64, []int, int, bool) {
	var (
		maxW   float64
		found  bool
		months = map[int]bool{}
		days   = map[int64]bool{}
	)
	err := s.store.Each(from, to, func(hour time.Time, m *energystats.Month, i int) {
		if m == nil {
			return
		}
		series := m.Roles[field]
		if series == nil || i >= len(series.KWh) || math.IsNaN(series.KWh[i]) {
			return
		}
		w := series.KWh[i] * 1000
		utc := hour.UTC()
		days[utc.Unix()/86400] = true
		switch {
		case !found || w > maxW:
			found, maxW = true, w
			months = map[int]bool{int(utc.Month()): true}
		case w == maxW:
			months[int(utc.Month())] = true
		}
	})
	if err != nil || !found {
		return 0, nil, 0, false
	}
	list := make([]int, 0, len(months))
	for month := range months {
		list = append(list, month)
	}
	sort.Ints(list)
	return maxW, list, len(days), true
}
