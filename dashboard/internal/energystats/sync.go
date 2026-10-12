package energystats

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

const (
	statsTier         = "1h"
	dayMs             = int64(24 * time.Hour / time.Millisecond)
	hourMs            = int64(time.Hour / time.Millisecond)
	maxDaysPerRequest = 31
	maxCensusDays     = 3651
	requestTimeout    = 2 * time.Minute
)

// haRoles sind die Rollen, die Go aus Home Assistant annimmt. Dazu kommen
// custom:<id>-Kategorien.
var haRoles = map[string]bool{
	"pv": true, "grid_import": true, "grid_export": true, "battery_charge": true, "battery_discharge": true,
	"load_total": true, "base": true, "wallbox": true, "heat_pump": true,
}

func roleOf(series string) (string, bool) {
	role, ok := strings.CutPrefix(series, "role:")
	if !ok {
		return "", false
	}
	if haRoles[role] {
		return role, true
	}
	if id, custom := strings.CutPrefix(role, "custom:"); custom && id != "" {
		return role, true
	}
	return "", false
}

type SyncStatus struct {
	Connected   bool `json:"connected"`
	Current     bool `json:"current"`
	MissingDays int  `json:"missing_days"`
	// BackfillFrom ist der aelteste Tag, den das Nachladen noch holen will
	// (UTC-Tagesbeginn). nil, wenn nichts fehlt.
	BackfillFrom *time.Time `json:"backfill_from,omitempty"`
}

type syncJob struct {
	series string
	days   []int64
}

type pendingRequest struct {
	id     string
	peer   string
	series string
	days   int
	oldest int64
	sent   time.Time
}

// Sync holt Stunden der Stufe 1h beim Companion ab. Er ist ein
// Pseudo-Peer mit der Kennung historyexchange.StatsPeerID: der Server
// reicht ihm Angebote des Companion weiter und faengt Lieferungen an ihn
// ab. Immer nur eine Nachfrage gleichzeitig, neueste Tage zuerst.
type Sync struct {
	store *Store
	now   func() time.Time
	logf  func(format string, args ...any)

	mu        sync.Mutex
	send      func(to string, payload []byte) bool
	hasPeer   func(id string) bool
	companion string
	offered   map[string]historyexchange.Census
	queue     []syncJob
	// tried merkt je "serie|tag" die angebotene Stundenzahl beim letzten
	// Versuch. Liefert HA weniger als angeboten (Luecke im Recorder), wird
	// der Tag erst wieder angefragt, wenn sich die Zahl aendert.
	tried   map[string]int
	pending *pendingRequest
	seq     int
}

func NewSync(store *Store, now func() time.Time) *Sync {
	return &Sync{store: store, now: now, logf: log.Printf, offered: map[string]historyexchange.Census{}, tried: map[string]int{}}
}

// Attach verbindet Sync mit dem Hub: send stellt eine Nachfrage zu,
// hasPeer prueft, ob der Companion noch verbunden ist.
func (s *Sync) Attach(send func(to string, payload []byte) bool, hasPeer func(id string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.send, s.hasPeer = send, hasPeer
}

func (s *Sync) OnOffer(peer string, coverage map[string]historyexchange.Census) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offered := map[string]historyexchange.Census{}
	for series, c := range coverage {
		if _, ok := roleOf(series); !ok {
			continue
		}
		if c.Step != dayMs || c.From%dayMs != 0 || len(c.N) == 0 || len(c.N) > maxCensusDays {
			continue
		}
		offered[series] = c
	}
	// Neuer Companion-Peer (Neuverbindung, HA neu gestartet): neue
	// Sitzung. Die offene Nachfrage an den alten Peer kommt nie an, und was
	// er nicht liefern konnte, kann der neue vielleicht.
	if peer != s.companion {
		s.pending = nil
		s.tried = map[string]int{}
	}
	s.companion = peer
	s.offered = offered
	s.planLocked()
	s.nextLocked()
}

