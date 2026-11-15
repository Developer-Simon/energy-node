package performance

import (
	"sort"
	"sync"
	"time"
)

const (
	defaultInterval = 15 * time.Second
	defaultWindow   = 6 * time.Hour
)

// fixedUnits are the device's own units that are not in the service map.
// The dashboard's own unit (DashboardUnit) is always read by the Source.
var fixedUnits = []string{
	"automation.service",
	"mosquitto.service",
	"tailscaled.service",
	"caddy.service",
}

// Options configures the Monitor. Interval and Window fall back to 15 s and
// 6 h when they are zero.
type Options struct {
	Interval, Window, Warmup time.Duration     // 15 s, 6 h, 5 min
	Units                    map[string]string // Dienst-ID -> Unit-Name
	Busy                     func() bool       // Update oder Installer laeuft
	Clients                  func() int
	Device                   func() DeviceSignals
	RoundS                   func(serviceID string) (float64, bool) // Rundendauer je Dienst, false: unbekannt
}

// DeviceSignals are the device readings that come from outside the Source.
type DeviceSignals struct {
	TempC     *float64
	Throttled bool
}

// Monitor samples the Source every Interval and keeps the samples for Window
// in memory. Samples during Warmup after the first Tick and while Busy reports
// true are dropped.
type Monitor struct {
	src     Source
	counter *Counter
	opts    Options

	mu         sync.Mutex
	started    bool
	start      time.Time // first Tick, starts the warmup
	last       Raw       // baseline of the previous Tick
	lastAt     time.Time
	device     DeviceSignals
	cpu        *Ring
	unitCPU    map[string]*Ring
	clients    *Ring
	swap       *Ring
	ramFree    *Ring
	prevCount  map[string]int64
	msgPerMin  map[string]float64
	changeDir  string  // data dir of last-change.json, empty: memory only
	change     *Change // applied change under evaluation, nil when none
	budget     func(Snapshot) Budget
	overBudget OverBudgetTracker
}

// Snapshot is the monitor's view at one point in time.
type Snapshot struct {
	MeasuredFor   time.Duration
	CPUP95        float64 // % der Gesamtleistung aller Kerne
	Cores         int
	TempC         *float64
	Throttled     bool
	RAMFreePct    float64 // nur gueltig, wenn RAMFreeKnown
	RAMFreeKnown  bool    // false, solange im Fenster keine RAM-Probe liegt
	SwapPagesPerS float64
	ClientsMax    int  // hoechster Wert im Fenster, mindestens 1
	ClientsSeen   bool // false: nie ein Client ausser dem aktuellen
	Units         []UnitStats
	External      *SourceRate
}

// UnitStats is the load of one unit. ID is the service ID for service units and
// the unit name for Fixed units.
type UnitStats struct {
	ID        string
	CPUP95    float64
	RAMBytes  int64
	HasRAM    bool
	RoundS    *float64
	MsgPerMin float64
	Fixed     bool
}

// NewMonitor creates a Monitor. It samples nothing until Tick is called.
func NewMonitor(src Source, counter *Counter, opts Options) *Monitor {
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Window <= 0 {
		opts.Window = defaultWindow
	}
	return &Monitor{
		src:       src,
		counter:   counter,
		opts:      opts,
		cpu:       newSeriesRing(opts),
		unitCPU:   map[string]*Ring{},
		clients:   newSeriesRing(opts),
		swap:      newSeriesRing(opts),
		ramFree:   newSeriesRing(opts),
		prevCount: map[string]int64{},
		msgPerMin: map[string]float64{},
	}
}

func newSeriesRing(opts Options) *Ring {
	return NewRing(int(opts.Window/opts.Interval)+2, opts.Window)
}

// Tick reads the Source once. The first Tick only stores the baseline.
func (m *Monitor) Tick(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := m.src.Read(m.readNames())
	if err != nil {
		return
	}
	if m.opts.Device != nil {
		m.device = m.opts.Device()
	}
	if !m.started {
		m.started = true
		m.start = now
		m.last, m.lastAt = raw, now
		m.countMessages(now, 0)
		return
	}

	dt := now.Sub(m.lastAt)
	m.countMessages(now, dt)
	sampled := false
	if dt > 0 && now.Sub(m.start) >= m.opts.Warmup && !m.busy() {
		m.sample(now, dt, raw)
		sampled = true
	}
	m.last, m.lastAt = raw, now
	if sampled {
		m.evaluateOverBudget(now, dt)
	}
	m.evaluateChange(now)
}

