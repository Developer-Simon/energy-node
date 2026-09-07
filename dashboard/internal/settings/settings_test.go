package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func TestStorePersistsSettingsAndLayout(t *testing.T) {
	store := NewStore(t.TempDir())
	if got, err := store.LoadSettings(); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(got, Default()) {
		t.Fatalf("got %#v, want defaults", got)
	}
	if err := store.SaveSettings(Settings{HealthScoreThreshold: 4, SweepIntervalSeconds: 120, ShowDiscoveryTooltips: false, ShowRuntimeStatus: false}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadSettings(); err != nil {
		t.Fatal(err)
	} else if got.ShowDiscoveryTooltips {
		t.Fatal("discovery tooltips unexpectedly enabled after saving false")
	} else if got.ShowRuntimeStatus {
		t.Fatal("runtime status unexpectedly enabled after saving false")
	}
	if err := store.SaveLayout(Layout{Pages: []Page{{ID: "overview", Name: "Overview", Groups: []Group{{ID: "main", Name: "Main"}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLayout(Layout{Pages: []Page{{ID: "", Name: "invalid"}}}); err == nil {
		t.Fatal("expected invalid layout to be rejected")
	}
}

func TestLoadLayoutMigratesLegacyEntityIDs(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"pages":[{"id":"overview","name":"Overview","order":0,"groups":[{"id":"main","name":"Main","entity_ids":["sensor.power"]}]}],"favorites":[]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	layout, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	if layout.Version != 3 || len(layout.Pages[0].Groups[0].Items) != 1 {
		t.Fatalf("migrated layout = %#v", layout)
	}
	item := layout.Pages[0].Groups[0].Items[0]
	if item.ID != "entity:sensor.power" || item.Type != "entity_value" || item.Ref != "sensor.power" || item.Span != "1" || !item.Visible {
		t.Fatalf("migrated item = %#v", item)
	}
}

// TestLoadLayoutMigratesEntityCardsToEntityValue deckt die Abschaffung des
// Kartentyps "entity" ab (Spec 2026-08-23, Abschnitt 4): ein gespeichertes
// Layout mit dieser Karte muss weiter laden - aber als Wert-Karte auf
// demselben Ref, damit niemand eine Kachel verliert. Nach dem Zurueckschreiben
// steht der alte Typ nirgends mehr im Dokument.
func TestLoadLayoutMigratesEntityCardsToEntityValue(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":3,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[
		{"id":"entity:ent1","type":"entity","ref":"ent1","span":"1","visible":true},
		{"id":"wert","type":"entity_value","ref":"ent2","span":"1","visible":true}
	]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	layout, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	items := layout.Pages[0].Groups[0].Items
	if len(items) != 2 {
		t.Fatalf("items = %#v, want 2", items)
	}
	if items[0].Type != "entity_value" || items[0].Ref != "ent1" || items[0].ID != "entity:ent1" {
		t.Fatalf("migrated item = %#v, want entity_value auf ent1 unter unveraenderter ID", items[0])
	}
	if items[1].Type != "entity_value" || items[1].Ref != "ent2" {
		t.Fatalf("bestehende Wert-Karte veraendert: %#v", items[1])
	}

	if err := store.SaveLayout(layout); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(dir + "/layout.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), `"type":"entity"`) {
		t.Fatalf("geschriebenes Dokument enthaelt weiterhin den Kartentyp entity: %s", written)
	}
}

// TestCardTypeForRejectsRemovedEntityType: der Typ ist aus dem Katalog raus.
// Damit weist validateLayout() ihn ab - erreichen kann ihn nichts mehr, weil
// jeder Validierungspfad ueber normalizeLayout() laeuft (siehe oben).
func TestCardTypeForRejectsRemovedEntityType(t *testing.T) {
	if _, ok := CardTypeFor("entity"); ok {
		t.Fatal("Kartentyp entity steht noch im Katalog")
	}
}

func TestSaveLayoutAcceptsAValidFlowScaleAndRejectsAnInvalidOne(t *testing.T) {
	base := func(flowScale string) Layout {
		return Layout{Pages: []Page{{
			ID: "overview", Name: "Overview", Order: 0,
			Groups: []Group{{ID: "main", Name: "Main", Items: []Item{
				{ID: "item", Type: "energy_flow", Span: "full", Visible: true, FlowScale: flowScale},
			}}},
		}}}
	}
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(base("speed")); err != nil {
		t.Fatalf("SaveLayout(flow_scale=speed) = %v, want nil", err)
	}
	if err := store.SaveLayout(base("bogus")); err == nil {
		t.Fatal("SaveLayout accepted an unknown flow_scale value")
	}
}

func TestStoreDefaultsMissingDeviceViewToCompact(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"health_score_threshold":3,"sweep_interval_seconds":300,"show_discovery_tooltips":true,"show_runtime_status":false}`
	if err := os.WriteFile(dir+"/settings.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if value.DeviceViewMode != DeviceViewModeCompact || value.ShowRuntimeStatus {
		t.Fatalf("migrated settings = %#v", value)
	}
}

func TestStoreMigratesLegacyAnalysisViewToControl(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"health_score_threshold":3,"sweep_interval_seconds":300,"show_discovery_tooltips":true,"show_runtime_status":true,"device_view_mode":"analysis"}`
	if err := os.WriteFile(dir+"/settings.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatalf("legacy analysis value must load, not error: %v", err)
	}
	if value.DeviceViewMode != DeviceViewModeControl {
		t.Fatalf("legacy analysis view should migrate to control, got %q", value.DeviceViewMode)
	}
}

func TestStoreRejectsUnknownDeviceViewMode(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveSettings(Settings{
		HealthScoreThreshold: 3, SweepIntervalSeconds: 300,
		ShowDiscoveryTooltips: true, ShowRuntimeStatus: true, DeviceViewMode: "unknown",
	}); err == nil {
		t.Fatal("unknown device view mode was accepted")
	}
}

func TestStorePersistsEnergyAssignments(t *testing.T) {
	store := NewStore(t.TempDir())
	want := EnergyConfig{Assignments: map[string]energy.Assignment{
		"meter_power": {Role: energy.RoleGridImport, Scale: 0.5, Invert: true},
	}}
	if err := store.SaveEnergy(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignments["meter_power"] != want.Assignments["meter_power"] {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestLoadEnergyBackfillsInterpretationDefaultsForALegacyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/energy.json", []byte(`{"assignments":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(dir).LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	if got.Interpretation != energy.DefaultInterpretation() {
		t.Fatalf("interpretation = %#v, want defaults %#v", got.Interpretation, energy.DefaultInterpretation())
	}
}

func TestLoadEnergyKeepsOtherFieldsForAPartiallySpecifiedInterpretation(t *testing.T) {
	dir := t.TempDir()
	body := `{"assignments":{},"interpretation":{"gap_mode":"diagnostic"}}`
	if err := os.WriteFile(dir+"/energy.json", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(dir).LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	want := energy.DefaultInterpretation()
	want.GapMode = energy.GapModeDiagnostic
	if got.Interpretation != want {
		t.Fatalf("interpretation = %#v, want %#v", got.Interpretation, want)
	}
}

func TestSaveEnergyRejectsAnUnknownGapMode(t *testing.T) {
	store := NewStore(t.TempDir())
	value := EnergyConfig{Assignments: map[string]energy.Assignment{}, Interpretation: energy.Interpretation{
		GapMode: "bogus", LoadMode: energy.LoadModeAuto, GapToleranceMode: energy.ToleranceModeAbsolute, GapToleranceW: 25, GapTolerancePercent: 2,
	}}
	if err := store.SaveEnergy(value); err == nil {
		t.Fatal("expected an unknown gap_mode to be rejected")
	}
}

func TestStorePersistsEnergyInterpretation(t *testing.T) {
	store := NewStore(t.TempDir())
	want := EnergyConfig{Assignments: map[string]energy.Assignment{}, Interpretation: energy.Interpretation{
		GapMode: energy.GapModeDiagnostic, LoadMode: energy.LoadModeCalculated, GapToleranceMode: energy.ToleranceModePercent, GapToleranceW: 25, GapTolerancePercent: 5,
		SurplusThresholdW: 800, ImportThresholdW: 1500,
	}}
	if err := store.SaveEnergy(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadEnergy()
	if err != nil {
		t.Fatal(err)
	}
	if got.Interpretation != want.Interpretation {
		t.Fatalf("got %#v, want %#v", got.Interpretation, want.Interpretation)
	}
}

func TestLayoutRevisionsCanBeReadAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	first := Layout{Pages: []Page{{ID: "overview", Name: "Overview", Order: 0, Groups: []Group{}}}}
	second := Layout{Pages: []Page{{ID: "energy", Name: "Energy", Order: 0, Groups: []Group{}}}}
	if err := store.SaveLayout(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLayout(second); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.LayoutRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d layout revisions, want 1", len(revisions))
	}
	data, err := store.ReadLayoutRevision(revisions[0].Name)
	if err != nil || !json.Valid(data) {
		t.Fatalf("read layout revision: %v, %s", err, data)
	}
	restored, err := store.RestoreLayout(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Pages[0].ID != "overview" {
		t.Fatalf("restored layout %#v, want first layout", restored)
	}
	if revisions, err = store.LayoutRevisions(); err != nil || len(revisions) != 2 {
		t.Fatalf("got %d revisions after restore, err %v; want 2", len(revisions), err)
	}
}

func TestStorePersistsDeviceMap(t *testing.T) {
	store := NewStore(t.TempDir())
	if got, err := store.LoadDeviceMap(); err != nil {
		t.Fatal(err)
	} else if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("got %#v, want no nodes/edges before anything is saved", got)
	}

	value := DeviceMap{
		Version: 1,
		Nodes:   []DeviceMapNode{{DeviceID: "shelly_1", X: 12.5, Y: -4}},
		Edges:   []RelationOverride{{ID: "relation-1", ChildID: "shelly_1", ParentID: "bridge_1", Kind: "via_device", CreatedAt: time.Now().UTC()}},
	}
	if err := store.SaveDeviceMap(value); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadDeviceMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].DeviceID != "shelly_1" || got.Nodes[0].X != 12.5 {
		t.Fatalf("got %#v, want persisted node", got)
	}
	if len(got.Edges) != 1 || got.Edges[0].ID != "relation-1" {
		t.Fatalf("got %#v, want persisted edge", got)
	}
	if got.View.GridSize != 40 || got.View.EdgeStyle != "straight" {
		t.Fatalf("got view %#v, want defaulted grid_size=40, edge_style=straight for a document saved without one", got.View)
	}

	viewed := DeviceMap{
		Version: 1,
		View:    DeviceMapView{SnapToGrid: true, ShowGrid: true, GridSize: 20, EdgeStyle: "curved"},
	}
	if err := store.SaveDeviceMap(viewed); err != nil {
		t.Fatal(err)
	}
	if got, err = store.LoadDeviceMap(); err != nil {
		t.Fatal(err)
	} else if got.View != (DeviceMapView{SnapToGrid: true, ShowGrid: true, GridSize: 20, EdgeStyle: "curved"}) {
		t.Fatalf("got view %#v, want the saved view round-tripped", got.View)
	}

	if err := store.SaveDeviceMap(DeviceMap{View: DeviceMapView{EdgeStyle: "diagonal"}}); err == nil {
		t.Fatal("expected an unknown edge_style to be rejected by the schema")
	}

	if err := store.SaveDeviceMap(DeviceMap{Nodes: []DeviceMapNode{{DeviceID: "dup"}, {DeviceID: "dup"}}}); err == nil {
		t.Fatal("expected duplicate device-map node to be rejected")
	}
	if err := store.SaveDeviceMap(DeviceMap{Edges: []RelationOverride{{ID: "dup", ChildID: "a", ParentID: "b", Kind: "via_device"}, {ID: "dup", ChildID: "c", ParentID: "d", Kind: "via_device"}}}); err == nil {
		t.Fatal("expected duplicate device-map edge id to be rejected")
	}
}

func TestDeviceMapRevisionsCanBeReadAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	first := DeviceMap{Version: 1, Nodes: []DeviceMapNode{{DeviceID: "device_a", X: 1, Y: 1}}}
	second := DeviceMap{Version: 1, Nodes: []DeviceMapNode{{DeviceID: "device_b", X: 2, Y: 2}}}
	if err := store.SaveDeviceMap(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeviceMap(second); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.DeviceMapRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d device-map revisions, want 1", len(revisions))
	}
	data, err := store.ReadDeviceMapRevision(revisions[0].Name)
	if err != nil || !json.Valid(data) {
		t.Fatalf("read device-map revision: %v, %s", err, data)
	}
	restored, err := store.RestoreDeviceMap(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Nodes) != 1 || restored.Nodes[0].DeviceID != "device_a" {
		t.Fatalf("restored device-map %#v, want first device-map", restored)
	}
	if revisions, err = store.DeviceMapRevisions(); err != nil || len(revisions) != 2 {
		t.Fatalf("got %d revisions after restore, err %v; want 2", len(revisions), err)
	}
}

func TestSchemaDeclaresDashboardDefaults(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Default any `json:"default"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(settingsSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["health_score_threshold"].Default; got != float64(3) {
		t.Fatalf("health score default = %#v, want 3", got)
	}
	if got := schema.Properties["sweep_interval_seconds"].Default; got != float64(300) {
		t.Fatalf("sweep interval default = %#v, want 300", got)
	}
	if got := schema.Properties["show_discovery_tooltips"].Default; got != true {
		t.Fatalf("discovery tooltip default = %#v, want true", got)
	}
	if got := schema.Properties["show_runtime_status"].Default; got != true {
		t.Fatalf("runtime status default = %#v, want true", got)
	}
}

func TestSchemaDeclaresDeviceViewModeEnum(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(settingsSchema, &schema); err != nil {
		t.Fatal(err)
	}
	mode := schema.Properties["device_view_mode"].Enum
	if len(mode) != 2 || mode[0] != DeviceViewModeControl || mode[1] != DeviceViewModeCompact {
		t.Fatalf("device_view_mode enum = %v", mode)
	}
}

// TestSaveLayoutAcceptsTheSixAlternativeEnergyGraphicTypes covers the item
// types added for energiegrafiken-sechs-varianten.md: each is a singleton
// type with no per-item configuration (same as energy_flow), so accepting
// it is purely a layout.schema.json enum change - the rest of Item,
// cloneLayout, normalizeLayout and validateLayout are untouched.
func TestSaveLayoutAcceptsTheSixAlternativeEnergyGraphicTypes(t *testing.T) {
	for _, itemType := range []string{"energy_band", "energy_ring", "energy_board", "energy_day", "energy_schema", "energy_status"} {
		t.Run(itemType, func(t *testing.T) {
			store := NewStore(t.TempDir())
			layout := Layout{Pages: []Page{{
				ID: "overview", Name: "Overview", Order: 0,
				Groups: []Group{{ID: "main", Name: "Main", Items: []Item{{ID: "item", Type: itemType, Span: "full", Visible: true}}}},
			}}}
			if err := store.SaveLayout(layout); err != nil {
				t.Fatalf("SaveLayout(%s) = %v, want nil", itemType, err)
			}
			got, err := store.LoadLayout()
			if err != nil {
				t.Fatal(err)
			}
			if got.Pages[0].Groups[0].Items[0].Type != itemType {
				t.Fatalf("loaded item type = %q, want %q", got.Pages[0].Groups[0].Items[0].Type, itemType)
			}
		})
	}
}

func TestSaveLayoutRejectsAnUnknownItemType(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Pages: []Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []Group{{ID: "main", Name: "Main", Items: []Item{{ID: "item", Type: "energy_bogus", Span: "full", Visible: true}}}},
	}}}
	if err := store.SaveLayout(layout); err == nil {
		t.Fatal("SaveLayout accepted an unknown item type")
	}
}

func TestSaveLayoutAcceptsEntityValueAndEntityGroupTypes(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Pages: []Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []Group{{ID: "main", Name: "Main", Items: []Item{
			{ID: "value", Type: "entity_value", Ref: "sensor.battery_soc", Span: "1", Visible: true},
			{ID: "group", Type: "entity_group", Span: "1", Visible: true, Title: "Temperatursensoren", EntityRefs: []string{"sensor.energy_temp", "sensor.lager_temp"}},
		}}},
	}}}
	if err := store.SaveLayout(layout); err != nil {
		t.Fatalf("SaveLayout(entity_value, entity_group) = %v, want nil", err)
	}
	got, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	items := got.Pages[0].Groups[0].Items
	if items[0].Type != "entity_value" || items[0].Ref != "sensor.battery_soc" {
		t.Fatalf("entity_value item = %#v", items[0])
	}
	group := items[1]
	if group.Type != "entity_group" || group.Title != "Temperatursensoren" {
		t.Fatalf("entity_group item = %#v", group)
	}
	if len(group.EntityRefs) != 2 || group.EntityRefs[0] != "sensor.energy_temp" || group.EntityRefs[1] != "sensor.lager_temp" {
		t.Fatalf("entity_group.EntityRefs = %#v, want the two refs in order", group.EntityRefs)
	}
}

// TestNormalizeLayoutEntityGroupTitleAndRefsAreTypeScoped spiegelt
// TestLayoutDefaultsEnergyGraphicOptionsPerType: Title bekommt einen Default,
// wenn er beim Speichern leer war, und beide Felder bleiben leer/nil bei
// jedem anderen Kartentyp - selbst wenn ihn jemand direkt in die Item-Werte
// schreibt, statt ueber den Editor.
func TestNormalizeLayoutEntityGroupTitleAndRefsAreTypeScoped(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "group", Type: "entity_group", Span: "1", Visible: true},
			{ID: "value", Type: "entity_value", Ref: "sensor.x", Span: "1", Visible: true, Title: "sollte verschwinden", EntityRefs: []string{"sensor.x"}},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	group, value := loaded.Pages[0].Groups[0].Items[0], loaded.Pages[0].Groups[0].Items[1]
	if group.Title != "Entitäten" {
		t.Fatalf("entity_group ohne Titel = %q, want den Default \"Entitäten\"", group.Title)
	}
	if value.Title != "" || len(value.EntityRefs) != 0 {
		t.Fatalf("entity_value darf keine entity_group-Felder tragen: %#v", value)
	}
}

func TestNormalizeLayoutDefaultsBatteryStatusDisplay(t *testing.T) {
	layout := normalizeLayout(Layout{Version: 3, Pages: []Page{{ID: "p", Name: "P", Groups: []Group{{
		ID: "g", Name: "G", Items: []Item{
			{ID: "a", Type: "battery_status", Span: "1", Visible: true},
			{ID: "b", Type: "battery_status", Display: "trajectory", Span: "2", Visible: true},
			{ID: "c", Type: "battery_status", Display: "compact", Span: "1", Visible: true},
		},
	}}}}})
	items := layout.Pages[0].Groups[0].Items
	if items[0].Display != "column" {
		t.Fatalf("Display ohne Wert = %q, want \"column\"", items[0].Display)
	}
	if items[1].Display != "trajectory" {
		t.Fatalf("Display trajectory = %q, want \"trajectory\"", items[1].Display)
	}
	// "compact" gehoert dem Geraetetyp, nicht dieser Karte.
	if items[2].Display != "column" {
		t.Fatalf("fremdes Display = %q, want \"column\"", items[2].Display)
	}
}

func TestNormalizeLayoutBatteryTrajectoryWindow(t *testing.T) {
	layout := normalizeLayout(Layout{Version: 3, Pages: []Page{{ID: "p", Name: "P", Groups: []Group{{
		ID: "g", Name: "G", Items: []Item{
			// Trajektorie ohne Angabe -> Sechs-Stunden-Default, Projektion folgt.
			{ID: "a", Type: "battery_status", Display: "trajectory", Span: "2", Visible: true},
			// Eigene Werte, Projektion abweichend.
			{ID: "b", Type: "battery_status", Display: "trajectory", Span: "2", Visible: true, BatteryWindow: "24", BatteryProjectionWindow: "3"},
			// Unbekannte Werte fallen zurueck: window auf den Default, eine
			// ungueltige Projektion auf "" (= folgt dem Fenster).
			{ID: "c", Type: "battery_status", Display: "trajectory", Span: "2", Visible: true, BatteryWindow: "7", BatteryProjectionWindow: "99"},
			// Die Saeule kennt kein Zeitfenster.
			{ID: "d", Type: "battery_status", Display: "column", Span: "1", Visible: true, BatteryWindow: "12"},
		},
	}}}}})
	items := layout.Pages[0].Groups[0].Items
	if items[0].BatteryWindow != "6" || items[0].BatteryProjectionWindow != "" {
		t.Fatalf("trajectory default = %q/%q, want \"6\"/\"\"", items[0].BatteryWindow, items[0].BatteryProjectionWindow)
	}
	if items[1].BatteryWindow != "24" || items[1].BatteryProjectionWindow != "3" {
		t.Fatalf("trajectory explizit = %q/%q, want \"24\"/\"3\"", items[1].BatteryWindow, items[1].BatteryProjectionWindow)
	}
	if items[2].BatteryWindow != "6" || items[2].BatteryProjectionWindow != "" {
		t.Fatalf("trajectory fallback = %q/%q, want \"6\"/\"\"", items[2].BatteryWindow, items[2].BatteryProjectionWindow)
	}
	if items[3].BatteryWindow != "" || items[3].BatteryProjectionWindow != "" {
		t.Fatalf("Saeule traegt ein Zeitfenster: %q/%q", items[3].BatteryWindow, items[3].BatteryProjectionWindow)
	}
}

func TestStoreDefaultsThemeToMint(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"health_score_threshold":3,"sweep_interval_seconds":300,"show_discovery_tooltips":true,"show_runtime_status":false,"device_view_mode":"control"}`
	if err := os.WriteFile(dir+"/settings.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if value.Theme != ThemeMint {
		t.Fatalf("migrated theme = %q, want %q", value.Theme, ThemeMint)
	}
}

func TestStoreRejectsUnknownTheme(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveSettings(Settings{
		HealthScoreThreshold: 3, SweepIntervalSeconds: 300,
		ShowDiscoveryTooltips: true, ShowRuntimeStatus: true,
		DeviceViewMode: DeviceViewModeControl, Theme: "kupferwerk",
	}); err == nil {
		t.Fatal("unknown theme was accepted")
	}
}

func TestStorePersistsTheme(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveSettings(Settings{
		HealthScoreThreshold: 3, SweepIntervalSeconds: 300,
		ShowDiscoveryTooltips: true, ShowRuntimeStatus: true,
		DeviceViewMode: DeviceViewModeControl, Theme: ThemeTageslicht,
	}); err != nil {
		t.Fatal(err)
	}
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if value.Theme != ThemeTageslicht {
		t.Fatalf("theme = %q, want %q", value.Theme, ThemeTageslicht)
	}
}

func TestSchemaDeclaresThemeEnum(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(settingsSchema, &schema); err != nil {
		t.Fatal(err)
	}
	theme := schema.Properties["theme"].Enum
	want := []string{ThemeMint, ThemeStromblau, ThemeSignalgelb, ThemeTageslicht}
	if len(theme) != len(want) {
		t.Fatalf("theme enum = %#v", theme)
	}
	for i := range want {
		if theme[i] != want[i] {
			t.Fatalf("theme enum = %#v, want %#v", theme, want)
		}
	}
}

func TestNormalizeLayoutDefaultsEnergyFlowSpeedReferenceToRelative1000W(t *testing.T) {
	dir := t.TempDir()
	// Legacy layout.json ohne die neuen Felder - normalizeLayout() muss sie
	// beim Laden nachtragen, genau wie es fuer FlowScale schon tut.
	legacy := `{"version":3,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[{"id":"item","type":"energy_flow","span":"full","visible":true,"flow_scale":"speed"}]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	layout, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	item := layout.Pages[0].Groups[0].Items[0]
	if item.SpeedReferenceMode != EnergyFlowSpeedReferenceModeRelative {
		t.Fatalf("defaulted speed_reference_mode = %q, want %q", item.SpeedReferenceMode, EnergyFlowSpeedReferenceModeRelative)
	}
	if item.SpeedReferenceWatts != 1000 {
		t.Fatalf("defaulted speed_reference_watts = %d, want 1000", item.SpeedReferenceWatts)
	}
}

func TestSaveLayoutAcceptsAValidSpeedReferenceModeAndRejectsAnInvalidOne(t *testing.T) {
	base := func(mode string) Layout {
		return Layout{Pages: []Page{{
			ID: "overview", Name: "Overview", Order: 0,
			Groups: []Group{{ID: "main", Name: "Main", Items: []Item{
				{ID: "item", Type: "energy_flow", Span: "full", Visible: true, FlowScale: "speed", SpeedReferenceMode: mode, SpeedReferenceWatts: 1000},
			}}},
		}}}
	}
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(base(EnergyFlowSpeedReferenceModeFixed)); err != nil {
		t.Fatalf("SaveLayout(speed_reference_mode=fixed) = %v, want nil", err)
	}
	if err := store.SaveLayout(base("bogus")); err == nil {
		t.Fatal("SaveLayout accepted an unknown speed_reference_mode value")
	}
}

func TestSaveLayoutPersistsSpeedReferenceFixedModeAndWatts(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(Layout{Pages: []Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []Group{{ID: "main", Name: "Main", Items: []Item{
			{ID: "item", Type: "energy_flow", Span: "full", Visible: true, FlowScale: "speed", SpeedReferenceMode: EnergyFlowSpeedReferenceModeFixed, SpeedReferenceWatts: 3000},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	layout, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	item := layout.Pages[0].Groups[0].Items[0]
	if item.SpeedReferenceMode != EnergyFlowSpeedReferenceModeFixed || item.SpeedReferenceWatts != 3000 {
		t.Fatalf("speed reference = %q/%d, want fixed/3000", item.SpeedReferenceMode, item.SpeedReferenceWatts)
	}
}

func TestStorePersistsBatterySoCCapacity(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	want := EnergyConfig{Assignments: map[string]energy.Assignment{
		"bank_a_soc": {Role: energy.RoleBatterySoC, CapacityKWh: 12.8},
	}}
	if err := store.SaveEnergy(want); err != nil {
		t.Fatalf("SaveEnergy: %v", err)
	}
	// Frischer Store, damit wirklich von der Platte gelesen wird und nicht
	// aus dem Cache in s.energyValue.
	got, err := NewStore(dir).LoadEnergy()
	if err != nil {
		t.Fatalf("LoadEnergy: %v", err)
	}
	if got.Assignments["bank_a_soc"] != want.Assignments["bank_a_soc"] {
		t.Fatalf("assignment = %#v, want %#v", got.Assignments["bank_a_soc"], want.Assignments["bank_a_soc"])
	}
}

// TestLayoutMigratesV2GeometryToFlowOrder deckt den Kern von Spec F ab: aus
// (y, x) wird Reihenfolge, aus w wird die Groessenklasse, h faellt ersatzlos
// weg und keines der vier Felder steht danach noch im geschriebenen JSON.
func TestLayoutMigratesV2GeometryToFlowOrder(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":2,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[
		{"id":"c","type":"entity","ref":"e3","span":"1","visible":true,"x":2,"y":1,"w":1,"h":1},
		{"id":"a","type":"entity","ref":"e1","span":"1","visible":true,"x":0,"y":0,"w":1,"h":2},
		{"id":"b","type":"energy_board","ref":"","span":"2","visible":true,"x":1,"y":0,"w":3,"h":1}
	]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	layout, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	if layout.Version != 3 {
		t.Fatalf("version = %d, want 3", layout.Version)
	}
	items := layout.Pages[0].Groups[0].Items
	gotOrder := []string{items[0].ID, items[1].ID, items[2].ID}
	if gotOrder[0] != "a" || gotOrder[1] != "b" || gotOrder[2] != "c" {
		t.Fatalf("Reihenfolge = %v, want [a b c]", gotOrder)
	}
	if items[2].Span != "1" || items[1].Span != "3" {
		t.Fatalf("Spannweiten = %q/%q, want 1/3", items[2].Span, items[1].Span)
	}
	for _, item := range items {
		if item.X != 0 || item.Y != 0 || item.W != 0 || item.H != 0 {
			t.Fatalf("Altgeometrie ueberlebt: %#v", item)
		}
		if item.Height != 0 {
			t.Fatalf("h wurde als Zwangshoehe uebernommen: %#v", item)
		}
	}

	// Das wieder geschriebene Dokument darf keines der vier Felder tragen.
	if err := store.SaveLayout(layout); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(dir + "/layout.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"x"`, `"y"`, `"w"`, `"h"`} {
		if strings.Contains(string(written), field) {
			t.Fatalf("geschriebenes v3-Dokument enthaelt %s: %s", field, written)
		}
	}
}

// TestLayoutMigrationKeepsV1DocumentOrder: v1-Dokumente haben ueberhaupt keine
// Geometrie, alle Items liegen damit auf (0,0). Nur eine *stabile* Sortierung
// haelt dort die Dokumentreihenfolge.
func TestLayoutMigrationKeepsV1DocumentOrder(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":2,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[
		{"id":"eins","type":"entity","ref":"e1","span":"1","visible":true},
		{"id":"zwei","type":"entity","ref":"e2","span":"1","visible":true},
		{"id":"drei","type":"entity","ref":"e3","span":"1","visible":true}
	]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	layout, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	items := layout.Pages[0].Groups[0].Items
	for index, want := range []string{"eins", "zwei", "drei"} {
		if items[index].ID != want {
			t.Fatalf("Position %d = %q, want %q", index, items[index].ID, want)
		}
	}
}

// TestLayoutMigrationMapsFullWidth: w=4 bedeutete im v2-Raster "volle Breite",
// nicht "vier Spuren". Die Absicht wiegt schwerer als die Zahl.
func TestLayoutMigrationMapsFullWidth(t *testing.T) {
	for _, testCase := range []struct {
		width int
		want  string
	}{
		{1, "1"}, {2, "2"}, {3, "3"}, {4, "full"},
	} {
		dir := t.TempDir()
		legacy := fmt.Sprintf(`{"version":2,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[
			{"id":"a","type":"diagnostics","span":"1","visible":true,"x":0,"y":0,"w":%d,"h":1}
		]}]}]}`, testCase.width)
		if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
			t.Fatal(err)
		}
		layout, err := NewStore(dir).LoadLayout()
		if err != nil {
			t.Fatalf("w=%d: %v", testCase.width, err)
		}
		if got := layout.Pages[0].Groups[0].Items[0].Span; got != testCase.want {
			t.Errorf("w=%d migriert zu %q, want %q", testCase.width, got, testCase.want)
		}
	}
}

// TestLayoutMigrationClampsToMinSpan: eine energy_band-Kachel, die im alten
// Raster eine Spalte breit war, wird hochgeklemmt statt abgewiesen - sonst
// waere ein bestehendes Layout nach dem Update nicht mehr ladbar.
func TestLayoutMigrationClampsToMinSpan(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":2,"pages":[{"id":"p","name":"P","order":0,"groups":[{"id":"g","name":"G","items":[
		{"id":"a","type":"energy_band","span":"1","visible":true,"x":0,"y":0,"w":1,"h":1}
	]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	layout, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatalf("hochklemmbares Layout wurde abgewiesen: %v", err)
	}
	if got := layout.Pages[0].Groups[0].Items[0].Span; got != "2" {
		t.Fatalf("span = %q, want 2", got)
	}
}

