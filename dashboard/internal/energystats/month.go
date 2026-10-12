// Package energystats fuehrt die Energiestatistik des Dashboards: je
// Stunde und Rolle die Energie in kWh, aus eigener Integration der
// Live-Werte oder aus der Langzeitstatistik von Home Assistant. Tag, Monat
// und Jahr sind Summen dieser Stunden. Siehe
// .docs/superpowers/specs/2026-10-11-energiestatistik-design.md.
package energystats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Quelle einer Stunde, ein Byte je Stunde in Series.Source. "h" ersetzt
// "o", nie umgekehrt: Home Assistant hat die vollstaendigere Sicht.
const (
	SourceNone byte = '-'
	SourceOwn  byte = 'o'
	SourceHA   byte = 'h'
)

const monthFormatVersion = 1

// Series sind die Stundenwerte einer Rolle in einem Monat. KWh[i] ist NaN,
// solange die Stunde keine Daten hat.
type Series struct {
	KWh    []float64
	Source []byte
}

// Month haelt alle Stunden eines UTC-Monats. Covered zaehlt je Stunde die
// Sekunden, die die eigene Aufzeichnung gedeckt hat, damit eine nach einem
// Neustart unvollstaendige Stunde erkennbar bleibt.
type Month struct {
	Start   time.Time
	Covered []int32
	Roles   map[string]*Series
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// NewMonth legt den leeren Monat an, in den t faellt.
func NewMonth(t time.Time) *Month {
	start := monthStart(t)
	hours := int(start.AddDate(0, 1, 0).Sub(start) / time.Hour)
	return &Month{Start: start, Covered: make([]int32, hours), Roles: map[string]*Series{}}
}

func (m *Month) Hours() int { return len(m.Covered) }

// Index liefert den Platz der Stunde, die hour enthaelt.
func (m *Month) Index(hour time.Time) (int, bool) {
	offset := hour.UTC().Sub(m.Start)
	if offset < 0 {
		return 0, false
	}
	i := int(offset / time.Hour)
	return i, i < m.Hours()
}

func (m *Month) series(role string) *Series {
	if s, ok := m.Roles[role]; ok {
		return s
	}
	s := &Series{KWh: make([]float64, m.Hours()), Source: bytes.Repeat([]byte{SourceNone}, m.Hours())}
	for i := range s.KWh {
		s.KWh[i] = math.NaN()
	}
	m.Roles[role] = s
	return s
}

// AddOwn addiert eine eigene Messung. HA-Stunden bleiben unberuehrt, eine
// eigene Stunde waechst (Neustart mitten in der Stunde). Meldet, ob sich
// etwas geaendert hat.
func (m *Month) AddOwn(hour time.Time, kwh map[string]float64, covered int32) bool {
	i, ok := m.Index(hour)
	if !ok {
		return false
	}
	changed := false
	for role, value := range kwh {
		s := m.series(role)
		switch s.Source[i] {
		case SourceHA:
			continue
		case SourceOwn:
			s.KWh[i] += value
		default:
			s.KWh[i] = value
			s.Source[i] = SourceOwn
		}
		changed = true
	}
	if changed {
		m.Covered[i] = min(m.Covered[i]+covered, 3600)
	}
	return changed
}

// SetHA setzt den Wert aus Home Assistant und ersetzt dabei eine eigene
// Messung. Wird in Teil 2 (Companion) beschrieben.
func (m *Month) SetHA(hour time.Time, role string, kwh float64) bool {
	i, ok := m.Index(hour)
	if !ok {
		return false
	}
	s := m.series(role)
	if s.Source[i] == SourceHA && s.KWh[i] == kwh {
		return false
	}
	s.KWh[i] = kwh
	s.Source[i] = SourceHA
	return true
}

type monthFile struct {
	Version int                   `json:"version"`
	Month   string                `json:"month"`
	Covered []int32               `json:"covered"`
	Roles   map[string]seriesFile `json:"roles"`
}

type seriesFile struct {
	KWh    []*float64 `json:"kwh"`
	Source string     `json:"src"`
}

// MarshalJSON schreibt die Spaltenform der Monatsdatei. kWh auf 6 Stellen
// (1 mWh) gerundet, NaN als null.
func (m *Month) MarshalJSON() ([]byte, error) {
	file := monthFile{Version: monthFormatVersion, Month: m.Start.Format("2006-01"), Covered: m.Covered, Roles: map[string]seriesFile{}}
	for role, s := range m.Roles {
		values := make([]*float64, len(s.KWh))
		for i, v := range s.KWh {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			rounded := math.Round(v*1e6) / 1e6
			values[i] = &rounded
		}
		file.Roles[role] = seriesFile{KWh: values, Source: string(s.Source)}
	}
	return json.Marshal(file)
}

func decodeMonth(data []byte) (*Month, error) {
	var file monthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Version != monthFormatVersion {
		return nil, fmt.Errorf("energystats: unbekannte Version %d", file.Version)
	}
	start, err := time.Parse("2006-01", file.Month)
	if err != nil {
		return nil, fmt.Errorf("energystats: Monat %q: %w", file.Month, err)
	}
	m := NewMonth(start)
	if len(file.Covered) != m.Hours() {
		return nil, fmt.Errorf("energystats: %d Deckungswerte, erwartet %d", len(file.Covered), m.Hours())
	}
	copy(m.Covered, file.Covered)
	for role, stored := range file.Roles {
		if len(stored.KWh) != m.Hours() || len(stored.Source) != m.Hours() {
			return nil, fmt.Errorf("energystats: Rolle %q hat falsche Laenge", role)
		}
		s := m.series(role)
		for i, value := range stored.KWh {
			source := stored.Source[i]
			if source != SourceNone && source != SourceOwn && source != SourceHA {
				return nil, fmt.Errorf("energystats: Rolle %q, Stunde %d: Quelle %q", role, i, source)
			}
			if value == nil || source == SourceNone {
				continue
			}
			s.KWh[i] = *value
			s.Source[i] = source
		}
	}
	return m, nil
}
