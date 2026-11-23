package autolearn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PVModelStore holds the fitted roof model and keeps it in a JSON file. The
// nightly detector writes it, the minute path never reads it. A missing or
// unreadable file starts empty. Save writes only when the model changed since
// the last save.
type PVModelStore struct {
	mu    sync.Mutex
	path  string
	model PVModel
	ok    bool
	dirty bool
}

// NewPVModelStore reads the model from path. A missing file gives an empty
// store, an invalid file is ignored and the store starts empty.
func NewPVModelStore(path string) *PVModelStore {
	s := &PVModelStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var m PVModel
	if err := json.Unmarshal(data, &m); err != nil {
		return s
	}
	s.model = m
	s.ok = true
	return s
}

// Model returns the stored model and whether one exists.
func (s *PVModelStore) Model() (PVModel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.model, s.ok
}

// Set replaces the stored model. The next Save writes it.
func (s *PVModelStore) Set(m PVModel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = m
	s.ok = true
	s.dirty = true
}

// Save writes the model atomically: a temporary file in the same directory,
// then a rename over the target. Nothing is written when the model did not
// change since the last save.
func (s *PVModelStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || !s.ok {
		return nil
	}
	data, err := json.Marshal(s.model)
	if err != nil {
		return fmt.Errorf("autolearn: pv model could not be encoded: %w", err)
	}
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".pvmodel-*.tmp")
	if err != nil {
		return fmt.Errorf("autolearn: save pv model: %w", err)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmpName, s.path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("autolearn: save pv model: %w", err)
	}
	s.dirty = false
	return nil
}

// NewPVModelDetector returns the nightly detector that refits the roof model
// from the sky raster. raster is called once per run and must return a copy
// that the fit may read freely. A nil raster or a failed fit leaves the stored
// model untouched. The detector returns no candidates.
func NewPVModelDetector(raster func() *Raster, store *PVModelStore) Detector {
	return Detector{
		Name:      "pv_model",
		Group:     "energy",
		Light:     false,
		PerDevice: false,
		Run: func(ctx context.Context, in *Input, _ string) []Candidate {
			if ctx.Err() != nil {
				return nil
			}
			r := raster()
			if r == nil {
				return nil
			}
			m, ok := FitPV(r, in.Limits.GeoStepDeg, in.Limits.GeoSurfaces)
			if !ok {
				return nil
			}
			m.FittedAt = in.Now.Unix()
			store.Set(m)
			return nil
		},
	}
}

// Snapshot returns a deep copy of the raster for the detectors. Unlike Raster
// it is safe to call while the minute hub runs ObserveMinute.
func (t *PVTracker) Snapshot() *Raster {
	t.mu.Lock()
	defer t.mu.Unlock()
	return LoadRaster(t.raster.State(), t.raster.azDeg, t.raster.elDeg)
}

// Reset verwirft das Modell und loescht die Datei. Ohne Modell schreibt Save
// nichts, eine fehlende Datei ist der leere Stand.
func (s *PVModelStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = PVModel{}
	s.ok = false
	s.dirty = false
	return removeIfExists(s.path)
}
