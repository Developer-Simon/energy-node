package learnrec

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

// State ist der Lernstand ueber Neustarts hinweg. FirstStart ist der Unixzeit
// des ersten Starts, Fresh gilt, wenn vor diesem Start nichts gelernt wurde.
//
// AutoLevel ist die Stufe, auf die eine Herabstufung die automatische Wahl
// gedrueckt hat. Leer heisst keine Herabstufung. Siehe AutoLevel.
type State struct {
	FirstStart int64 `json:"first_start"`
	Fresh      bool  `json:"fresh"`
	AutoLevel  Level `json:"auto_level,omitempty"`
	// DailyShare ist der Anteil der Lernlast je Kalendertag (YYYY-MM-DD), die
	// letzten shareDays Tage. Fehlt das Feld, startet die Liste leer.
	DailyShare map[string]float64 `json:"daily_share,omitempty"`
}

// LowerAutoLevel rueckt die automatisch gewaehlte Stufe um eine nach unten
// (full auf balanced, balanced auf eco) und speichert das in path. Auf eco
// passiert nichts, dann wird auch nichts geschrieben. Der Rueckgabewert ist
// der neue Stand, auch wenn das Speichern fehlschlaegt: die Herabstufung gilt
// dann bis zum Neustart.
func LowerAutoLevel(path string, st State, cores int) (State, error) {
	current := ResolveLevel(autoSetting, cores, st.AutoLevel)
	lowered := lowerLevel(current)
	if lowered == current {
		return st, nil
	}
	st.AutoLevel = lowered
	return st, writeState(path, st)
}

// LoadState liest den Stand aus path. Fehlt die Datei, wird sie mit firstStart
// und Fresh = !hadLearning angelegt und danach nie mehr geaendert. Eine
// unlesbare oder kaputte Datei wird geloggt und bleibt unveraendert. Dann gilt
// der Stand aus den Argumenten.
func LoadState(path string, firstStart time.Time, hadLearning bool, logf func(string, ...any)) State {
	if logf == nil {
		logf = log.Printf
	}
	computed := State{FirstStart: firstStart.Unix(), Fresh: !hadLearning}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := writeState(path, computed); err != nil {
			logf("learnrec: write %s: %v", path, err)
		}
		return computed
	}
	if err != nil {
		logf("learnrec: read %s: %v", path, err)
		return computed
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		logf("learnrec: %s is damaged, using the current state: %v", path, err)
		return computed
	}
	return st
}

func writeState(path string, st State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return energystats.WriteFileAtomic(filepath.Dir(path), path, data)
}

// shareDays ist, wie viele Tage Tagesanteile der Lernlast der Stand behaelt.
const shareDays = 14

// dayLayout ist das Datumsformat der Schluessel in DailyShare.
const dayLayout = "2006-01-02"

// RecordShare setzt den Tagesanteil fuer den Tag von now und wirft Tage weg,
// die aelter als die letzten shareDays sind. Der Rueckgabewert ist der neue
// Stand, persistiert wird er nicht.
func RecordShare(st State, now time.Time, share float64) State {
	shares := make(map[string]float64, shareDays)
	for day, s := range st.DailyShare {
		shares[day] = s
	}
	shares[now.Format(dayLayout)] = share
	if len(shares) > shareDays {
		days := make([]string, 0, len(shares))
		for day := range shares {
			days = append(days, day)
		}
		sort.Strings(days)
		for _, day := range days[:len(days)-shareDays] {
			delete(shares, day)
		}
	}
	st.DailyShare = shares
	return st
}

// SaveState schreibt den Stand nach path. Fuer Aufrufer ausserhalb des Pakets,
// etwa den Saver des Hubs.
func SaveState(path string, st State) error {
	return writeState(path, st)
}
