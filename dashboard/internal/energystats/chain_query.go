package energystats

import (
	"slices"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// minEfficiencyKWh is the source energy below which a segment has no
// efficiency, the ratio of two tiny numbers says nothing.
const minEfficiencyKWh = 0.01

// roundTripMinKWh is the charge below which a round trip has no value.
const roundTripMinKWh = 0.1

// ChainInfo describes one measurement chain for the queries.
type ChainInfo struct {
	ID      string
	Chain   energy.Chain
	Storage bool // the role of the counting point stores energy
}

// ChainTotals are the sums of a chain over a period. Efficiency is the
// product of its segment efficiencies and nil if one segment has no data.
// SourceKWh is the source energy entering the first segment, both directions
// summed. It is 0 when Efficiency is nil, so the stats can weight the
// efficiencies of several chains by it.
type ChainTotals struct {
	LossKWh    float64  `json:"loss_kwh"`
	StandbyKWh float64  `json:"standby_kwh"`
	Efficiency *float64 `json:"efficiency"`
	SourceKWh  float64  `json:"source_kwh"`
	HasData    bool     `json:"has_data"`
}

type ChainPeriod struct {
	ID      string        `json:"id"`
	Label   string        `json:"label"`
	Buckets []ChainTotals `json:"buckets"`
	Total   ChainTotals   `json:"total"`
}

type ChainStatsResponse struct {
	Period   string        `json:"period"`
	At       string        `json:"at"`
	Revision uint64        `json:"revision"`
	Chains   []ChainPeriod `json:"chains"`
}

type ChainDay struct {
	Local string `json:"local"`
	ChainTotals
}

type ChainEfficiency struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	RoundTrip30D *float64   `json:"round_trip_30d"`
	StandbyW30D  *float64   `json:"standby_w_30d"`
	LossMonthKWh float64    `json:"loss_month_kwh"`
	Month        string     `json:"month"` // "2026-11", local time
	Days         []ChainDay `json:"days"`  // 7 days, oldest first
}

type EfficiencyResponse struct {
	Revision uint64            `json:"revision"`
	Chains   []ChainEfficiency `json:"chains"`
}

// chainAccumulator sums the hours of one chain. It keeps the energy of
// every directed pair of neighbouring points and the standby of the first
// point.
type chainAccumulator struct {
	info    ChainInfo
	keys    []ChainPair // both directions of every segment
	sums    map[ChainPair]PairEnergy
	standby StandbyEnergy
	hasData bool
}

func newChainAccumulator(info ChainInfo) *chainAccumulator {
	a := &chainAccumulator{info: info, sums: map[ChainPair]PairEnergy{}}
	points := info.Chain.Points
	for i := 0; i+1 < len(points); i++ {
		forward := ChainPair{Source: points[i], Target: points[i+1]}
		backward := ChainPair{Source: points[i+1], Target: points[i]}
		a.keys = append(a.keys, forward, backward)
		a.sums[forward] = PairEnergy{}
		a.sums[backward] = PairEnergy{}
	}
	return a
}

// add takes the hour with index i of month m. A nil month has no data.
func (a *chainAccumulator) add(m *ChainMonth, i int) {
	if m == nil {
		return
	}
	for _, pair := range a.keys {
		e, ok := m.Pairs[pair][i]
		if !ok {
			continue
		}
		sum := a.sums[pair]
		sum.SourceKWh += e.SourceKWh
		sum.TargetKWh += e.TargetKWh
		a.sums[pair] = sum
		a.hasData = true
	}
	if points := a.info.Chain.Points; len(points) > 0 {
		if e, ok := m.Standby[points[0]][i]; ok {
			a.standby.KWh += e.KWh
			a.standby.Seconds += e.Seconds
			a.hasData = true
		}
	}
}

// totals sums the loss of all segments, both directions included, plus the
// standby. The efficiency is the product of the segment efficiencies.
func (a *chainAccumulator) totals() ChainTotals {
	points := a.info.Chain.Points
	t := ChainTotals{StandbyKWh: a.standby.KWh, HasData: a.hasData}
	t.LossKWh = a.standby.KWh
	efficiency := 1.0
	complete := len(points) >= 2
	var first float64
	for i := 0; i+1 < len(points); i++ {
		forward := ChainPair{Source: points[i], Target: points[i+1]}
		backward := ChainPair{Source: points[i+1], Target: points[i]}
		source := a.sums[forward].SourceKWh + a.sums[backward].SourceKWh
		target := a.sums[forward].TargetKWh + a.sums[backward].TargetKWh
		if i == 0 {
			first = source
		}
		t.LossKWh += source - target
		if source < minEfficiencyKWh {
			complete = false
			continue
		}
		efficiency *= target / source
	}
	if complete {
		t.Efficiency = &efficiency
		t.SourceKWh = first
	}
	return t
}

// roundTrip is the discharged energy divided by the charged energy at the
// counting point. Charging follows the point order, reversed flips it. The
// neighbour of the counting point is the next point, or the previous one at
// the end of the chain.
func (a *chainAccumulator) roundTrip() *float64 {
	chain := a.info.Chain
	if !a.info.Storage || len(chain.Points) < 2 {
		return nil
	}
	if chain.Direction == energy.ChainDirectionFixedDown || chain.Direction == energy.ChainDirectionFixedUp {
		return nil
	}
	c := slices.Index(chain.Points, chain.Counting)
	if c < 0 {
		return nil
	}
	lo := c
	if c+1 >= len(chain.Points) {
		lo = c - 1
	}
	forward := ChainPair{Source: chain.Points[lo], Target: chain.Points[lo+1]}
	backward := ChainPair{Source: forward.Target, Target: forward.Source}
	charging, discharging := forward, backward
	if chain.Direction == energy.ChainDirectionReversed {
		charging, discharging = backward, forward
	}
	// The counting point reads the source side of a pair it starts and the
	// target side of a pair it ends.
	atCounting := func(pair ChainPair) float64 {
		e := a.sums[pair]
		if pair.Source == chain.Counting {
			return e.SourceKWh
		}
		return e.TargetKWh
	}
	charged := atCounting(charging)
	if charged < roundTripMinKWh {
		return nil
	}
	value := atCounting(discharging) / charged
	return &value
}