// SetBudget sets the budget the over-budget check compares with. fn runs inside
// Tick and receives the snapshot of that tick, so it must not call the Monitor.
func (m *Monitor) SetBudget(fn func(Snapshot) Budget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budget = fn
}

// OverBudget returns the over-budget finding and whether it stands. The finding
// follows the readings of Tick, now is not used for the decision.
func (m *Monitor) OverBudget(now time.Time) (OverBudgetInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overBudget.Info()
}

// evaluateOverBudget feeds the tracker with the P95 of the last five minutes.
// The caller holds m.mu and has just kept a sample. An empty window skips the
// reading. A gap longer than two intervals (a stall) counts as one interval.
func (m *Monitor) evaluateOverBudget(now time.Time, step time.Duration) {
	if m.budget == nil {
		return
	}
	values := m.cpu.Since(now.Add(-overBudgetWindow))
	if len(values) == 0 {
		return
	}
	if step > 2*m.opts.Interval {
		step = m.opts.Interval
	}
	budget := m.budget(m.snapshotLocked(now))
	m.overBudget.Feed(percentile(values, 0.95), budget, step)
}

// AttachChange sets the data dir of the change file and loads the change in it.
// A change that was running when the dashboard stopped is checked again from now.
func (m *Monitor) AttachChange(dir string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.changeDir = dir
	c, err := LoadChange(dir)
	if err != nil {
		return err
	}
	m.change = c
	if c != nil && c.State == "running" {
		c.Restarted = true
		c.At = now
		return m.saveChangeLocked()
	}
	return nil
}

// RecordChange holds c as the current change and saves it when a data dir is attached.
func (m *Monitor) RecordChange(c Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	held := cloneChange(c)
	m.change = &held
	return m.saveChangeLocked()
}

// CurrentChange returns a copy of the held change, nil when none is held.
func (m *Monitor) CurrentChange() *Change {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.change == nil {
		return nil
	}
	c := cloneChange(*m.change)
	return &c
}

// MarkUndone sets the held change to undone and saves it. Without a held change it does nothing.
func (m *Monitor) MarkUndone() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.change == nil {
		return nil
	}
	m.change.State = "undone"
	return m.saveChangeLocked()
}

// evaluateChange judges a running change once its check window has passed.
// The caller holds m.mu. The change file is written only when the state changes.
func (m *Monitor) evaluateChange(now time.Time) {
	if m.change == nil || m.change.State != "running" {
		return
	}
	// m.cpu is read directly, SamplesSince would take m.mu again.
	m.change.Evaluate(now, m.cpu.Since(m.change.At))
	if m.change.State != "running" {
		_ = m.saveChangeLocked()
	}
}

// saveChangeLocked writes the held change when a data dir is attached.
// The caller holds m.mu.
func (m *Monitor) saveChangeLocked() error {
	if m.changeDir == "" || m.change == nil {
		return nil
	}
	return SaveChange(m.changeDir, *m.change)
}

// cloneChange copies c, including its maps and Measured.
func cloneChange(c Change) Change {
	out := c
	out.Before = cloneRows(c.Before)
	out.After = cloneRows(c.After)
	if c.Measured != nil {
		v := *c.Measured
		out.Measured = &v
	}
	return out
}

func cloneRows(rows map[RowID]float64) map[RowID]float64 {
	if rows == nil {
		return nil
	}
	out := make(map[RowID]float64, len(rows))
	for row, v := range rows {
		out[row] = v
	}
	return out
}

// Snapshot returns the window ending at now.
func (m *Monitor) Snapshot(now time.Time) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(now)
}

// snapshotLocked is Snapshot for a caller that holds m.mu.
func (m *Monitor) snapshotLocked(now time.Time) Snapshot {
	m.cpu.Prune(now)
	m.clients.Prune(now)
	m.swap.Prune(now)
	m.ramFree.Prune(now)
	for _, r := range m.unitCPU {
		r.Prune(now)
	}

	clientsMax := 1
	for _, v := range m.clients.Values() {
		if int(v) > clientsMax {
			clientsMax = int(v)
		}
	}

	ramFree, ramKnown := lastSample(m.ramFree.Values())

	return Snapshot{
		MeasuredFor:   time.Duration(m.cpu.Len()) * m.opts.Interval,
		CPUP95:        m.cpu.P95(),
		Cores:         m.last.Cores,
		TempC:         m.device.TempC,
		Throttled:     m.device.Throttled,
		RAMFreePct:    ramFree,
		RAMFreeKnown:  ramKnown,
		SwapPagesPerS: m.swap.P95(),
		ClientsMax:    clientsMax,
		ClientsSeen:   clientsMax > 1,
		Units:         m.unitStats(),
		External:      m.external(now),
	}
}

