// Package registryevents faechert die SSE-Ruempfe an alle offenen
// /api/v1/events-Verbindungen. Ein einziger Poll-Loop beobachtet die
// Versionen aller abonnierten Themen und baut jedes Thema einmal je
// Aenderung fuer alle Abonnenten. Jeder Abonnent bekommt einen Rumpf, der
// nur seine Themen traegt.
//
// Der Loop laeuft nur, solange mindestens ein Abonnent verbunden ist. Faellt
// der letzte weg, haelt er an - ein Dashboard ohne offene Tabs kostet nichts.
package registryevents

import (
	"bytes"
	"strconv"
	"sync"
	"time"
)

// fallbackInterval greift nur, wenn interval() einen nicht-positiven Wert
// liefert: time.NewTicker panict bei <= 0, und ein fehlkonfigurierter Wert
// soll nicht den Prozess mitnehmen.
const fallbackInterval = time.Second

// Version ist der Stand der Quelle eines Themas. Zwei Zahlen, weil die
// Registry-Themen zusaetzlich an der Generation der Geraete-Einstellungen
// haengen.
type Version [2]uint64

// Mode sagt, fuer wen ein Teil eines Themas gedacht ist.
type Mode int

const (
	// ModeFull: ein neu verbundener Abonnent, er braucht den ganzen Stand.
	ModeFull Mode = iota
	// ModeDelta: das Thema hat sich seit dem letzten Rumpf geaendert.
	ModeDelta
	// ModeIdle: nur ein anderes Thema hat sich geaendert. Der Zweig muss
	// trotzdem im Rumpf stehen, sonst tauscht der Client.
	ModeIdle
)

// Part ist der Beitrag eines Themas zum Rumpf: JSON-Member ohne Klammern,
// etwa `"energy":{...}` oder `"entities":{...},"entities_delta":true`. Ein
// leerer Teil fehlt im Rumpf.
type Part struct {
	Full, Delta, Idle []byte
	// Data ist der gebaute Stand fuer Topic.Select. Ohne Select ungenutzt.
	Data any
}

// Bytes liefert den Teil fuer mode.
func (p Part) Bytes(mode Mode) []byte {
	switch mode {
	case ModeDelta:
		return p.Delta
	case ModeIdle:
		return p.Idle
	default:
		return p.Full
	}
}

// Topic ist ein abonnierbares Thema. Build wird nur fuer eine neue Version
// gerufen und darf bei gleicher Version denselben Teil liefern. ok=false
// heisst "Bau gescheitert": der Zweig fehlt im naechsten Rumpf, der naechste
// Poll versucht es erneut.
type Topic struct {
	Name    string
	Version func() Version
	Build   func(Version) (Part, bool)
	// Select baut den Teil je Abonnent aus part.Data, wenn gesetzt. selection
	// ist die Auswahl, mit der sich der Abonnent angemeldet hat
	// (SubscribeWith). Full, Delta und Idle des Teils sind dann ungenutzt.
	Select func(part Part, mode Mode, selection string) []byte
}

// Compose setzt einen Rumpf aus der Registry-Version und den Members
// zusammen. Leere Members fallen weg.
func Compose(head uint64, members ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteString(`{"version":`)
	b.WriteString(strconv.FormatUint(head, 10))
	for _, member := range members {
		if len(member) == 0 {
			continue
		}
		b.WriteByte(',')
		b.Write(member)
	}
	b.WriteByte('}')
	return b.Bytes()
}

type subscriber struct {
	ch        chan []byte
	mask      uint64
	selection string
}

type topicState struct {
	built   bool
	version Version
	part    Part
}

// Hub verteilt die Ruempfe an die Abonnenten. head, die Themen und interval
// werden injiziert, damit das Paket frei von registry-, energy-,
// diagnostics- und settings-Abhaengigkeiten bleibt.
type Hub struct {
	queueSize int
	head      func() uint64
	topics    []Topic
	defaults  uint64
	interval  func() time.Duration

	// pollMu serialisiert refresh(): Subscribe() ruft es direkt, der Loop
	// getaktet. Es schuetzt auch state, lastHead und headSeen.
	pollMu   sync.Mutex
	state    []topicState
	lastHead uint64
	headSeen bool

	mu          sync.Mutex
	subs        map[int]*subscriber
	nextID      int
	loopRunning bool
	stopLoop    chan struct{}
}

// NewHub baut den Hub, startet aber noch keinen Loop - das uebernimmt der
// erste Subscribe(). head ist die Registry-Version: sie steht in jedem Rumpf,
// und ihr Wechsel erreicht jeden Abonnenten, auch einen ohne Themen.
// defaults sind die Themen eines Abonnenten ohne Auswahl. queueSize ist der
// Vorlauf je Abonnent; laeuft er ueber, wird der Abonnent getrennt.
func NewHub(queueSize int, head func() uint64, topics []Topic, defaults []string, interval func() time.Duration) *Hub {
	if queueSize < 1 {
		queueSize = 1
	}
	if len(topics) > 64 {
		panic("registryevents: hoechstens 64 Themen")
	}
	h := &Hub{
		queueSize: queueSize,
		head:      head,
		topics:    topics,
		interval:  interval,
		state:     make([]topicState, len(topics)),
		subs:      map[int]*subscriber{},
	}
	h.defaults = h.maskOf(defaults)
	return h
}