// TestLayoutRejectsSpanBelowMinimum: in einem *v3*-Dokument gibt es kein
// Hochklemmen mehr - wer span "1" fuer energy_flow schickt, bekommt einen
// Fehler, in dem Item-ID und Typ stehen.
func TestLayoutRejectsSpanBelowMinimum(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "flow", Type: "energy_flow", Span: "1", Visible: true},
		}}},
	}}})
	if err == nil {
		t.Fatal("zu schmale Spannweite wurde angenommen")
	}
	for _, fragment := range []string{"flow", "energy_flow"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("Fehlertext %q nennt %q nicht", err, fragment)
		}
	}
}

func TestLayoutRejectsUnknownItemType(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "x", Type: "gibt-es-nicht", Span: "1", Visible: true},
		}}},
	}}})
	if err == nil {
		t.Fatal("unbekannter Item-Typ wurde angenommen")
	}
}

// TestLayoutSaveLoadIsStable: ein zweiter Durchlauf durch
// normalizeLayout/validateLayout darf nichts mehr aendern. Das ist die
// Zusage, dass die Migration einmal laeuft und nicht bei jedem Speichern.
func TestLayoutSaveLoadIsStable(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "sum", Type: "energy_summary", Span: "full", Visible: true},
			{ID: "board", Type: "energy_board", Span: "3", Visible: true, Height: 3},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	first, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	if err := NewStore(dir).SaveLayout(first); err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(dir).LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("zweiter Durchlauf aenderte das Layout:\n%#v\n%#v", first, second)
	}
}

