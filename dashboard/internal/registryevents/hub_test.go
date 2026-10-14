package registryevents

import (
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTopic ist ein Thema mit steuerbarer Version. Es zaehlt seine Baulaeufe,
// damit sichtbar wird, ob der Hub die Arbeit an die Zahl der Aenderungen
// bindet und nicht an die Zahl der Abonnenten.
type fakeTopic struct {
	name    string
	version atomic.Uint64
	builds  atomic.Int64
	fail    atomic.Bool
}

func (f *fakeTopic) topic() Topic {
	return Topic{
		Name:    f.name,
		Version: func() Version { return Version{f.version.Load()} },
		Build: func(v Version) (Part, bool) {
			f.builds.Add(1)
			if f.fail.Load() {
				return Part{}, false
			}
			n := strconv.FormatUint(v[0], 10)
			return Part{
				Full:  []byte(`"` + f.name + `":"full` + n + `"`),
				Delta: []byte(`"` + f.name + `":"delta` + n + `"`),
				Idle:  []byte(`"` + f.name + `":"idle` + n + `"`),
			}, true
		},
	}
}

type fixture struct {
	head atomic.Uint64
	a, b *fakeTopic
	hub  *Hub
}

func newFixture(queueSize int) *fixture {
	f := &fixture{a: &fakeTopic{name: "a"}, b: &fakeTopic{name: "b"}}
	f.hub = NewHub(queueSize, f.head.Load, []Topic{f.a.topic(), f.b.topic()}, []string{"a", "b"},
		func() time.Duration { return 10 * time.Millisecond })
	return f
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

func nothing(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case payload := <-ch:
		t.Fatalf("unerwartete Nachricht: %s", payload)
	case <-time.After(30 * time.Millisecond):
	}
}

func decode(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("Rumpf ist kein JSON: %v (%s)", err, payload)
	}
	return body
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

func TestThemaOhneAbonnentWirdNichtGebaut(t *testing.T) {
	f := newFixture(4)
	_, _, cancel := f.hub.Subscribe([]string{"a"})
	defer cancel()
	f.a.version.Store(2)
	f.b.version.Store(2)
	f.hub.poll()
	if got := f.b.builds.Load(); got != 0 {
		t.Errorf("b ohne Abonnent gebaut: %d", got)
	}
	if got := f.a.builds.Load(); got != 2 {
		t.Errorf("a builds = %d, want 2 (Subscribe + Versionswechsel)", got)
	}
}

func TestThemaWirdEinmalJeVersionGebaut(t *testing.T) {
	f := newFixture(4)
	_, _, c1 := f.hub.Subscribe([]string{"a"})
	defer c1()
	_, _, c2 := f.hub.Subscribe([]string{"a", "b"})
	defer c2()
	f.hub.poll()
	f.hub.poll()
	if got := f.a.builds.Load(); got != 1 {
		t.Errorf("a builds bei gleicher Version = %d, want 1", got)
	}
	f.a.version.Store(5)
	f.hub.poll()
	f.hub.poll()
	if got := f.a.builds.Load(); got != 2 {
		t.Errorf("a builds nach einem Versionswechsel = %d, want 2", got)
	}
}

func TestNeuerAbonnentBekommtVollenRumpfSeinerThemen(t *testing.T) {
	f := newFixture(4)
	f.head.Store(7)
	latest, _, cancel := f.hub.Subscribe([]string{"b"})
	defer cancel()
	body := decode(t, latest)
	if body["version"] != float64(7) || body["b"] != "full0" {
		t.Errorf("Rumpf = %v", body)
	}
	if _, ok := body["a"]; ok {
		t.Errorf("Rumpf traegt nicht abonniertes Thema a: %v", body)
	}
}

func TestVerbundenerAbonnentBekommtDeltaUndLeerlauf(t *testing.T) {
	f := newFixture(4)
	_, ch, cancel := f.hub.Subscribe([]string{"a", "b"})
	defer cancel()
	f.a.version.Store(3)
	f.hub.poll()
	body := decode(t, receive(t, ch))
	if body["a"] != "delta3" {
		t.Errorf("a = %v, want delta3", body["a"])
	}
	// b hat sich nicht geaendert, traegt aber seinen Leerlauf-Teil: ein
	// fehlender Zweig liesse den Client tauschen.
	if body["b"] != "idle0" {
		t.Errorf("b = %v, want idle0", body["b"])
	}
}

func TestAbonnentOhneGeaendertesThemaBekommtNichts(t *testing.T) {
	f := newFixture(4)
	_, chA, cancelA := f.hub.Subscribe([]string{"a"})
	defer cancelA()
	_, chB, cancelB := f.hub.Subscribe([]string{"b"})
	defer cancelB()
	f.b.version.Store(4)
	f.hub.poll()
	if body := decode(t, receive(t, chB)); body["b"] != "delta4" {
		t.Errorf("b = %v, want delta4", body["b"])
	}
	nothing(t, chA)
}

// Automationen, Device Map und Energie-Anlage haengen am Takt von
// registry-updated. Ein Client ohne Themen bekommt darum jeden Wechsel der
// Registry-Version als nackte Version.
func TestKopfwechselOhneThemenSchicktNurVersion(t *testing.T) {
	f := newFixture(4)
	latest, ch, cancel := f.hub.Subscribe([]string{})
	defer cancel()
	if string(latest) != `{"version":0}` {
		t.Errorf("latest = %s", latest)
	}
	f.head.Store(9)
	f.hub.poll()
	if got := string(receive(t, ch)); got != `{"version":9}` {
		t.Errorf("Rumpf = %s, want {\"version\":9}", got)
	}
	if f.a.builds.Load() != 0 || f.b.builds.Load() != 0 {
		t.Error("ohne Abonnenten wurde ein Thema gebaut")
	}
}

func TestNilHeisstStandardthemenUnbekannteWerdenIgnoriert(t *testing.T) {
	f := newFixture(4)
	latest, _, cancel := f.hub.Subscribe(nil)
	defer cancel()
	body := decode(t, latest)
	if body["a"] != "full0" || body["b"] != "full0" {
		t.Errorf("Standardrumpf = %v", body)
	}
	other, _, cancelOther := f.hub.Subscribe([]string{"x", "a"})
	defer cancelOther()
	if body := decode(t, other); body["a"] != "full0" || len(body) != 2 {
		t.Errorf("Rumpf mit unbekanntem Thema = %v", body)
	}
}

func TestFehlgeschlagenerBauLaesstZweigWegUndWirdWiederholt(t *testing.T) {
	f := newFixture(4)
	_, ch, cancel := f.hub.Subscribe([]string{"a", "b"})
	defer cancel()
	f.a.fail.Store(true)
	f.a.version.Store(2)
	f.hub.poll()
	body := decode(t, receive(t, ch))
	if _, ok := body["a"]; ok {
		t.Errorf("gescheitertes Thema steht im Rumpf: %v", body)
	}
	f.a.fail.Store(false)
	f.hub.poll()
	if body := decode(t, receive(t, ch)); body["a"] != "delta2" {
		t.Errorf("Wiederholung lieferte a = %v, want delta2", body["a"])
	}
}

func TestComposeSetztMembersZusammen(t *testing.T) {
	got := string(Compose(3, []byte(`"a":1`), nil, []byte(`"b":{}`)))
	if got != `{"version":3,"a":1,"b":{}}` {
		t.Errorf("Compose = %s", got)
	}
}

func TestLangsamerAbonnentWirdGetrennt(t *testing.T) {
	f := newFixture(1)
	_, ch, cancel := f.hub.Subscribe(nil)
	defer cancel()
	// Der Abonnent liest nicht. Der erste Push fuellt den Puffer, der
	// zweite laeuft ueber - dann wird getrennt statt unbegrenzt gepuffert.
	f.a.version.Store(2)
	f.hub.poll()
	f.a.version.Store(3)
	f.hub.poll()
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
	f := newFixture(4)
	if f.hub.Running() {
		t.Fatal("Loop laeuft ohne Abonnenten")
	}
	_, _, cancelA := f.hub.Subscribe(nil)
	eventually(t, true, f.hub.Running)
	_, _, cancelB := f.hub.Subscribe(nil)
	cancelA()
	if !f.hub.Running() {
		t.Error("Loop haelt an, obwohl noch ein Abonnent verbunden ist")
	}
	cancelB()
	eventually(t, false, f.hub.Running)
}

func TestLoopVerteiltVersionswechselOhneDirektenPoll(t *testing.T) {
	f := newFixture(4)
	_, ch, cancel := f.hub.Subscribe([]string{"a"})
	defer cancel()
	f.a.version.Store(9)
	// Kein poll() - der Hintergrund-Loop muss die Aenderung selbst aufgreifen.
	if body := decode(t, receive(t, ch)); body["a"] != "delta9" {
		t.Errorf("Rumpf vom Loop = %v, want a=delta9", body)
	}
}

func TestCancelIstMehrfachAufrufbar(t *testing.T) {
	f := newFixture(4)
	_, _, cancel := f.hub.Subscribe(nil)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); cancel() }()
	}
	wg.Wait()
	if f.hub.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount nach cancel = %d, want 0", f.hub.SubscriberCount())
	}
}
