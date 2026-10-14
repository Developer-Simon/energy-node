package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registryevents"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// Themen des SSE-Stroms. Die Namen sind zugleich die Zweige im Rumpf.
const (
	topicEnergy      = "energy"
	topicEntities    = "entities"
	topicDiagnostics = "diagnostics"
	// topicStructure traegt die drei globalen Fingerabdruecke. Uebergang bis
	// zum Thema devices, danach entfaellt es.
	topicStructure = "structure"
)

// defaultEventTopics gilt ohne ?topics=. Ein aelterer Client bekommt damit
// denselben Rumpf wie vor den Themen.
var defaultEventTopics = []string{topicEnergy, topicEntities, topicDiagnostics, topicStructure}

// topicCache haelt den zuletzt gebauten Teil eines Themas. Ein zweiter
// Aufruf bei gleicher Version liefert denselben Teil - das ist nicht nur
// Sparsamkeit: ein Delta darf nicht gegen sich selbst gerechnet werden, er
// waere leer.
type topicCache struct {
	mu      sync.Mutex
	valid   bool
	version registryevents.Version
	part    registryevents.Part
}

func (c *topicCache) get(v registryevents.Version, build func() (registryevents.Part, bool)) (registryevents.Part, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.version == v {
		return c.part, true
	}
	part, ok := build()
	if !ok {
		return registryevents.Part{}, false
	}
	c.valid, c.version, c.part = true, v, part
	return part, true
}

// eventCache baut die Themen des SSE-Stroms. Ohne ihn wuerde jede offene
// Verbindung bei derselben Aenderung dieselbe Arbeit noch einmal machen: ein
// reg.Snapshot() (Deep-Copy aller Geraete), ein energy.Aggregate() und ein
// Diagnose-Lauf. Gemessen kostete der Fragment-Weg 0,89 ms je Client und
// Aenderung; hier faellt das einmal fuer alle an, und nur fuer Themen, die
// jemand abonniert hat.
type eventCache struct {
	reg      *registry.Registry
	store    *settings.Store
	resolver *energy.Resolver
	engine   *diagnostics.Engine

	// Ein Snapshot je Version fuer alle Registry-Themen. Die Themen lesen
	// ihn nur.
	mu          sync.Mutex
	snapValid   bool
	snapVersion registryevents.Version
	devices     []registry.DeviceView

	energy, entities, diagnostics, structure topicCache
	// lastValues gehoert zum entities-Thema und wird nur unter entities.mu
	// gelesen und geschrieben.
	lastValues map[string]registry.ValueView
}

func newEventCache(reg *registry.Registry, store *settings.Store, resolver *energy.Resolver, engine *diagnostics.Engine) *eventCache {
	return &eventCache{reg: reg, store: store, resolver: resolver, engine: engine}
}

// topics liefert die Themen fuer den Hub. Die Reihenfolge ist die der
// Zweige im Rumpf.
func (c *eventCache) topics() []registryevents.Topic {
	return []registryevents.Topic{
		{Name: topicEnergy, Version: c.registryVersion, Build: c.buildEnergy},
		{Name: topicEntities, Version: c.registryVersion, Build: c.buildEntities},
		{Name: topicDiagnostics, Version: c.registryVersion, Build: c.buildDiagnostics},
		{Name: topicStructure, Version: c.registryVersion, Build: c.buildStructure},
	}
}

// registryVersion ist die Version der Registry-Themen. Die
// Geraete-Einstellungen gehoeren dazu: die Zeilenauswahl der Kompaktkarte
// und die Fingerabdruecke haengen an FavoriteRefs (ApplyDevicePrefs).
func (c *eventCache) registryVersion() registryevents.Version {
	v := registryevents.Version{c.reg.Version()}
	if c.store != nil {
		v[1] = c.store.DevicePrefsGeneration()
	}
	return v
}

// snapshot zieht reg.Snapshot() einmal je Version. Liest der Hub die
// Versionen zweier Themen in einem Poll knapp vor und nach einer Aenderung,
// entstehen zwei Snapshots - selten und harmlos.
func (c *eventCache) snapshot(v registryevents.Version) []registry.DeviceView {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapValid && c.snapVersion == v {
		return c.devices
	}
	devices := c.reg.Snapshot()
	if c.store != nil {
		if prefs, err := c.store.DevicePrefsByID(); err == nil {
			webui.ApplyDevicePrefs(devices, prefs)
		}
	}
	c.snapValid, c.snapVersion, c.devices = true, v, devices
	return devices
}

// member serialisiert einen Zweig als `"name":<json>`.
func member(name string, value any) ([]byte, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	out := make([]byte, 0, len(name)+3+len(data))
	out = append(out, '"')
	out = append(out, name...)
	out = append(out, '"', ':')
	out = append(out, data...)
	return out, true
}

func sameParts(m []byte) registryevents.Part {
	return registryevents.Part{Full: m, Delta: m, Idle: m}
}

func (c *eventCache) buildEnergy(v registryevents.Version) (registryevents.Part, bool) {
	return c.energy.get(v, func() (registryevents.Part, bool) {
		snapshot := energy.Aggregate(c.snapshot(v), c.resolver, time.Now().UTC())
		m, ok := member(topicEnergy, snapshot.WithInterpretation(c.resolver.Interpretation()))
		return sameParts(m), ok
	})
}

