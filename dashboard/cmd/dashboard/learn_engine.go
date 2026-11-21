package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// seasonBackfillYears is how far back the energy statistics refill the season
// store the first time, when season.json is missing.
const seasonBackfillYears = 3

// seasonCloser closes the previous ISO week in the season store. It runs in the
// engine round goroutine, so it needs no lock. done is the calendar day of the
// last attempt, so each day is attempted once. A dashboard that was down on
// Monday closes that week on the next day it runs.
type seasonCloser struct {
	store   *autolearn.SeasonStore
	days    func(from, to time.Time) ([]autolearn.DayFile, error)
	learned func() map[string][]float64
	loc     *time.Location
	done    string
}

// Run closes the previous ISO week when the store has no week for it yet, or
// only a week refilled from the statistics. It runs on every weekday and never
// closes the current week.
func (c *seasonCloser) Run(now time.Time) {
	local := now.In(c.loc)
	day := local.Format("2006-01-02")
	if day == c.done {
		return
	}
	thisMonday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, c.loc)
	thisMonday = thisMonday.AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
	prevMonday := thisMonday.AddDate(0, 0, -7)
	year, week := prevMonday.ISOWeek()
	key := fmt.Sprintf("%d-W%02d", year, week)
	// A week refilled from the statistics is closed again from the day files,
	// the day files win over the statistics.
	if ws := c.store.Weeks(key, key); len(ws) == 0 || ws[0].Source != "days" {
		c.close(now, prevMonday, thisMonday)
	}
	c.done = day
}

// close reads the day files from prevMonday up to thisMonday and puts the week
// when it has four day files. The store is pruned and saved in any case.
func (c *seasonCloser) close(now, prevMonday, thisMonday time.Time) {
	days, err := c.days(prevMonday, thisMonday.AddDate(0, 0, -1))
	if err != nil {
		log.Printf("autolearn: season week day files incomplete: %v", err)
	}
	if w, ok := autolearn.WeekOf(days, c.learned(), c.loc); ok {
		c.store.Put(w)
	} else {
		log.Printf("autolearn: season week %s not closed, fewer than four day files", prevMonday.Format("2006-01-02"))
	}
	c.store.Prune(now)
	if err := c.store.Save(); err != nil {
		log.Printf("autolearn: season store not saved: %v", err)
	}
}

// learnedClockMinutes reads the learned clock times of the store as minutes of
// the day, keyed by the learned key. WeekOf takes the median of each key.
func learnedClockMinutes(store *autolearn.LearnedStore) func() map[string][]float64 {
	return func() map[string][]float64 {
		out := map[string][]float64{}
		for _, e := range store.Entries() {
			if e.Kind != autolearn.ValueTime {
				continue
			}
			text, ok := e.Value.(string)
			if !ok {
				continue
			}
			var h, m int
			if _, err := fmt.Sscanf(text, "%d:%d", &h, &m); err != nil {
				continue
			}
			out[e.Key] = append(out[e.Key], float64(h*60+m))
		}
		return out
	}
}

// backfillSeason fills the season store once from the hourly energy statistics
// of the last seasonBackfillYears years. Day files already in the store win,
// the store keeps them. The store is saved at the end, so the file exists and
// the backfill does not run again.
func backfillSeason(store *autolearn.SeasonStore, stats *energystats.Store, now time.Time, loc *time.Location) error {
	from := now.AddDate(-seasonBackfillYears, 0, 0)
	var hours []energystats.HourRecord
	err := stats.Each(from, now, func(hour time.Time, m *energystats.Month, i int) {
		if m == nil {
			return
		}
		rec := energystats.HourRecord{Hour: hour, KWh: map[string]float64{}}
		for _, role := range []string{"grid_export", "pv", "load_total"} {
			s := m.Roles[role]
			if s == nil || i >= len(s.KWh) || math.IsNaN(s.KWh[i]) || s.Source[i] == energystats.SourceNone {
				continue
			}
			if s.Source[i] == energystats.SourceOwn && m.Covered[i] < energystats.MinOwnCoverSeconds {
				continue
			}
			rec.KWh[role] = s.KWh[i]
		}
		if len(rec.KWh) > 0 {
			hours = append(hours, rec)
		}
	})
	if err != nil {
		return err
	}
	// The current ISO week is still running, its statistics are incomplete.
	// The season closer writes it once it is over.
	cy, cw := now.In(loc).ISOWeek()
	current := fmt.Sprintf("%d-W%02d", cy, cw)
	for _, w := range autolearn.WeeksFromStats(hours, loc) {
		if w.Key >= current {
			continue
		}
		store.Put(w)
	}
	store.Prune(now)
	return store.Save()
}

