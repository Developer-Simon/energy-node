package energystats

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// Stundenmittel fuer die Verlaufsseite. Die Serien heissen wie im Browser-
// Verlauf (history-recorder.js), damit die Seite sie ohne Umrechnung neben
// ihre eigenen 5-Minuten-Saetze legen kann.

var ErrInvalidRange = errors.New("energystats: invalid range")

const (
	MaxHistorySpan = 400 * 24 * time.Hour
	// Eine eigene Stunde mit weniger Aufzeichnung waere als Mittel zu
	// niedrig und erschiene im Verlauf als Einbruch.
	MinOwnCoverSeconds = 3000
	DerivedLoadSeries  = "berechnet:hausverbrauch"
)

type HistoryRow struct {
	Series string  `json:"series"`
	TS     int64   `json:"ts"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Avg    float64 `json:"avg"`
	N      int     `json:"n"`
	U      string  `json:"u"`
}

func HistoryRows(store *Store, from, to time.Time, categories map[string]energy.Category) ([]HistoryRow, error) {
	from = from.UTC().Truncate(time.Hour)
	if !to.After(from) || to.Sub(from) > MaxHistorySpan {
		return nil, ErrInvalidRange
	}
	bySeries := map[string][]HistoryRow{}
	nonZero := map[string]bool{}
	add := func(series string, hour time.Time, w float64) {
		bySeries[series] = append(bySeries[series], HistoryRow{Series: series, TS: hour.UnixMilli(), Min: w, Max: w, Avg: w, N: 1, U: "W"})
		if w != 0 {
			nonZero[series] = true
		}
	}
	err := store.Each(from, to, func(hour time.Time, m *Month, i int) {
		if m == nil {
			return
		}
		value := func(role string) (float64, bool) {
			s := m.Roles[role]
			if s == nil || math.IsNaN(s.KWh[i]) {
				return 0, false
			}
			switch s.Source[i] {
			case SourceHA:
			case SourceOwn:
				if m.Covered[i] < MinOwnCoverSeconds {
					return 0, false
				}
			default:
				return 0, false
			}
			return s.KWh[i] * 1000, true
		}
		for role, series := range map[string]string{"pv": "role:pv", "wallbox": "role:wallbox", "heat_pump": "role:heat_pump", "load_total": DerivedLoadSeries} {
			if w, ok := value(role); ok {
				add(series, hour, w)
			}
		}
		if imp, ok := value("grid_import"); ok {
			if exp, ok := value("grid_export"); ok {
				add("role:grid", hour, imp-exp)
			}
		}
		if charge, ok := value("battery_charge"); ok {
			if discharge, ok := value("battery_discharge"); ok {
				add("role:battery", hour, charge-discharge)
			}
		}
		for role := range m.Roles {
			id, ok := strings.CutPrefix(role, "custom:")
			if !ok {
				continue
			}
			category, known := categories[id]
			if !known || category.Base == energy.CategoryStorage {
				continue
			}
			if w, ok := value(role); ok {
				add("role:custom:"+string(category.Base)+":"+id, hour, w)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(bySeries))
	for name := range bySeries {
		if nonZero[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := []HistoryRow{}
	for _, name := range names {
		out = append(out, bySeries[name]...)
	}
	return out, nil
}

func (s *Service) HistoryRows(from, to time.Time) ([]HistoryRow, error) {
	var categories map[string]energy.Category
	if s.Categories != nil {
		categories = s.Categories()
	}
	return HistoryRows(s.Store, from, to, categories)
}
