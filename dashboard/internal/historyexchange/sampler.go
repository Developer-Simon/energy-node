// dashboard/internal/historyexchange/sampler.go
// Der Sampler fuellt den Ringpuffer aus dem Dashboard selbst. Anders als der
// Browser misst er nur wenige Serien (role:grid und die Ausgaben der
// Szenario-Regler), einmal pro Minute aus der Registry. Die Anzeige
// verwendet diese Zeilen, der Dienst liest sie nie zurueck. Begruendung in
// AGENTS.md und docs/developing/data-flow.md.
package historyexchange

import (
	"sort"
	"sync"
	"time"
)

type accumulator struct {
	min, max, sum float64
	n             int
}

// Sampler verdichtet Messwerte zu Minutenzeilen. Er haelt nur die offene
// Minute im Speicher und schreibt sie erst, wenn die Minute gewechselt hat.
type Sampler struct {
	mutex  sync.Mutex
	buffer *Buffer
	minute int64
	acc    map[string]*accumulator
}

func NewSampler() *Sampler {
	return &Sampler{acc: map[string]*accumulator{}}
}

// Attach legt fest, wohin abgeschlossene Minuten geschrieben werden.
func (s *Sampler) Attach(b *Buffer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.buffer = b
}

// Observe rechnet die Werte in die offene Minute ein. Wechselt die Minute,
// wird die abgeschlossene Minute als Zeile je Serie geschrieben und
// zurueckgegeben. Ein Zeitsprung rueckwaerts zaehlt wie ein Minutenwechsel.
func (s *Sampler) Observe(now time.Time, values map[string]float64) []Row {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	minute := now.UnixMilli() / 60000 * 60000
	var rows []Row
	if s.minute != 0 && minute != s.minute && len(s.acc) > 0 {
		names := make([]string, 0, len(s.acc))
		for name := range s.acc {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			a := s.acc[name]
			rows = append(rows, Row{
				Series: name,
				TS:     s.minute,
				Min:    a.min,
				Max:    a.max,
				Avg:    a.sum / float64(a.n),
				N:      a.n,
				U:      "W",
			})
		}
		if s.buffer != nil {
			s.buffer.Append(rows, now.UnixMilli())
		}
		s.acc = map[string]*accumulator{}
	}
	s.minute = minute

	for name, value := range values {
		a, ok := s.acc[name]
		if !ok {
			a = &accumulator{min: value, max: value}
			s.acc[name] = a
		}
		if value < a.min {
			a.min = value
		}
		if value > a.max {
			a.max = value
		}
		a.sum += value
		a.n++
	}
	return rows
}
