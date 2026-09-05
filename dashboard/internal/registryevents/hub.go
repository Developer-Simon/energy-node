// Package registryevents faechert einen gemeinsam gebauten SSE-Rumpf an
// alle offenen /api/v1/events-Verbindungen. Bis hierher fuehrte jede
// Verbindung ihren eigenen time.Ticker und las pro Tick die Einstellungen -
// die Grundlast skalierte mit der Zahl offener Tabs, nicht mit der Zahl der
// Aenderungen. Der Hub ersetzt das durch genau einen Poll-Loop: er liest
// reg.Version() im interval()-Takt und baut den Rumpf einmal je Aenderung
// fuer alle Abonnenten.
//
// Der Loop laeuft nur, solange mindestens ein Abonnent verbunden ist. Faellt
// der letzte weg, haelt er an - ein Dashboard ohne offene Tabs kostet nichts.
package registryevents

import (
	"sync"
	"time"
)

// fallbackInterval greift nur, wenn interval() einen nicht-positiven Wert
// liefert: time.NewTicker panict bei <= 0, und ein fehlkonfigurierter Wert
// soll nicht den Prozess mitnehmen.
const fallbackInterval = time.Second

// Hub verteilt den Rumpf an die Abonnenten. version, build und interval
// werden injiziert, damit das Paket frei von registry-, energy-,
// diagnostics- und settings-Abhaengigkeiten bleibt.
type Hub struct {
	queueSize int
	version   func() uint64
	build     func(version uint64) (full, delta []byte)
	interval  func() time.Duration

	// pollMu serialisiert poll(): Subscribe() ruft es direkt, der Loop
	// ruft es getaktet - ohne die Sperre koennten beide denselben
	// Versionswechsel doppelt bauen.
	pollMu sync.Mutex

	mu          sync.Mutex
	subs        map[int]chan []byte
	nextID      int
	latest      []byte
	lastVersion uint64
	loopRunning bool
	stopLoop    chan struct{}
}

// NewHub baut den Hub, startet aber noch keinen Loop - das uebernimmt der
// erste Subscribe(). queueSize ist der Vorlauf je Abonnent; laeuft er ueber,
// wird der Abonnent getrennt (siehe deliverLocked).
func NewHub(queueSize int, version func() uint64, build func(version uint64) (full, delta []byte), interval func() time.Duration) *Hub {
	if queueSize < 1 {
		queueSize = 1
	}
	return &Hub{
		queueSize: queueSize,
		version:   version,
		build:     build,
		interval:  interval,
		subs:      map[int]chan []byte{},
	}
}

// Subscribe meldet eine Verbindung an. Rueckgabe: der zuletzt gebaute Rumpf
// (nil, wenn noch nie gebaut wurde), der Lesekanal und die - mehrfach
// aufrufbare - Abmeldefunktion. Beim ersten Abonnenten startet der
// Poll-Loop.
//
// Der neue Abonnent bekommt den aktuellen Rumpf ueber den Rueckgabewert,
// nicht ueber den Kanal - der Kanal traegt nur spaetere Aenderungen. Damit
// zwischen "Rumpf lesen" und "Kanal registrieren" keine Aenderung
// durchrutscht, laeuft beides unter pollMu.
func (h *Hub) Subscribe() (latest []byte, ch <-chan []byte, cancel func()) {
	h.pollMu.Lock()
	// Bringt latest auf Stand und verteilt einen etwaigen Versionswechsel
	// an die bereits verbundenen Abonnenten - bei unveraenderter Version
	// ein billiger No-op.
	h.refresh()

	h.mu.Lock()
	id := h.nextID
	h.nextID++
	out := make(chan []byte, h.queueSize)
	h.subs[id] = out
	latest = h.latest
	h.startLoopLocked()
	h.mu.Unlock()

	h.pollMu.Unlock()

	var once sync.Once
	return latest, out, func() { once.Do(func() { h.remove(id) }) }
}

func (h *Hub) remove(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(id)
	if len(h.subs) == 0 {
		h.stopLoopLocked()
	}
}

func (h *Hub) removeLocked(id int) {
	ch, ok := h.subs[id]
	if !ok {
		return
	}
	delete(h.subs, id)
	close(ch)
}

// deliverLocked schiebt den Rumpf in den Kanal eines Abonnenten. Laeuft der
// Kanal ueber, wird der Abonnent getrennt: seine EventSource verbindet neu
// und beginnt mit einem frischen Rumpf. Puffern statt trennen liesse den
// Speicherbedarf des Hubs unbeschraenkt wachsen.
func (h *Hub) deliverLocked(id int, ch chan []byte, payload []byte) {
	select {
	case ch <- payload:
	default:
		h.removeLocked(id)
	}
}

// poll fuehrt genau eine Iteration aus: hat sich die Version geaendert (oder
// gibt es noch keinen Rumpf), wird einmal gebaut und an alle Abonnenten
// verteilt. Der Loop ruft das getaktet, Tests rufen es direkt.
func (h *Hub) poll() {
	h.pollMu.Lock()
	defer h.pollMu.Unlock()
	h.refresh()
}

// refresh baut den Rumpf neu, wenn sich die Version geaendert hat, und
// verteilt ihn an alle Abonnenten. Erwartet, dass der Aufrufer pollMu
// haelt - so bauen der Loop und ein gleichzeitiges Subscribe() denselben
// Versionswechsel nicht doppelt.
func (h *Hub) refresh() {
	version := h.version()

	h.mu.Lock()
	// latest == nil heisst "noch nie gebaut" oder "letzter Bau schlug
	// fehl" - beides soll beim naechsten Durchlauf erneut versucht werden.
	fresh := version == h.lastVersion && h.latest != nil
	h.mu.Unlock()
	if fresh {
		return
	}

	// Der Bau laeuft bewusst ohne h.mu - er ist der teure Teil, und
	// Subscribe()/remove() sollen darauf nicht warten.
	full, delta := h.build(version)

	h.mu.Lock()
	h.latest = full
	h.lastVersion = version
	for id, ch := range h.subs {
		h.deliverLocked(id, ch, delta)
	}
	h.mu.Unlock()
}

func (h *Hub) startLoopLocked() {
	if h.loopRunning {
		return
	}
	h.loopRunning = true
	stop := make(chan struct{})
	h.stopLoop = stop
	go h.runLoop(stop)
}

func (h *Hub) stopLoopLocked() {
	if !h.loopRunning {
		return
	}
	h.loopRunning = false
	close(h.stopLoop)
	h.stopLoop = nil
}

func (h *Hub) runLoop(stop <-chan struct{}) {
	current := h.tickInterval()
	ticker := time.NewTicker(current)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if next := h.tickInterval(); next != current {
				current = next
				ticker.Reset(current)
			}
			h.poll()
		}
	}
}

func (h *Hub) tickInterval() time.Duration {
	d := h.interval()
	if d <= 0 {
		return fallbackInterval
	}
	return d
}

// Running meldet, ob der Poll-Loop gerade laeuft. Nur fuer Tests und
// Diagnose gedacht.
func (h *Hub) Running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loopRunning
}

// SubscriberCount ist die Zahl der aktuell verbundenen Abonnenten.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Version reicht die aktuelle Registry-Version durch - der SSE-Handler
// braucht sie fuer den Rueckfall, wenn kein Rumpf gebaut werden konnte.
func (h *Hub) Version() uint64 { return h.version() }