// standbyW is the mean standby power in W, nil without covered seconds.
func (a *chainAccumulator) standbyW() *float64 {
	if a.standby.Seconds <= 0 {
		return nil
	}
	value := a.standby.KWh * 1000 * 3600 / float64(a.standby.Seconds)
	return &value
}

// chainSums adds the hours in [starts[0], end) to one accumulator per chain
// and bucket. An hour goes to the last bucket that starts at or before it.
// The result is indexed [chain][bucket].
func chainSums(store *ChainStore, chains []ChainInfo, starts []time.Time, end time.Time) ([][]*chainAccumulator, error) {
	sums := make([][]*chainAccumulator, len(chains))
	for c, info := range chains {
		sums[c] = make([]*chainAccumulator, len(starts))
		for b := range starts {
			sums[c][b] = newChainAccumulator(info)
		}
	}
	if len(chains) == 0 || len(starts) == 0 {
		return sums, nil
	}
	b := 0
	err := store.Each(starts[0], end, func(hour time.Time, m *ChainMonth, i int) {
		for b+1 < len(starts) && !hour.Before(starts[b+1]) {
			b++
		}
		for c := range sums {
			sums[c][b].add(m, i)
		}
	})
	return sums, err
}

// chains returns the measurement chains, none without a store.
func (s *Service) chains() []ChainInfo {
	if s.ChainStore == nil || s.Chains == nil {
		return nil
	}
	return s.Chains()
}

func (s *Service) currentTime() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) location() *time.Location {
	if s.Location != nil {
		return s.Location
	}
	return time.Local
}

func (s *Service) revision() uint64 {
	if s.Store == nil {
		return 0
	}
	return s.Store.Revision()
}

// ChainStats sums the measurement chains per bucket of the period. Like
// Query, hours from the current one on do not count.
func (s *Service) ChainStats(period, at string) (ChainStatsResponse, error) {
	now := s.currentTime()
	revision := s.revision()
	from, to, err := bounds(period, at, s.location(), now)
	if err != nil {
		return ChainStatsResponse{}, err
	}
	chains := s.chains()
	starts := bucketStarts(period, from, to)
	end := now.UTC().Truncate(time.Hour)
	if to.Before(end) {
		end = to
	}
	buckets, err := chainSums(s.ChainStore, chains, starts, end)
	if err != nil {
		return ChainStatsResponse{}, err
	}
	total, err := chainSums(s.ChainStore, chains, []time.Time{from}, end)
	if err != nil {
		return ChainStatsResponse{}, err
	}
	res := ChainStatsResponse{Period: period, At: from.Format(periodLayouts[period]), Revision: revision, Chains: make([]ChainPeriod, len(chains))}
	for c, info := range chains {
		p := ChainPeriod{ID: info.ID, Label: info.Chain.Label, Buckets: make([]ChainTotals, len(starts)), Total: total[c][0].totals()}
		for b := range starts {
			p.Buckets[b] = buckets[c][b].totals()
		}
		res.Chains[c] = p
	}
	return res, nil
}

// Efficiency reports per chain the round trip and standby of the last 30
// days, and the loss of the local month and of the last seven days.
func (s *Service) Efficiency() (EfficiencyResponse, error) {
	now := s.currentTime()
	loc := s.location()
	revision := s.revision()
	chains := s.chains()
	cutoff := now.UTC().Truncate(time.Hour)
	local := now.In(loc)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	dayStarts := make([]time.Time, 7)
	for i := range dayStarts {
		dayStarts[i] = time.Date(local.Year(), local.Month(), local.Day()-6+i, 0, 0, 0, 0, loc)
	}
	rolling, err := chainSums(s.ChainStore, chains, []time.Time{cutoff.AddDate(0, 0, -30)}, cutoff)
	if err != nil {
		return EfficiencyResponse{}, err
	}
	month, err := chainSums(s.ChainStore, chains, []time.Time{monthStart}, cutoff)
	if err != nil {
		return EfficiencyResponse{}, err
	}
	days, err := chainSums(s.ChainStore, chains, dayStarts, cutoff)
	if err != nil {
		return EfficiencyResponse{}, err
	}
	res := EfficiencyResponse{Revision: revision, Chains: make([]ChainEfficiency, len(chains))}
	for c, info := range chains {
		e := ChainEfficiency{
			ID:           info.ID,
			Label:        info.Chain.Label,
			RoundTrip30D: rolling[c][0].roundTrip(),
			StandbyW30D:  rolling[c][0].standbyW(),
			LossMonthKWh: month[c][0].totals().LossKWh,
			Month:        local.Format("2006-01"),
			Days:         make([]ChainDay, len(dayStarts)),
		}
		for d, start := range dayStarts {
			e.Days[d] = ChainDay{Local: start.Format("2006-01-02T15:04"), ChainTotals: days[c][d].totals()}
		}
		res.Chains[c] = e
	}
	return res, nil
}
