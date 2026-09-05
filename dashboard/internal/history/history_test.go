package history

import (
	"testing"
	"time"
)

func TestBrowserStoreFiltersByTTLAndQuery(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := NewBrowserStore(2 * time.Hour)
	samples := []Sample{
		{EntityID: "pv", Timestamp: now.Add(-90 * time.Minute), Value: 100},
		{EntityID: "pv", Timestamp: now.Add(-30 * time.Minute), Value: 200},
		{EntityID: "pv", Timestamp: now.Add(-3 * time.Hour), Value: 300},
		{EntityID: "grid", Timestamp: now.Add(-15 * time.Minute), Value: 50},
	}

	got := store.Filter(samples, Query{EntityID: "pv", Limit: 1}, now)
	if len(got) != 1 || got[0].Value != 200 {
		t.Fatalf("got %#v, want latest PV sample", got)
	}
}

func TestBrowserStoreRejectsFutureAndIncompleteSamples(t *testing.T) {
	now := time.Now().UTC()
	store := NewBrowserStore(0)
	for _, sample := range []Sample{
		{EntityID: "pv", Timestamp: now.Add(time.Minute)},
		{Timestamp: now},
	} {
		if store.Accept(sample, now) {
			t.Fatalf("accepted invalid sample %#v", sample)
		}
	}
	if store.TTL != DefaultTTL {
		t.Fatalf("TTL = %s, want %s", store.TTL, DefaultTTL)
	}
}