func (s *Sync) planLocked() {
	s.queue = nil
	names := make([]string, 0, len(s.offered))
	for name := range s.offered {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := s.offered[name]
		role, _ := roleOf(name)
		own, err := s.store.HADays(role, time.UnixMilli(c.From).UTC(), len(c.N))
		if err != nil {
			s.logf("energystats: Abgleich %s nicht moeglich: %v", name, err)
			continue
		}
		var days []int64
		for i := len(c.N) - 1; i >= 0; i-- {
			if c.N[i] <= own[i] {
				continue
			}
			day := c.From + int64(i)*dayMs
			if s.tried[triedKey(name, day)] == c.N[i] {
				continue
			}
			days = append(days, day)
		}
		for len(days) > 0 {
			k := min(maxDaysPerRequest, len(days))
			s.queue = append(s.queue, syncJob{series: name, days: days[:k]})
			days = days[k:]
		}
	}
}

func triedKey(series string, day int64) string { return fmt.Sprintf("%s|%d", series, day) }

func (s *Sync) nextLocked() {
	if s.pending != nil || s.send == nil || len(s.queue) == 0 || s.companion == "" {
		return
	}
	if s.hasPeer != nil && !s.hasPeer(s.companion) {
		s.queue = nil
		return
	}
	job := s.queue[0]
	s.queue = s.queue[1:]
	days := append([]int64(nil), job.days...)
	sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })
	c := s.offered[job.series]
	var spans [][2]int64
	for _, day := range days {
		if n := len(spans); n > 0 && spans[n-1][1] == day {
			spans[n-1][1] = day + dayMs
		} else {
			spans = append(spans, [2]int64{day, day + dayMs})
		}
		s.tried[triedKey(job.series, day)] = c.N[(day-c.From)/dayMs]
	}
	s.seq++
	id := fmt.Sprintf("stats-%d", s.seq)
	payload, err := json.Marshal(map[string]any{
		"peer": historyexchange.StatsPeerID, "req_id": id, "tier": statsTier, "series": job.series, "ranges": spans,
	})
	if err != nil || !s.send(s.companion, payload) {
		s.queue = nil
		return
	}
	s.pending = &pendingRequest{id: id, peer: s.companion, series: job.series, days: len(days), oldest: days[0], sent: s.now()}
}

func (s *Sync) OnDeliver(peer, reqID string, rows []historyexchange.Row, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil || s.pending.id != reqID || s.pending.peer != peer {
		return
	}
	now := s.now()
	records := make([]HARecord, 0, len(rows))
	for _, row := range rows {
		if row.Series != s.pending.series || row.TS%hourMs != 0 {
			continue
		}
		role, ok := roleOf(row.Series)
		hour := time.UnixMilli(row.TS).UTC()
		if !ok || hour.Add(time.Hour).After(now) {
			continue
		}
		if math.IsNaN(row.Avg) || math.IsInf(row.Avg, 0) || row.Avg < 0 {
			continue
		}
		records = append(records, HARecord{Hour: hour, Role: role, KWh: row.Avg / 1000})
	}
	if len(records) > 0 {
		if err := s.store.SetHA(records); err != nil {
			s.logf("energystats: Stunden aus Home Assistant nicht gespeichert: %v", err)
		}
	}
	if final {
		s.pending = nil
		s.nextLocked()
	}
}

// Tick gibt eine haengende Nachfrage nach requestTimeout frei.
func (s *Sync) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil && s.now().Sub(s.pending.sent) > requestTimeout {
		s.pending = nil
		s.nextLocked()
	}
}

func (s *Sync) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st SyncStatus
	oldest := int64(0)
	note := func(day int64) {
		if oldest == 0 || day < oldest {
			oldest = day
		}
	}
	for _, job := range s.queue {
		st.MissingDays += len(job.days)
		for _, day := range job.days {
			note(day)
		}
	}
	if s.pending != nil {
		st.MissingDays += s.pending.days
		note(s.pending.oldest)
	}
	if oldest != 0 {
		from := time.UnixMilli(oldest).UTC()
		st.BackfillFrom = &from
	}
	st.Connected = s.companion != "" && s.hasPeer != nil && s.hasPeer(s.companion)
	if !st.Connected || len(s.offered) == 0 {
		return st
	}
	last := s.now().UTC().Truncate(time.Hour).Add(-time.Hour)
	st.Current = true
	for series := range s.offered {
		role, _ := roleOf(series)
		latest, _ := s.store.SourceAt(role, last)
		before, _ := s.store.SourceAt(role, last.Add(-time.Hour))
		if latest != SourceHA && before != SourceHA {
			st.Current = false
			break
		}
	}
	return st
}
