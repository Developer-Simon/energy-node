package energystats

import (
	"math"
	"time"
)

// MaxHold begrenzt, wie lange ein Messwert ohne neue Probe weiterzaehlt.
// Steht der Recorder (Neustart, haengender Prozess), wird der Luecke so
// hoechstens dieser Zeitraum zugerechnet.
const MaxHold = 5 * time.Minute

// Sample ist eine Probe: Leistung in W je Rolle, gueltig ab At bis zur
// naechsten Probe. Power nil heisst "kein gueltiger Messwert", das
// folgende Intervall zaehlt nicht.
type Sample struct {
	At    time.Time
	Power map[string]float64
}

// HourRecord ist eine abgeschlossene (oder per Flush angebrochene) Stunde.
type HourRecord struct {
	Hour    time.Time
	KWh     map[string]float64
	Covered int32
}

// Integrator rechnet Proben zeitgewichtet zu Stunden-kWh um. Rein, ohne
// Uhr und ohne Ein-/Ausgabe. Der Nullwert ist benutzbar.
type Integrator struct {
	last    Sample
	hasLast bool
	hour    time.Time
	kwh     map[string]float64
	covered time.Duration
}

// Add nimmt die naechste Probe und liefert die Stunden, die dadurch
// abgeschlossen sind. Eine Probe, die nicht nach der vorigen liegt, wird
// ignoriert.
func (g *Integrator) Add(s Sample) []HourRecord {
	s.At = s.At.UTC()
	if g.kwh == nil {
		g.kwh = map[string]float64{}
	}
	if g.hasLast && !s.At.After(g.last.At) {
		return nil
	}
	var done []HourRecord
	if g.hasLast && g.last.Power != nil {
		end := s.At
		if limit := g.last.At.Add(MaxHold); end.After(limit) {
			end = limit
		}
		for from := g.last.At; from.Before(end); {
			hour := from.Truncate(time.Hour)
			until := hour.Add(time.Hour)
			if until.After(end) {
				until = end
			}
			done = append(done, g.switchTo(hour)...)
			seconds := until.Sub(from).Seconds()
			for role, watts := range g.last.Power {
				g.kwh[role] += watts * seconds / 3.6e6
			}
			g.covered += until.Sub(from)
			from = until
		}
	}
	done = append(done, g.switchTo(s.At.Truncate(time.Hour))...)
	g.last, g.hasLast = s, true
	return done
}

// Flush liefert die angefangene Stunde (beim Herunterfahren) und beginnt
// danach ohne vorige Probe.
func (g *Integrator) Flush() []HourRecord {
	done := g.take()
	g.hasLast = false
	return done
}

// Reset verwirft die angefangene Stunde, etwa nach einem Uhrsprung.
func (g *Integrator) Reset() {
	g.kwh = map[string]float64{}
	g.covered = 0
	g.hasLast = false
}

func (g *Integrator) switchTo(hour time.Time) []HourRecord {
	if hour.Equal(g.hour) {
		return nil
	}
	done := g.take()
	g.hour = hour
	return done
}

func (g *Integrator) take() []HourRecord {
	if g.covered <= 0 {
		g.kwh = map[string]float64{}
		g.covered = 0
		return nil
	}
	record := HourRecord{Hour: g.hour, KWh: g.kwh, Covered: int32(math.Round(g.covered.Seconds()))}
	g.kwh = map[string]float64{}
	g.covered = 0
	return []HourRecord{record}
}