// engineGroupsFor sagt je Gruppe der Engine, ob sie rechnen darf. Gewohnheiten
// (habit) und Zuverlaessigkeit (reliability) laufen nur bei eingeschaltetem
// Muster-Lernen und dann jede fuer sich. Der Befundteil der Regelpruefung und
// die Vorschlaege nutzen dieselben Werte.
func engineGroupsFor(value settings.Settings) map[string]bool {
	patterns := !value.AutomationPatternsDisabled
	return map[string]bool{
		"habit":       patterns && !value.AutomationHabitsDisabled,
		"reliability": patterns && !value.AutomationReliabilityDisabled,
		"energy":      patterns && !value.AutomationEnergyDisabled,
	}
}

// anyGroupOn reports whether a group of the engine is switched on.
func anyGroupOn(groups map[string]bool) bool {
	for _, on := range groups {
		if on {
			return true
		}
	}
	return false
}

// registerEnergyDetectors meldet die Erkenner der Gruppe Energie bei der Engine.
// Sie laufen nur, wenn der Schalter Energie an ist. reserve liefert die PV-Reserve
// der letzten Minute, check ist die Selbstpruefung der Prognose des Rest-Ueberschusses.
func registerEnergyDetectors(e *autolearn.Engine, reserve func() (float64, bool, bool), check *autolearn.ForecastCheck) {
	e.Register(autolearn.TemperatureDetector)
	e.Register(autolearn.PVReserveDetector(reserve))
	e.Register(autolearn.LearnCyclesDetector)
	e.Register(autolearn.NewLearnSurplusDetector(check))
}

// learnOutdoor finds the outdoor temperature entity by its unique id among the
// devices, without the dashboard's own device. Its state topic and value
// template come with it. Without a match the zero value is returned.
func learnOutdoor(devices []registry.DeviceView, uniqueID string) autolearn.OutdoorSource {
	uid := strings.TrimSpace(uniqueID)
	if uid == "" {
		return autolearn.OutdoorSource{}
	}
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		for _, e := range dev.Entities {
			if e.UniqueID == uid {
				return autolearn.OutdoorSource{EntityID: uid, Topic: e.StateTopic, ValueTemplate: e.ValueTemplate}
			}
		}
	}
	return autolearn.OutdoorSource{}
}

// registerLearnDetectors meldet die Erkenner der Lernwerte bei der Engine. Sie
// laufen in der Gruppe Gewohnheiten wie die Vorschlaege und liefern Werte statt
// Befunde.
func registerLearnDetectors(e *autolearn.Engine) {
	for _, det := range []autolearn.Detector{
		autolearn.LearnSwitchTimeDetector, autolearn.LearnRuntimeDetector,
		autolearn.LearnDayBoundsDetector, autolearn.LearnHouseStateDetector,
	} {
		e.Register(det)
	}
}

// cycleRunner findet die Zyklen der Geraete in den Minutenrahmen und gibt sie an
// den Tagesspeicher weiter. Sein Zustand liegt in cycles.json, damit ein Neustart
// dort weitermacht. mu schuetzt den Tracker, weil Minute und Save verschiedene
// Goroutinen nutzen.
type cycleRunner struct {
	mu      sync.Mutex
	tracker *autolearn.CycleTracker
	days    *autolearn.DayStore
	origin  func(device string, at time.Time) string
	path    string
}

// newCycleRunner liest den Zustand aus path. Fehlt die Datei, startet er leer.
func newCycleRunner(path string, days *autolearn.DayStore, origin func(device string, at time.Time) string, logf func(string, ...any)) *cycleRunner {
	var state []byte
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		state = data
	case !os.IsNotExist(err):
		logf("autolearn: %s unreadable, starting empty: %v", path, err)
	}
	return &cycleRunner{tracker: autolearn.NewCycleTracker(state), days: days, origin: origin, path: path}
}

