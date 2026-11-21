package autolearn

import (
	"math"
	"sort"
	"time"
)

const (
	// weatherHalfLife ist die Zeit, in der k nach einer Messung halb zum
	// Saisonmedian zurueckkehrt.
	weatherHalfLife = 20 * time.Minute
	// weatherWindow ist das Fenster fuer kLow und fuer "frisch".
	weatherWindow = 2 * time.Hour
	// weatherRing ist die feste Anzahl gespeicherter Messungen. Eine Messung je
	// ungedrosselter Minute fuellt das Zwei-Stunden-Fenster mit 120 Eintraegen.
	weatherRing = 128
	// weatherKMax begrenzt k nach oben, falls die Bestkurve zu niedrig schaetzt.
	weatherKMax = 1.2
	// weatherKDefault ist das Saisonmedian, solange keines gesetzt ist.
	weatherKDefault = 0.5
)

// weatherSample ist eine gemessene oder angehobene k zum Zeitpunkt at.
type weatherSample struct {
	at time.Time
	k  float64
}

// Weather misst den Wetterfaktor k = PV-Ist / Bestkurve. Gemessen wird nur in
// ungedrosselten Minuten. Ohne neue Messung altert k zum Saisonmedian.
type Weather struct {
	median    float64
	hasMedian bool
	ring      [weatherRing]weatherSample
	next      int
	n         int
}

// WeatherSample ist eine gespeicherte Messung fuer Neustarts.
type WeatherSample struct {
	At int64   `json:"at"`
	K  float64 `json:"k"`
}

// WeatherState ist der gespeicherte Zustand von Weather fuer Neustarts.
type WeatherState struct {
	Median  *float64        `json:"median,omitempty"`
	Samples []WeatherSample `json:"samples,omitempty"`
}

// SetSeasonMedian legt das Saisonmedian fest, zu dem k ohne Messung altert.
func (w *Weather) SetSeasonMedian(k float64) {
	if !finite(k) {
		return
	}
	w.median = clampK(k)
	w.hasMedian = true
}

// Observe speichert k = pv / best, aber nur in ungedrosselten Minuten mit
// positiver Bestkurve. Andere Minuten lassen die Messungen unveraendert.
func (w *Weather) Observe(at time.Time, pv, best float64, unthrottled bool) {
	if !unthrottled || !finite(pv) || !finite(best) || best <= 0 {
		return
	}
	w.push(weatherSample{at: at, k: clampK(pv / best)})
}

// LiftForLoad hebt k auf (pv + jump) / best, wenn eine zugeschaltete Last
// von der PV gedeckt wird. Nach unten wird nichts veraendert. Die Hebung gilt
// als Messung.
func (w *Weather) LiftForLoad(at time.Time, pv, jump, best float64) {
	if !finite(pv) || !finite(jump) || !finite(best) || best <= 0 {
		return
	}
	k := clampK((pv + jump) / best)
	if k <= w.aged(at) {
		return
	}
	w.push(weatherSample{at: at, k: k})
}

// K liefert den aktuellen Wetterfaktor k zur Zeit at, dazu das 25. Perzentil
// der Messungen der letzten zwei Stunden (kLow) und ob es in diesem Fenster
// eine Messung gibt (fresh). Ohne Messung im Fenster ist kLow gleich k.
func (w *Weather) K(at time.Time) (k, kLow float64, fresh bool) {
	k = w.aged(at)
	var vals [weatherRing]float64
	m := 0
	start := w.start()
	for i := 0; i < w.n; i++ {
		s := w.ring[(start+i)%weatherRing]
		d := at.Sub(s.at)
		if d < weatherWindow && d > -weatherWindow {
			vals[m] = s.k
			m++
		}
	}
	if m == 0 {
		return k, k, false
	}
	sort.Float64s(vals[:m])
	kLow = vals[int(0.25*float64(m-1))]
	return k, kLow, true
}

// Reserve berechnet die ungenutzte PV-Leistung als max(0, best * kLow - pv).
// Ohne frische Messung wird der Wert halbiert und est gesetzt.
func Reserve(best, pv, kLow float64, fresh bool) (w float64, est bool) {
	w = math.Max(0, best*kLow-pv)
	if !fresh {
		return w / 2, true
	}
	return w, false
}

// State liefert den Zustand fuer die Speicherung ueber Neustarts.
func (w *Weather) State() WeatherState {
	var st WeatherState
	if w.hasMedian {
		m := w.median
		st.Median = &m
	}
	start := w.start()
	for i := 0; i < w.n; i++ {
		s := w.ring[(start+i)%weatherRing]
		st.Samples = append(st.Samples, WeatherSample{At: s.at.Unix(), K: s.k})
	}
	return st
}

// LoadWeather baut Weather aus einem gespeicherten Zustand. Ungueltige Werte
// werden uebersprungen. Bei mehr Messungen als Platz bleiben die neuesten.
func LoadWeather(st WeatherState) *Weather {
	w := &Weather{}
	if st.Median != nil {
		w.SetSeasonMedian(*st.Median)
	}
	samples := st.Samples
	if len(samples) > weatherRing {
		samples = samples[len(samples)-weatherRing:]
	}
	for _, s := range samples {
		if s.At <= 0 || !finite(s.K) {
			continue
		}
		w.push(weatherSample{at: time.Unix(s.At, 0), k: clampK(s.K)})
	}
	return w
}

// push haengt eine Messung an den Ring an. Die aelteste faellt beim Ueberlauf weg.
func (w *Weather) push(s weatherSample) {
	w.ring[w.next] = s
	w.next = (w.next + 1) % weatherRing
	if w.n < weatherRing {
		w.n++
	}
}

// start liefert den Index der aeltesten gespeicherten Messung.
func (w *Weather) start() int {
	return (w.next - w.n + weatherRing) % weatherRing
}

// season liefert das Saisonmedian, mit Standardwert ohne Angabe.
func (w *Weather) season() float64 {
	if w.hasMedian {
		return w.median
	}
	return weatherKDefault
}

// aged liefert k zur Zeit at. Die letzte Messung zerfaellt mit 20 Minuten
// Halbwertszeit zum Saisonmedian. Ohne Messung gilt das Saisonmedian.
func (w *Weather) aged(at time.Time) float64 {
	med := w.season()
	if w.n == 0 {
		return med
	}
	last := w.ring[(w.next-1+weatherRing)%weatherRing]
	dt := at.Sub(last.at)
	if dt <= 0 {
		return last.k
	}
	return med + (last.k-med)*math.Pow(0.5, dt.Seconds()/weatherHalfLife.Seconds())
}

// clampK begrenzt k auf 0 bis weatherKMax.
func clampK(x float64) float64 {
	return math.Max(0, math.Min(weatherKMax, x))
}
