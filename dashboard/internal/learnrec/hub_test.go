package learnrec

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type fakeMinute struct{ frames []Frame }

func (f *fakeMinute) ObserveMinute(fr Frame) { f.frames = append(f.frames, fr) }

type fakeSaver struct {
	name  string
	calls int
	err   error
	order *[]string
}

func (s *fakeSaver) Save() error {
	s.calls++
	if s.order != nil {
		*s.order = append(*s.order, s.name)
	}
	return s.err
}

func hubDay(h, m, s int) time.Time {
	return time.Date(2026, 11, 20, h, m, s, 0, time.UTC)
}

func powerDev(id, uid, watts string) registry.DeviceView {
	return registry.DeviceView{ID: id, Entities: []registry.EntityView{{
		UniqueID: uid, Component: "sensor", Value: watts, HasValue: true, UnitOfMeasurement: "W",
	}}}
}

func switchDev(id, uid, value string) registry.DeviceView {
	return registry.DeviceView{ID: id, Entities: []registry.EntityView{{
		UniqueID: uid, Component: "switch", Value: value, HasValue: true,
		PayloadOn: "ON", PayloadOff: "OFF",
	}}}
}

func newTestHub(t *testing.T, limits Limits) *Hub {
	t.Helper()
	return NewHub(time.UTC, func() Limits { return limits }, func(f string, a ...any) { t.Logf(f, a...) })
}

func TestHubMinuteFrame(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	mc := &fakeMinute{}
	h.AddMinute("m", mc)
	for i := 0; i < 6; i++ {
		w := "100"
		if i >= 3 {
			w = "200"
		}
		h.Observe(hubDay(10, 0, i*10), []registry.DeviceView{powerDev("d", "e", w)}, energy.Snapshot{}, nil)
	}
	if len(mc.frames) != 0 {
		t.Fatalf("frames before minute change = %d, want 0", len(mc.frames))
	}
	h.Observe(hubDay(10, 1, 0), []registry.DeviceView{powerDev("d", "e", "100")}, energy.Snapshot{}, nil)
	if len(mc.frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(mc.frames))
	}
	f := mc.frames[0]
	if !f.Minute.Equal(hubDay(10, 0, 0)) {
		t.Errorf("Minute = %v, want 10:00", f.Minute)
	}
	if len(f.Devices) != 1 || f.Devices[0].ID != "d" {
		t.Fatalf("Devices = %+v, want one device d", f.Devices)
	}
	dm := f.Devices[0]
	if dm.Power["e"] != 150 {
		t.Errorf("Power[e] = %v, want 150", dm.Power["e"])
	}
	if !dm.Reachable {
		t.Error("Reachable = false, want true")
	}
}

func TestHubSwitchAtMinuteEnd(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	mc := &fakeMinute{}
	h.AddMinute("m", mc)
	h.Observe(hubDay(10, 0, 10), []registry.DeviceView{switchDev("d", "s", "ON")}, energy.Snapshot{}, nil)
	h.Observe(hubDay(10, 0, 50), []registry.DeviceView{switchDev("d", "s", "OFF")}, energy.Snapshot{}, nil)
	h.Observe(hubDay(10, 1, 0), nil, energy.Snapshot{}, nil)
	if len(mc.frames) != 1 || len(mc.frames[0].Devices) != 1 {
		t.Fatalf("frames = %+v, want one frame with one device", mc.frames)
	}
	on, ok := mc.frames[0].Devices[0].Switch["s"]
	if !ok {
		t.Fatal("Switch[s] missing")
	}
	if on {
		t.Error("Switch[s] = true, want false (off at minute end)")
	}
}

func TestHubMaxDevices(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 2})
	mc := &fakeMinute{}
	h.AddMinute("m", mc)
	h.Observe(hubDay(10, 0, 0), []registry.DeviceView{
		powerDev("c", "ec", "10"), powerDev("a", "ea", "10"), powerDev("b", "eb", "10"),
	}, energy.Snapshot{}, nil)
	h.Observe(hubDay(10, 1, 0), nil, energy.Snapshot{}, nil)
	if len(mc.frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(mc.frames))
	}
	got := mc.frames[0].Devices
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("Devices = %+v, want a and b", got)
	}
}

func TestHubSkipsNodeDevice(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	mc := &fakeMinute{}
	h.AddMinute("m", mc)
	h.Observe(hubDay(10, 0, 0), []registry.DeviceView{
		powerDev(energydiscovery.DeviceIdentifier, "n", "100"),
		powerDev("d", "e", "50"),
	}, energy.Snapshot{}, nil)
	h.Observe(hubDay(10, 1, 0), nil, energy.Snapshot{}, nil)
	if len(mc.frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(mc.frames))
	}
	got := mc.frames[0].Devices
	if len(got) != 1 || got[0].ID != "d" {
		t.Errorf("Devices = %+v, want only d", got)
	}
}

