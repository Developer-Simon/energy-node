package performance

import (
	"math"
	"sort"
	"time"
)

// ringSample ist ein Messwert mit seinem Zeitpunkt.
type ringSample struct {
	at    time.Time
	value float64
}

// Ring ist ein fester Ringpuffer mit Zeitfenster. Er verdraengt den aeltesten
// Wert, sobald die Kapazitaet voll ist, und wirft beim Schreiben alle Werte
// ab, die aelter als window sind. window 0 heisst: nur die Kapazitaet zaehlt.
type Ring struct {
	samples []ringSample
	start   int // Index des aeltesten Werts
	count   int
	window  time.Duration
}

// NewRing legt einen Ringpuffer mit capacity Plaetzen und dem Zeitfenster
// window an. capacity unter 1 wird auf 1 angehoben.
func NewRing(capacity int, window time.Duration) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{samples: make([]ringSample, capacity), window: window}
}

// Add haengt einen Wert an. Danach fliegen alle Werte aus dem Puffer, die
// vor at - window liegen.
func (r *Ring) Add(at time.Time, value float64) {
	if r.count == len(r.samples) {
		r.start = (r.start + 1) % len(r.samples)
		r.count--
	}
	end := (r.start + r.count) % len(r.samples)
	r.samples[end] = ringSample{at: at, value: value}
	r.count++
	r.Prune(at)
}

// Prune wirft alle Werte ab, die vor now - window liegen. Ohne Fenster
// geschieht nichts.
func (r *Ring) Prune(now time.Time) {
	if r.window <= 0 {
		return
	}
	cutoff := now.Add(-r.window)
	for r.count > 0 && r.samples[r.start].at.Before(cutoff) {
		r.start = (r.start + 1) % len(r.samples)
		r.count--
	}
}

// Len ist die Zahl der gespeicherten Werte.
func (r *Ring) Len() int {
	return r.count
}

// Values liefert alle gespeicherten Werte, der aelteste zuerst.
func (r *Ring) Values() []float64 {
	return r.since(time.Time{}, false)
}

// Since liefert die Werte ab t (einschliesslich), der aelteste zuerst.
func (r *Ring) Since(t time.Time) []float64 {
	return r.since(t, true)
}

func (r *Ring) since(t time.Time, filter bool) []float64 {
	out := make([]float64, 0, r.count)
	for i := 0; i < r.count; i++ {
		s := r.samples[(r.start+i)%len(r.samples)]
		if filter && s.at.Before(t) {
			continue
		}
		out = append(out, s.value)
	}
	return out
}

// P95 ist das 95. Perzentil nach dem Nearest-Rank-Verfahren. Leer heisst 0.
func (r *Ring) P95() float64 {
	return percentile(r.Values(), 0.95)
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}
