// Package chainwatch learns the normal loss of measurement chain segments per
// load class. The table is kept in learned.json next to the chain data.
package chainwatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

const (
	ClassWidthW       = 50.0
	MaxClass          = 399
	LearnWindow       = 4320
	LearnedSamples    = 1080
	LearnedAfter      = 7 * 24 * time.Hour
	LearnedMinSamples = 60
	CurveMinSamples   = 30
	BestRangeSlack    = 0.01 // percentage point below the maximum
	BestRangeMinClass = 3
)

const learnedFormatVersion = 1

// bin holds the learned means of one load class. N counts the samples up to
// LearnWindow, DevW is the mean absolute deviation of the loss.
type bin struct {
	N                      int
	SourceW, TargetW, DevW float64
}

// pairEntry is one direction of a pair with its load classes.
type pairEntry struct {
	FirstSeen                  time.Time
	ScaleSource, ScaleTarget   float64
	InvertSource, InvertTarget bool
	Agree, Opposite            int
	Classes                    map[int]*bin
}

// Table is the learned model of the chain segments. It is read by many
// goroutines at once, so it has its own lock.
type Table struct {
	mu    sync.RWMutex
	pairs map[energystats.ChainPair]*pairEntry
	dirty bool
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{pairs: map[energystats.ChainPair]*pairEntry{}}
}

// ClassOf returns the load class of a counting power in W. The sign does not
// matter, classes above MaxClass share the last one.
func ClassOf(countingW float64) int {
	f := math.Abs(countingW) / ClassWidthW
	if f >= MaxClass+1 {
		return MaxClass
	}
	return int(f)
}

// Learn adds one sample of a segment in the load class of countingW. Each bin
// is an exponential mean over the last LearnWindow samples.
func (t *Table) Learn(pair energystats.ChainPair, countingW, sourceW, targetW float64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.entryLocked(pair, now)
	class := ClassOf(countingW)
	b := entry.Classes[class]
	if b == nil {
		b = &bin{}
		entry.Classes[class] = b
	}

	alpha := 1 / float64(min(b.N+1, LearnWindow))
	if b.N > 0 {
		dev := math.Abs((sourceW - targetW) - (b.SourceW - b.TargetW))
		b.DevW += alpha * (dev - b.DevW)
	}
	b.SourceW += alpha * (sourceW - b.SourceW)
	b.TargetW += alpha * (targetW - b.TargetW)
	if b.N < LearnWindow {
		b.N++
	}
	t.dirty = true
}

// Segment returns what is learned about a segment in the load class of
// countingW. Learned is false for unknown classes.
func (t *Table) Segment(pair energystats.ChainPair, countingW float64, now time.Time) energy.ChainSegmentInsight {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry := t.pairs[pair]
	if entry == nil {
		return energy.ChainSegmentInsight{}
	}
	b := entry.Classes[ClassOf(countingW)]
	if b == nil {
		return energy.ChainSegmentInsight{}
	}
	return energy.ChainSegmentInsight{
		Learned:    learnedBin(entry, b, now),
		LossW:      b.SourceW - b.TargetW,
		DeviationW: b.DevW,
	}
}

// PairLearned reports whether any load class of either direction of a-b is
// learned.
func (t *Table) PairLearned(a, b string, now time.Time) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, pair := range []energystats.ChainPair{{Source: a, Target: b}, {Source: b, Target: a}} {
		entry := t.pairs[pair]
		if entry == nil {
			continue
		}
		for _, bn := range entry.Classes {
			if learnedBin(entry, bn, now) {
				return true
			}
		}
	}
	return false
}

// Dirty reports whether the table changed since it was loaded or last saved.
func (t *Table) Dirty() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dirty
}

// Reset drops the learned data of a pair in both directions.
func (t *Table) Reset(pair energystats.ChainPair) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetLocked(pair)
}

func (t *Table) resetLocked(pair energystats.ChainPair) {
	reverse := energystats.ChainPair{Source: pair.Target, Target: pair.Source}
	for _, p := range []energystats.ChainPair{pair, reverse} {
		if _, ok := t.pairs[p]; ok {
			delete(t.pairs, p)
			t.dirty = true
		}
	}
}

// Prune drops every pair that is not adjacent in one of the chains. A counting
// change alone keeps the data.
func (t *Table) Prune(chains map[string]energy.Chain) {
	t.mu.Lock()
	defer t.mu.Unlock()

	keep := map[energystats.ChainPair]bool{}
	for _, chain := range chains {
		for i := 0; i+1 < len(chain.Points); i++ {
			x, y := chain.Points[i], chain.Points[i+1]
			keep[energystats.ChainPair{Source: x, Target: y}] = true
			keep[energystats.ChainPair{Source: y, Target: x}] = true
		}
	}
	for pair := range t.pairs {
		if !keep[pair] {
			delete(t.pairs, pair)
			t.dirty = true
		}
	}
}

