package learnrec

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

const (
	haTierMinute = "1m"
	haTierFive   = "5m"
	// haMinuteWindow ist die Spanne, die der Companion in 1m vorhaelt.
	haMinuteWindow = 7 * 24 * time.Hour
	// haFiveWindow ist die Spanne fuer 5m. Aeltere Tage holt der Abgleich nicht.
	haFiveWindow = 30 * 24 * time.Hour
	// haOfferTTL ist, wie lange ein Angebot fuer Fresh1m gilt.
	haOfferTTL = 10 * time.Minute
	// haLag ist der Abstand, den die 1m-Deckung bis jetzt haben muss.
	haLag = 5 * time.Minute
	// haRequestTimeout gibt eine haengende Nachfrage nach dieser Zeit frei.
	haRequestTimeout = 2 * time.Minute
	// haDayBytes ist der Platz eines Schluessels je Tag im RAM.
	haDayBytes = minutesPerDay * 4
	// haMaxBuckets begrenzt ein Deckungsraster aus dem Angebot.
	haMaxBuckets = 100000
)

// haJob ist eine geplante Nachfrage fuer einen Schluessel und einen Tag.
type haJob struct {
	key    string
	series string
	day    string
	tier   string
	start  time.Time
	end    time.Time
}

// haPending ist die Nachfrage, auf deren Antwort der Abgleich wartet.
type haPending struct {
	id   string
	peer string
	job  haJob
	sent time.Time
}

// HASync holt fehlende Minuten- und 5-Minuten-Werte fuer die Lernfunktionen
// beim Companion ab. Er ist ein Pseudo-Peer mit der Kennung
// historyexchange.LearnPeerID. Die Antworten liegen nur im RAM, nie auf
// einem Laufwerk. Immer nur eine Nachfrage gleichzeitig.
type HASync struct {
	now    func() time.Time
	limits func() Limits
	loc    *time.Location

	mu        sync.Mutex
	send      func(to string, payload []byte) bool
	hasPeer   func(id string) bool
	companion string
	offered   map[string]map[string]historyexchange.Census // Stufe -> Serie
	lastOffer time.Time
	queue     []haJob
	pending   *haPending
	deadline  time.Time
	seq       int
	days      map[string]map[string]*[minutesPerDay]float32
	fiveMin   map[string]map[string]bool
}

// NewHASync legt den Abgleich an. loc bestimmt die lokalen Tage, wie beim
// MinuteStore. Ohne loc gilt time.Local.
func NewHASync(now func() time.Time, limits func() Limits, loc *time.Location) *HASync {
	if loc == nil {
		loc = time.Local
	}
	return &HASync{
		now:     now,
		limits:  limits,
		loc:     loc,
		offered: map[string]map[string]historyexchange.Census{},
		days:    map[string]map[string]*[minutesPerDay]float32{},
		fiveMin: map[string]map[string]bool{},
	}
}

