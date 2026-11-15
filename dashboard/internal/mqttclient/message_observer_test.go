package mqttclient

import (
	"context"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestMessageObserverSeesEveryStateMessage(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)

	type seen struct {
		topic string
		at    time.Time
	}
	var got []seen
	client.SetMessageObserver(func(topic string, at time.Time) {
		got = append(got, seen{topic, at})
	})

	before := time.Now()
	client.handleState(nil, fakeMessage{topic: "outstation/shelly/state", payload: []byte("1")})
	client.handleState(nil, fakeMessage{topic: "outstation/shelly/state", payload: []byte("1")})

	if len(got) != 2 {
		t.Fatalf("observer calls = %d, want 2 (also for unchanged payloads)", len(got))
	}
	if got[0].topic != "outstation/shelly/state" {
		t.Fatalf("observer topic = %q", got[0].topic)
	}
	if got[0].at.Before(before) {
		t.Fatalf("observer time %v is before the call", got[0].at)
	}
}

func TestMessageObserverNilIsSafe(t *testing.T) {
	client := NewWithContext(context.Background(), Config{}, registry.New(), nil)
	client.handleState(nil, fakeMessage{topic: "outstation/shelly/state", payload: []byte("1")})
}
