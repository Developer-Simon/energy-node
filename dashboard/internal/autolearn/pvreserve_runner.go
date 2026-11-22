package autolearn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// PVReserveRunner turns each minute into the PV reserve and the weather factor
// k. It runs after the PVTracker on the minute hub, so the tracker has already
// observed the same minute. It owns the Weather and saves it to pvweather.json.
// ObserveMinute does no file access. Current is read by the detector.
type PVReserveRunner struct {
	mu       sync.Mutex
	tracker  *PVTracker
	models   *PVModelStore
	location func() (lat, lon float64, ok bool)
	weather  *Weather
	path     string
	saved    []byte
	have     bool
	w        float64
	est      bool
}

// NewPVReserveRunner creates the runner. location reports the site coordinates,
// ok false means no reserve. path is the file Save writes to, empty means no
// file.
func NewPVReserveRunner(tracker *PVTracker, models *PVModelStore, location func() (lat, lon float64, ok bool), weather *Weather, path string) *PVReserveRunner {
	return &PVReserveRunner{tracker: tracker, models: models, location: location, weather: weather, path: path}
}

// LoadPVWeather reads the weather state from path. A missing or unreadable file
// starts empty and is logged when it is not missing.
func LoadPVWeather(path string, logf func(string, ...any)) *Weather {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) && logf != nil {
			logf("autolearn: %s unreadable, starting empty: %v", path, err)
		}
		return LoadWeather(WeatherState{})
	}
	var st WeatherState
	if err := json.Unmarshal(data, &st); err != nil {
		if logf != nil {
			logf("autolearn: %s invalid, starting empty: %v", path, err)
		}
		return LoadWeather(WeatherState{})
	}
	return LoadWeather(st)
}

// ObserveMinute computes the best PV power of the minute from the model and the
// live raster, feeds the weather factor and writes k into the frame balance
// under "k" for the day store. Without model, location or PV power it computes
// nothing and Current reports not ok.
func (r *PVReserveRunner) ObserveMinute(f learnrec.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.have = false
	pv := f.Balance["pv"]
	model, ok := r.models.Model()
	if !ok || r.location == nil || pv <= 0 {
		return
	}
	lat, lon, ok := r.location()
	if !ok {
		return
	}
	az, el := sun.Position(f.Minute, lat, lon)
	best := r.tracker.BestAt(model, az, el)
	r.weather.Observe(f.Minute, pv, best, r.tracker.LastUnthrottled())
	k, kLow, fresh := r.weather.K(f.Minute)
	r.w, r.est = Reserve(best, pv, kLow, fresh)
	r.have = true
	if f.Balance != nil {
		f.Balance["k"] = k
	}
}

// Current returns the reserve of the last minute, whether it is an estimate, and
// whether there is one. ok is false without model, location or before the first
// minute.
func (r *PVReserveRunner) Current() (w float64, est bool, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.w, r.est, r.have
}

// Save writes the weather state atomically, but only when it changed since the
// last save.
func (r *PVReserveRunner) Save() error {
	r.mu.Lock()
	data, err := json.Marshal(r.weather.State())
	same := bytes.Equal(data, r.saved)
	path := r.path
	r.mu.Unlock()
	if err != nil {
		return fmt.Errorf("autolearn: pv weather could not be encoded: %w", err)
	}
	if same || path == "" {
		return nil
	}
	if err := writePVWeatherFile(path, data); err != nil {
		return err
	}
	r.mu.Lock()
	r.saved = data
	r.mu.Unlock()
	return nil
}

// writePVWeatherFile writes data to a temporary file in the directory of path,
// then renames it over path.
func writePVWeatherFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pvweather-*.tmp")
	if err != nil {
		return fmt.Errorf("autolearn: save pv weather: %w", err)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("autolearn: save pv weather: %w", err)
	}
	return nil
}

// PVReserveDetector publishes the PV reserve as the learned value
// site/pv_reserve_w in W, rounded to 50 W. current is the runner's Current. The
// detector returns one learned value and no finding.
func PVReserveDetector(current func() (float64, bool, bool)) Detector {
	return Detector{
		Name:      "learn_pv_reserve",
		Group:     "energy",
		Light:     true,
		PerDevice: false,
		Run: func(ctx context.Context, _ *Input, _ string) []Candidate {
			if ctx.Err() != nil {
				return nil
			}
			w, est, ok := current()
			if !ok {
				return nil
			}
			return []Candidate{{
				Detector: "learn_pv_reserve",
				NEff:     1,
				MinNEff:  1,
				Value: &Value{Key: "site/pv_reserve_w", Kind: ValueNumber, Unit: "W", Group: "energy",
					Num: math.Round(w/pvReserveStepW) * pvReserveStepW, N: 1, Estimate: est},
			}}
		},
	}
}

// pvReserveStepW is the rounding step of the published PV reserve in W.
const pvReserveStepW = 50.0