// TestLayoutDefaultsEnergyGraphicOptionsPerType prueft
// normalizeEnergyGraphicOptions (siehe
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md): ein leeres
// Feld bekommt den Typ-Default, sobald item.Type passt.
func TestLayoutDefaultsEnergyGraphicOptionsPerType(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "band", Type: "energy_band", Span: "2", Visible: true},
			{ID: "status", Type: "energy_status", Span: "1", Visible: true},
			{ID: "schema", Type: "energy_schema", Span: "2", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	items := loaded.Pages[0].Groups[0].Items
	band, status, schema := items[0], items[1], items[2]
	if band.HeightReference != "fill" || band.ScaleMode != "linear" || band.Unit != "auto" || band.BundleThreshold != "0" || band.Animate != "on" {
		t.Fatalf("energy_band defaults not applied: %#v", band)
	}
	if status.BeamSpan != "6000" || status.ShowAdvice != "on" {
		t.Fatalf("energy_status defaults not applied: %#v", status)
	}
	// energy_schema animiert per Vorgabe nicht (anders als energy_band/
	// energy_ring) - ein bewegter Pfeil auf jeder frisch angelegten Karte
	// waere ueberraschend, siehe Spec Abschnitt 3.
	if schema.StrokeMode != "power" || schema.EntityLabels != "power" || schema.HideInactive != "off" || schema.DisplaySize != "m" || schema.Animate != "off" {
		t.Fatalf("energy_schema defaults not applied: %#v", schema)
	}
	// Felder des jeweils anderen Kartentyps bleiben leer.
	if band.BeamSpan != "" || band.ShowAdvice != "" {
		t.Fatalf("energy_band must not carry energy_status fields: %#v", band)
	}
	if status.HeightReference != "" || status.Animate != "" {
		t.Fatalf("energy_status must not carry energy_band fields: %#v", status)
	}
	if schema.HeightReference != "" {
		t.Fatalf("energy_schema must not carry energy_band fields: %#v", schema)
	}
}