// ObserveMinute gibt den Minutenrahmen an den Tracker und die beendeten Zyklen an
// den Tagesspeicher weiter.
func (c *cycleRunner) ObserveMinute(f learnrec.Frame) {
	c.mu.Lock()
	cs := c.tracker.ObserveMinute(f, c.origin)
	c.mu.Unlock()
	if len(cs) > 0 {
		c.days.AddCycles(cs)
	}
}

// Save schreibt den Zustand des Trackers atomar nach cycles.json.
func (c *cycleRunner) Save() error {
	c.mu.Lock()
	data := c.tracker.State()
	c.mu.Unlock()
	if data == nil {
		return fmt.Errorf("cycle state could not be encoded")
	}
	return energystats.WriteFileAtomic(filepath.Dir(c.path), c.path, data)
}

// saverFunc macht eine Funktion zum Saver des Hubs.
type saverFunc func() error

// Save ruft die Funktion auf.
func (f saverFunc) Save() error { return f() }

// dailyShareWindow is how long the engine must have run in this process before
// its load share covers a full day. Before that the window is mostly empty.
const dailyShareWindow = 24 * time.Hour

// dailyShareSaver schreibt den Tagesanteil der Lernlast (Engine.Load, die
// Last der letzten 24 Stunden) in den Lernstand. Der Hub ruft ihn hoechstens
// stuendlich auf. Geschrieben wird nur, wenn eine Gruppe der Engine rechnet
// und sie seit mindestens 24 Stunden in diesem Prozess laeuft, sonst bleibt
// der Stand unveraendert. mu schuetzt st, weil die Herabstufung denselben Stand aendert.
func dailyShareSaver(engine *autolearn.Engine, groups func() map[string]bool, mu *sync.Mutex, st *learnrec.State, path string, now func() time.Time) saverFunc {
	started := now()
	return func() error {
		if !anyGroupOn(groups()) || now().Sub(started) < dailyShareWindow {
			return nil
		}
		_, share, _ := engine.Load()
		mu.Lock()
		defer mu.Unlock()
		*st = learnrec.RecordShare(*st, now(), share)
		return learnrec.SaveState(path, *st)
	}
}

// engineRunner haengt die Automationen-Engine an den Minutentakt des Hubs.
// ObserveMinute laeuft im Hub und damit im MQTT-Pfad, also legt es nur einen
// Tick in einen Kanal der Laenge 1 und kehrt sofort zurueck. Ist der Kanal
// schon voll, faellt der Tick weg, die naechste Minute holt ihn nach. Run
// rechnet in einer eigenen Goroutine.
type engineRunner struct {
	engine      *autolearn.Engine
	groups      func() map[string]bool
	onDowngrade func()
	// afterRound runs after every round, set before Run starts.
	afterRound func()
	ticks      chan time.Time
}

func newEngineRunner(engine *autolearn.Engine, groups func() map[string]bool, onDowngrade func()) *engineRunner {
	return &engineRunner{
		engine:      engine,
		groups:      groups,
		onDowngrade: onDowngrade,
		ticks:       make(chan time.Time, 1),
	}
}

// ObserveMinute gibt den Minutenbeginn an die Engine weiter, ohne zu warten.
func (r *engineRunner) ObserveMinute(f learnrec.Frame) {
	select {
	case r.ticks <- f.Minute:
	default:
	}
}

// Run verarbeitet die Ticks, bis ctx endet. Meldet die Engine eine Ueberlast,
// ruft es onDowngrade auf.
func (r *engineRunner) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-r.ticks:
			r.engine.Tick(at, r.groups())
			if r.engine.Downgrade() {
				r.onDowngrade()
			}
			if r.afterRound != nil {
				r.afterRound()
			}
		}
	}
}

