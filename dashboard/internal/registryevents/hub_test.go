package registryevents

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource haelt eine steuerbare Version und zaehlt, wie oft der Rumpf
// gebaut wurde - so wird sichtbar, ob der Hub die Arbeit an die Zahl der
// Aenderungen bindet und nicht an die Zahl der Abonnenten.
type fakeSource struct {
	version atomic.Uint64
	builds  atomic.Int64
	full    []byte
	delta   []byte
}

func (f *fakeSource) build(version uint64) (full, delta []byte) {
	f.builds.Add(1)
	full = f.full
	delta = f.delta
	if full == nil {
		full = []byte("v" + strconv.FormatUint(version, 10))
	}
	if delta == nil {
		delta = full
	}
	return full, delta
}

func newTestHub(queueSize int, src *fakeSource) *Hub {
	return NewHub(queueSize, src.version.Load, src.build, func() time.Duration { return 10 * time.Millisecond })
}

func receive(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case payload, open := <-ch:
		if !open {
			t.Fatal("Kanal ist geschlossen")
		}
		return payload
	case <-time.After(time.Second):
		t.Fatal("keine Nachricht erhalten")
		return nil
	}
}

func eventually(t *testing.T, want bool, get func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if get() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("Bedingung nicht innerhalb der Frist erreicht (want %v)", want)
}

func TestPollBautRumpfEinmalProVersion(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)
	_, _, cancel := hub.Subscribe()
	defer cancel()

	// Subscribe hat einmal gebaut. Weitere Polls ohne Versionswechsel
	// bauen nicht neu.
	if got := src.builds.Load(); got != 1 {
		t.Fatalf("builds nach Subscribe = %d, want 1", got)
	}
	hub.poll()
	hub.poll()
	if got := src.builds.Load(); got != 1 {
		t.Errorf("builds nach zwei Polls ohne Aenderung = %d, want 1", got)
	}

	src.version.Store(2)
	hub.poll()
	if got := src.builds.Load(); got != 2 {
		t.Errorf("builds nach Versionswechsel = %d, want 2", got)
	}
}

func TestSubscriberErhaeltRumpfBeiVersionswechsel(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)
	_, ch, cancel := hub.Subscribe()
	defer cancel()

	src.version.Store(5)
	hub.poll()

	if got := string(receive(t, ch)); got != "v5" {
		t.Errorf("Rumpf = %s, want v5", got)
	}
}

func TestZweiterSubscriberTeiltDenselbenRumpf(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)
	firstLatest, _, cancelFirst := hub.Subscribe()
	defer cancelFirst()
	secondLatest, _, cancelSecond := hub.Subscribe()
	defer cancelSecond()

	if string(firstLatest) != string(secondLatest) {
		t.Errorf("Abonnenten sehen verschiedene Ruempfe: %s vs %s", firstLatest, secondLatest)
	}
	if got := src.builds.Load(); got != 1 {
		t.Errorf("builds fuer zwei Abonnenten bei gleicher Version = %d, want 1", got)
	}
}

func TestLangsamerAbonnentWirdGetrennt(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(1, src)
	_, ch, cancel := hub.Subscribe()
	defer cancel()

	// Der Abonnent liest nicht. Der erste Push fuellt den Puffer, der
	// zweite laeuft ueber - dann wird getrennt statt unbegrenzt gepuffert.
	src.version.Store(2)
	hub.poll()
	src.version.Store(3)
	hub.poll()

	drained := false
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
			if drained {
				t.Fatal("Kanal liefert weiter, obwohl er haette geschlossen sein muessen")
			}
			drained = true
		case <-time.After(time.Second):
			t.Fatal("Kanal wurde nicht geschlossen")
		}
	}
}

func TestLoopLaeuftNurMitAbonnenten(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)

	if hub.Running() {
		t.Fatal("Loop laeuft ohne Abonnenten")
	}

	_, _, cancelA := hub.Subscribe()
	eventually(t, true, hub.Running)

	_, _, cancelB := hub.Subscribe()
	cancelA()
	if !hub.Running() {
		t.Error("Loop haelt an, obwohl noch ein Abonnent verbunden ist")
	}

	cancelB()
	eventually(t, false, hub.Running)
}

func TestLoopVerteiltVersionswechselOhneDirektenPoll(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)
	_, ch, cancel := hub.Subscribe()
	defer cancel()

	src.version.Store(9)

	// Kein hub.poll() - der Hintergrund-Loop muss die Aenderung im
	// interval()-Takt selbst aufgreifen.
	if got := string(receive(t, ch)); got != "v9" {
		t.Errorf("Rumpf vom Loop = %s, want v9", got)
	}
}

func TestCancelIstMehrfachAufrufbar(t *testing.T) {
	src := &fakeSource{}
	hub := newTestHub(4, src)
	_, _, cancel := hub.Subscribe()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); cancel() }()
	}
	wg.Wait()
	if hub.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount nach cancel = %d, want 0", hub.SubscriberCount())
	}
}

// Ein spaet dazukommender Abonnent bekommt den vollen Rumpf ueber den
// Rueckgabewert, ein bereits verbundener den Delta ueber den Kanal.
func TestHubFullToNewcomerDeltaToExisting(t *testing.T) {
	src := &fakeSource{}
	src.full = []byte("FULL")
	src.delta = []byte("DELTA")
	hub := newTestHub(4, src)

	latestA, chA, cancelA := hub.Subscribe()
	defer cancelA()
	if string(latestA) != "FULL" {
		t.Fatalf("erster Abonnent latest = %q, want FULL", latestA)
	}

	src.version.Store(1)
	select {
	case got := <-chA:
		if string(got) != "DELTA" {
			t.Errorf("bestehender Abonnent bekam %q, want DELTA", got)
		}
	case <-time.After(time.Second):
		t.Fatal("bestehender Abonnent bekam nichts")
	}

	latestB, _, cancelB := hub.Subscribe()
	defer cancelB()
	if string(latestB) != "FULL" {
		t.Errorf("spaeter Abonnent latest = %q, want FULL", latestB)
	}
}
