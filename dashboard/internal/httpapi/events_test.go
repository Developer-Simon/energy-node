package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registryevents"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// bodies setzt den Standardrumpf so zusammen, wie ihn ein Client ohne
// ?topics= bekommt: voll fuer neue, Delta fuer verbundene Abonnenten. Die
// Tests aus der Zeit vor den Themen pruefen damit den alten Vertrag weiter.
func (c *eventCache) bodies(reg *registry.Registry, store *settings.Store, resolver *energy.Resolver, engine *diagnostics.Engine, version uint64) (full, delta []byte) {
	if c.reg == nil {
		c.reg, c.store, c.resolver, c.engine = reg, store, resolver, engine
	}
	v := registryevents.Version{version}
	if store != nil {
		v[1] = store.DevicePrefsGeneration()
	}
	var fullMembers, deltaMembers [][]byte
	for _, topic := range c.topics() {
		part, ok := topic.Build(v)
		if !ok {
			return nil, nil
		}
		fullMembers = append(fullMembers, part.Bytes(registryevents.ModeFull))
		deltaMembers = append(deltaMembers, part.Bytes(registryevents.ModeDelta))
	}
	return registryevents.Compose(version, fullMembers...), registryevents.Compose(version, deltaMembers...)
}

// Der Cache je Thema ist der Grund, warum die Arbeit mit der Zahl der
// Aenderungen skaliert statt mit der Zahl offener Tabs: zwei Verbindungen
// bei derselben Version teilen sich einen Bau.
func TestEventCacheBuildsOncePerVersion(t *testing.T) {
	reg := registry.New()
	cache := newEventCache(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil))

	first, ok := cache.buildEnergy(registryevents.Version{7})
	if !ok || len(first.Full) == 0 {
		t.Fatal("erster Bau lieferte keinen Teil")
	}
	second, _ := cache.buildEnergy(registryevents.Version{7})
	if &first.Full[0] != &second.Full[0] {
		t.Error("zweiter Bau bei gleicher Version hat neu gerechnet statt den Cache zu nutzen")
	}
	third, _ := cache.buildEnergy(registryevents.Version{8})
	if &third.Full[0] == &first.Full[0] {
		t.Error("neue Version hat den alten Cache wiederverwendet")
	}
}

func TestEventBodyCarriesAllThreeBranches(t *testing.T) {
	reg := registry.New()
	cache := &eventCache{}
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 3)

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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 1)

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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), 4)

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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())
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

	full1, delta1 := cache.bodies(reg, nil, resolver, engine, reg.Version())
	if full1 == nil || delta1 == nil {
		t.Fatal("erster Aufruf lieferte keinen Rumpf")
	}
	// Erster Lauf: previous == nil, der Delta ist deckungsgleich mit dem vollen Rumpf.
	if decodeEntities(t, delta1)["e1"].Value == "" {
		t.Error("erster Delta sollte alle Werte tragen")
	}

	reg.UpdateTopicWithQoS("dev-a/power", []byte("2"), false, 0, time.Now().UTC())
	full2, delta2 := cache.bodies(reg, nil, resolver, engine, reg.Version())

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

	_, _ = cache.bodies(reg, nil, resolver, engine, reg.Version())
	reg.UpdateTopicWithQoS("dev-a/power", []byte("2"), false, 0, time.Now().UTC())
	v := reg.Version()
	_, deltaA := cache.bodies(reg, nil, resolver, engine, v)
	_, deltaB := cache.bodies(reg, nil, resolver, engine, v)
	if string(deltaA) != string(deltaB) {
		t.Errorf("Delta bei gleicher Version instabil:\n a=%s\n b=%s", deltaA, deltaB)
	}
	partA, _ := cache.buildEntities(registryevents.Version{v})
	partB, _ := cache.buildEntities(registryevents.Version{v})
	if &partA.Delta[0] != &partB.Delta[0] {
		t.Error("zweiter Bau bei gleicher Version hat neu serialisiert statt den Cache zu nutzen")
	}
}

// Alle Registry-Themen einer Version teilen sich einen reg.Snapshot().
func TestEventCacheSharesOneSnapshotPerVersion(t *testing.T) {
	reg := registry.New()
	cache := newEventCache(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil))
	v := registryevents.Version{1}
	a := cache.snapshot(v)
	b := cache.snapshot(v)
	if len(a) != len(b) || cache.snapVersion != v || !cache.snapValid {
		t.Fatal("snapshot hat die Version nicht gemerkt")
	}
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "dev-a"},
		Entity: registry.EntityInfo{UniqueID: "e1", ObjectID: "power", Component: "sensor", StateTopic: "dev-a/power"},
	})
	if got := cache.snapshot(registryevents.Version{2}); len(got) != 1 {
		t.Errorf("neue Version sieht %d Geraete, want 1", len(got))
	}
}

// Ein unveraendertes entities-Thema traegt im Leerlauf ein leeres Delta,
// damit der Zweig im Rumpf steht.
func TestEntitiesIdleIsEmptyDelta(t *testing.T) {
	reg := registry.New()
	cache := newEventCache(reg, nil, energy.NewResolver(nil), nil)
	part, ok := cache.buildEntities(registryevents.Version{1})
	if !ok {
		t.Fatal("Bau gescheitert")
	}
	if got := string(part.Idle); got != `"entities":{},"entities_delta":true` {
		t.Errorf("Idle = %s", got)
	}
}

// Ohne Diagnose-Engine bleibt das Thema leer und fehlt im Rumpf, wie frueher
// mit omitempty.
func TestDiagnosticsWithoutEngineIsEmpty(t *testing.T) {
	reg := registry.New()
	cache := newEventCache(reg, nil, energy.NewResolver(nil), nil)
	part, ok := cache.buildDiagnostics(registryevents.Version{1})
	if !ok || len(part.Full) != 0 {
		t.Errorf("part = %+v, ok = %v", part, ok)
	}
}

func TestRequestedTopics(t *testing.T) {
	cases := []struct {
		url  string
		want []string
	}{
		{"/api/v1/events", nil},
		{"/api/v1/events?topics=", []string{}},
		{"/api/v1/events?topics=energy,%20entities,,x", []string{"energy", "entities", "x"}},
	}
	for _, tc := range cases {
		got := requestedTopics(httptest.NewRequest("GET", tc.url, nil))
		if (got == nil) != (tc.want == nil) || strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: got %#v, want %#v", tc.url, got, tc.want)
		}
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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())

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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())
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
	data, _ := cache.bodies(reg, nil, energy.NewResolver(nil), diagnostics.NewEngine(reg, nil), reg.Version())

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

func TestEnergyStatsTopicOnlyWithService(t *testing.T) {
	reg := registry.New()
	without := newEventCache(reg, nil, energy.NewResolver(nil), nil)
	for _, topic := range without.topics() {
		if topic.Name == topicEnergyStats {
			t.Fatal("Thema energy_stats ohne Dienst")
		}
	}
	with := newEventCache(reg, nil, energy.NewResolver(nil), nil)
	with.stats = &energystats.Service{Store: energystats.NewStore(t.TempDir())}
	part, ok := with.buildEnergyStats(registryevents.Version{4})
	if !ok || string(part.Full) != `"energy_stats":{"rev":4}` || string(part.Idle) != string(part.Full) {
		t.Errorf("part = %q / %q, ok = %v", part.Full, part.Idle, ok)
	}
}
