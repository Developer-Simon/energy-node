package chainsuggest

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// suggestSaveEvery ist der Abstand der Speicherungen, solange etwas offen ist,
// suggestSaveRetry die Wartezeit nach einem fehlgeschlagenen Speichern.
const (
	suggestSaveEvery = time.Hour
	suggestSaveRetry = 5 * time.Minute
)

const suggestFileVersion = 1

// suggestFile ist das Format von energystats/chains/suggest.json.
type suggestFile struct {
	Version   int                       `json:"version"`
	Days      map[string][]suggestEntry `json:"days"`
	Devices   map[string]string         `json:"devices"`
	Dismissed [][2]string               `json:"dismissed"`
}

// suggestEntry ist ein Paar an einem Tag: s = [n, Σa, Σb, Σa², Σb², Σab, Σ|a−b|].
type suggestEntry struct {
	A string    `json:"a"`
	B string    `json:"b"`
	S []float64 `json:"s"`
}

// Load liest die Summen und verworfenen Paare aus path. Eine fehlende Datei
// ergibt einen leeren Sammler. Eine kaputte Datei ergibt ebenfalls einen leeren
// Sammler und genau einen Logeintrag.
func Load(path string, th Thresholds, loc *time.Location, logf func(string, ...any)) *Collector {
	if logf == nil {
		logf = log.Printf
	}
	c := NewCollector(th, loc)
	c.logf = logf
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c
	}
	if err == nil {
		err = c.restore(data)
	}
	if err != nil {
		logf("chainsuggest: %s unreadable, starting empty: %v", path, err)
		c = NewCollector(th, loc)
		c.logf = logf
	}
	return c
}

// restore fuellt den leeren Sammler aus der Datei. Bei einem Fehler bleibt er
// unveraendert, damit die Aufrufer ihn verwerfen koennen.
func (c *Collector) restore(data []byte) error {
	var f suggestFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version != suggestFileVersion {
		return fmt.Errorf("version %d", f.Version)
	}

	totals := map[pairDay]*sums{}
	for dayStr, entries := range f.Days {
		t, err := time.Parse("2006-01-02", dayStr)
		if err != nil {
			return fmt.Errorf("day %q: %w", dayStr, err)
		}
		day := int(t.Unix() / 86400)
		for _, e := range entries {
			if e.A == "" || e.B == "" || e.A == e.B || len(e.S) != 7 {
				return fmt.Errorf("day %s: bad entry %q/%q", dayStr, e.A, e.B)
			}
			key := orderedPair(e.A, e.B)
			totals[pairDay{a: key.a, b: key.b, day: day}] = &sums{
				n: int(e.S[0]), sa: e.S[1], sb: e.S[2], saa: e.S[3], sbb: e.S[4], sab: e.S[5], sad: e.S[6],
			}
		}
	}

	dismissed := map[pairKey]bool{}
	for _, d := range f.Dismissed {
		if d[0] == "" || d[1] == "" || d[0] == d[1] {
			return fmt.Errorf("bad dismissed pair %q/%q", d[0], d[1])
		}
		dismissed[orderedPair(d[0], d[1])] = true
	}

	owner := map[string]string{}
	for id, dev := range f.Devices {
		if id == "" || dev == "" {
			return fmt.Errorf("bad device entry %q=%q", id, dev)
		}
		owner[id] = dev
	}

	c.sums = totals
	c.dismissed = dismissed
	c.owner = owner
	return nil
}

