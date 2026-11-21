package autolearn

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

const (
	cycleBins       = 32       // histogram bins over log10(W)
	cycleLogMax     = 4.5      // upper end of the histogram, log10(W)
	cycleMinHist    = 2 * 1440 // minutes with power before a threshold is trusted
	cycleMinShare   = 0.10     // minimal share of minutes on each side of the threshold
	cycleMinGapLog  = 0.5      // minimal distance of the two group means, log10(W)
	cycleStartMin   = 2        // minutes above the threshold before a cycle opens
	cycleGapStart   = 15       // initial and lowest pause that ends a cycle, minutes
	cycleGapMax     = 90       // upper bound of the pause that ends a cycle, minutes
	cycleGapHistory = 50       // cycles whose pauses feed the percentile
	cyclePercentile = 0.95     // percentile of the pauses that sets the pause
	cycleMaxMinutes = 24 * 60  // a cycle open for longer is cut at this length
)

// CycleRec is one finished cycle of a device. Grid, PV and Bat are the shares
// of the cycle energy that came from grid import, PV and battery discharge.
type CycleRec struct {
	Device string  `json:"d"`
	Start  int64   `json:"at"`
	DurS   int     `json:"dur"`
	Wh     float32 `json:"wh"`
	OpW    float32 `json:"op"`
	PeakW  float32 `json:"peak"`
	Grid   float32 `json:"grid"`
	PV     float32 `json:"pv"`
	Bat    float32 `json:"bat"`
	End    string  `json:"end"`
}

// cycleSample is one minute of a device with its power and source shares.
type cycleSample struct {
	At time.Time `json:"t"`
	W  float64   `json:"w"`
	G  float64   `json:"g"`
	P  float64   `json:"p"`
	B  float64   `json:"b"`
}

// openCycle is a cycle that has started and not ended yet. Samples run from
// the start to the last minute above the threshold, Tail holds the minutes
// below the threshold after that.
type openCycle struct {
	Start     time.Time     `json:"start"`
	LastAbove time.Time     `json:"last"`
	Samples   []cycleSample `json:"samples"`
	Tail      []cycleSample `json:"tail,omitempty"`
	Pauses    []int         `json:"pauses,omitempty"`
}

// devCycleState is the learning state of one device.
type devCycleState struct {
	Hist [cycleBins]int `json:"hist"`
	Thr  float64        `json:"thr"`
	Run  []cycleSample  `json:"run,omitempty"`
	Open *openCycle     `json:"open,omitempty"`
	Gaps [][]int        `json:"gaps,omitempty"`
}

type cycleStateJSON struct {
	Devices map[string]*devCycleState `json:"devices"`
}

// CycleTracker finds the cycles of the devices from their minute power. The
// threshold of each device comes from an Otsu split of its power histogram.
type CycleTracker struct {
	devices   map[string]*devCycleState
	firstCall bool
}

// NewCycleTracker restores a tracker from State. Invalid or empty state gives
// a fresh tracker.
func NewCycleTracker(state []byte) *CycleTracker {
	t := &CycleTracker{devices: map[string]*devCycleState{}, firstCall: true}
	if len(state) == 0 {
		return t
	}
	var s cycleStateJSON
	if json.Unmarshal(state, &s) != nil {
		return t
	}
	for id, d := range s.Devices {
		if d != nil {
			t.devices[id] = d
		}
	}
	return t
}

// State returns the JSON of the tracker, to be restored with NewCycleTracker.
func (t *CycleTracker) State() []byte {
	b, err := json.Marshal(cycleStateJSON{Devices: t.devices})
	if err != nil {
		return nil
	}
	return b
}

