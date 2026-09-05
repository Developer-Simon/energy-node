// Package historyexchange traegt den Austausch der Browser-Historie
// zwischen gleichzeitig geoeffneten Dashboard-Clients. Der Hub ist ein
// reiner Verteiler: er kennt Peers und ihre Kanaele, aber weder HTTP noch
// den Inhalt der Nachrichten. Messdaten liegen weiterhin in der IndexedDB
// der Browser - der Server speichert nichts auf Platte.
package historyexchange

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

// ServerPeerID ist die feste Kennung des Ringpuffers. Er tritt gegenueber
// den Clients als gewoehnlicher Peer auf, damit im Browser genau ein
// Codepfad genuegt.
const ServerPeerID = "server"

// Message ist ein SSE-Ereignis auf dem Weg zu genau einem Peer.
type Message struct {
	Event string
	Data  json.RawMessage
}

type peer struct {
	out    chan Message
	closed bool
}

type Hub struct {
	mutex     sync.Mutex
	peers     map[string]*peer
	queueSize int
}

func NewHub(queueSize int) *Hub {
	if queueSize < 1 {
		queueSize = 1
	}
	return &Hub{peers: map[string]*peer{}, queueSize: queueSize}
}

func newPeerID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		// Ohne Zufall waere jede Kennung gleich; ein fester Praefix mit
		// Laenge null ist hier kein sinnvoller Rueckfall, deshalb Panik -
		// crypto/rand schlaegt auf einem laufenden Linux nicht fehl.
		panic(err)
	}
	return "p-" + hex.EncodeToString(raw)
}

// Join meldet einen Peer an und liefert seine Kennung, seinen Lesekanal und
// die Abmeldefunktion. leave ist mehrfach aufrufbar.
func (h *Hub) Join() (string, <-chan Message, func()) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	id := newPeerID()
	for _, taken := h.peers[id]; taken; _, taken = h.peers[id] {
		id = newPeerID()
	}
	entry := &peer{out: make(chan Message, h.queueSize)}
	h.peers[id] = entry
	return id, entry.out, func() { h.remove(id) }
}

func (h *Hub) remove(id string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.removeLocked(id)
}

func (h *Hub) removeLocked(id string) {
	entry, ok := h.peers[id]
	if !ok {
		return
	}
	delete(h.peers, id)
	if !entry.closed {
		entry.closed = true
		close(entry.out)
	}
}

// deliverLocked schiebt eine Nachricht in den Kanal eines Peers. Laeuft der
// Kanal ueber, wird der Peer getrennt: er verbindet neu und beginnt mit
// einem frischen Angebot. Puffern statt trennen liesse den Speicherbedarf
// des Hubs unbeschraenkt wachsen.
func (h *Hub) deliverLocked(id string, entry *peer, msg Message) {
	select {
	case entry.out <- msg:
	default:
		h.removeLocked(id)
	}
}

func (h *Hub) Broadcast(from string, msg Message) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	for id, entry := range h.peers {
		if id == from {
			continue
		}
		h.deliverLocked(id, entry, msg)
	}
}

func (h *Hub) SendTo(to string, msg Message) bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	entry, ok := h.peers[to]
	if !ok {
		return false
	}
	h.deliverLocked(to, entry, msg)
	return true
}

func (h *Hub) Peers() []string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	names := make([]string, 0, len(h.peers))
	for id := range h.peers {
		names = append(names, id)
	}
	sort.Strings(names)
	return names
}

func (h *Hub) Has(id string) bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	_, ok := h.peers[id]
	return ok
}
