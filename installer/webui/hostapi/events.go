package hostapi

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// SubscriberBuffer ist die Zahl der Ereignisse, die ein einzelner Abonnent
// hinterherhaengen darf, bevor er abgeworfen wird. Er verbindet sich danach
// neu und holt ueber ?since= alles nach.
const SubscriberBuffer = 256

// Event ist ein Ereignis des Stroms. At ist der Zeitpunkt der
// Veroeffentlichung in Unix-Millisekunden - die Oberflaeche rechnet Dauern
// daraus, nicht aus der Ankunftszeit, damit ein nachgeholtes Ereignis seine
// Zeit behaelt.
type Event struct {
	Seq  int64  `json:"seq"`
	Type string `json:"type"`
	At   int64  `json:"at"`
	Data any    `json:"data"`
}

// Bus verteilt Ereignisse an alle Abonnenten und haelt die letzten keep
// Ereignisse fuer die Wiederholung nach einem Verbindungsabriss vor.
type Bus struct {
	// id benennt diesen Bus. Ein neu gestarteter Wirt hat einen neuen Bus,
	// dessen seq wieder bei 1 anfaengt; an einer anderen id erkennt die
	// Seite, dass ihr since nicht mehr zu diesem Bus passt.
	id          string
	mu          sync.Mutex
	seq         int64
	keep        int
	history     []Event
	subscribers map[int]chan Event
	nextSub     int
}

// NewBus baut einen leeren Bus, der die letzten keep Ereignisse behaelt.
func NewBus(keep int) *Bus {
	if keep < 1 {
		keep = 1
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(busCount.Add(1), 36)
	return &Bus{id: id, keep: keep, subscribers: map[int]chan Event{}}
}

// busCount haelt zwei Busse auseinander, die in derselben Nanosekunde entstehen.
var busCount atomic.Int64

// ID ist die Kennung dieses Busses (siehe Bus.id).
func (b *Bus) ID() string {
	return b.id
}

// RestoreBus baut einen Bus, der eine bereits gelaufene Sequenz fortsetzt.
// Plan D braucht das: nach dem Selbst-Update ist der Dashboard-Prozess ein
// neuer, die Sequenz des Laufs aber dieselbe.
func RestoreBus(events []Event, keep int) *Bus {
	bus := NewBus(keep)
	for _, event := range events {
		if event.Seq > bus.seq {
			bus.seq = event.Seq
		}
		bus.history = append(bus.history, event)
	}
	bus.trim()
	return bus
}

// Publish vergibt die naechste Sequenznummer, merkt sich das Ereignis und
// stellt es jedem Abonnenten zu. Ein Abonnent, dessen Puffer voll ist, wird
// abgeworfen - ein nicht lesender Browser darf keinen Lauf bremsen.
func (b *Bus) Publish(typ string, data any) Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.seq++
	event := Event{Seq: b.seq, Type: typ, At: time.Now().UnixMilli(), Data: data}
	b.history = append(b.history, event)
	b.trim()

	for id, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			close(ch)
			delete(b.subscribers, id)
		}
	}
	return event
}

// Since liefert alle noch vorgehaltenen Ereignisse nach seq.
func (b *Bus) Since(seq int64) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.since(seq)
}

// Seq ist die zuletzt vergebene Sequenznummer.
func (b *Bus) Seq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Subscribe liefert einen Kanal, der zuerst den Rueckstand ab since und danach
// jedes neue Ereignis traegt. Die zurueckgegebene Funktion bestellt ab; sie
// ist mehrfach aufrufbar.
func (b *Bus) Subscribe(since int64) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, SubscriberBuffer)
	for _, event := range b.since(since) {
		select {
		case ch <- event:
		default:
		}
	}
	id := b.nextSub
	b.nextSub++
	b.subscribers[id] = ch

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if existing, ok := b.subscribers[id]; ok {
				close(existing)
				delete(b.subscribers, id)
			}
		})
	}
	return ch, cancel
}

func (b *Bus) since(seq int64) []Event {
	out := make([]Event, 0, len(b.history))
	for _, event := range b.history {
		if event.Seq > seq {
			out = append(out, event)
		}
	}
	return out
}

func (b *Bus) trim() {
	if len(b.history) > b.keep {
		b.history = append([]Event(nil), b.history[len(b.history)-b.keep:]...)
	}
}
