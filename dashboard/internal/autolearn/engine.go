package autolearn

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

const (
	// engineDailyHour and engineDailyMinute are the local time from which the
	// daily detectors run, once per day.
	engineDailyHour   = 3
	engineDailyMinute = 30
	// engineCatchUpAfter is the delay after the start before a catch-up daily
	// round runs. engineDailyMaxAge is the age of the last daily round from
	// which on a catch-up is due.
	engineCatchUpAfter = 5 * time.Minute
	engineDailyMaxAge  = 24 * time.Hour
	// engineStreak is the number of overruns in a row that asks for a downgrade.
	engineStreak = 3
	// engineWindow is the window for the overrun count and the compute share.
	engineWindow = 24 * time.Hour
	// engineHistoryDays is how far back the detectors read the day profiles.
	// The day store prunes itself to its own retention.
	engineHistoryDays = 366
	// engineShareEco and engineShareOther are the compute shares of one core
	// over the window that ask for a downgrade (eco and the other levels).
	engineShareEco   = 0.05
	engineShareOther = 0.10
)

// Detector is one learning step. Light detectors run every LightEvery, the
// others once a day at 03:30. PerDevice runs the detector once per device.
// Run must return when its context ends and must not change in.
type Detector struct {
	Name      string
	Group     string
	Light     bool
	PerDevice bool
	Run       func(ctx context.Context, in *Input, device string) []Candidate
}

// pending is a detector run that is still to do, on one device.
type pending struct {
	det    Detector
	device string
}

// spend is the compute time of one round per detector.
type spend struct {
	at   time.Time
	took map[string]time.Duration
}

// Engine runs the registered detectors in rounds from Tick. It keeps the last
// candidates of each detector, the work that did not finish within the budget,
// and the compute load, and it asks for a downgrade when the load is too high.
type Engine struct {
	days      *DayStore
	reactions *Reactions
	readDoc   func() ([]byte, error)
	devices   func() map[string]DeviceInfo
	limits    func() learnrec.Limits
	loc       *time.Location

	// tickMu serialises Tick, so one round runs at a time.
	tickMu sync.Mutex

	mu        sync.Mutex
	detectors []Detector
	queue     []pending
	startAt   time.Time
	catchUp   time.Duration
	lastLight time.Time
	lastDaily time.Time
	dailyDay  string
	last      map[string][]Candidate
	streak    map[string]int
	overruns  []time.Time
	spend     []spend
	now       time.Time
	downgrade bool
}

// NewEngine builds an engine without detectors. The functions are read on
// every round, so settings changes apply on the next round.
func NewEngine(days *DayStore, reactions *Reactions, readDoc func() ([]byte, error), devices func() map[string]DeviceInfo, limits func() learnrec.Limits, loc *time.Location) *Engine {
	if loc == nil {
		loc = time.Local
	}
	return &Engine{
		days:      days,
		reactions: reactions,
		readDoc:   readDoc,
		devices:   devices,
		limits:    limits,
		loc:       loc,
		catchUp:   engineCatchUpAfter,
		last:      map[string][]Candidate{},
		streak:    map[string]int{},
	}
}

