// Package history defines the browser-history data contract and its retention
// policy. Samples are stored in IndexedDB by the browser, not on the Pi.
package history

import (
	"sort"
	"time"
)

const DefaultTTL = 6 * time.Hour

type Sample struct {
	EntityID  string    `json:"entity_id"`
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit,omitempty"`
	Source    string    `json:"source"`
}

type Query struct {
	EntityID string
	Start    time.Time
	End      time.Time
	Limit    int
}

type BrowserStore struct {
	TTL time.Duration
}

func NewBrowserStore(ttl time.Duration) BrowserStore {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return BrowserStore{TTL: ttl}
}

func (s BrowserStore) Accept(sample Sample, now time.Time) bool {
	if sample.EntityID == "" || sample.Timestamp.IsZero() || sample.Timestamp.After(now) {
		return false
	}
	return !sample.Timestamp.Before(now.Add(-s.TTL))
}

func (s BrowserStore) Filter(samples []Sample, query Query, now time.Time) []Sample {
	result := make([]Sample, 0, len(samples))
	for _, sample := range samples {
		if !s.Accept(sample, now) || (query.EntityID != "" && sample.EntityID != query.EntityID) {
			continue
		}
		if !query.Start.IsZero() && sample.Timestamp.Before(query.Start) {
			continue
		}
		if !query.End.IsZero() && sample.Timestamp.After(query.End) {
			continue
		}
		result = append(result, sample)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Timestamp.Before(result[j].Timestamp) })
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[len(result)-query.Limit:]
	}
	return result
}
