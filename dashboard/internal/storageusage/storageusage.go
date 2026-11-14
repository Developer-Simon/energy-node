// Package storageusage misst, wie viel Platz die Systemplatte für Installation,
// Laufzeitdaten und Backups belegt. Die Messung läuft über den Dateibaum und
// folgt keinen Symlinks, damit ein eingehängtes Zusatzlaufwerk nicht mitgezählt
// wird.
package storageusage

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Sources nennt die Ordner der Systemplatte je Teil.
type Sources struct {
	Ref     string // Pfad auf der Systemplatte, bestimmt das Gerät
	Install []string
	Runtime []string
	Backups []string
	Exclude []string // Ordner, die nie zu Install zählen
}

// Parts ist der belegte Platz je Teil in Byte.
type Parts struct{ Install, Runtime, Backups int64 }

// Measure misst die Teile der Systemplatte. Fehlt Ref, gibt Parts{} zurück.
func Measure(src Sources) Parts {
	if src.Ref == "" {
		return Parts{}
	}
	info, err := os.Stat(src.Ref)
	if err != nil {
		return Parts{}
	}
	dev, ok := deviceOf(info)
	if !ok {
		return Parts{}
	}
	sameDev := func(fi fs.FileInfo) bool {
		d, ok := deviceOf(fi)
		return ok && d == dev
	}
	return measure(src, sameDev)
}

// measure zählt die Teile mit einer eigenen Prüfung des Geräts. Nur Install
// bekommt die Ausschlussliste, denn Runtime und Backups liegen oft genau in
// einem ausgeschlossenen Ordner.
func measure(src Sources, sameDev func(fs.FileInfo) bool) Parts {
	return Parts{
		Install: sumTree(src.Install, src.Exclude, sameDev),
		Runtime: sumTree(src.Runtime, nil, sameDev),
		Backups: sumTree(src.Backups, nil, sameDev),
	}
}

// sumTree addiert den belegten Platz aller regulären Dateien unter roots.
// Ordner aus exclude und Ordner von einem anderen Gerät werden übersprungen.
// Symlinks werden nicht verfolgt, Lesefehler überspringen nur den Eintrag.
func sumTree(roots, exclude []string, sameDev func(fs.FileInfo) bool) int64 {
	skip := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		skip[filepath.Clean(e)] = true
	}

	var total int64
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				if skip[filepath.Clean(path)] {
					return fs.SkipDir
				}
				fi, err := d.Info()
				if err != nil || !sameDev(fi) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			total += allocatedBytes(fi)
			return nil
		})
	}
	return total
}

// allocatedBytes liefert den tatsächlich belegten Platz einer Datei in Byte.
func allocatedBytes(fi fs.FileInfo) int64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return int64(st.Blocks) * 512
}

// deviceOf liefert die Gerätenummer eines Eintrags.
func deviceOf(fi fs.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

// Meter hält den letzten Messwert zwischen und startet Messungen im
// Hintergrund. Der Wert gilt nur für den key, unter dem er gemessen wurde.
type Meter struct {
	measure func() Parts
	ttl     time.Duration
	now     func() time.Time
	spawn   func(func())

	mu         sync.Mutex
	has        bool
	key        string
	parts      Parts
	at         time.Time
	running    bool
	runningKey string
}

// NewMeter legt einen Meter an, der measure höchstens alle ttl neu aufruft.
func NewMeter(measure func() Parts, ttl time.Duration) *Meter {
	return &Meter{
		measure: measure,
		ttl:     ttl,
		now:     time.Now,
		spawn:   func(f func()) { go f() },
	}
}

// Get liefert den Messwert für key und ob es einen gibt. Fehlt er oder ist er
// älter als ttl, startet Get eine Messung im Hintergrund, höchstens eine zur
// selben Zeit. Ein veralteter Wert desselben key wird bis dahin weiter
// geliefert, ein Wert eines anderen key nie.
func (m *Meter) Get(key string) (Parts, bool) {
	m.mu.Lock()
	served := m.has && m.key == key
	fresh := served && m.now().Sub(m.at) <= m.ttl
	var start bool
	if !m.running && !fresh {
		start = true
		m.running = true
		m.runningKey = key
	}
	p := m.parts
	m.mu.Unlock()

	// spawn läuft außerhalb der Sperre, damit eine synchrone Ausführung
	// nicht blockiert.
	if start {
		m.spawn(func() {
			result := m.measure()
			m.mu.Lock()
			defer m.mu.Unlock()
			m.running = false
			m.has = true
			m.key = key
			m.parts = result
			m.at = m.now()
		})
	}
	return p, served
}