// SetCatchUpAfter changes the delay after the start before the catch-up daily
// round. The demo sets 0, so suggestions appear on the first minute tick.
// A negative delay counts as 0.
func (e *Engine) SetCatchUpAfter(d time.Duration) {
	if d < 0 {
		d = 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.catchUp = d
}

// Register adds a detector. A detector without Run is ignored.
func (e *Engine) Register(d Detector) {
	if d.Run == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detectors = append(e.detectors, d)
}

// Tick runs one round if a detector is due at now. Light detectors are due
// every LightEvery, the others once a day from 03:30 local time. Only the
// detectors of the enabled groups run. Work that did not finish last time
// runs first. Tick blocks for at most the budget, so it belongs in its own
// goroutine.
func (e *Engine) Tick(now time.Time, groups map[string]bool) {
	e.tickMu.Lock()
	defer e.tickMu.Unlock()

	limits := e.limits()
	local := now.In(e.loc)
	devices := e.devices()
	dayKey := local.Format("2006-01-02")
	dailyAt := time.Date(local.Year(), local.Month(), local.Day(), engineDailyHour, engineDailyMinute, 0, 0, e.loc)

	e.mu.Lock()
	e.now = now
	if e.startAt.IsZero() {
		e.startAt = now
		// A start after the 03:30 slot leaves today's slot to the catch-up.
		if !local.Before(dailyAt) {
			e.dailyDay = dayKey
		}
	}
	detectors := append([]Detector(nil), e.detectors...)
	queue := e.queue
	e.queue = nil
	lightDue := e.lastLight.IsZero() || now.Sub(e.lastLight) >= limits.LightEvery
	dailyDue := !local.Before(dailyAt) && e.dailyDay != dayKey
	// Catch-up: a daily round runs once, five minutes after the start (or after
	// SetCatchUpAfter), when the last one is more than a day old or there was none.
	if !dailyDue && now.Sub(e.startAt) >= e.catchUp &&
		(e.lastDaily.IsZero() || now.Sub(e.lastDaily) > engineDailyMaxAge) {
		dailyDue = true
	}
	// A round only counts when one of its detectors is in an enabled group.
	// Otherwise the timer stays, and the round runs at once when the group is
	// switched on.
	if lightDue && !anyEnabled(detectors, groups, true) {
		lightDue = false
	}
	if dailyDue && !anyEnabled(detectors, groups, false) {
		dailyDue = false
	}
	if lightDue {
		e.lastLight = now
	}
	if dailyDue {
		e.dailyDay = dayKey
		e.lastDaily = now
	}
	// Unfinished work only goes into a round that is due. Otherwise it waits
	// for the next due round.
	if !lightDue && !dailyDue {
		e.queue = queue
		queue = nil
	}
	e.mu.Unlock()

	seen := map[string]bool{}
	var work []pending
	add := func(p pending) {
		key := p.det.Name + "\x00" + p.device
		if seen[key] || !groups[p.det.Group] {
			return
		}
		seen[key] = true
		work = append(work, p)
	}
	for _, p := range queue {
		add(p)
	}
	for _, d := range detectors {
		if !(d.Light && lightDue || !d.Light && dailyDue) {
			continue
		}
		if !d.PerDevice {
			add(pending{det: d})
			continue
		}
		for _, id := range sortedDeviceIDs(devices) {
			add(pending{det: d, device: id})
		}
	}
	if len(work) == 0 {
		return
	}

	in := e.input(now, limits, devices)
	tasks := make([]Task, len(work))
	for i, p := range work {
		p := p
		tasks[i] = Task{Detector: p.det.Name, Device: p.device, Run: func(ctx context.Context) []Candidate {
			out := p.det.Run(ctx, in, p.device)
			for j := range out {
				out[j].Detector = p.det.Name
			}
			return out
		}}
	}
	done, unfinished, took := RunPool(context.Background(), limits.Workers, limits.Budget, tasks)
	e.record(now, limits, work, done, unfinished, took)
}

// record stores the results of a round, the unfinished work, the overruns and
// the compute time, and sets the downgrade signal when a limit is hit.
func (e *Engine) record(now time.Time, limits learnrec.Limits, work []pending, done []Candidate, unfinished []Task, took map[string]time.Duration) {
	byKey := map[string]pending{}
	ran := map[string]bool{}
	for _, p := range work {
		byKey[p.det.Name+"\x00"+p.device] = p
		ran[p.det.Name] = true
	}
	late := map[string]bool{}
	var carry []pending
	for _, t := range unfinished {
		late[t.Detector] = true
		carry = append(carry, byKey[t.Detector+"\x00"+t.Device])
	}
	byDet := map[string][]Candidate{}
	for _, c := range done {
		byDet[c.Detector] = append(byDet[c.Detector], c)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.queue = carry
	for name := range ran {
		if late[name] {
			e.streak[name]++
			e.overruns = append(e.overruns, now)
			continue
		}
		e.streak[name] = 0
		e.last[name] = byDet[name]
	}
	e.spend = append(e.spend, spend{at: now, took: took})
	e.pruneLocked(now)

	tripped := false
	for name := range late {
		if e.streak[name] >= engineStreak {
			tripped = true
		}
	}
	if e.shareLocked(now) > shareLimit(limits) {
		tripped = true
	}
	if tripped {
		e.downgrade = true
		e.streak = map[string]int{}
		e.spend = nil
		e.overruns = nil
	}
}

// Candidates returns the findings of the last run of every detector, in the
// order of the detector names. Learned values are left out, see Values.
func (e *Engine) Candidates() []Candidate {
	var out []Candidate
	for _, c := range e.lastInOrder() {
		if c.Value == nil {
			out = append(out, c)
		}
	}
	return out
}

// Values returns the learned values of the last run of every detector, in the
// order of the detector names.
func (e *Engine) Values() []Value {
	var out []Value
	for _, c := range e.lastInOrder() {
		if c.Value != nil {
			out = append(out, *c.Value)
		}
	}
	return out
}

// lastInOrder returns the candidates of the last run of every detector, in the
// order of the detector names.
func (e *Engine) lastInOrder() []Candidate {
	e.mu.Lock()
	defer e.mu.Unlock()
	names := make([]string, 0, len(e.last))
	for name := range e.last {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []Candidate
	for _, name := range names {
		out = append(out, e.last[name]...)
	}
	return out
}

// Load reports the overruns in the last 24 hours, the compute share of one
// core over the same window and the compute time per detector.
func (e *Engine) Load() (overruns int, share float64, took map[string]time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	took = map[string]time.Duration{}
	cutoff := e.now.Add(-engineWindow)
	for _, o := range e.overruns {
		if !o.Before(cutoff) {
			overruns++
		}
	}
	var total time.Duration
	for _, s := range e.spend {
		if s.at.Before(cutoff) {
			continue
		}
		for name, d := range s.took {
			took[name] += d
			total += d
		}
	}
	return overruns, total.Seconds() / engineWindow.Seconds(), took
}

// Downgrade reports whether the load asked for a lower level since the last
// call. The signal is read once: the caller lowers the level, the engine
// starts counting again from zero.
func (e *Engine) Downgrade() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.downgrade
	e.downgrade = false
	return v
}

// input builds what one round reads. A missing or broken day store or rule
// document is logged and replaced by an empty one, so the round still runs.
func (e *Engine) input(now time.Time, limits learnrec.Limits, devices map[string]DeviceInfo) *Input {
	in := &Input{Now: now, Loc: e.loc, Devices: devices, Reactions: e.reactions, Limits: limits}
	if e.days != nil {
		days, err := e.days.Days(now.AddDate(0, 0, -engineHistoryDays), now)
		if err != nil {
			log.Printf("autolearn: day profiles incomplete: %v", err)
		}
		in.Days = days
	}
	if e.readDoc != nil {
		raw, err := e.readDoc()
		if err != nil {
			log.Printf("autolearn: rule document unreadable: %v", err)
		} else if doc, err := ParseDoc(raw); err != nil {
			log.Printf("autolearn: rule document invalid: %v", err)
		} else {
			in.Doc = doc
		}
	}
	in.Base = BuildBase(in.Days, e.loc)
	return in
}

// pruneLocked drops the records older than the window.
func (e *Engine) pruneLocked(now time.Time) {
	cutoff := now.Add(-engineWindow)
	kept := e.spend[:0]
	for _, s := range e.spend {
		if !s.at.Before(cutoff) {
			kept = append(kept, s)
		}
	}
	e.spend = kept
	keptOver := e.overruns[:0]
	for _, o := range e.overruns {
		if !o.Before(cutoff) {
			keptOver = append(keptOver, o)
		}
	}
	e.overruns = keptOver
}

// shareLocked is the compute share of one core over the window.
func (e *Engine) shareLocked(now time.Time) float64 {
	cutoff := now.Add(-engineWindow)
	var total time.Duration
	for _, s := range e.spend {
		if s.at.Before(cutoff) {
			continue
		}
		for _, d := range s.took {
			total += d
		}
	}
	return total.Seconds() / engineWindow.Seconds()
}

// shareLimit is the compute share that asks for a downgrade. eco is the level
// with the limits of one core, the others get the larger share.
func shareLimit(limits learnrec.Limits) float64 {
	if limits == learnrec.LimitsFor(learnrec.LevelEco, 1) {
		return engineShareEco
	}
	return engineShareOther
}

// anyEnabled reports whether a detector of the given kind (light or daily) is
// in an enabled group.
func anyEnabled(detectors []Detector, groups map[string]bool, light bool) bool {
	for _, d := range detectors {
		if d.Light == light && groups[d.Group] {
			return true
		}
	}
	return false
}

func sortedDeviceIDs(devices map[string]DeviceInfo) []string {
	ids := make([]string, 0, len(devices))
	for id := range devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