// learnedBin reports whether a class is learned: LearnedSamples samples, or
// LearnedMinSamples samples and a pair that is at least LearnedAfter old.
func learnedBin(entry *pairEntry, b *bin, now time.Time) bool {
	return b.N >= LearnedSamples ||
		(b.N >= LearnedMinSamples && now.Sub(entry.FirstSeen) >= LearnedAfter)
}

func (t *Table) entryLocked(pair energystats.ChainPair, now time.Time) *pairEntry {
	if entry := t.pairs[pair]; entry != nil {
		return entry
	}
	entry := &pairEntry{
		FirstSeen:   now,
		ScaleSource: 1,
		ScaleTarget: 1,
		Classes:     map[int]*bin{},
	}
	t.pairs[pair] = entry
	return entry
}

// tableFile is the content of learned.json. A class is
// [N, source W, target W, deviation W].
type tableFile struct {
	Version int        `json:"version"`
	Pairs   []pairFile `json:"pairs"`
}

type pairFile struct {
	Source       string                `json:"source"`
	Target       string                `json:"target"`
	FirstSeen    time.Time             `json:"first_seen"`
	ScaleSource  float64               `json:"scale_source"`
	ScaleTarget  float64               `json:"scale_target"`
	InvertSource bool                  `json:"invert_source"`
	InvertTarget bool                  `json:"invert_target"`
	Agree        int                   `json:"agree"`
	Opposite     int                   `json:"opposite"`
	Classes      map[string][4]float64 `json:"classes"`
}

// Load reads the table from path. A missing file gives an empty table. A
// damaged file or an unknown version is moved to path.corrupt, replacing an
// older backup, and logged.
func Load(path string, logf func(string, ...any)) *Table {
	if logf == nil {
		logf = log.Printf
	}
	t := NewTable()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t
	}
	if err != nil {
		logf("chainwatch: read %s: %v", path, err)
		return t
	}
	pairs, err := decodeTable(data)
	if err != nil {
		logf("chainwatch: %s is damaged, moved to %s.corrupt: %v", path, path, err)
		if rerr := os.Rename(path, path+".corrupt"); rerr != nil {
			logf("chainwatch: move %s aside: %v", path, rerr)
		}
		return t
	}
	t.pairs = pairs
	return t
}

func decodeTable(data []byte) (map[energystats.ChainPair]*pairEntry, error) {
	var file tableFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Version != learnedFormatVersion {
		return nil, fmt.Errorf("unknown version %d", file.Version)
	}
	pairs := map[energystats.ChainPair]*pairEntry{}
	for _, pf := range file.Pairs {
		entry := &pairEntry{
			FirstSeen:    pf.FirstSeen,
			ScaleSource:  pf.ScaleSource,
			ScaleTarget:  pf.ScaleTarget,
			InvertSource: pf.InvertSource,
			InvertTarget: pf.InvertTarget,
			Agree:        pf.Agree,
			Opposite:     pf.Opposite,
			Classes:      map[int]*bin{},
		}
		for key, v := range pf.Classes {
			class, err := strconv.Atoi(key)
			if err != nil {
				return nil, fmt.Errorf("class %q: %w", key, err)
			}
			entry.Classes[class] = &bin{N: int(v[0]), SourceW: v[1], TargetW: v[2], DevW: v[3]}
		}
		pairs[energystats.ChainPair{Source: pf.Source, Target: pf.Target}] = entry
	}
	return pairs, nil
}

// Save writes the table to path atomically. The output is sorted and the
// values are rounded to three decimals, so an unchanged table gives the same
// bytes.
func (t *Table) Save(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(t.fileLocked())
	if err != nil {
		return err
	}
	if err := energystats.WriteFileAtomic(filepath.Dir(path), path, data); err != nil {
		return err
	}
	t.dirty = false
	return nil
}

func (t *Table) fileLocked() tableFile {
	file := tableFile{Version: learnedFormatVersion, Pairs: []pairFile{}}
	for pair, entry := range t.pairs {
		pf := pairFile{
			Source:       pair.Source,
			Target:       pair.Target,
			FirstSeen:    entry.FirstSeen.UTC(),
			ScaleSource:  round3(entry.ScaleSource),
			ScaleTarget:  round3(entry.ScaleTarget),
			InvertSource: entry.InvertSource,
			InvertTarget: entry.InvertTarget,
			Agree:        entry.Agree,
			Opposite:     entry.Opposite,
			Classes:      map[string][4]float64{},
		}
		for class, b := range entry.Classes {
			pf.Classes[strconv.Itoa(class)] = [4]float64{
				float64(b.N), round3(b.SourceW), round3(b.TargetW), round3(b.DevW),
			}
		}
		file.Pairs = append(file.Pairs, pf)
	}
	sort.Slice(file.Pairs, func(i, j int) bool {
		a, b := file.Pairs[i], file.Pairs[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Target < b.Target
	})
	return file
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
