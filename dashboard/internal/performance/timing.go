package performance

import (
	"net/http"
	"runtime/metrics"
	"strings"
	"sync/atomic"
	"time"
)

// Part benennt einen Teil der Dashboard-Arbeit, dessen Laufzeit gemessen wird.
type Part string

const (
	PartPush       Part = "push"       // Bau und Versand des SSE-Stroms
	PartHTTP       Part = "http"       // alle Anfragen ausser /api/v1/events
	PartMQTT       Part = "mqtt"       // handleState je Zustandsnachricht
	PartNode       Part = "node"       // Takte des Node-Agents
	PartBackground Part = "background" // uebrige Takte in main.go
)

// TimedParts in fester Reihenfolge, fuer Schleifen und Antwort.
var TimedParts = []Part{PartPush, PartHTTP, PartMQTT, PartNode, PartBackground}

// eventsPath ist der SSE-Strom. Seine Laufzeit ist die Lebensdauer der
// Verbindung und wuerde die HTTP-Summe verfaelschen, daher wird er ausgelassen.
const eventsPath = "/api/v1/events"

// Timing summiert die Laufzeit je Teil in Nanosekunden. Ein nil-Timing
// ignoriert alle Aufrufe.
type Timing struct {
	nanos map[Part]*atomic.Int64
}

// NewTiming legt einen Zaehler mit allen bekannten Teilen an.
func NewTiming() *Timing {
	t := &Timing{nanos: make(map[Part]*atomic.Int64, len(TimedParts))}
	for _, p := range TimedParts {
		t.nanos[p] = new(atomic.Int64)
	}
	return t
}

// Add rechnet d dem Teil p zu. Unbekannte Teile werden ignoriert.
func (t *Timing) Add(p Part, d time.Duration) {
	if t == nil {
		return
	}
	if c, ok := t.nanos[p]; ok {
		c.Add(int64(d))
	}
}

// Since rechnet die Zeit seit start dem Teil p zu, gedacht fuer
// defer t.Since(PartX, time.Now()).
func (t *Timing) Since(p Part, start time.Time) {
	if t == nil {
		return
	}
	t.Add(p, time.Since(start))
}

// Totals liefert die Summen seit Start je Teil.
func (t *Timing) Totals() map[Part]time.Duration {
	out := make(map[Part]time.Duration, len(TimedParts))
	if t == nil {
		return out
	}
	for _, p := range TimedParts {
		out[p] = time.Duration(t.nanos[p].Load())
	}
	return out
}

// Middleware misst jede Anfrage als PartHTTP, ausser dem SSE-Strom.
// Die Pruefung ueber das Suffix erlaubt, dass basepath.Middleware davor liegt.
func (t *Timing) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, eventsPath) {
			next.ServeHTTP(w, r)
			return
		}
		defer t.Since(PartHTTP, time.Now())
		next.ServeHTTP(w, r)
	})
}

// RuntimeCPU liefert die kumulierte CPU-Zeit in Sekunden fuer GC und Nutzercode.
type RuntimeCPU func() (gcS, userS float64)

var cpuSamples = []metrics.Sample{
	{Name: "/cpu/classes/gc/total:cpu-seconds"},
	{Name: "/cpu/classes/user:cpu-seconds"},
}

// ReadRuntimeCPU liest die beiden CPU-Klassen aus runtime/metrics.
// Die Samples sind Paketvariablen, damit jeder Aufruf ohne Allokation auskommt.
// Die Funktion ist nicht nebenläufig sicher, weil metrics.Read die Samples beschreibt.
func ReadRuntimeCPU() (gcS, userS float64) {
	metrics.Read(cpuSamples)
	return sampleFloat(cpuSamples[0]), sampleFloat(cpuSamples[1])
}

func sampleFloat(s metrics.Sample) float64 {
	if s.Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return s.Value.Float64()
}