// ObserveMinute feeds one completed minute and returns the cycles that ended
// in it. origin tells who switched a device off at a time, nil counts every
// end as self.
func (t *CycleTracker) ObserveMinute(f learnrec.Frame, origin func(device string, at time.Time) string) []CycleRec {
	var out []CycleRec
	if t.firstCall {
		// An open cycle whose last minute lies more than G back was cut off by
		// a restart and is dropped. A run of above-threshold minutes that is
		// that old is stale as well.
		for _, st := range t.devices {
			g := minutesDur(gapMinutes(st))
			if st.Open != nil && f.Minute.Sub(st.Open.lastMinute()) > g {
				st.Open = nil
			}
			if n := len(st.Run); n > 0 && f.Minute.Sub(st.Run[n-1].At) > g {
				st.Run = nil
			}
		}
		t.firstCall = false
	}

	// Without a positive load the source shares are unknown, so they stay 0
	// instead of claiming all the power came from PV.
	load := f.Balance["load_total"]
	var gridShare, batShare, pvShare float64
	if load > 0 {
		gridShare = clamp01(f.Balance["grid_import"] / load)
		batShare = clamp01(f.Balance["battery_discharge"] / load)
		pvShare = math.Max(0, 1-gridShare-batShare)
	}

	for _, dm := range f.Devices {
		w, ok := devicePower(dm)
		if !ok {
			continue
		}
		st := t.devices[dm.ID]
		if st == nil {
			st = &devCycleState{}
			t.devices[dm.ID] = st
		}
		s := cycleSample{At: f.Minute, W: w, G: gridShare, P: pvShare, B: batShare}
		st.Hist[histBin(w)]++
		st.Thr = otsuThreshold(st.Hist)
		above := st.Thr > 0 && w >= st.Thr

		if st.Open != nil && st.Open.minutes() >= cycleMaxMinutes {
			// A cycle that never ends is cut at the cap, this minute starts afresh.
			out = append(out, finishCycle(dm.ID, st.Open, "self"))
			addGap(st, st.Open.Pauses)
			st.Open = nil
		}
		if st.Open == nil {
			if !above {
				st.Run = nil
				continue
			}
			// A run only counts minutes that follow each other.
			if n := len(st.Run); n > 0 && s.At.Sub(st.Run[n-1].At) > time.Minute {
				st.Run = nil
			}
			st.Run = append(st.Run, s)
			if len(st.Run) < cycleStartMin {
				continue
			}
			st.Open = &openCycle{Start: st.Run[0].At, LastAbove: s.At, Samples: st.Run}
			st.Run = nil
			continue
		}

		oc := st.Open
		if above {
			if len(oc.Tail) > 0 {
				oc.Pauses = append(oc.Pauses, len(oc.Tail))
				oc.Samples = append(oc.Samples, oc.Tail...)
				oc.Tail = nil
			}
			oc.Samples = append(oc.Samples, s)
			oc.LastAbove = s.At
			continue
		}
		oc.Tail = append(oc.Tail, s)
		if len(oc.Tail) >= gapMinutes(st) {
			out = append(out, finishCycle(dm.ID, oc, endOf(origin, dm.ID, oc.LastAbove)))
			addGap(st, oc.Pauses)
			st.Open = nil
		}
	}
	return out
}

// lastMinute is the last minute of the cycle, above or below the threshold.
func (oc *openCycle) lastMinute() time.Time {
	if n := len(oc.Tail); n > 0 {
		return oc.Tail[n-1].At
	}
	return oc.LastAbove
}

// devicePower sums the minute power of the entities of a device. ok is false
// when no entity has a finite value.
func devicePower(dm learnrec.DeviceMinute) (float64, bool) {
	var sum float64
	has := false
	for _, w := range dm.Power {
		if finite(w) {
			sum += w
			has = true
		}
	}
	return sum, has
}

// histBin maps a power to its bin over log10(W). Values up to 1 W share bin 0.
func histBin(w float64) int {
	width := cycleLogMax / cycleBins
	x := 0.0
	if w > 1 {
		x = math.Log10(w)
	}
	b := int(x / width)
	return min(max(b, 0), cycleBins-1)
}

