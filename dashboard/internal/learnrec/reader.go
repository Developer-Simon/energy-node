package learnrec

import (
	"math"
	"sort"
	"time"
)

// Series ist eine Reihe Minutenwerte ab From. Je Minute ein Wert, Luecken sind NaN.
type Series struct {
	From   time.Time
	Values []float32
}

// Reader liest die Minutenaufzeichnung. Range liefert die Werte eines Schluessels
// im Zeitraum [from, to). Coverage zaehlt die Tage im Zeitraum, die zu mindestens
// 80 % Minuten mit Werten in b:load_total haben.
type Reader interface {
	Range(key string, from, to time.Time) Series
	Coverage(from, to time.Time) (days int)
}

var _ Reader = (*MinuteStore)(nil)

// keyLister ist optional. Ein Reader, der sie hat, nennt die Schluessel mit Werten
// im Zeitraum, damit das Nachlesen sie findet.
type keyLister interface {
	Keys(from, to time.Time) []string
}

var _ keyLister = (*MinuteStore)(nil)

// coverageMinMinutes ist die Zahl der Minuten, ab der ein Tag als abgedeckt gilt.
const coverageMinMinutes = minutesPerDay * 80 / 100

// coverageKey ist der Schluessel, an dem Coverage einen Tag misst.
const coverageKey = "b:load_total"

// minuteStart liefert den Beginn der lokalen Minute von t.
func minuteStart(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), 0, 0, loc)
}

// dayStrings liefert die lokalen Tage, die den Zeitraum [from, to) beruehren.
func dayStrings(from, to time.Time, loc *time.Location) []string {
	l := from.In(loc)
	day := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	var out []string
	for day.Before(to) {
		out = append(out, day.Format(minuteDayLayout))
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// Range liefert die Minutenwerte des Schluessels key ab der Minute von from bis
// vor to. Minuten ohne Wert sind NaN.
func (s *MinuteStore) Range(key string, from, to time.Time) Series {
	start := minuteStart(from, s.loc)
	n := 0
	if to.After(start) {
		n = int((to.Sub(start) + time.Minute - 1) / time.Minute)
	}
	out := Series{From: start, Values: make([]float32, n)}

	cache := map[string]*[minutesPerDay]float32{}
	loaded := map[string]bool{}
	for i := range out.Values {
		t := start.Add(time.Duration(i) * time.Minute).In(s.loc)
		day := t.Format(minuteDayLayout)
		if !loaded[day] {
			loaded[day] = true
			vals, _ := s.Day(day)
			cache[day] = vals[key]
		}
		arr := cache[day]
		if arr == nil {
			out.Values[i] = minuteGap
			continue
		}
		out.Values[i] = arr[t.Hour()*60+t.Minute()]
	}
	return out
}

// Keys nennt die Schluessel, zu denen im Zeitraum [from, to) Werte liegen, sortiert.
func (s *MinuteStore) Keys(from, to time.Time) []string {
	set := map[string]bool{}
	for _, day := range dayStrings(from, to, s.loc) {
		vals, ok := s.Day(day)
		if !ok {
			continue
		}
		for key := range vals {
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

// Coverage zaehlt die Tage im Zeitraum [from, to), an denen mindestens 80 % der
// Minuten einen Wert in b:load_total haben.
func (s *MinuteStore) Coverage(from, to time.Time) int {
	n := 0
	for _, day := range dayStrings(from, to, s.loc) {
		vals, ok := s.Day(day)
		if !ok {
			continue
		}
		arr := vals[coverageKey]
		if arr == nil {
			continue
		}
		have := 0
		for _, v := range arr {
			if !math.IsNaN(float64(v)) {
				have++
			}
		}
		if have >= coverageMinMinutes {
			n++
		}
	}
	return n
}