// TestLayoutKeepsAnExplicitEnergyGraphicOption prueft, dass ein explizit
// gesetzter Wert (hier "abs" statt des Defaults "fill") die Normalisierung
// uebersteht.
func TestLayoutKeepsAnExplicitEnergyGraphicOption(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "band", Type: "energy_band", Span: "2", Visible: true, HeightReference: "abs", Animate: "off"},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	band := loaded.Pages[0].Groups[0].Items[0]
	if band.HeightReference != "abs" {
		t.Fatalf("height_reference = %q, want the explicit value abs to survive normalization", band.HeightReference)
	}
	if band.Animate != "off" {
		t.Fatalf("animate = %q, want the explicit value off to survive normalization", band.Animate)
	}
}

// TestLayoutRejectsUnknownEnergyGraphicOptionValue prueft, dass ein Wert
// ausserhalb des Enums (nicht bloss ein leeres Feld) trotzdem auf den
// Default zurueckfaellt statt durchgereicht zu werden - defaultString()
// vergleicht gegen die erlaubte Menge, nicht nur gegen "".
func TestLayoutRejectsUnknownEnergyGraphicOptionValue(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []Group{{ID: "g", Name: "G", Items: []Item{
			{ID: "band", Type: "energy_band", Span: "2", Visible: true, HeightReference: "does-not-exist"},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatal(err)
	}
	band := loaded.Pages[0].Groups[0].Items[0]
	if band.HeightReference != "fill" {
		t.Fatalf("height_reference = %q, want the default fill for an out-of-enum input", band.HeightReference)
	}
}

// TestSettingsWidePanelsDefault: die Vorgabe gibt genau die vier Tabs frei,
// die von Breite profitieren. Die textlastigen Tabs behalten die
// 1180px-Lesebreite.
func TestSettingsWidePanelsDefault(t *testing.T) {
	value, err := NewStore(t.TempDir()).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"overview", "devices", "history", "layout"}
	if !reflect.DeepEqual(value.WidePanels, want) {
		t.Fatalf("WidePanels = %v, want %v", value.WidePanels, want)
	}
}

// Eine leere Auswahl ist eine gueltige Auswahl - "alle Tabs gedeckelt" darf
// nicht als "nicht gesetzt" durchgehen und auf die Vorgabe zurueckfallen.
func TestSettingsWidePanelsAcceptsEmptySelection(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.WidePanels = []string{}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.WidePanels) != 0 {
		t.Fatalf("WidePanels = %v, want leer", reloaded.WidePanels)
	}
}

