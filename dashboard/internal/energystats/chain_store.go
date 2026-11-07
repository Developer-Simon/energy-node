package energystats

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
)

const chainFormatVersion = 1

// chainMaxSeconds caps the covered seconds of one hour, as Month does for
// its role series.
const chainMaxSeconds = 3600

// ChainMonth holds the measurement chain hours of one UTC month. Hour
// indexes count from the start of the month, like Month.Index.
type ChainMonth struct {
	Start   time.Time
	Pairs   map[ChainPair]map[int]PairEnergy
	Standby map[string]map[int]StandbyEnergy
}

func newChainMonth(start time.Time) *ChainMonth {
	return &ChainMonth{
		Start:   start,
		Pairs:   map[ChainPair]map[int]PairEnergy{},
		Standby: map[string]map[int]StandbyEnergy{},
	}
}

func chainHours(start time.Time) int {
	return int(start.AddDate(0, 1, 0).Sub(start) / time.Hour)
}

// chainIndex returns the position of hour within the month that starts at
// start, false if hour lies outside of it.
func chainIndex(start, hour time.Time) (int, bool) {
	offset := hour.UTC().Sub(start)
	if offset < 0 {
		return 0, false
	}
	i := int(offset / time.Hour)
	return i, i < chainHours(start)
}

// ChainStore keeps the measurement chain hours in <dir>/<jjjj-mm>.json. Like
// Store, a month is read on first access, a damaged file is moved to
// .corrupt, and only months with a file or a pending write stay cached.
type ChainStore struct {
	dir      string
	onChange func()
	logf     func(format string, args ...any)

	mu      sync.Mutex
	months  map[string]*ChainMonth
	missing map[string]bool
	dirty   map[string]bool
}

// NewChainStore creates a store in dir. onChange runs after every Add that
// changed an hour, so that the energy statistics reload. It may be nil.
func NewChainStore(dir string, onChange func()) *ChainStore {
	return &ChainStore{
		dir:      dir,
		onChange: onChange,
		logf:     log.Printf,
		months:   map[string]*ChainMonth{},
		missing:  map[string]bool{},
		dirty:    map[string]bool{},
	}
}

func (s *ChainStore) path(key string) string { return filepath.Join(s.dir, key+".json") }

// monthLocked returns the month of t. create=false returns nil for a month
// without a file.
func (s *ChainStore) monthLocked(t time.Time, create bool) (*ChainMonth, error) {
	start := monthStart(t)
	key := monthKey(start)
	if m, ok := s.months[key]; ok {
		return m, nil
	}
	if !create && s.missing[key] {
		return nil, nil
	}
	path := s.path(key)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			s.missing[key] = true
			return nil, nil
		}
		delete(s.missing, key)
		m := newChainMonth(start)
		s.months[key] = m
		return m, nil
	case err != nil:
		return nil, fmt.Errorf("energystats: %s not readable: %w", path, err)
	}
	m, err := decodeChainMonth(data)
	if err == nil && !m.Start.Equal(start) {
		err = fmt.Errorf("month %s instead of %s", m.Start.Format("2006-01"), key)
	}
	if err != nil {
		s.logf("energystats: %s is damaged and is moved to .corrupt: %v", path, err)
		if renameErr := os.Rename(path, path+".corrupt"); renameErr != nil {
			return nil, fmt.Errorf("energystats: %s not movable: %w", path, renameErr)
		}
		m = newChainMonth(start)
	}
	s.months[key] = m
	return m, nil
}

// Add adds a closed chain hour to the stored hour. Energy is summed, covered
// seconds are capped at one hour. The file is written at once.
func (s *ChainStore) Add(hour ChainHour) error {
	changed, err := s.add(hour)
	if changed && s.onChange != nil {
		s.onChange()
	}
	return err
}

func (s *ChainStore) add(hour ChainHour) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hourStart := hour.Hour.UTC().Truncate(time.Hour)
	m, err := s.monthLocked(hourStart, true)
	if err != nil {
		return false, err
	}
	i, _ := chainIndex(m.Start, hourStart)
	changed := false
	for pair, e := range hour.Pairs {
		if e.Seconds <= 0 {
			continue
		}
		cells := m.Pairs[pair]
		if cells == nil {
			cells = map[int]PairEnergy{}
			m.Pairs[pair] = cells
		}
		cur := cells[i]
		cur.SourceKWh += e.SourceKWh
		cur.TargetKWh += e.TargetKWh
		cur.Seconds = min(cur.Seconds+e.Seconds, chainMaxSeconds)
		cells[i] = cur
		changed = true
	}
	for entity, e := range hour.Standby {
		if e.Seconds <= 0 {
			continue
		}
		cells := m.Standby[entity]
		if cells == nil {
			cells = map[int]StandbyEnergy{}
			m.Standby[entity] = cells
		}
		cur := cells[i]
		cur.KWh += e.KWh
		cur.Seconds = min(cur.Seconds+e.Seconds, chainMaxSeconds)
		cells[i] = cur
		changed = true
	}
	if !changed {
		return false, nil
	}
	s.dirty[monthKey(m.Start)] = true
	return true, s.writeDirtyLocked()
}

func (s *ChainStore) writeDirtyLocked() error {
	var firstErr error
	for key := range s.dirty {
		if err := s.writeLocked(key, s.months[key]); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(s.dirty, key)
	}
	return firstErr
}

func (s *ChainStore) writeLocked(key string, m *ChainMonth) error {
	data, err := encodeChainMonth(m)
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.dir, s.path(key), data)
}