func TestHubBeforeClockReady(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	mc := &fakeMinute{}
	tc := &fakeTickRecorder{}
	h.AddMinute("m", mc)
	h.AddTick("t", tc)
	h.Observe(time.Date(1970, 1, 1, 10, 0, 0, 0, time.UTC), []registry.DeviceView{powerDev("d", "e", "10")}, energy.Snapshot{}, nil)
	h.Observe(time.Date(1970, 1, 1, 10, 1, 0, 0, time.UTC), nil, energy.Snapshot{}, nil)
	if len(mc.frames) != 0 {
		t.Errorf("frames = %d, want 0", len(mc.frames))
	}
	if len(tc.ats) != 0 {
		t.Errorf("ticks = %d, want 0", len(tc.ats))
	}
}

func TestHubSavesHourly(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	s := &fakeSaver{name: "s", err: errSave}
	h.AddSaver("s", s)

	for m := 0; m < 60; m++ {
		h.Observe(hubDay(10, m, 0), nil, energy.Snapshot{}, nil)
	}
	if s.calls != 0 {
		t.Fatalf("calls during 10:xx = %d, want 0", s.calls)
	}
	h.Observe(hubDay(11, 0, 0), nil, energy.Snapshot{}, nil)
	if s.calls != 1 {
		t.Fatalf("calls at 11:00 = %d, want 1", s.calls)
	}
	h.Observe(hubDay(11, 1, 0), nil, energy.Snapshot{}, nil)
	h.Observe(hubDay(11, 4, 59), nil, energy.Snapshot{}, nil)
	if s.calls != 1 {
		t.Fatalf("calls before retry at 11:04:59 = %d, want 1", s.calls)
	}
	s.err = nil
	h.Observe(hubDay(11, 5, 0), nil, energy.Snapshot{}, nil)
	if s.calls != 2 {
		t.Fatalf("calls at 11:05 = %d, want 2", s.calls)
	}
	h.Observe(hubDay(11, 6, 0), nil, energy.Snapshot{}, nil)
	if s.calls != 2 {
		t.Fatalf("calls after success at 11:06 = %d, want 2", s.calls)
	}
	h.Observe(hubDay(12, 0, 0), nil, energy.Snapshot{}, nil)
	if s.calls != 3 {
		t.Fatalf("calls at 12:00 = %d, want 3", s.calls)
	}
}

func TestHubTickPassThrough(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	var got []time.Time
	h.AddTick("t", TickFunc(func(at time.Time, _ energy.Snapshot) { got = append(got, at) }))
	times := []time.Time{hubDay(10, 0, 0), hubDay(10, 0, 30), hubDay(10, 1, 5)}
	for _, at := range times {
		h.Observe(at, nil, energy.Snapshot{}, nil)
	}
	if len(got) != len(times) {
		t.Fatalf("ticks = %d, want %d", len(got), len(times))
	}
	for i := range times {
		if !got[i].Equal(times[i]) {
			t.Errorf("tick %d at %v, want %v", i, got[i], times[i])
		}
	}
}

func TestHubCloseSavesInOrder(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	var order []string
	a := &fakeSaver{name: "a", order: &order}
	b := &fakeSaver{name: "b", order: &order}
	h.AddSaver("a", a)
	h.AddSaver("b", b)
	h.Close()
	if a.calls != 1 || b.calls != 1 {
		t.Errorf("calls a=%d b=%d, want 1 each", a.calls, b.calls)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("order = %v, want [a b]", order)
	}
}

func TestHubStatsRecordsSave(t *testing.T) {
	h := newTestHub(t, Limits{MaxDevices: 128})
	h.AddSaver("s", &fakeSaver{name: "s"})
	if _, ok := h.Stats()["s"]; ok {
		t.Fatal("Stats has entry before any save")
	}
	h.Close()
	d, ok := h.Stats()["s"]
	if !ok {
		t.Fatal("Stats missing entry for s after Close")
	}
	if d < 0 {
		t.Errorf("duration = %v, want >= 0", d)
	}
}

type fakeTickRecorder struct{ ats []time.Time }

func (f *fakeTickRecorder) ObserveTick(at time.Time, _ energy.Snapshot) { f.ats = append(f.ats, at) }

var errSave = errTest("disk full")

type errTest string

func (e errTest) Error() string { return string(e) }