func TestSettingsRejectsUnknownWidePanel(t *testing.T) {
	store := NewStore(t.TempDir())
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.WidePanels = []string{"gibt-es-nicht"}
	if err := store.SaveSettings(value); err == nil {
		t.Fatal("unbekannter Tab-Schluessel wurde angenommen")
	}
}

// TestSettingsStatusBarItemsDefault: die Vorgabe zeigt MQTT, Storage,
// Uptime und die Dashboard-Version, aber nicht den Cache-Status.
func TestSettingsStatusBarItemsDefault(t *testing.T) {
	value, err := NewStore(t.TempDir()).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mqtt", "storage", "uptime", "version"}
	if !reflect.DeepEqual(value.StatusBarItems, want) {
		t.Fatalf("StatusBarItems = %v, want %v", value.StatusBarItems, want)
	}
}

func TestSettingsStatusBarItemsAcceptsEmptySelection(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.StatusBarItems = []string{}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(dir).LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.StatusBarItems) != 0 {
		t.Fatalf("StatusBarItems = %v, want leer", reloaded.StatusBarItems)
	}
}

func TestSettingsRejectsUnknownStatusBarItem(t *testing.T) {
	store := NewStore(t.TempDir())
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	value.StatusBarItems = []string{"gibt-es-nicht"}
	if err := store.SaveSettings(value); err == nil {
		t.Fatal("unbekannter Statuszeilen-Schluessel wurde angenommen")
	}
}

func TestSettingsRevisionsCanBeReadAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	first := Default()
	first.HealthScoreThreshold = 3
	second := Default()
	second.HealthScoreThreshold = 7
	if err := store.SaveSettings(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(second); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.SettingsRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d settings revisions, want 1", len(revisions))
	}
	data, err := store.ReadSettingsRevision(revisions[0].Name)
	if err != nil || !json.Valid(data) {
		t.Fatalf("read settings revision: %v, %s", err, data)
	}
	restored, err := store.RestoreSettings(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if restored.HealthScoreThreshold != 3 {
		t.Fatalf("restored threshold %d, want 3", restored.HealthScoreThreshold)
	}
	// Der Restore ist selbst ein Speichervorgang und legt deshalb eine
	// weitere Revision an - genau wie bei Layout.
	if revisions, err = store.SettingsRevisions(); err != nil || len(revisions) != 2 {
		t.Fatalf("got %d revisions after restore, err %v; want 2", len(revisions), err)
	}
	// Der Store muss den wiederhergestellten Wert auch zwischengespeichert
	// haben, nicht nur auf die Platte geschrieben.
	if loaded, err := store.LoadSettings(); err != nil || loaded.HealthScoreThreshold != 3 {
		t.Fatalf("loaded threshold %d, err %v; want 3", loaded.HealthScoreThreshold, err)
	}
}

func TestEnergyRevisionsCanBeReadAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	first := EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.pv": {Role: "pv", Scale: 1},
	}}
	second := EnergyConfig{Assignments: map[string]energy.Assignment{
		"sensor.grid": {Role: "grid", Scale: 1},
	}}
	if err := store.SaveEnergy(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEnergy(second); err != nil {
		t.Fatal(err)
	}
	revisions, err := store.EnergyRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("got %d energy revisions, want 1", len(revisions))
	}
	data, err := store.ReadEnergyRevision(revisions[0].Name)
	if err != nil || !json.Valid(data) {
		t.Fatalf("read energy revision: %v, %s", err, data)
	}
	restored, err := store.RestoreEnergy(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Assignments["sensor.pv"]; !ok {
		t.Fatalf("restored assignments %#v, want sensor.pv", restored.Assignments)
	}
	if _, ok := restored.Assignments["sensor.grid"]; ok {
		t.Fatalf("restored assignments %#v, want sensor.grid gone", restored.Assignments)
	}
}

