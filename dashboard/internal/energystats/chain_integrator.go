package energystats

import (
	"math"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// ChainPair ist ein Leistungspaar einer Messkette: Quelle und Ziel eines
// Segments in Flussrichtung.
type ChainPair struct{ Source, Target string }

// PairPower ist die Leistung eines Paars in W, als Betrag.
type PairPower struct{ SourceW, TargetW float64 }

// PairEnergy ist die Energie eines Paars in kWh. Gezaehlt werden nur die
// Sekunden, in denen das Paar gemessen wurde.
type PairEnergy struct {
	SourceKWh, TargetKWh float64
	Seconds              int32
}

// StandbyEnergy ist die Standby-Energie einer Entitaet in kWh.
type StandbyEnergy struct {
	KWh     float64
	Seconds int32
}

// ChainSample ist der Momentanzustand aller Messketten zu einem Zeitpunkt,
// gueltig ab At bis zur naechsten Probe.
type ChainSample struct {
	At      time.Time
	Pairs   map[ChainPair]PairPower
	Standby map[string]float64 // Entitaet des ersten Punkts -> W
}

// ChainHour ist eine abgeschlossene (oder per Flush angebrochene) Stunde der
// Messketten.
type ChainHour struct {
	Hour    time.Time
	Pairs   map[ChainPair]PairEnergy
	Standby map[string]StandbyEnergy
}

// ChainSampleOf leitet aus den Messketten die Probe ab. Eine Kette im Standby
// (erster Punkt misst, alle anderen Punkte liegen unter ChainMinInputW)
// liefert nur den Standby-Eintrag, keine Paare.
func ChainSampleOf(at time.Time, chains []energy.ChainState) ChainSample {
	s := ChainSample{At: at, Pairs: map[ChainPair]PairPower{}, Standby: map[string]float64{}}
	for _, chain := range chains {
		if len(chain.Points) == 0 {
			continue
		}
		if w, ok := chainStandby(chain); ok {
			s.Standby[chain.Points[0].EntityID] = w
			continue
		}
		if chain.Flow == "idle" {
			continue
		}
		abs := make(map[string]float64, len(chain.Points))
		for _, p := range chain.Points {
			abs[p.EntityID] = math.Abs(p.Value)
		}
		for _, seg := range chain.Segments {
			if !seg.Available || seg.Efficiency == nil {
				continue
			}
			s.Pairs[ChainPair{Source: seg.From, Target: seg.To}] = PairPower{
				SourceW: abs[seg.From],
				TargetW: abs[seg.To],
			}
		}
	}
	return s
}

// chainStandby reports the power of the first point when the chain is in
// standby: every point is fresh, the first one reads at least ChainIdleW and
// every other one stays below ChainMinInputW.
func chainStandby(chain energy.ChainState) (float64, bool) {
	source := chain.Points[0]
	if !source.HasValue || source.Freshness != "fresh" {
		return 0, false
	}
	w := math.Abs(source.Value)
	if w < energy.ChainIdleW {
		return 0, false
	}
	for _, p := range chain.Points[1:] {
		if p.Freshness != "fresh" || math.Abs(p.Value) >= energy.ChainMinInputW {
			return 0, false
		}
	}
	return w, true
}

// ChainIntegrator rechnet Messketten-Proben zeitgewichtet zu Stunden um. Wie
// Integrator zaehlt eine Probe bis zur naechsten, hoechstens MaxHold. Der
// Nullwert ist benutzbar.
type ChainIntegrator struct {
	last    ChainSample
	hasLast bool
	hour    time.Time
	pairs   map[ChainPair]*pairAcc
	standby map[string]*standbyAcc
}

type pairAcc struct {
	sourceWs, targetWs float64 // W mal Sekunden
	covered            time.Duration
}

type standbyAcc struct {
	wattSeconds float64
	covered     time.Duration
}

// Add nimmt die naechste Probe und liefert die Stunden, die dadurch
// abgeschlossen sind. Eine Probe, die nicht nach der vorigen liegt, wird
// ignoriert.
func (g *ChainIntegrator) Add(s ChainSample) []ChainHour {
	s.At = s.At.UTC()
	if g.pairs == nil {
		g.pairs = map[ChainPair]*pairAcc{}
		g.standby = map[string]*standbyAcc{}
	}
	if g.hasLast && !s.At.After(g.last.At) {
		return nil
	}
	var done []ChainHour
	if g.hasLast {
		end := s.At
		if limit := g.last.At.Add(MaxHold); end.After(limit) {
			end = limit
		}
		eachHourPart(g.last.At, end, func(hour time.Time, seconds float64) {
			done = append(done, g.switchTo(hour)...)
			g.accumulate(seconds)
		})
	}
	done = append(done, g.switchTo(s.At.Truncate(time.Hour))...)
	g.last, g.hasLast = s, true
	return done
}

// accumulate rechnet den Abschnitt der letzten Probe an, nur fuer die Paare
// und Standby-Entitaeten, die in ihr stehen.
func (g *ChainIntegrator) accumulate(seconds float64) {
	d := secondsToDuration(seconds)
	for pair, p := range g.last.Pairs {
		a := g.pairs[pair]
		if a == nil {
			a = &pairAcc{}
			g.pairs[pair] = a
		}
		a.sourceWs += p.SourceW * seconds
		a.targetWs += p.TargetW * seconds
		a.covered += d
	}
	for entity, w := range g.last.Standby {
		a := g.standby[entity]
		if a == nil {
			a = &standbyAcc{}
			g.standby[entity] = a
		}
		a.wattSeconds += w * seconds
		a.covered += d
	}
}

// Flush liefert die angefangene Stunde (beim Herunterfahren) und beginnt
// danach ohne vorige Probe.
func (g *ChainIntegrator) Flush() []ChainHour {
	done := g.take()
	g.hasLast = false
	return done
}

// Reset verwirft die angefangene Stunde, etwa nach einem Uhrsprung.
func (g *ChainIntegrator) Reset() {
	g.pairs = map[ChainPair]*pairAcc{}
	g.standby = map[string]*standbyAcc{}
	g.hasLast = false
}

func (g *ChainIntegrator) switchTo(hour time.Time) []ChainHour {
	if hour.Equal(g.hour) {
		return nil
	}
	done := g.take()
	g.hour = hour
	return done
}

// take liefert die angefangene Stunde, wenn sie Paare oder Standby enthaelt.
func (g *ChainIntegrator) take() []ChainHour {
	if len(g.pairs) == 0 && len(g.standby) == 0 {
		g.pairs = map[ChainPair]*pairAcc{}
		g.standby = map[string]*standbyAcc{}
		return nil
	}
	record := ChainHour{
		Hour:    g.hour,
		Pairs:   make(map[ChainPair]PairEnergy, len(g.pairs)),
		Standby: make(map[string]StandbyEnergy, len(g.standby)),
	}
	for pair, a := range g.pairs {
		record.Pairs[pair] = PairEnergy{
			SourceKWh: a.sourceWs / 3.6e6,
			TargetKWh: a.targetWs / 3.6e6,
			Seconds:   int32(math.Round(a.covered.Seconds())),
		}
	}
	for entity, a := range g.standby {
		record.Standby[entity] = StandbyEnergy{
			KWh:     a.wattSeconds / 3.6e6,
			Seconds: int32(math.Round(a.covered.Seconds())),
		}
	}
	g.pairs = map[ChainPair]*pairAcc{}
	g.standby = map[string]*standbyAcc{}
	return []ChainHour{record}
}
