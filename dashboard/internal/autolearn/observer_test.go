package autolearn

import (
	"testing"
	"time"
)

var testBase = time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC)

func newTestObserver() *Observer {
	return NewObserver(time.UTC)
}

func TestObserverCountsDifferenceIntoHour(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 5})
	o.ObserveTriggers(testBase.Add(10*time.Minute), map[string]int{"r1": 8})

	st := o.Snapshot().Rules["r1"]
	hour := testBase.Unix() / 3600
	if got := st.Hours[hour]; got != 3 {
		t.Fatalf("hour count = %d, want 3", got)
	}
	if len(st.Events) != 1 || st.Events[0] != testBase.Add(10*time.Minute).Unix() {
		t.Fatalf("events = %v, want one event at the second observation", st.Events)
	}
}

func TestObserverDecreaseTakesNewValueWithoutTrigger(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 5})
	o.ObserveTriggers(testBase.Add(time.Minute), map[string]int{"r1": 8})
	o.ObserveTriggers(testBase.Add(2*time.Minute), map[string]int{"r1": 0})

	st := o.Snapshot().Rules["r1"]
	if total := sumHours(st.Hours); total != 3 {
		t.Fatalf("total = %d, want 3 after decrease", total)
	}
	if len(st.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(st.Events))
	}

	// The new baseline is 0, so the next increase counts from there.
	o.ObserveTriggers(testBase.Add(3*time.Minute), map[string]int{"r1": 2})
	if total := sumHours(o.Snapshot().Rules["r1"].Hours); total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
}

func TestObserverFirstSightingSetsFirstSeenWithoutTrigger(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 7})

	st, ok := o.Snapshot().Rules["r1"]
	if !ok {
		t.Fatal("rule r1 missing after first sighting")
	}
	if !st.FirstSeen.Equal(testBase) {
		t.Fatalf("FirstSeen = %v, want %v", st.FirstSeen, testBase)
	}
	if total := sumHours(st.Hours); total != 0 {
		t.Fatalf("first sighting triggered %d times, want 0", total)
	}
	if len(st.Events) != 0 {
		t.Fatalf("first sighting added %d events, want 0", len(st.Events))
	}
}

func TestObserverEventRingCapsAt200(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 0})
	for i := 1; i <= 250; i++ {
		at := testBase.Add(time.Duration(i) * time.Second)
		o.ObserveTriggers(at, map[string]int{"r1": i})
	}

	st := o.Snapshot().Rules["r1"]
	if len(st.Events) != 200 {
		t.Fatalf("events = %d, want 200", len(st.Events))
	}
	// The oldest 50 events are dropped, so the first kept one is the 51st.
	want := testBase.Add(51 * time.Second).Unix()
	if st.Events[0] != want {
		t.Fatalf("first event = %d, want %d", st.Events[0], want)
	}
	if last := st.Events[199]; last != testBase.Add(250*time.Second).Unix() {
		t.Fatalf("last event = %d, want the newest", last)
	}
}

func TestObserverDropsHoursOlderThan30Days(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 0})
	o.ObserveTriggers(testBase.Add(time.Minute), map[string]int{"r1": 2})

	later := testBase.Add(31 * 24 * time.Hour)
	o.ObserveTriggers(later, map[string]int{"r1": 3})

	st := o.Snapshot().Rules["r1"]
	if _, ok := st.Hours[testBase.Unix()/3600]; ok {
		t.Fatal("hour older than 30 days was kept")
	}
	if got := st.Hours[later.Unix()/3600]; got != 1 {
		t.Fatalf("current hour = %d, want 1", got)
	}
}

func TestObserverIgnoresClockBefore2020(t *testing.T) {
	o := newTestObserver()
	epoch := time.Unix(0, 0).UTC()
	o.ObserveTriggers(epoch, map[string]int{"r1": 1})
	o.ObserveTriggers(epoch.Add(time.Minute), map[string]int{"r1": 5})
	o.ObserveValues(epoch, map[string]float64{"k": 3})

	st := o.Snapshot()
	if len(st.Rules) != 0 || len(st.Values) != 0 {
		t.Fatalf("clock in 1970 collected data: %+v", st)
	}
}

func TestObserverValuesKeepDailyMinMax(t *testing.T) {
	o := newTestObserver()
	o.ObserveValues(testBase, map[string]float64{"k": 5})
	o.ObserveValues(testBase.Add(time.Hour), map[string]float64{"k": 2})
	o.ObserveValues(testBase.Add(2*time.Hour), map[string]float64{"k": 9})
	o.ObserveValues(testBase.Add(24*time.Hour), map[string]float64{"k": 1})

	ranges := o.Snapshot().Values["k"]
	if len(ranges) != 2 {
		t.Fatalf("ranges = %d, want 2", len(ranges))
	}
	if ranges[0].Min != 2 || ranges[0].Max != 9 {
		t.Fatalf("day 1 = %+v, want min 2 max 9", ranges[0])
	}
	if ranges[1].Min != 1 || ranges[1].Max != 1 {
		t.Fatalf("day 2 = %+v, want min 1 max 1", ranges[1])
	}
}

func TestObserverValuesDropDaysOlderThan30(t *testing.T) {
	o := newTestObserver()
	o.ObserveValues(testBase, map[string]float64{"k": 5})
	o.ObserveValues(testBase.Add(31*24*time.Hour), map[string]float64{"k": 6})

	ranges := o.Snapshot().Values["k"]
	if len(ranges) != 1 || ranges[0].Min != 6 {
		t.Fatalf("ranges = %+v, want only the current day", ranges)
	}
}

func TestObserverPruneRemovesForeignIDsAndKeys(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"keep": 1, "drop": 1})
	o.ObserveValues(testBase, map[string]float64{"k1": 1, "k2": 2})

	o.Prune(map[string]bool{"keep": true}, map[string]bool{"k1": true})

	st := o.Snapshot()
	if _, ok := st.Rules["drop"]; ok {
		t.Fatal("rule drop survived Prune")
	}
	if _, ok := st.Rules["keep"]; !ok {
		t.Fatal("rule keep was removed by Prune")
	}
	if _, ok := st.Values["k2"]; ok {
		t.Fatal("value key k2 survived Prune")
	}
	if _, ok := st.Values["k1"]; !ok {
		t.Fatal("value key k1 was removed by Prune")
	}
}

func TestObserverSnapshotIsACopy(t *testing.T) {
	o := newTestObserver()
	o.ObserveTriggers(testBase, map[string]int{"r1": 0})
	o.ObserveTriggers(testBase.Add(time.Minute), map[string]int{"r1": 1})
	snap := o.Snapshot()
	snap.Rules["r1"].Hours[testBase.Unix()/3600] = 99

	if got := sumHours(o.Snapshot().Rules["r1"].Hours); got != 1 {
		t.Fatalf("mutating the snapshot changed the observer: total %d", got)
	}
}

func sumHours(h map[int64]int) int {
	total := 0
	for _, n := range h {
		total += n
	}
	return total
}