// SetPersistence legt fest, wohin gespeichert wird. Ohne Aufruf wird nichts
// geschrieben. logf nimmt Speicherfehler auf, nil nimmt log.Printf.
func (c *Collector) SetPersistence(path string, logf func(string, ...any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path = path
	if logf != nil {
		c.logf = logf
	}
}

// Close speichert, wenn etwas offen ist. Der Sammler bleibt danach nutzbar.
func (c *Collector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty || c.path == "" {
		return
	}
	if err := c.saveLocked(); err != nil {
		c.logf("chainsuggest: save %s: %v", c.path, err)
	}
}

// Dismiss verwirft ein Paar dauerhaft, die Reihenfolge ist egal. Mit Persistenz
// wird sofort gespeichert. Ein Fehler dabei laesst das Paar trotzdem verworfen,
// der naechste Speichervorgang versucht es erneut.
func (c *Collector) Dismiss(a, b string) error {
	if a == "" || b == "" {
		return errors.New("chainsuggest: dismiss needs two entity ids")
	}
	if a == b {
		return errors.New("chainsuggest: dismiss needs two different entity ids")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	key := orderedPair(a, b)
	if c.dismissed[key] {
		return nil
	}
	c.dismissed[key] = true
	c.dirty = true
	if c.path == "" {
		return nil
	}
	if err := c.saveLocked(); err != nil {
		return fmt.Errorf("chainsuggest: save dismissed pair: %w", err)
	}
	return nil
}

// ThresholdsFromEnv waehlt die Schwellen. ENERGY_NODE_CHAIN_SUGGEST_FAST=1
// ergibt die Demo-Schwellen, jeder andere Wert die Standardschwellen.
func ThresholdsFromEnv(getenv func(string) string) Thresholds {
	if getenv == nil {
		getenv = os.Getenv
	}
	if strings.TrimSpace(getenv("ENERGY_NODE_CHAIN_SUGGEST_FAST")) == "1" {
		return FastThresholds
	}
	return DefaultThresholds
}

// saveDueLocked speichert, wenn etwas offen ist und die Frist abgelaufen war.
// Der erste Aufruf setzt die Frist auf eine Stunde spaeter.
func (c *Collector) saveDueLocked(at time.Time) {
	if c.path == "" {
		return
	}
	if c.next.IsZero() {
		c.next = at.Add(suggestSaveEvery)
	}
	if at.Before(c.next) {
		return
	}
	c.next = at.Add(suggestSaveEvery)
	if !c.dirty {
		return
	}
	if err := c.saveLocked(); err != nil {
		c.logf("chainsuggest: save %s: %v, retry in %s", c.path, err, suggestSaveRetry)
		c.next = at.Add(suggestSaveRetry)
	}
}

// saveLocked rundet die Summen auf drei Stellen, damit Speicher und Datei
// uebereinstimmen, und schreibt die Datei atomar. Die Ausgabe ist sortiert.
func (c *Collector) saveLocked() error {
	f := suggestFile{
		Version:   suggestFileVersion,
		Days:      map[string][]suggestEntry{},
		Devices:   make(map[string]string, len(c.owner)),
		Dismissed: [][2]string{},
	}
	for key, s := range c.sums {
		s.saa = round3(s.saa)
		s.sbb = round3(s.sbb)
		s.sa = round3(s.sa)
		s.sb = round3(s.sb)
		s.sab = round3(s.sab)
		s.sad = round3(s.sad)
		dayStr := time.Unix(int64(key.day)*86400, 0).UTC().Format("2006-01-02")
		f.Days[dayStr] = append(f.Days[dayStr], suggestEntry{
			A: key.a, B: key.b,
			S: []float64{float64(s.n), s.sa, s.sb, s.saa, s.sbb, s.sab, s.sad},
		})
	}
	for _, entries := range f.Days {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].A != entries[j].A {
				return entries[i].A < entries[j].A
			}
			return entries[i].B < entries[j].B
		})
	}
	for id, dev := range c.owner {
		f.Devices[id] = dev
	}
	for key := range c.dismissed {
		f.Dismissed = append(f.Dismissed, [2]string{key.a, key.b})
	}
	sort.Slice(f.Dismissed, func(i, j int) bool {
		if f.Dismissed[i][0] != f.Dismissed[j][0] {
			return f.Dismissed[i][0] < f.Dismissed[j][0]
		}
		return f.Dismissed[i][1] < f.Dismissed[j][1]
	})

	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(c.path), c.path, data); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// orderedPair ordnet die kleinere Entitaets-ID zuerst.
func orderedPair(a, b string) pairKey {
	if b < a {
		a, b = b, a
	}
	return pairKey{a: a, b: b}
}

func round3(x float64) float64 {
	return math.Round(x*1000) / 1000
}