// otsuThreshold returns the power at the Otsu cut of the histogram, or 0 when
// the histogram gives no trusted split. A split is trusted when it has enough
// minutes, both sides hold at least a tenth of them and the two means lie far
// enough apart on the log scale.
func otsuThreshold(hist [cycleBins]int) float64 {
	total := 0
	for _, h := range hist {
		total += h
	}
	if total < cycleMinHist {
		return 0
	}
	width := cycleLogMax / cycleBins
	center := func(i int) float64 { return (float64(i) + 0.5) * width }
	var totalSum float64
	for i, h := range hist {
		totalSum += float64(h) * center(i)
	}
	bestT, bestVar := -1, -1.0
	var bestW0 int
	var bestM0, bestM1 float64
	var w0 int
	var s0 float64
	for t := 1; t < cycleBins; t++ {
		w0 += hist[t-1]
		s0 += float64(hist[t-1]) * center(t-1)
		w1 := total - w0
		if w0 == 0 || w1 == 0 {
			continue
		}
		m0 := s0 / float64(w0)
		m1 := (totalSum - s0) / float64(w1)
		between := float64(w0) * float64(w1) * (m0 - m1) * (m0 - m1)
		if between > bestVar {
			bestT, bestVar, bestW0 = t, between, w0
			bestM0, bestM1 = m0, m1
		}
	}
	if bestT < 0 {
		return 0
	}
	minSide := cycleMinShare * float64(total)
	if float64(bestW0) < minSide || float64(total-bestW0) < minSide {
		return 0
	}
	if bestM1-bestM0 < cycleMinGapLog {
		return 0
	}
	return math.Pow(10, float64(bestT)*width)
}

// gapMinutes is the pause that ends a cycle: twice the 95th percentile of the
// pauses of the last 50 cycles, kept between 15 and 90 minutes. Without pauses
// it is 15. Only pauses shorter than G stay inside a cycle, so G grows from
// the recorded pauses.
func gapMinutes(st *devCycleState) int {
	var all []float64
	for _, pauses := range st.Gaps {
		for _, p := range pauses {
			all = append(all, float64(p))
		}
	}
	if len(all) == 0 {
		return cycleGapStart
	}
	sort.Float64s(all)
	idx := int(math.Ceil(cyclePercentile*float64(len(all)))) - 1
	idx = min(max(idx, 0), len(all)-1)
	return min(max(int(2*all[idx]), cycleGapStart), cycleGapMax)
}

// addGap keeps the pauses of a finished cycle for the percentile.
func addGap(st *devCycleState, pauses []int) {
	st.Gaps = append(st.Gaps, pauses)
	if len(st.Gaps) > cycleGapHistory {
		st.Gaps = st.Gaps[len(st.Gaps)-cycleGapHistory:]
	}
}

// minutes is the number of minutes the cycle has run, above or below the
// threshold.
func (oc *openCycle) minutes() int {
	return len(oc.Samples) + len(oc.Tail)
}

func minutesDur(n int) time.Duration {
	return time.Duration(n) * time.Minute
}

// endOf names the cause of the end of a cycle: the origin of the switch-off
// when it is known, self otherwise.
func endOf(origin func(device string, at time.Time) string, device string, at time.Time) string {
	if origin == nil {
		return "self"
	}
	switch o := Origin(origin(device, at)); o {
	case OriginRule, OriginHand, OriginExternal:
		return string(o)
	}
	return "self"
}

// finishCycle turns the samples of an open cycle into its record.
func finishCycle(device string, oc *openCycle, end string) CycleRec {
	var sumW, wG, wP, wB float64
	ws := make([]float64, 0, len(oc.Samples))
	var peak float64
	for i, s := range oc.Samples {
		sumW += s.W
		wG += s.W * s.G
		wP += s.W * s.P
		wB += s.W * s.B
		ws = append(ws, s.W)
		if i < cycleStartMin {
			peak = math.Max(peak, s.W)
		}
	}
	sort.Float64s(ws)
	op := Median(ws[len(ws)/2:])
	rec := CycleRec{
		Device: device,
		Start:  oc.Start.Unix(),
		DurS:   int(oc.LastAbove.Sub(oc.Start).Seconds()) + 60,
		Wh:     float32(sumW / 60),
		OpW:    float32(op),
		PeakW:  float32(peak),
		End:    end,
	}
	if sumW > 0 {
		rec.Grid = float32(wG / sumW)
		rec.PV = float32(wP / sumW)
		rec.Bat = float32(wB / sumW)
	}
	return rec
}

func clamp01(x float64) float64 {
	return math.Min(math.Max(x, 0), 1)
}