// SamplesSince returns the total CPU samples in percent since t, the oldest first.
func (m *Monitor) SamplesSince(t time.Time) []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cpu.Since(t)
}

// readNames lists the units to read: the service units and the fixed units.
func (m *Monitor) readNames() []string {
	names := make([]string, 0, len(m.opts.Units)+len(fixedUnits))
	for _, unit := range m.opts.Units {
		names = append(names, unit)
	}
	names = append(names, fixedUnits...)
	sort.Strings(names)
	return names
}

func (m *Monitor) busy() bool {
	return m.opts.Busy != nil && m.opts.Busy()
}

// countMessages updates the message rate of each service from the counter
// deltas. dt 0 only sets the baseline.
func (m *Monitor) countMessages(now time.Time, dt time.Duration) {
	if m.counter == nil {
		return
	}
	for id := range m.opts.Units {
		count := m.counter.Count(id)
		if prev, ok := m.prevCount[id]; ok && dt > 0 {
			m.msgPerMin[id] = float64(count-prev) / dt.Minutes()
		}
		m.prevCount[id] = count
	}
}

// sample stores one kept sample. A counter that runs backwards drops only the
// sample of that series.
func (m *Monitor) sample(now time.Time, dt time.Duration, raw Raw) {
	prev := m.last
	secs := dt.Seconds()

	if raw.CPUTotal > prev.CPUTotal && raw.CPUBusy >= prev.CPUBusy {
		total := raw.CPUTotal - prev.CPUTotal
		busy := raw.CPUBusy - prev.CPUBusy
		m.cpu.Add(now, float64(busy)/float64(total)*100)
	}

	for name, u := range raw.Units {
		p, ok := prev.Units[name]
		if !ok || u.CPUUsec < p.CPUUsec || raw.Cores < 1 {
			continue
		}
		pct := float64(u.CPUUsec-p.CPUUsec) / (secs * float64(raw.Cores) * 1e6) * 100
		ring, ok := m.unitCPU[name]
		if !ok {
			ring = newSeriesRing(m.opts)
			m.unitCPU[name] = ring
		}
		ring.Add(now, pct)
	}

	if raw.MemTotalKB > 0 {
		m.ramFree.Add(now, float64(raw.MemAvailKB)/float64(raw.MemTotalKB)*100)
	}
	if raw.SwapIn >= prev.SwapIn && raw.SwapOut >= prev.SwapOut {
		pages := (raw.SwapIn - prev.SwapIn) + (raw.SwapOut - prev.SwapOut)
		m.swap.Add(now, float64(pages)/secs)
	}
	if m.opts.Clients != nil {
		m.clients.Add(now, float64(m.opts.Clients()))
	}
}

// unitStats lists the units of the last Tick, sorted by ID. A unit that is
// not in the last sample (stopped) is left out.
func (m *Monitor) unitStats() []UnitStats {
	out := make([]UnitStats, 0, len(m.last.Units))
	for name, u := range m.last.Units {
		id, fixed := m.unitID(name)
		st := UnitStats{ID: id, Fixed: fixed}
		if r, ok := m.unitCPU[name]; ok {
			st.CPUP95 = r.P95()
		}
		if u.HasMem {
			st.RAMBytes, st.HasRAM = u.MemBytes, true
		}
		if !fixed {
			st.MsgPerMin = m.msgPerMin[id]
			st.RoundS = m.roundS(id)
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// roundS returns the poll round duration of a service, nil when it is unknown.
func (m *Monitor) roundS(serviceID string) *float64 {
	if m.opts.RoundS == nil {
		return nil
	}
	v, ok := m.opts.RoundS(serviceID)
	if !ok {
		return nil
	}
	return &v
}

// unitID maps a unit name to its service ID. A unit that is not in the
// service map is Fixed and keeps its name as ID.
func (m *Monitor) unitID(name string) (string, bool) {
	for id, unit := range m.opts.Units {
		if unit == name {
			return id, false
		}
	}
	return name, true
}

func (m *Monitor) external(now time.Time) *SourceRate {
	if m.counter == nil {
		return nil
	}
	return m.counter.External(now)
}

// lastSample liefert den juengsten Wert und ob es ueberhaupt einen gibt.
func lastSample(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	return values[len(values)-1], true
}