func (h *Hub) maskOf(names []string) uint64 {
	var mask uint64
	for _, name := range names {
		for i, topic := range h.topics {
			if topic.Name == name {
				mask |= uint64(1) << i
			}
		}
	}
	return mask
}

// Subscribe meldet eine Verbindung an. topics nil heisst Standardthemen, eine
// leere Liste heisst keine Themen, unbekannte Namen fallen weg. Rueckgabe:
// der volle Rumpf fuer diese Themen, der Lesekanal fuer spaetere Aenderungen
// und die mehrfach aufrufbare Abmeldefunktion. Beim ersten Abonnenten
// startet der Poll-Loop.
//
// Bauen, Zusammensetzen und Anmelden laufen unter pollMu, damit zwischen
// "vollen Rumpf lesen" und "Kanal registrieren" keine Aenderung
// durchrutscht.
//
// selection geht an Topic.Select; ohne Select-Thema ist sie ohne Wirkung.
func (h *Hub) SubscribeWith(topics []string, selection string) (latest []byte, ch <-chan []byte, cancel func()) {
	mask := h.defaults
	if topics != nil {
		mask = h.maskOf(topics)
	}

	h.pollMu.Lock()
	// Baut die Themen des Neuen mit und verteilt einen etwaigen Wechsel an
	// die bereits verbundenen Abonnenten.
	h.refresh(mask)
	latest = h.compose(mask, ModeFull, 0, selection)

	h.mu.Lock()
	id := h.nextID
	h.nextID++
	out := make(chan []byte, h.queueSize)
	h.subs[id] = &subscriber{ch: out, mask: mask, selection: selection}
	h.startLoopLocked()
	h.mu.Unlock()

	h.pollMu.Unlock()

	var once sync.Once
	return latest, out, func() { once.Do(func() { h.remove(id) }) }
}

// Subscribe ist SubscribeWith ohne Auswahl.
func (h *Hub) Subscribe(topics []string) (latest []byte, ch <-chan []byte, cancel func()) {
	return h.SubscribeWith(topics, "")
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
	sub, ok := h.subs[id]
	if !ok {
		return
	}
	delete(h.subs, id)
	close(sub.ch)
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

// poll fuehrt genau eine Iteration aus. Der Loop ruft das getaktet, Tests
// rufen es direkt.
func (h *Hub) poll() {
	h.pollMu.Lock()
	defer h.pollMu.Unlock()
	h.refresh(0)
}

// refresh baut jedes Thema mit Abonnenten (oder in extra), dessen Version
// sich geaendert hat, und verteilt an jeden Abonnenten, dessen Themen oder
// dessen Registry-Version sich geaendert haben, einen Rumpf. Abonnenten mit
// derselben Maske teilen sich den Rumpf. Erwartet, dass der Aufrufer pollMu
// haelt.
func (h *Hub) refresh(extra uint64) {
	h.mu.Lock()
	active := extra
	for _, sub := range h.subs {
		active |= sub.mask
	}
	h.mu.Unlock()

	head := h.head()
	headChanged := !h.headSeen || head != h.lastHead
	h.lastHead, h.headSeen = head, true

	var changed uint64
	for i, topic := range h.topics {
		bit := uint64(1) << i
		if active&bit == 0 {
			continue
		}
		version := topic.Version()
		state := &h.state[i]
		if state.built && state.version == version {
			continue
		}
		// Der Bau laeuft ohne h.mu - er ist der teure Teil, und
		// Subscribe()/remove() sollen darauf nicht warten.
		part, ok := topic.Build(version)
		if !ok {
			// Der Zweig fehlt im naechsten Rumpf (der Client tauscht), der
			// naechste Poll baut erneut.
			*state = topicState{}
			changed |= bit
			continue
		}
		*state = topicState{built: true, version: version, part: part}
		changed |= bit
	}
	if !headChanged && changed == 0 {
		return
	}

	type bodyKey struct {
		mask      uint64
		selection string
	}
	bodies := map[bodyKey][]byte{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.subs {
		if !headChanged && sub.mask&changed == 0 {
			continue
		}
		key := bodyKey{sub.mask, sub.selection}
		body, ok := bodies[key]
		if !ok {
			body = h.compose(sub.mask, ModeDelta, changed, sub.selection)
			bodies[key] = body
		}
		h.deliverLocked(id, sub.ch, body)
	}
}

// compose setzt den Rumpf fuer eine Maske zusammen. Bei ModeDelta bekommt
// ein Thema ausserhalb von changed seinen Leerlauf-Teil. Hat ein Thema
// Select, baut es seinen Teil fuer selection. Erwartet pollMu.
func (h *Hub) compose(mask uint64, mode Mode, changed uint64, selection string) []byte {
	members := make([][]byte, 0, len(h.topics))
	for i, topic := range h.topics {
		bit := uint64(1) << i
		if mask&bit == 0 || !h.state[i].built {
			continue
		}
		m := mode
		if mode == ModeDelta && changed&bit == 0 {
			m = ModeIdle
		}
		if topic.Select != nil {
			members = append(members, topic.Select(h.state[i].part, m, selection))
			continue
		}
		members = append(members, h.state[i].part.Bytes(m))
	}
	return Compose(h.lastHead, members...)
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
