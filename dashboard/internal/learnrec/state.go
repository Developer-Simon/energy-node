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
type State struct {
	FirstStart int64 `json:"first_start"`
	Fresh      bool  `json:"fresh"`
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