// Each calls fn for every full hour in [from, to). m is nil for months
// without a file, i is the hour index within m.
func (s *ChainStore) Each(from, to time.Time, fn func(hour time.Time, m *ChainMonth, i int)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hour := from.UTC().Truncate(time.Hour); hour.Before(to); hour = hour.Add(time.Hour) {
		m, err := s.monthLocked(hour, false)
		if err != nil {
			return err
		}
		i := 0
		if m != nil {
			i, _ = chainIndex(m.Start, hour)
		}
		fn(hour, m, i)
	}
	return nil
}

// chainMonthFile is the file format of <jjjj-mm>.json: sparse hours, each
// pair as [source kWh, target kWh, seconds], each standby entity as
// [kWh, seconds]. Hour keys are the indexes within the month.
type chainMonthFile struct {
	Version int                `json:"version"`
	Start   time.Time          `json:"start"`
	Pairs   []chainPairFile    `json:"pairs"`
	Standby []chainStandbyFile `json:"standby"`
}

type chainPairFile struct {
	Source string               `json:"source"`
	Target string               `json:"target"`
	Hours  map[string][]float64 `json:"hours"`
}

type chainStandbyFile struct {
	Entity string               `json:"entity"`
	Hours  map[string][]float64 `json:"hours"`
}

// roundKWh rounds to 1 mWh, as the role month files do.
func roundKWh(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// encodeChainMonth writes the sparse form with sorted lists. Empty pairs and
// entities are left out.
func encodeChainMonth(m *ChainMonth) ([]byte, error) {
	file := chainMonthFile{Version: chainFormatVersion, Start: m.Start, Pairs: []chainPairFile{}, Standby: []chainStandbyFile{}}
	for pair, cells := range m.Pairs {
		if len(cells) == 0 {
			continue
		}
		entry := chainPairFile{Source: pair.Source, Target: pair.Target, Hours: make(map[string][]float64, len(cells))}
		for i, e := range cells {
			entry.Hours[strconv.Itoa(i)] = []float64{roundKWh(e.SourceKWh), roundKWh(e.TargetKWh), float64(e.Seconds)}
		}
		file.Pairs = append(file.Pairs, entry)
	}
	sort.Slice(file.Pairs, func(a, b int) bool {
		pa, pb := file.Pairs[a], file.Pairs[b]
		if pa.Source != pb.Source {
			return pa.Source < pb.Source
		}
		return pa.Target < pb.Target
	})
	for entity, cells := range m.Standby {
		if len(cells) == 0 {
			continue
		}
		entry := chainStandbyFile{Entity: entity, Hours: make(map[string][]float64, len(cells))}
		for i, e := range cells {
			entry.Hours[strconv.Itoa(i)] = []float64{roundKWh(e.KWh), float64(e.Seconds)}
		}
		file.Standby = append(file.Standby, entry)
	}
	sort.Slice(file.Standby, func(a, b int) bool { return file.Standby[a].Entity < file.Standby[b].Entity })
	return json.Marshal(file)
}

// decodeChainMonth rejects anything the encoder would not have written, so
// that a damaged file is moved aside instead of being read partially.
func decodeChainMonth(data []byte) (*ChainMonth, error) {
	var file chainMonthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Version != chainFormatVersion {
		return nil, fmt.Errorf("unknown chain format version %d", file.Version)
	}
	if !file.Start.Equal(monthStart(file.Start)) {
		return nil, fmt.Errorf("start %s is not the beginning of a month", file.Start.Format(time.RFC3339))
	}
	m := newChainMonth(file.Start.UTC())
	hours := chainHours(m.Start)
	for _, entry := range file.Pairs {
		pair := ChainPair{Source: entry.Source, Target: entry.Target}
		for key, values := range entry.Hours {
			i, err := parseChainHour(key, hours)
			if err != nil {
				return nil, err
			}
			if len(values) != 3 {
				return nil, fmt.Errorf("pair %s->%s hour %s: %d values, expected 3", pair.Source, pair.Target, key, len(values))
			}
			seconds, err := parseChainSeconds(values[2])
			if err != nil {
				return nil, err
			}
			if m.Pairs[pair] == nil {
				m.Pairs[pair] = map[int]PairEnergy{}
			}
			m.Pairs[pair][i] = PairEnergy{SourceKWh: values[0], TargetKWh: values[1], Seconds: seconds}
		}
	}
	for _, entry := range file.Standby {
		for key, values := range entry.Hours {
			i, err := parseChainHour(key, hours)
			if err != nil {
				return nil, err
			}
			if len(values) != 2 {
				return nil, fmt.Errorf("standby %s hour %s: %d values, expected 2", entry.Entity, key, len(values))
			}
			seconds, err := parseChainSeconds(values[1])
			if err != nil {
				return nil, err
			}
			if m.Standby[entry.Entity] == nil {
				m.Standby[entry.Entity] = map[int]StandbyEnergy{}
			}
			m.Standby[entry.Entity][i] = StandbyEnergy{KWh: values[0], Seconds: seconds}
		}
	}
	return m, nil
}

func parseChainHour(key string, hours int) (int, error) {
	i, err := strconv.Atoi(key)
	if err != nil || i < 0 || i >= hours {
		return 0, fmt.Errorf("hour key %q outside of the month", key)
	}
	return i, nil
}

func parseChainSeconds(v float64) (int32, error) {
	if v != math.Trunc(v) || v < 0 || v > chainMaxSeconds {
		return 0, fmt.Errorf("seconds %v outside of 0..%d", v, chainMaxSeconds)
	}
	return int32(v), nil
}