func TestReadSettingsRevisionRejectsPathTraversal(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SaveSettings(Default()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../settings.json", "sub/dir.json", "settings", ""} {
		if _, err := store.ReadSettingsRevision(name); err == nil {
			t.Fatalf("read revision %q succeeded, want rejection", name)
		}
	}
}

func TestSettingsRevisionsAreCappedAtLimit(t *testing.T) {
	store := NewStore(t.TempDir())
	// revisionLimit+3 Speichervorgänge erzeugen revisionLimit+2 Revisionen,
	// weil der erste Speichervorgang keine Vorgängerdatei zum Sichern hat.
	for i := 0; i < revisionLimit+3; i++ {
		value := Default()
		value.HealthScoreThreshold = i + 1
		if err := store.SaveSettings(value); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	revisions, err := store.SettingsRevisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != revisionLimit {
		t.Fatalf("got %d revisions, want the limit of %d", len(revisions), revisionLimit)
	}
	// Die verbliebene älteste Revision muss den Wert aus dem dritten
	// Speichervorgang tragen: die beiden davor sind weggeschnitten worden.
	data, err := store.ReadSettingsRevision(revisions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	var oldest Settings
	if err := json.Unmarshal(data, &oldest); err != nil {
		t.Fatal(err)
	}
	if oldest.HealthScoreThreshold != 3 {
		t.Fatalf("oldest kept revision has threshold %d, want 3", oldest.HealthScoreThreshold)
	}
}

func TestSweepIntervalDefaultAppliesWithoutSettingsFile(t *testing.T) {
	store := NewStore(t.TempDir())
	store.SetSweepIntervalDefault(120)
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if value.SweepIntervalSeconds != 120 {
		t.Fatalf("SweepIntervalSeconds = %d, erwartet 120", value.SweepIntervalSeconds)
	}
}

func TestSweepIntervalFromSettingsFileWinsOverDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"sweep_interval_seconds": 45}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	store.SetSweepIntervalDefault(120)
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if value.SweepIntervalSeconds != 45 {
		t.Fatalf("SweepIntervalSeconds = %d, erwartet 45", value.SweepIntervalSeconds)
	}
}

func TestSweepIntervalDefaultAppliesWhenKeyAbsentFromSettingsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"theme": "mint"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	store.SetSweepIntervalDefault(120)
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if value.SweepIntervalSeconds != 120 {
		t.Fatalf("SweepIntervalSeconds = %d, erwartet 120", value.SweepIntervalSeconds)
	}
}

// measured_split gibt es nur bei den drei Grafiken, die eine variable
// Positionsliste rendern. energy_day (Historie kennt nur Rollensummen),
// energy_schema (feste Zweiggeometrie) und energy_flow (feste Knoten) zeigen
// den gemessenen Verbrauch immer gesammelt und duerfen das Feld deshalb gar
// nicht erst tragen.
func TestSaveLayoutNormalizesMeasuredSplit(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Pages: []Page{{ID: "p", Name: "P", Order: 0, Groups: []Group{{ID: "g", Name: "G", Items: []Item{
		{ID: "band", Type: "energy_band", Span: "full", Visible: true},
		{ID: "ring", Type: "energy_ring", Span: "2", Visible: true, MeasuredSplit: "entities"},
		{ID: "board", Type: "energy_board", Span: "2", Visible: true, MeasuredSplit: "quatsch"},
		{ID: "day", Type: "energy_day", Span: "2", Visible: true, MeasuredSplit: "entities"},
	}}}}}}

	if err := store.SaveLayout(layout); err != nil {
		t.Fatalf("SaveLayout = %v, want nil", err)
	}
	saved, err := store.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout = %v, want nil", err)
	}
	items := saved.Pages[0].Groups[0].Items
	for _, want := range []struct {
		index int
		value string
	}{{0, "sum"}, {1, "entities"}, {2, "sum"}, {3, ""}} {
		if got := items[want.index].MeasuredSplit; got != want.value {
			t.Fatalf("items[%d].MeasuredSplit = %q, want %q", want.index, got, want.value)
		}
	}
}