// buildEntities liefert alle Werte (Full) und die seit dem letzten Bau
// geaenderten (Delta). Im Leerlauf ein leeres Delta.
func (c *eventCache) buildEntities(v registryevents.Version) (registryevents.Part, bool) {
	return c.entities.get(v, func() (registryevents.Part, bool) {
		values := registry.ValueViews(c.snapshot(v))
		full, ok := member(topicEntities, values)
		if !ok {
			return registryevents.Part{}, false
		}
		delta, ok := member(topicEntities, changedValues(c.lastValues, values))
		if !ok {
			return registryevents.Part{}, false
		}
		delta = append(delta, `,"entities_delta":true`...)
		c.lastValues = values
		return registryevents.Part{
			Full:  full,
			Delta: delta,
			Idle:  []byte(`"entities":{},"entities_delta":true`),
		}, true
	})
}

func (c *eventCache) buildDiagnostics(v registryevents.Version) (registryevents.Part, bool) {
	return c.diagnostics.get(v, func() (registryevents.Part, bool) {
		if c.engine == nil {
			return registryevents.Part{}, true
		}
		now := time.Now().UTC()
		summary := diagnostics.Summarize(c.engine.Evaluate(now), c.engine.Health(now), c.snapshot(v))
		m, ok := member(topicDiagnostics, &summary)
		return sameParts(m), ok
	})
}

// buildStructure traegt die globalen Fingerabdruecke, die dashboard.js mit
// data-structure, data-structure-compact und data-structure-availability
// des Fragments vergleicht.
func (c *eventCache) buildStructure(v registryevents.Version) (registryevents.Part, bool) {
	return c.structure.get(v, func() (registryevents.Part, bool) {
		devices := c.snapshot(v)
		fields := []struct{ name, value string }{
			{"structure", registry.StructureFingerprint(devices)},
			{"structure_compact", webui.CompactStructureFingerprint(devices)},
			{"structure_availability", webui.AvailabilityStructureFingerprint(devices)},
		}
		var out []byte
		for _, field := range fields {
			m, ok := member(field.name, field.value)
			if !ok {
				return registryevents.Part{}, false
			}
			if len(out) > 0 {
				out = append(out, ',')
			}
			out = append(out, m...)
		}
		return sameParts(out), true
	})
}

// changedValues sind die Eintraege aus now, die in previous fehlen oder sich
// unterscheiden. previous == nil (erster Lauf) heisst: alles ist neu, der
// Delta ist dann deckungsgleich mit dem vollen Rumpf. Eine entfernte
// Entitaet steht bewusst nicht drin - ihr Verschwinden aendert den
// StructureFingerprint, und der Client tauscht dann sein Fragment ganz.
func changedValues(previous, now map[string]registry.ValueView) map[string]registry.ValueView {
	changed := make(map[string]registry.ValueView, len(now))
	for id, value := range now {
		if old, ok := previous[id]; !ok || old != value {
			changed[id] = value
		}
	}
	return changed
}

// requestedTopics liest ?topics=. Ohne Parameter nil (Standardthemen), mit
// leerem Parameter eine leere Liste: dann kommen nur Versionswechsel.
func requestedTopics(r *http.Request) []string {
	query := r.URL.Query()
	if !query.Has("topics") {
		return nil
	}
	topics := []string{}
	for _, name := range strings.Split(query.Get("topics"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			topics = append(topics, name)
		}
	}
	return topics
}

// registryStreamKeepAlive ist der Abstand zwischen zwei Lebenszeichen auf
// einem stillen /api/v1/events-Strom. Ohne sie schliessen Zwischenstellen
// (caddy, tailscale) den Strom nach wenigen Minuten - und der Server merkt
// einen weggebrochenen Client erst beim naechsten echten Schreibversuch.
// Variable statt Konstante, damit Tests sie verkuerzen koennen.
var registryStreamKeepAlive = 20 * time.Second

// registryStreamQueueSize ist der Vorlauf je Verbindung im Hub. Vier
// Ruempfe reichen: eine Verbindung, die nicht mitkommt, wird getrennt und
// beginnt nach dem EventSource-Reconnect mit einem frischen Rumpf.
const registryStreamQueueSize = 4

// handleEvents bedient den /api/v1/events-SSE-Strom. Alle Verbindungen
// teilen sich einen Poll-Loop im Hub. Er baut jedes abonnierte Thema einmal
// je Aenderung und schickt jeder Verbindung nur ihre Themen (?topics=).
func handleEvents(reg *registry.Registry, store *settings.Store, resolver *energy.Resolver, engine *diagnostics.Engine) http.HandlerFunc {
	cache := newEventCache(reg, store, resolver, engine)
	hub := registryevents.NewHub(
		registryStreamQueueSize,
		reg.Version,
		cache.topics(),
		defaultEventTopics,
		func() time.Duration { return liveUpdateInterval(store) },
	)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming is not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		latest, updates, cancel := hub.Subscribe(requestedTopics(r))
		defer cancel()

		writeEvent := func(payload []byte) bool {
			if _, err := fmt.Fprintf(w, "event: registry\ndata: %s\n\n", payload); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}

		if !writeEvent(latest) {
			return
		}

		keepAlive := time.NewTicker(registryStreamKeepAlive)
		defer keepAlive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case payload, open := <-updates:
				// Kanal zu: der Hub hat diese Verbindung als zu langsam
				// getrennt. Die EventSource verbindet von selbst neu.
				if !open {
					return
				}
				if !writeEvent(payload) {
					return
				}
			case <-keepAlive.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
