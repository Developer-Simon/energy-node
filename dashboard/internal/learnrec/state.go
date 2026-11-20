package learnrec

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
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
