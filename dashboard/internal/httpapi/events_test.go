package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// Der Cache ist der Grund, warum die Arbeit mit der Zahl der Aenderungen
// skaliert statt mit der Zahl offener Tabs: zwei Verbindungen, die bei
// derselben Registry-Version schreiben, teilen sich einen Lauf - und seit
// dem Uebersichts-Push teilen sie sich auch den einen reg.Snapshot().
func TestEventCacheBuildsOncePerVersion(t *testing.T) {
	reg := registry.New()
	resolver := energy.NewResolver(nil)
	engine := diagnostics.NewEngine(reg, nil)
	cache := &eventCache{}

	first, _ := cache.bodies(reg, resolver, engine, 7)
	second, _ := cache.bodies(reg, resolver, engine, 7)
	if first == nil {
		t.Fatal("erster Aufruf lieferte keinen Rumpf")
	}
	if &first[0] != &second[0] {
		t.Error("zweiter Aufruf bei gleicher Version hat neu gerechnet statt den Cache zu nutzen")
	}

	third, _ := cache.bodies(reg, resolver, engine, 8)
	if len(third) > 0 && &third[0] == &first[0] {
		t.Error("neue Version hat den alten Cache wiederverwendet")
	}
}

func TestEventBodyCarriesAllThreeBranches(t *testing.T) {
	reg := registry.New()
	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 3)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v (%s)", err, data)
	}
	for _, key := range []string{"version", "energy", "entities", "diagnostics"} {
		if _, ok := body[key]; !ok {
			t.Errorf("Rumpf ohne %q: %s", key, data)
		}
	}
	if string(body["version"]) != "3" {
		t.Errorf("version = %s, erwartet 3", body["version"])
	}
	// Eine Leerzeile beendet ein SSE-Ereignis; ein Umbruch im Rumpf wuerde
	// den Strom zerreissen.
	if strings.ContainsAny(string(data), "\n\r") {
		t.Errorf("Rumpf enthaelt Zeilenumbrueche: %q", data)
	}
}

// Ohne Geraete ist "entities" ein leeres Objekt, nicht null - der Client
// unterscheidet "Zweig da, nichts drin" von "Zweig fehlt, also Rueckfall auf
// den Fragment-Tausch".
func TestEventBodyEntitiesIsObjectWhenEmpty(t *testing.T) {
	reg := registry.New()
	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 1)

	var body struct {
		Entities map[string]registry.ValueView `json:"entities"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v", err)
	}
	if body.Entities == nil {
		t.Errorf("entities ist null statt {}: %s", data)
	}
}

// Der vierte Zweig sagt dem Browser, wann sein Fragment veraltet ist. Fehlt
// er, faellt der Client auf den Tausch zurueck - richtig, aber der Gewinn
// dieses Plans waere weg. Der Test haelt darum fest, dass er da ist.
func TestEventBodyCarriesStructureFingerprint(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "relay", Component: "switch", Name: "Relais"},
	})
	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 4)

	var body struct {
		Structure string `json:"structure"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v (%s)", err, data)
	}
	if want := registry.StructureFingerprint(reg.Snapshot()); body.Structure != want {
		t.Errorf("structure = %q, erwartet %q", body.Structure, want)
	}
	if body.Structure == "" {
		t.Error("structure ist leer - der Client wuerde dauerhaft tauschen")
	}
}

