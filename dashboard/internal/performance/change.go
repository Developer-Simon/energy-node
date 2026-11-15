package performance

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

// changeFile is the only file the performance package persists.
const changeFile = "last-change.json"

// checkWindow is how long an applied change runs before its effect is judged.
const checkWindow = 15 * time.Minute

// Change records the last applied polling rate change and its effect check.
type Change struct {
	At        time.Time         `json:"at"`
	Before    map[RowID]float64 `json:"before"`
	After     map[RowID]float64 `json:"after"`
	Expected  float64           `json:"expected_pct"`
	Available float64           `json:"available_pct"`
	State     string            `json:"state"` // running | ok | bad | undone
	Measured  *float64          `json:"measured_pct,omitempty"`
	Restarted bool              `json:"restarted,omitempty"`
}

// RollbackRequest is the body of a rollback call.
type RollbackRequest struct {
	OnlyUnchanged bool `json:"only_unchanged"`
}

// LoadChange reads <dir>/performance/last-change.json. A missing file yields nil.
func LoadChange(dir string) (*Change, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "performance", changeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Change
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveChange writes the change atomically to <dir>/performance/last-change.json.
func SaveChange(dir string, c Change) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(dir, "performance", changeFile), append(raw, '\n'), 0o600)
}

// Evaluate decides the effect of a running change once the check window has passed.
// Samples are the measured device load values since the change was applied.
// Changes that are already decided, or that are still inside the window, are left alone.
func (c *Change) Evaluate(now time.Time, samples []float64) {
	if c.State != "running" || now.Sub(c.At) < checkWindow || len(samples) == 0 {
		return
	}
	measured := percentile(samples, 0.95)
	c.Measured = &measured
	if measured > c.Available {
		c.State = "bad"
	} else {
		c.State = "ok"
	}
}

// Conflicts returns the rows whose current value differs from the value the change set.
// Rows missing from current are not reported.
func Conflicts(c Change, current map[RowID]float64) map[RowID]float64 {
	conflicts := map[RowID]float64{}
	for row, after := range c.After {
		now, ok := current[row]
		if ok && now != after {
			conflicts[row] = now
		}
	}
	return conflicts
}