// Attach verbindet den Abgleich mit dem Hub, wie energystats.Sync.
func (s *HASync) Attach(send func(to string, payload []byte) bool, hasPeer func(id string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.send, s.hasPeer = send, hasPeer
}

// OnOffer nimmt das Angebot des Companion an. coverage ist Stufe -> Serie ->
// Deckung. Andere Stufen als 1m und 5m bleiben weg.
func (s *HASync) OnOffer(peer string, coverage map[string]map[string]historyexchange.Census) {
	s.mu.Lock()
	defer s.mu.Unlock()
	offered := map[string]map[string]historyexchange.Census{}
	for tier, series := range coverage {
		if tier != haTierMinute && tier != haTierFive {
			continue
		}
		for name, c := range series {
			if c.Step <= 0 || len(c.N) == 0 || len(c.N) > haMaxBuckets {
				continue
			}
			if offered[tier] == nil {
				offered[tier] = map[string]historyexchange.Census{}
			}
			offered[tier][name] = c
		}
	}
	// Neuer Companion: die Nachfrage an den alten kommt nicht mehr an.
	if peer != s.companion {
		s.pending = nil
	}
	s.companion = peer
	s.offered = offered
	s.lastOffer = s.now()
	s.nextLocked()
}

// Prefetch plant Nachfragen fuer keys (Lern-Schluessel wie e:<id> oder b:pv)
// an den Tagen days (lokal, 2006-01-02). Die juengsten Tage kommen zuerst,
// und es wird nur so viel geplant, wie RAMMinuteBytes/2 erlaubt. Antworten
// nach deadline werden verworfen. Ein neuer Aufruf ersetzt den alten Lauf.
func (s *HASync) Prefetch(keys []string, days []string, deadline time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = nil
	s.pending = nil
	s.days = map[string]map[string]*[minutesPerDay]float32{}
	s.fiveMin = map[string]map[string]bool{}
	s.deadline = deadline

	now := s.now()
	budget := s.limits().RAMMinuteBytes / 2
	sorted := append([]string(nil), days...)
	sort.Sort(sort.Reverse(sort.StringSlice(sorted)))

	var spent int64
	for _, day := range sorted {
		start, err := time.ParseInLocation(minuteDayLayout, day, s.loc)
		if err != nil {
			continue
		}
		end := start.AddDate(0, 0, 1)
		var tier string
		switch age := now.Sub(start); {
		case age < haMinuteWindow:
			tier = haTierMinute
		case age < haFiveWindow:
			tier = haTierFive
		default:
			continue
		}
		var jobs []haJob
		for _, key := range keys {
			series, ok := haSeriesFor(key)
			if !ok || !s.coveredLocked(tier, series, start, end) {
				continue
			}
			jobs = append(jobs, haJob{key: key, series: series, day: day, tier: tier, start: start, end: end})
		}
		if len(jobs) == 0 {
			continue
		}
		cost := int64(len(jobs)) * haDayBytes
		if spent+cost > budget {
			break
		}
		spent += cost
		s.queue = append(s.queue, jobs...)
	}
	s.nextLocked()
}

// OnDeliver verarbeitet eine Lieferung des Companion. Antworten auf eine
// andere oder keine Nachfrage, mit anderer Stufe, und Antworten nach der
// Frist, werden verworfen.
func (s *HASync) OnDeliver(peer, reqID, tier string, rows []historyexchange.Row, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending
	if p == nil || p.id != reqID || p.peer != peer || p.job.tier != tier {
		return
	}
	if s.deadline.IsZero() || !s.now().After(s.deadline) {
		s.storeLocked(p.job, rows)
	}
	if final {
		s.pending = nil
		s.nextLocked()
	}
}

// Day liefert die Werte eines Tages als Kopien, je Lern-Schluessel. Luecken
// sind NaN. fiveMin markiert je Schluessel, dass Werte aus 5m stammen.
func (s *HASync) Day(day string) (map[string]*[minutesPerDay]float32, map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := map[string]*[minutesPerDay]float32{}
	for key, arr := range s.days[day] {
		c := *arr
		values[key] = &c
	}
	fiveMin := map[string]bool{}
	for key, ok := range s.fiveMin[day] {
		fiveMin[key] = ok
	}
	return values, fiveMin
}

// Days zaehlt die Tage, zu denen der Zwischenspeicher Werte hat.
func (s *HASync) Days() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.days)
}

// WaitIdle blockiert, bis keine Nachfrage mehr offen ist und die Queue leer
// ist. Liefert true bei Leerlauf, false wenn ctx vorher endet.
func (s *HASync) WaitIdle(ctx context.Context) bool {
	for {
		s.mu.Lock()
		idle := s.pending == nil && len(s.queue) == 0
		s.mu.Unlock()
		if idle {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// keyMinutes liefert die Minuten eines Schluessels an einem Tag als Kopie und
// ob die Werte aus 5m stammen. Ohne Werte liefert es nil.
func (s *HASync) keyMinutes(day, key string) (*[minutesPerDay]float32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arr := s.days[day][key]
	if arr == nil {
		return nil, false
	}
	c := *arr
	return &c, s.fiveMin[day][key]
}

// keysOf nennt die Schluessel mit Werten an einem Tag.
func (s *HASync) keysOf(day string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.days[day]))
	for key := range s.days[day] {
		out = append(out, key)
	}
	return out
}

// Fresh1m meldet, ob der Companion innerhalb der letzten 10 Minuten eine
// 1m-Deckung angeboten hat, die bis hoechstens 5 Minuten vor now reicht und
// dort Saetze hat. Die eigene Aufzeichnung pausiert nur dann.
func (s *HASync) Fresh1m(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastOffer.IsZero() || now.Sub(s.lastOffer) > haOfferTTL {
		return false
	}
	if s.hasPeer != nil && !s.hasPeer(s.companion) {
		return false
	}
	probe := now.Add(-haLag)
	for _, c := range s.offered[haTierMinute] {
		end := c.From + int64(len(c.N))*c.Step
		if end < probe.UnixMilli() || probe.UnixMilli() < c.From {
			continue
		}
		idx := (probe.UnixMilli() - c.From) / c.Step
		if idx < int64(len(c.N)) && c.N[idx] > 0 {
			return true
		}
	}
	return false
}

// Tick gibt eine haengende Nachfrage nach haRequestTimeout frei.
func (s *HASync) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil && s.now().Sub(s.pending.sent) > haRequestTimeout {
		s.pending = nil
		s.nextLocked()
	}
}