// learnDevices baut die Geraete, die die Erkenner sehen. Der Dienst der
// Automationen selbst fehlt, wie im Hub. Die Energierolle kommt aus dem
// Resolver, also aus den Zuordnungen und der Heuristik der Energieansicht.
func learnDevices(devices []registry.DeviceView, resolver *energy.Resolver) map[string]autolearn.DeviceInfo {
	out := make(map[string]autolearn.DeviceInfo, len(devices))
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		info := autolearn.DeviceInfo{ID: dev.ID, Name: dev.Name, Role: deviceRole(resolver, dev.Entities)}
		for _, e := range dev.Entities {
			info.Entities = append(info.Entities, e.UniqueID)
			if e.Component == "light" {
				info.Light = true
			}
			if isSwitchComponent(e.Component) {
				info.Switchable = true
			}
		}
		sort.Strings(info.Entities)
		out[dev.ID] = info
	}
	return out
}

// deviceRole returns the energy role of a device from its entities. A wallbox
// or heat pump role of any entity wins, otherwise the first role that is not
// the switch role. Without a resolver or a role the result is empty.
func deviceRole(resolver *energy.Resolver, entities []registry.EntityView) energy.Role {
	if resolver == nil {
		return ""
	}
	var role energy.Role
	for _, e := range entities {
		resolved, ok := resolver.Resolve(e)
		if !ok || resolved.Role == "" || resolved.Role == energy.RoleSwitch {
			continue
		}
		if resolved.Role == energy.RoleWallbox || resolved.Role == energy.RoleHeatPump {
			return resolved.Role
		}
		if role == "" {
			role = resolved.Role
		}
	}
	return role
}

// pvSite is the site state the PV tracker reads every minute. It is replaced
// as a whole by pvSiteCache.refresh, so the minute path never reads a file.
type pvSite struct {
	hasLoc   bool
	lat, lon float64
	reg      *autolearn.RegulatorView
}

// pvSiteCache holds the latest pvSite. refresh reads the rule document and
// runs outside the minute path, from the hub's saver call.
type pvSiteCache struct {
	site    atomic.Pointer[pvSite]
	readDoc func() ([]byte, error)
	logf    func(string, ...any)
}

// refresh parses the rule document into a new pvSite. A document that cannot
// be read or parsed keeps the previous site.
func (c *pvSiteCache) refresh() {
	raw, err := c.readDoc()
	if err != nil {
		c.logf("autolearn: pv site: rule document unreadable: %v", err)
		return
	}
	doc, err := autolearn.ParseDoc(raw)
	if err != nil {
		c.logf("autolearn: pv site: rule document invalid: %v", err)
		return
	}
	site := &pvSite{}
	if doc.Location != nil {
		site.hasLoc = true
		site.lat = doc.Location.Lat
		site.lon = doc.Location.Lon
	}
	// The first enabled scenario with a regulator decides. Only a regulator
	// that regulates a balance field (grid_export or net) counts as present.
	for _, sc := range doc.Scenarios {
		if !sc.Enabled || sc.Regulator == nil {
			continue
		}
		target := sc.Regulator.Target
		site.reg = &autolearn.RegulatorView{
			DeadbandW: sc.Regulator.DeadbandW,
			MaxW:      sc.Regulator.MaxW,
			Present:   target == "grid_export" || target == "net",
		}
		break
	}
	c.site.Store(site)
}

// location returns the site coordinates, ok false when none are set.
func (c *pvSiteCache) location() (lat, lon float64, ok bool) {
	site := c.site.Load()
	if site == nil || !site.hasLoc {
		return 0, 0, false
	}
	return site.lat, site.lon, true
}

// regulator returns the regulator state of the latest refresh, nil when none.
func (c *pvSiteCache) regulator() *autolearn.RegulatorView {
	site := c.site.Load()
	if site == nil {
		return nil
	}
	return site.reg
}

// loadPVRaster reads the sky raster from path. A missing or unreadable file
// starts an empty raster, a size change is handled by LoadRaster.
func loadPVRaster(path string, limits learnrec.Limits, logf func(string, ...any)) *autolearn.Raster {
	data, err := os.ReadFile(path)
	if err != nil {
		data = nil
		if !os.IsNotExist(err) {
			logf("autolearn: %s unreadable, starting empty: %v", path, err)
		}
	}
	return autolearn.LoadRaster(data, limits.PVCellAzDeg, limits.PVCellElDeg)
}