func TestHistorySettingsDefaultsAndNormalisation(t *testing.T) {
	store := NewStore(t.TempDir())
	value, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if value.HistorySampleIntervalSeconds != 10 {
		t.Fatalf("default sample interval = %d, want 10", value.HistorySampleIntervalSeconds)
	}
	if value.HistoryRetentionMode != HistoryRetentionModeTime {
		t.Fatalf("default retention mode = %q, want %q", value.HistoryRetentionMode, HistoryRetentionModeTime)
	}
	// Sechs Stunden ist das Verhalten der bisherigen Browser-Historie und
	// bleibt bewusst der Default, damit ein Update nichts still veraendert.
	if value.HistoryRetentionHours != 6 {
		t.Fatalf("default retention hours = %d, want 6", value.HistoryRetentionHours)
	}
	if value.HistoryBudgetMB != 512 {
		t.Fatalf("default budget = %d, want 512", value.HistoryBudgetMB)
	}
	if value.HistoryRawWindowHours != 24 {
		t.Fatalf("default raw window = %d, want 24", value.HistoryRawWindowHours)
	}
	if value.HistoryMinuteWindowDays != 7 {
		t.Fatalf("default minute window = %d, want 7", value.HistoryMinuteWindowDays)
	}
	if value.HistoryExtraEntities == nil || len(value.HistoryExtraEntities) != 0 {
		t.Fatalf("default extra entities = %#v, want empty slice", value.HistoryExtraEntities)
	}
	if value.HistoryViews == nil || len(value.HistoryViews) != 0 {
		t.Fatalf("default views = %#v, want empty slice", value.HistoryViews)
	}

	// Ein Teil-Save ohne Historie-Felder darf nicht an der Schema-Validierung
	// scheitern - normalizeSettings muss die Nullwerte auffuellen.
	if err := store.SaveSettings(Settings{HealthScoreThreshold: 4, SweepIntervalSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.HistorySampleIntervalSeconds != 10 || reloaded.HistoryRetentionMode != HistoryRetentionModeTime {
		t.Fatalf("history settings not normalised after partial save: %#v", reloaded)
	}
}

func TestHistorySettingsRejectInvalidValues(t *testing.T) {
	base := Default()

	tooFast := base
	tooFast.HistorySampleIntervalSeconds = 4
	if err := validateSettings(tooFast); err == nil {
		t.Fatal("expected a sample interval below the 5s floor to be rejected")
	}

	badMode := base
	badMode.HistoryRetentionMode = "sowohl-als-auch"
	if err := validateSettings(badMode); err == nil {
		t.Fatal("expected an unknown retention mode to be rejected")
	}

	negativeBudget := base
	negativeBudget.HistoryBudgetMB = -1
	if err := validateSettings(negativeBudget); err == nil {
		t.Fatal("expected a negative storage budget to be rejected")
	}

	valid := base
	valid.HistoryRetentionMode = HistoryRetentionModeSize
	valid.HistoryBudgetMB = 1024
	valid.HistoryExtraEntities = []string{"shelly_em_power"}
	valid.HistoryViews = []HistoryView{{ID: "energie", Name: "Energie", Series: []string{"role:pv"}, RangeHours: 24, Aggregate: "avg"}}
	if err := validateSettings(valid); err != nil {
		t.Fatalf("expected a valid size-mode configuration to pass: %v", err)
	}
}

func TestAustauschIstStandardmaessigAn(t *testing.T) {
	if Default().HistoryExchangeDisabled {
		t.Fatal("der Verlauf-Austausch muss in der Vorgabe an sein")
	}
	// Der Nullwert eines nicht gesetzten Feldes muss dieselbe Bedeutung
	// haben wie die Vorgabe - sonst waere die Einstellung nicht
	// abschaltbar, weil normalizeSettings sie zurueckdrehte.
	normalized := normalizeSettings(Settings{})
	if normalized.HistoryExchangeDisabled {
		t.Fatal("nicht gesetzt muss 'an' bedeuten")
	}
}

func TestAustauschLaesstSichAbschalten(t *testing.T) {
	normalized := normalizeSettings(Settings{HistoryExchangeDisabled: true})
	if !normalized.HistoryExchangeDisabled {
		t.Fatal("ein bewusstes Abschalten darf nicht zurueckgedreht werden")
	}
	disabled := Default()
	disabled.HistoryExchangeDisabled = true
	if err := validateSettings(disabled); err != nil {
		t.Fatalf("abgeschaltete Einstellung ist ungueltig: %v", err)
	}
}

func TestSchemaKenntDenAustauschSchalter(t *testing.T) {
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type    string `json:"type"`
			Default any    `json:"default"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(settingsSchema, &schema); err != nil {
		t.Fatal(err)
	}
	property, ok := schema.Properties["history_exchange_disabled"]
	if !ok {
		t.Fatal("history_exchange_disabled fehlt im Schema")
	}
	if property.Type != "boolean" || property.Default != false {
		t.Fatalf("history_exchange_disabled = %+v, want boolean/false", property)
	}
	found := false
	for _, name := range schema.Required {
		if name == "history_exchange_disabled" {
			found = true
		}
	}
	if !found {
		t.Fatal("history_exchange_disabled fehlt in required")
	}
}

// Wie FlowScale bei energy_flow: der leere String ist das Sentinel fuer
// "nicht gesetzt", normalizeLayout fuellt den Typ-Default und raeumt das
// Feld bei jedem anderen Typ wieder weg. Ein Item, das den Typ wechselt,
// darf keine Darstellung aus seinem frueheren Leben mitschleppen.
func TestNormalizeLayoutFillsDeviceDisplay(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []Group{{ID: "g", Name: "Dashboard", Items: []Item{
			{ID: "device:a", Type: "device", Ref: "a", Span: "1", Visible: true},
			{ID: "device:b", Type: "device", Ref: "b", Span: "1", Visible: true, Display: "compact"},
			{ID: "diagnostics", Type: "diagnostics", Span: "1", Visible: true, Display: "compact"},
		}}},
	}}}
	if err := store.SaveLayout(layout); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	items := loaded.Pages[0].Groups[0].Items
	if items[0].Display != "detail" {
		t.Errorf("device ohne display = %q, want \"detail\"", items[0].Display)
	}
	if items[1].Display != "compact" {
		t.Errorf("device mit display=compact = %q, want \"compact\"", items[1].Display)
	}
	if items[2].Display != "" {
		t.Errorf("diagnostics behaelt display %q, want leer", items[2].Display)
	}
}

// Das Schema ist die Torwache: ein Tippfehler im Wert darf nicht still
// als "irgendwas" durchrutschen, sondern muss das Speichern ablehnen.
func TestSaveLayoutRejectsUnknownDisplay(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []Group{{ID: "g", Name: "Dashboard", Items: []Item{
			{ID: "device:a", Type: "device", Ref: "a", Span: "1", Visible: true, Display: "kompakt"},
		}}},
	}}}
	if err := store.SaveLayout(layout); err == nil {
		t.Fatal("SaveLayout hat display=\"kompakt\" angenommen")
	}
}

// Die kompakte Geraetekachel darf bis zu drei Entitaeten ihres Geraets fest
// zeigen - dasselbe Feld wie entity_group, nur fuer einen anderen Typ. Ein
// vierter Eintrag ist kein Fehler, er wird beim Speichern abgeschnitten:
// mehr als drei Zeilen passen optisch nicht in die Kachel.
func TestNormalizeLayoutKeepsAndCapsCompactDeviceEntityRefs(t *testing.T) {
	store := NewStore(t.TempDir())
	layout := Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []Group{{ID: "g", Name: "Dashboard", Items: []Item{
			{ID: "device:a", Type: "device", Ref: "a", Span: "1", Visible: true, Display: "compact",
				EntityRefs: []string{"a_1", "a_2", "a_3", "a_4"}},
			{ID: "device:b", Type: "device", Ref: "b", Span: "1", Visible: true, Display: "detail",
				EntityRefs: []string{"b_1"}},
			{ID: "device:c", Type: "device", Ref: "c", Span: "1", Visible: true, Display: "compact"},
		}}},
	}}}
	if err := store.SaveLayout(layout); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	loaded, err := store.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	items := loaded.Pages[0].Groups[0].Items
	if got := items[0].EntityRefs; len(got) != 3 || got[0] != "a_1" || got[2] != "a_3" {
		t.Errorf("compact device refs = %v, want [a_1 a_2 a_3]", got)
	}
	if items[1].EntityRefs != nil {
		t.Errorf("detail device behaelt entity_refs %v, want nil", items[1].EntityRefs)
	}
	if items[2].EntityRefs != nil {
		t.Errorf("compact device ohne Auswahl = %v, want nil", items[2].EntityRefs)
	}
}

// entity_group darf sich durch die neue device-Verzweigung nicht aendern:
// nach normalizeLayout weiterhin nie nil, sondern die leere Liste. (Der
// Save/Load-Roundtrip nil-t leere Slices ueber cloneLayout - ein
// vorbestehender Nebeneffekt, der nichts mit dieser Verzweigung zu tun hat -
// deshalb hier normalizeLayout direkt, wie die Nachbartests.)
func TestNormalizeLayoutEntityGroupRefsUnchanged(t *testing.T) {
	layout := normalizeLayout(Layout{Version: 3, Pages: []Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []Group{{ID: "g", Name: "Dashboard", Items: []Item{
			{ID: "grp", Type: "entity_group", Span: "1", Visible: true},
		}}},
	}}})
	if refs := layout.Pages[0].Groups[0].Items[0].EntityRefs; refs == nil {
		t.Error("entity_group entity_refs ist nil, want []string{}")
	}
}
