package performance

import (
	"sort"
	"sync"
	"time"
)

// externalIntervals ist die Zahl der Abstaende, aus denen der Median je
// fremdem Geraet gebildet wird.
const externalIntervals = 20

// SourceRate beschreibt das schnellste fremde Geraet: Name aus der Registry
// und der Median seines Nachrichtenabstands in Sekunden.
type SourceRate struct {
	Name   string
	EveryS float64
}

// Counter zaehlt Nachrichten je Dienst und misst fuer fremde Geraete den
// Abstand zwischen ihren Nachrichten. Die Zuordnung Topic zu Dienst oder
// Geraet kommt von aussen (attribute), damit der Zaehler die Registry nicht
// kennen muss.
type Counter struct {
	attribute func(topic string) (serviceID string, deviceID string)

	mu         sync.Mutex
	deviceName func(deviceID string) string
	counts     map[string]int64
	devices    map[string]*foreignDevice
}

// foreignDevice merkt sich den letzten Zeitpunkt und die letzten Abstaende
// eines fremden Geraets.
type foreignDevice struct {
	last      time.Time
	seen      bool
	intervals *Ring
}

// NewCounter legt einen Zaehler an. attribute liefert zu einem Topic die
// Dienst-ID, oder bei einem fremden Geraet die Geraete-ID mit leerer
// Dienst-ID.
func NewCounter(attribute func(topic string) (serviceID string, deviceID string)) *Counter {
	return &Counter{
		attribute: attribute,
		counts:    map[string]int64{},
		devices:   map[string]*foreignDevice{},
	}
}

// SetDeviceName setzt die Auflosung von Geraete-ID zu Anzeigename. Ohne sie
// oder bei einem leeren Ergebnis zaehlt die Geraete-ID selbst.
func (c *Counter) SetDeviceName(name func(deviceID string) string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceName = name
}

// Observe zaehlt eine Nachricht auf topic, die zum Zeitpunkt now ankam.
func (c *Counter) Observe(topic string, now time.Time) {
	serviceID, deviceID := c.attribute(topic)

	c.mu.Lock()
	defer c.mu.Unlock()
	if serviceID != "" {
		c.counts[serviceID]++
		return
	}
	if deviceID == "" {
		return
	}
	dev, ok := c.devices[deviceID]
	if !ok {
		dev = &foreignDevice{intervals: NewRing(externalIntervals, 0)}
		c.devices[deviceID] = dev
	}
	if dev.seen {
		if gap := now.Sub(dev.last); gap > 0 {
			dev.intervals.Add(now, gap.Seconds())
		}
	}
	dev.last = now
	dev.seen = true
}

// Count liefert die Zahl der Nachrichten, die seit dem Anlegen des Zaehlers
// auf Topics des Dienstes serviceID angekommen sind. Der Aufrufer bildet
// Raten aus Differenzen.
func (c *Counter) Count(serviceID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[serviceID]
}

// External liefert das schnellste fremde Geraet mit dem Median seiner
// letzten Abstaende. nil heisst: noch kein fremdes Geraet mit einem Abstand.
func (c *Counter) External() *SourceRate {
	c.mu.Lock()
	defer c.mu.Unlock()

	var best *SourceRate
	var bestID string
	for id, dev := range c.devices {
		values := dev.intervals.Values()
		if len(values) == 0 {
			continue
		}
		every := median(values)
		if best == nil || every < best.EveryS || (every == best.EveryS && id < bestID) {
			best = &SourceRate{EveryS: every}
			bestID = id
		}
	}
	if best == nil {
		return nil
	}
	best.Name = bestID
	if c.deviceName != nil {
		if name := c.deviceName(bestID); name != "" {
			best.Name = name
		}
	}
	return best
}

// median liefert den Median von values. Bei gerader Anzahl das Mittel der
// beiden mittleren Werte.
func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
