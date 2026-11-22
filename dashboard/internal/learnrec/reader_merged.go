package learnrec

import (
	"math"
	"sort"
	"time"
)

// mergedReader liest je Minute zuerst die eigene Aufzeichnung, dann die Werte
// von Home Assistant aus dem Zwischenspeicher. Eigene Minuten sind nie 5m.
type mergedReader struct {
	own *MinuteStore
	ha  *HASync
}

var _ Reader = (*mergedReader)(nil)
var _ keyLister = (*mergedReader)(nil)

// NewMergedReader verbindet den eigenen Speicher mit dem Abgleich. Ohne Abgleich
// ist es der eigene Speicher selbst.
func NewMergedReader(own *MinuteStore, ha *HASync) Reader {
	if ha == nil {
		return own
	}
	return &mergedReader{own: own, ha: ha}
}

// Range liefert je Minute den eigenen Wert, sonst den Wert von Home Assistant.
// Fehlt beides, ist die Minute NaN.
func (m *mergedReader) Range(key string, from, to time.Time) Series {
	out := m.own.Range(key, from, to)
	haDays := map[string]*[minutesPerDay]float32{}
	for i, v := range out.Values {
		if !math.IsNaN(float64(v)) {
			continue
		}
		at := out.From.Add(time.Duration(i) * time.Minute).In(m.own.loc)
		day := at.Format(minuteDayLayout)
		arr, loaded := haDays[day]
		if !loaded {
			arr, _ = m.ha.keyMinutes(day, key)
			haDays[day] = arr
		}
		if arr == nil {
			continue
		}
		if w := arr[at.Hour()*60+at.Minute()]; !math.IsNaN(float64(w)) {
			out.Values[i] = w
		}
	}
	return out
}

// Keys nennt die Schluessel des eigenen Speichers und die der HA-Tage im Zeitraum.
func (m *mergedReader) Keys(from, to time.Time) []string {
	set := map[string]bool{}
	for _, key := range m.own.Keys(from, to) {
		set[key] = true
	}
	for _, day := range dayStrings(from, to, m.own.loc) {
		for _, key := range m.ha.keysOf(day) {
			set[key] = true
		}
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Coverage zaehlt die Tage wie der eigene Speicher, mit einer Minute je Stelle aus
// eigener Aufzeichnung oder Home Assistant. Ein Tag, dessen Minuten zu mehr als
// 20 % aus 5m stammen, zaehlt nicht.
func (m *mergedReader) Coverage(from, to time.Time) int {
	n := 0
	for _, day := range dayStrings(from, to, m.own.loc) {
		vals, _ := m.own.Day(day)
		own := vals[coverageKey]
		ha, fiveMin := m.ha.keyMinutes(day, coverageKey)
		have, fromFive := 0, 0
		for i := 0; i < minutesPerDay; i++ {
			if own != nil && !math.IsNaN(float64(own[i])) {
				have++
				continue
			}
			if ha != nil && !math.IsNaN(float64(ha[i])) {
				have++
				if fiveMin {
					fromFive++
				}
			}
		}
		if have >= coverageMinMinutes && 5*fromFive <= have {
			n++
		}
	}
	return n
}
