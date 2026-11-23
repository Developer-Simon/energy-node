package autolearn

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// PVTracker collects the PV power of unthrottled minutes into the sky raster.
// ObserveMinute runs on the minute hub and does no file access. Save writes the
// raster atomically to its path.
type PVTracker struct {
	mu              sync.Mutex
	raster          *Raster
	location        func() (lat, lon float64, ok bool)
	regulator       func() *RegulatorView
	day             func() string
	path            string
	pruned          string
	keepDays        func() int
	lastUnthrottled bool
}

// NewPVTracker creates a tracker over raster. location reports the site
// coordinates, ok false means no collection. regulator returns the regulator
// state of the minute, nil means no regulator. day returns today as
// 2006-01-02. path is the file Save writes to.
func NewPVTracker(raster *Raster, location func() (lat, lon float64, ok bool), regulator func() *RegulatorView, day func() string, path string) *PVTracker {
	return &PVTracker{raster: raster, location: location, regulator: regulator, day: day, path: path}
}

// SetKeepDays sets how many days the raster keeps. It is read once a day when
// the raster is pruned. Without it the raster keeps pvRasterDays.
func (t *PVTracker) SetKeepDays(fn func() int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keepDays = fn
}

// ObserveMinute adds the PV power of an unthrottled minute at the sun position
// of the minute. It prunes the raster once per day.
func (t *PVTracker) ObserveMinute(f learnrec.Frame) {
	t.mu.Lock()
	defer t.mu.Unlock()
	day := t.day()
	if day != "" && day != t.pruned {
		if t.keepDays != nil {
			t.raster.PruneDays(day, t.keepDays())
		} else {
			t.raster.Prune(day)
		}
		t.pruned = day
	}
	var reg *RegulatorView
	if t.regulator != nil {
		reg = t.regulator()
	}
	t.lastUnthrottled = Unthrottled(f, reg)
	if !t.lastUnthrottled || f.Balance["pv"] <= 0 || t.location == nil {
		return
	}
	lat, lon, ok := t.location()
	if !ok {
		return
	}
	az, el := sun.Position(f.Minute, lat, lon)
	t.raster.Add(az, el, f.Balance["pv"], day)
}

// Raster returns the raster the tracker writes to. Callers must not use it
// while the hub runs ObserveMinute, except through Save or in tests.
func (t *PVTracker) Raster() *Raster {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.raster
}

// BestAt returns the expected PV power at az/el of model on the live raster.
// It takes the tracker mutex, so it must not be called from a path that holds
// it. The raster is not copied.
func (t *PVTracker) BestAt(model PVModel, az, el float64) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return model.Best(az, el, t.raster)
}

// LastUnthrottled reports whether the last observed minute was unthrottled.
func (t *PVTracker) LastUnthrottled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastUnthrottled
}

// Save writes the raster state atomically: a temporary file in the same
// directory, then a rename over the target.
func (t *PVTracker) Save() error {
	t.mu.Lock()
	data := t.raster.State()
	t.mu.Unlock()
	if data == nil {
		return fmt.Errorf("autolearn: pv raster could not be encoded")
	}
	if t.path == "" {
		return nil
	}
	dir := filepath.Dir(t.path)
	tmp, err := os.CreateTemp(dir, ".pvraster-*.tmp")
	if err != nil {
		return fmt.Errorf("autolearn: save pv raster: %w", err)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmpName, t.path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("autolearn: save pv raster: %w", err)
	}
	return nil
}

// Reset leert das Himmelsraster und loescht die Datei. Ein erneutes Save schreibt
// den leeren Raster.
func (t *PVTracker) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.raster.clear()
	t.lastUnthrottled = false
	return removeIfExists(t.path)
}

// removeIfExists loescht path. Eine fehlende Datei ist kein Fehler, ein leerer
// Pfad heisst: keine Datei.
func removeIfExists(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("autolearn: reset %s: %w", path, err)
	}
	return nil
}