// Der Fingerabdruck darf nicht mit der Registry-Version wandern: die zaehlt
// jeden MQTT-Wert mit, er soll genau das ignorieren.
func TestEventBodyStructureSurvivesAValueChange(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "node/power"},
	})
	before := registry.StructureFingerprint(reg.Snapshot())

	reg.UpdateTopicWithQoS("node/power", []byte("42"), false, 0, time.Now().UTC())

	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())
	var body struct {
		Structure string `json:"structure"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v", err)
	}
	if body.Structure != before {
		t.Errorf("ein Messwert hat den Fingerabdruck veraendert: %q -> %q", before, body.Structure)
	}
}

// Ein zweiter Messwert bewegt sich; der Delta-Rumpf traegt nur ihn, der
// volle Rumpf traegt weiter alle. entities_delta markiert den Delta.
func TestEventCacheDeltaCarriesOnlyChangedValues(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Power", StateTopic: "dev-a/power"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e2", ObjectID: "energy", Component: "sensor", Name: "Energy", StateTopic: "dev-a/energy"},
	})
	reg.UpdateTopicWithQoS("dev-a/power", []byte("1"), false, 0, time.Now().UTC())
	reg.UpdateTopicWithQoS("dev-a/energy", []byte("10"), false, 0, time.Now().UTC())

	cache := &eventCache{}
	resolver := energy.NewResolver(nil)
	engine := diagnostics.NewEngine(reg, nil)

	full1, delta1 := cache.bodies(reg, resolver, engine, reg.Version())
	if full1 == nil || delta1 == nil {
		t.Fatal("erster Aufruf lieferte keinen Rumpf")
	}
	// Erster Lauf: previous == nil, der Delta ist deckungsgleich mit dem vollen Rumpf.
	if decodeEntities(t, delta1)["e1"].Value == "" {
		t.Error("erster Delta sollte alle Werte tragen")
	}

	reg.UpdateTopicWithQoS("dev-a/power", []byte("2"), false, 0, time.Now().UTC())
	full2, delta2 := cache.bodies(reg, resolver, engine, reg.Version())

	fe := decodeEntities(t, full2)
	if len(fe) != 2 {
		t.Errorf("voller Rumpf traegt %d Entitaeten, erwartet 2", len(fe))
	}
	de := decodeEntities(t, delta2)
	if len(de) != 1 || de["e1"].Value != "2" {
		t.Errorf("Delta = %v, erwartet nur e1=2", de)
	}
	var probe struct {
		EntitiesDelta bool `json:"entities_delta"`
	}
	if err := json.Unmarshal(delta2, &probe); err != nil || !probe.EntitiesDelta {
		t.Errorf("Delta-Rumpf ohne entities_delta:true: %s", delta2)
	}
	var fprobe map[string]json.RawMessage
	_ = json.Unmarshal(full2, &fprobe)
	if _, ok := fprobe["entities_delta"]; ok {
		t.Errorf("voller Rumpf soll entities_delta nicht setzen: %s", full2)
	}
}

// Zwei Aufrufe bei derselben Version liefern denselben Delta - er darf nicht
// gegen sich selbst gerechnet werden (waere leer).
func TestEventCacheDeltaIsStableWithinAVersion(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Power", StateTopic: "dev-a/power"},
	})
	reg.UpdateTopicWithQoS("dev-a/power", []byte("1"), false, 0, time.Now().UTC())
	cache := &eventCache{}
	resolver := energy.NewResolver(nil)
	engine := diagnostics.NewEngine(reg, nil)

	_, _ = cache.bodies(reg, resolver, engine, reg.Version())
	reg.UpdateTopicWithQoS("dev-a/power", []byte("2"), false, 0, time.Now().UTC())
	v := reg.Version()
	_, deltaA := cache.bodies(reg, resolver, engine, v)
	_, deltaB := cache.bodies(reg, resolver, engine, v)
	if string(deltaA) != string(deltaB) {
		t.Errorf("Delta bei gleicher Version instabil:\n a=%s\n b=%s", deltaA, deltaB)
	}
	if &deltaA[0] != &deltaB[0] {
		t.Error("zweiter Aufruf bei gleicher Version hat neu serialisiert statt den Cache zu nutzen")
	}
}

func decodeEntities(t *testing.T, body []byte) map[string]registry.ValueView {
	t.Helper()
	var probe struct {
		Entities map[string]registry.ValueView `json:"entities"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v (%s)", err, body)
	}
	return probe.Entities
}

// Der fuenfte Zweig sagt der Kompakt-Ansicht, wann ihre Zeilenauswahl oder
// die Geraete-Ampel veraltet ist. Fehlt er, faellt der Client auf den
// Tausch zurueck - richtig, aber der Gewinn dieses Plans waere weg.
func TestEventBodyCarriesCompactStructureFingerprint(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "node/power"},
	})
	reg.UpdateTopicWithQoS("node/power", []byte("42"), false, 0, time.Now().UTC())

	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())

	var body struct {
		StructureCompact string `json:"structure_compact"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v (%s)", err, data)
	}
	if want := webui.CompactStructureFingerprint(reg.Snapshot()); body.StructureCompact != want {
		t.Errorf("structure_compact = %q, erwartet %q", body.StructureCompact, want)
	}
	if body.StructureCompact == "" {
		t.Error("structure_compact ist leer - die Kompakt-Ansicht wuerde dauerhaft tauschen")
	}
}

// Bewegt sich nur ein Messwert innerhalb derselben Zeilenauswahl, darf der
// Kompakt-Fingerabdruck nicht wandern - genau das ist sein Zweck.
func TestEventBodyCompactStructureSurvivesAValueTick(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "node/power"},
	})
	reg.UpdateTopicWithQoS("node/power", []byte("42"), false, 0, time.Now().UTC())
	before := webui.CompactStructureFingerprint(reg.Snapshot())

	reg.UpdateTopicWithQoS("node/power", []byte("1337"), false, 0, time.Now().UTC())

	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())
	var body struct {
		StructureCompact string `json:"structure_compact"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v", err)
	}
	if body.StructureCompact != before {
		t.Errorf("ein Messwert hat den Kompakt-Fingerabdruck veraendert: %q -> %q", before, body.StructureCompact)
	}
}

// Der Availability-Zweig deckt fuer die konfigurierte Kompaktkachel die
// Geraete-Ampel ab. Fehlt er, faellt der Client auf den Fragment-Tausch
// zurueck - der Gewinn dieses Plans waere weg.
func TestEventBodyCarriesAvailabilityStructureFingerprint(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "node/power"},
	})
	reg.UpdateTopicWithQoS("node/power", []byte("42"), false, 0, time.Now().UTC())

	cache := &eventCache{}
	data, _ := cache.bodies(reg, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())

	var body struct {
		StructureAvailability string `json:"structure_availability"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("Rumpf ist kein gueltiges JSON: %v (%s)", err, data)
	}
	if want := webui.AvailabilityStructureFingerprint(reg.Snapshot()); body.StructureAvailability != want {
		t.Errorf("structure_availability = %q, erwartet %q", body.StructureAvailability, want)
	}
	if body.StructureAvailability == "" {
		t.Error("structure_availability ist leer - die konfigurierte Kompaktkachel wuerde dauerhaft tauschen")
	}
}
