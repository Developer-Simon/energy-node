package energystats

import (
	"math"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// ChainPair is a power pair of a measurement chain: source and target of a
// segment in flow direction.
type ChainPair struct{ Source, Target string }

// PairPower is the power of a pair in W, as absolute values.
type PairPower struct{ SourceW, TargetW float64 }

// PairEnergy is the energy of a pair in kWh. Only the seconds in which the
// pair was measured are counted.
type PairEnergy struct {
	SourceKWh, TargetKWh float64
	Seconds              int32
}

// StandbyEnergy is the standby energy of an entity in kWh.
type StandbyEnergy struct {
	KWh     float64
	Seconds int32
}

// ChainSample is the state of all measurement chains at one point in time,
// valid from At until the next sample.
type ChainSample struct {
	At      time.Time
	Pairs   map[ChainPair]PairPower
	Standby map[string]float64 // entity of the first point -> W
}

// ChainHour is a closed hour (or one cut short by Flush) of the
// measurement chains.
type ChainHour struct {
	Hour    time.Time
	Pairs   map[ChainPair]PairEnergy
	Standby map[string]StandbyEnergy
}

// ChainSampleOf derives the sample from the chains. A chain in standby
// (first point measures, all other points are below ChainMinInputW)
// yields only the standby entry, no pairs.
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

// ChainIntegrator time-weights measurement chain samples into hours. Like
// Integrator, a sample counts up to the next one, at most MaxHold. The zero
// value is usable.
type ChainIntegrator struct {
	last    ChainSample
	hasLast bool
	hour    time.Time
	pairs   map[ChainPair]*pairAcc
	standby map[string]*standbyAcc
}

type pairAcc struct {
	sourceWs, targetWs float64 // W times seconds
	covered            time.Duration
}

type standbyAcc struct {
	wattSeconds float64
	covered     time.Duration
}

// Add takes the next sample and returns the hours closed by it. A sample
// that is not after the previous one is ignored.
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

// accumulate adds the interval of the last sample, only for the pairs and
// standby entities listed in it.
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

// Flush returns the unfinished hour (on shutdown) and starts afresh
// without a previous sample.
func (g *ChainIntegrator) Flush() []ChainHour {
	done := g.take()
	g.hasLast = false
	return done
}

// Reset discards the unfinished hour, for example after a clock jump.
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

// take returns the unfinished hour if it contains pairs or standby.
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
