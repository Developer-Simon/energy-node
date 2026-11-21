package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// fakePublisher records the publishes and fails while fail is set.
type fakePublisher struct {
	fail bool
	sent []string
}

func (f *fakePublisher) PublishRetained(topic, payload string) error {
	if f.fail {
		return errDisconnected
	}
	f.sent = append(f.sent, topic)
	return nil
}

var errDisconnected = errors.New("mqtt disconnected")

// learnedEngine is an engine that learns one usual_off value for stehlampe.
func learnedEngine(t *testing.T) *autolearn.Engine {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	e := autolearn.NewEngine(nil, nil, nil,
		func() map[string]autolearn.DeviceInfo { return map[string]autolearn.DeviceInfo{} },
		func() learnrec.Limits { return learnrec.LimitsFor(learnrec.LevelBalanced, 4) }, loc)
	e.Register(autolearn.Detector{Name: "learn_fake", Group: "habit", Light: true,
		Run: func(ctx context.Context, in *autolearn.Input, device string) []autolearn.Candidate {
			return []autolearn.Candidate{{Value: &autolearn.Value{Key: "device/stehlampe/usual_off",
				Device: "stehlampe", Kind: autolearn.ValueTime, Group: "habit", Text: "22:30", N: 12}}}
		}})
	return e
}

func TestPublishLearnedHabitOffForEightDaysSendsNothing(t *testing.T) {
	store := autolearn.LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	engine := learnedEngine(t)
	client := &fakePublisher{}
	exists := func(string) bool { return true }
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	for h := 0; h < 8*24; h++ {
		now := start.Add(time.Duration(h) * time.Hour)
		engine.Tick(now, map[string]bool{"habit": true})
		publishLearned(store, engine.Values, client, false, exists, now)
	}
	if len(client.sent) != 0 || len(store.Entries()) != 0 {
		t.Fatalf("habit off sent %v, entries %+v; want nothing", client.sent, store.Entries())
	}
}

func TestPublishLearnedFailedPublishIsRetriedNextRun(t *testing.T) {
	store := autolearn.LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	engine := learnedEngine(t)
	client := &fakePublisher{fail: true}
	exists := func(string) bool { return true }
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	engine.Tick(now, map[string]bool{"habit": true})
	publishLearned(store, engine.Values, client, true, exists, now)
	if len(client.sent) != 0 {
		t.Fatalf("sent while disconnected: %v", client.sent)
	}
	client.fail = false
	next := now.Add(time.Minute)
	engine.Tick(next, map[string]bool{"habit": true})
	publishLearned(store, engine.Values, client, true, exists, next)
	want := "outstation/energy_node/learned/device/stehlampe/usual_off"
	if len(client.sent) != 1 || client.sent[0] != want {
		t.Fatalf("next run sent %v, want %s once", client.sent, want)
	}
}