// Drop leert den Zwischenspeicher nach dem nächtlichen Lauf. Offene
// Nachfragen werden vergessen, ihre Antworten verworfen.
func (s *HASync) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = nil
	s.pending = nil
	s.deadline = time.Time{}
	s.days = map[string]map[string]*[minutesPerDay]float32{}
	s.fiveMin = map[string]map[string]bool{}
}

// Reset verwirft wie Drop den Zwischenspeicher, offene Nachfragen und Warteschlange,
// dazu die angebotenen Serien. Der Abgleich bleibt angemeldet und fragt neu an.
func (s *HASync) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offered = map[string]map[string]historyexchange.Census{}
	s.queue = nil
	s.pending = nil
	s.deadline = time.Time{}
	s.days = map[string]map[string]*[minutesPerDay]float32{}
	s.fiveMin = map[string]map[string]bool{}
	return nil
}

// HASeriesKey meldet, ob der Companion einen Lern-Schluessel liefern kann
// (e:<id>, b:pv, b:battery_soc).
func HASeriesKey(key string) bool {
	_, ok := haSeriesFor(key)
	return ok
}

// Offered meldet, ob der Companion Serien angeboten hat, die der Abgleich
// nutzen kann.
func (s *HASync) Offered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.offered) > 0
}

// haSeriesFor uebersetzt einen Lern-Schluessel in den Serienamen des
// Companion. Nur e:<id>, b:pv und b:battery_soc sind belegt; Netz, Last und
// Speicher bleiben offen, bis Vorzeichen und Einheit geprueft sind.
func haSeriesFor(key string) (string, bool) {
	if id, ok := strings.CutPrefix(key, "e:"); ok && id != "" {
		return id, true
	}
	switch key {
	case "b:pv":
		return "role:pv", true
	case "b:battery_soc":
		return "role:battery_soc", true
	}
	return "", false
}

// coveredLocked prueft, ob die Stufe der Serie im Bereich [from, to) Saetze hat.
func (s *HASync) coveredLocked(tier, series string, from, to time.Time) bool {
	c, ok := s.offered[tier][series]
	if !ok {
		return false
	}
	for i, n := range c.N {
		if n <= 0 {
			continue
		}
		b0 := c.From + int64(i)*c.Step
		b1 := b0 + c.Step
		if b0 < to.UnixMilli() && b1 > from.UnixMilli() {
			return true
		}
	}
	return false
}

// nextLocked sendet die naechste geplante Nachfrage, wenn keine offen ist.
func (s *HASync) nextLocked() {
	if s.pending != nil || s.send == nil || len(s.queue) == 0 || s.companion == "" {
		return
	}
	if s.hasPeer != nil && !s.hasPeer(s.companion) {
		s.queue = nil
		return
	}
	now := s.now()
	if !s.deadline.IsZero() && now.After(s.deadline) {
		s.queue = nil
		return
	}
	job := s.queue[0]
	s.queue = s.queue[1:]
	s.seq++
	id := fmt.Sprintf("learn-%d", s.seq)
	payload, err := json.Marshal(map[string]any{
		"peer":   historyexchange.LearnPeerID,
		"req_id": id,
		"tier":   job.tier,
		"series": job.series,
		"ranges": [][2]int64{{job.start.UnixMilli(), job.end.UnixMilli()}},
	})
	if err != nil || !s.send(s.companion, payload) {
		s.queue = nil
		return
	}
	s.pending = &haPending{id: id, peer: s.companion, job: job, sent: now}
}

// storeLocked legt die Zeilen einer Lieferung in den Tag. 1m-Zeilen gelten
// fuer eine Minute, 5m-Zeilen werden auf fuenf Minuten verteilt. Die Minute
// liegt im lokalen Kalender, wie beim MinuteStore.
func (s *HASync) storeLocked(job haJob, rows []historyexchange.Row) {
	span := 1
	if job.tier == haTierFive {
		span = 5
	}
	for _, row := range rows {
		if row.Series != job.series || math.IsNaN(row.Avg) || math.IsInf(row.Avg, 0) {
			continue
		}
		at := time.UnixMilli(row.TS).In(s.loc)
		if at.Before(job.start) || !at.Before(job.end) {
			continue
		}
		minute := at.Hour()*60 + at.Minute()
		if s.days[job.day] == nil {
			s.days[job.day] = map[string]*[minutesPerDay]float32{}
			s.fiveMin[job.day] = map[string]bool{}
		}
		arr := s.days[job.day][job.key]
		if arr == nil {
			arr = new([minutesPerDay]float32)
			for i := range arr {
				arr[i] = float32(math.NaN())
			}
			s.days[job.day][job.key] = arr
		}
		for i := minute; i < minute+span && i < minutesPerDay; i++ {
			arr[i] = float32(row.Avg)
		}
		if span == 5 {
			s.fiveMin[job.day][job.key] = true
		}
	}
}
