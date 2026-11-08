package energystats

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// lossTotalsMaxGap is the longest gap between two observations that still
// counts. A longer gap has no known loss in between.
const lossTotalsMaxGap = 60 * time.Second

// lossTotalsSaveEvery is the interval of the saves while the totals are dirty,
// lossTotalsSaveRetry the wait after a failed save. Same as the chain monitor.
const (
	lossTotalsSaveEvery = time.Hour
	lossTotalsSaveRetry = 5 * time.Minute
)

const lossTotalsVersion = 1

// LossTotals integrates the loss of every measurement chain to kWh. A chain
// starts at 0, and its total is dropped when the chain is deleted.
type LossTotals struct {
	mu    sync.Mutex
	path  string
	logf  func(string, ...any)
	kwh   map[string]float64
	last  map[string]lossSample
	dirty bool
	next  time.Time
}

// lossSample is the last observation of a chain. ok is false when the chain
// had no loss then.
type lossSample struct {
	at time.Time
	w  float64
	ok bool
}

type lossTotalsFile struct {
	Version int                `json:"version"`
	KWh     map[string]float64 `json:"kwh"`
}

// NewLossTotals loads the totals from path. A missing file starts at 0, an
// unreadable one starts at 0 too and is logged through logf.
func NewLossTotals(path string, logf func(string, ...any)) *LossTotals {
	if logf == nil {
		logf = log.Printf
	}
	l := &LossTotals{path: path, logf: logf, kwh: map[string]float64{}, last: map[string]lossSample{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l
	}
	var f lossTotalsFile
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err == nil && f.Version != lossTotalsVersion {
		err = fmt.Errorf("version %d", f.Version)
	}
	if err != nil {
		logf("energystats: loss totals %s unreadable, starting at 0: %v", path, err)
		return l
	}
	for id, kwh := range f.KWh {
		l.kwh[id] = kwh
	}
	return l
}

// Observe integrates the loss of every chain since its last observation. The
// loss counts as 0 when negative, and an interval counts only when both ends
// have a loss and are at most lossTotalsMaxGap apart.
func (l *LossTotals) Observe(at time.Time, chains []energy.ChainState) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.next.IsZero() {
		l.next = at.Add(lossTotalsSaveEvery)
	}
	seen := make(map[string]bool, len(chains))
	for _, chain := range chains {
		seen[chain.ID] = true
		w, ok := energy.ChainLossW(chain)
		prev := l.last[chain.ID]
		l.last[chain.ID] = lossSample{at: at, w: w, ok: ok}
		if !ok || !prev.ok {
			continue
		}
		gap := at.Sub(prev.at)
		if gap <= 0 || gap > lossTotalsMaxGap {
			continue
		}
		avgW := (clampLossW(prev.w) + clampLossW(w)) / 2
		l.kwh[chain.ID] += avgW * gap.Seconds() / 3600 / 1000
		l.dirty = true
	}
	for id := range l.last {
		if !seen[id] {
			delete(l.last, id)
		}
	}
	for id := range l.kwh {
		if !seen[id] {
			delete(l.kwh, id)
			l.dirty = true
		}
	}

	if !at.Before(l.next) {
		l.next = at.Add(lossTotalsSaveEvery)
		if l.dirty {
			if err := l.saveLocked(); err != nil {
				l.logf("energystats: save loss totals %s: %v, retry in %s", l.path, err, lossTotalsSaveRetry)
				l.next = at.Add(lossTotalsSaveRetry)
			}
		}
	}
}

// Totals returns a copy of the total of every chain in kWh.
func (l *LossTotals) Totals() map[string]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]float64, len(l.kwh))
	for id, kwh := range l.kwh {
		out[id] = kwh
	}
	return out
}

// Close saves the totals when they are dirty. The totals keep working
// afterwards.
func (l *LossTotals) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dirty {
		return
	}
	if err := l.saveLocked(); err != nil {
		l.logf("energystats: save loss totals %s: %v", l.path, err)
	}
}

// saveLocked rounds the totals to six decimals, so memory and file agree, and
// writes them atomically.
func (l *LossTotals) saveLocked() error {
	for id, kwh := range l.kwh {
		l.kwh[id] = math.Round(kwh*1e6) / 1e6
	}
	data, err := json.Marshal(lossTotalsFile{Version: lossTotalsVersion, KWh: l.kwh})
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(filepath.Dir(l.path), l.path, data); err != nil {
		return err
	}
	l.dirty = false
	return nil
}

func clampLossW(w float64) float64 {
	if w < 0 {
		return 0
	}
	return w
}
