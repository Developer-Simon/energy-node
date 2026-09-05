package historyexchange

import (
	"sort"
	"sync"
	"time"
)

// Row ist ein verdichteter Messwert, wie ihn der Browser fuehrt. Die
// JSON-Namen sind die der IndexedDB-Saetze - so wandert eine Lieferung ohne
// Umbenennen durch den Server.
type Row struct {
	Series string  `json:"series"`
	TS     int64   `json:"ts"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Avg    float64 `json:"avg"`
	N      int     `json:"n"`
	U      string  `json:"u,omitempty"`
}

// Census ist das Deckungsraster einer Serie: je Rasterbucket die Anzahl
// vorhandener Saetze. Dieselbe Form wie im Browser (history-coverage.js).
type Census struct {
	From int64 `json:"from"`
	Step int64 `json:"step"`
	N    []int `json:"n"`
}

type key struct {
	series string
	ts     int64
}

// Buffer haelt die juengsten Minutenwerte im Arbeitsspeicher. Er wird vom
// aufzeichnenden Client gefuellt, nicht vom Server selbst gemessen: die
// Vorzeichenlogik der Energie-Rollen liegt in EnergyModel im Browser, und
// sie ein zweites Mal in Go zu fuehren waere die teurere Haelfte des
// Tauschs (siehe history-recorder.js).
//
// Nichts davon beruehrt die SD-Karte. Ein Neustart leert den Puffer.
type Buffer struct {
	mutex     sync.Mutex
	rows      map[key]Row
	retention time.Duration
	maxRows   int
}

func NewBuffer(retention time.Duration, maxRows int) *Buffer {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if maxRows < 1 {
		maxRows = 1
	}
	return &Buffer{rows: map[key]Row{}, retention: retention, maxRows: maxRows}
}

func (b *Buffer) RetentionHours() int {
	return int(b.retention / time.Hour)
}

// Append nimmt neue Saetze auf und liefert, wieviele davon wirklich neu
// waren. Bereits bekannte Schluessel bleiben unveraendert - dieselbe
// Nie-ueberschreiben-Regel wie writeMissing() im Browser.
func (b *Buffer) Append(rows []Row, nowMs int64) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	cutoff := nowMs - b.retention.Milliseconds()
	added := 0
	for _, item := range rows {
		if item.TS < cutoff || item.TS > nowMs {
			continue
		}
		id := key{series: item.Series, ts: item.TS}
		if _, exists := b.rows[id]; exists {
			continue
		}
		b.rows[id] = item
		added++
	}
	b.evictLocked(cutoff)
	return added
}

func (b *Buffer) evictLocked(cutoff int64) {
	for id := range b.rows {
		if id.ts < cutoff {
			delete(b.rows, id)
		}
	}
	if len(b.rows) <= b.maxRows {
		return
	}
	stamps := make([]int64, 0, len(b.rows))
	for id := range b.rows {
		stamps = append(stamps, id.ts)
	}
	sort.Slice(stamps, func(left, right int) bool { return stamps[left] < stamps[right] })
	// Der Zeitstempel an der Position "zuviel" ist die neue Untergrenze;
	// alles davor faellt heraus. Ueber die Serien hinweg schneidet das
	// gleichmaessig ab, statt eine Serie zu bevorzugen.
	limit := stamps[len(stamps)-b.maxRows]
	for id := range b.rows {
		if id.ts < limit {
			delete(b.rows, id)
		}
	}
}

func (b *Buffer) Len() int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return len(b.rows)
}

func (b *Buffer) Census(fromMs, toMs, stepMs int64) map[string]Census {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if stepMs <= 0 || toMs <= fromMs {
		return map[string]Census{}
	}
	buckets := int((toMs - fromMs + stepMs - 1) / stepMs)
	result := map[string]Census{}
	for id := range b.rows {
		if id.ts < fromMs || id.ts >= toMs {
			continue
		}
		census, ok := result[id.series]
		if !ok {
			census = Census{From: fromMs, Step: stepMs, N: make([]int, buckets)}
		}
		census.N[(id.ts-fromMs)/stepMs]++
		result[id.series] = census
	}
	return result
}

func (b *Buffer) Rows(series string, ranges [][2]int64, limit int) []Row {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	found := []Row{}
	for id, item := range b.rows {
		if id.series != series {
			continue
		}
		for _, span := range ranges {
			if id.ts >= span[0] && id.ts < span[1] {
				found = append(found, item)
				break
			}
		}
	}
	sort.Slice(found, func(left, right int) bool { return found[left].TS < found[right].TS })
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found
}
