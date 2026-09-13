package hostapi_test

import (
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestPublishNumbersEventsFromOne(t *testing.T) {
	bus := hostapi.NewBus(10)
	first := bus.Publish("step", map[string]string{"id": "10"})
	second := bus.Publish("step", map[string]string{"id": "20"})
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq = %d,%d; want 1,2", first.Seq, second.Seq)
	}
	if bus.Seq() != 2 {
		t.Errorf("Seq() = %d, want 2", bus.Seq())
	}
}

func TestSinceReplaysEverythingAfterTheGivenSequence(t *testing.T) {
	bus := hostapi.NewBus(10)
	for _, id := range []string{"10", "20", "30"} {
		bus.Publish("step", map[string]string{"id": id})
	}
	got := bus.Since(1)
	if len(got) != 2 || got[0].Seq != 2 || got[1].Seq != 3 {
		t.Fatalf("Since(1) = %+v, want the events 2 and 3", got)
	}
	if len(bus.Since(0)) != 3 {
		t.Errorf("Since(0) must replay everything the bus still keeps")
	}
	if len(bus.Since(3)) != 0 {
		t.Errorf("Since(latest) must be empty")
	}
}

func TestTheBusForgetsBeyondItsKeepLimit(t *testing.T) {
	bus := hostapi.NewBus(2)
	for _, id := range []string{"10", "20", "30"} {
		bus.Publish("step", map[string]string{"id": id})
	}
	got := bus.Since(0)
	if len(got) != 2 || got[0].Seq != 2 {
		t.Fatalf("Since(0) = %+v, want only the last two events", got)
	}
}

func TestRestoreBusContinuesTheSequence(t *testing.T) {
	bus := hostapi.RestoreBus([]hostapi.Event{
		{Seq: 41, Type: "step", Data: map[string]string{"id": "10"}},
		{Seq: 42, Type: "step", Data: map[string]string{"id": "20"}},
	}, 10)
	if bus.Seq() != 42 {
		t.Fatalf("Seq() = %d, want the highest restored sequence", bus.Seq())
	}
	next := bus.Publish("step", map[string]string{"id": "30"})
	if next.Seq != 43 {
		t.Errorf("the first event after a restore has seq %d, want 43", next.Seq)
	}
	if len(bus.Since(41)) != 2 {
		t.Errorf("a restored bus must be able to replay what it was given")
	}
}

func TestSubscribeDeliversNewEventsAndTheBacklog(t *testing.T) {
	bus := hostapi.NewBus(10)
	bus.Publish("step", map[string]string{"id": "10"})

	events, cancel := bus.Subscribe(0)
	defer cancel()

	if first := <-events; first.Seq != 1 {
		t.Fatalf("the backlog must arrive first, got seq %d", first.Seq)
	}
	bus.Publish("step", map[string]string{"id": "20"})
	if second := <-events; second.Seq != 2 {
		t.Fatalf("got seq %d, want the freshly published event", second.Seq)
	}
}

func TestCancelStopsDeliveryAndClosesTheChannel(t *testing.T) {
	bus := hostapi.NewBus(10)
	events, cancel := bus.Subscribe(0)
	cancel()
	if _, open := <-events; open {
		t.Fatalf("the channel must be closed after cancel")
	}
	// Ein Publish nach dem Abbestellen darf nicht blockieren und nicht panisch
	// in einen geschlossenen Kanal schreiben.
	bus.Publish("step", map[string]string{"id": "10"})
}

func TestASlowSubscriberIsDroppedInsteadOfBlockingTheRun(t *testing.T) {
	bus := hostapi.NewBus(1000)
	events, cancel := bus.Subscribe(0)
	defer cancel()
	for i := 0; i < hostapi.SubscriberBuffer+5; i++ {
		bus.Publish("log", map[string]string{"line": "x"})
	}
	drained := 0
	for range events {
		drained++
	}
	if drained > hostapi.SubscriberBuffer {
		t.Fatalf("drained %d events, want at most the buffer size", drained)
	}
}
