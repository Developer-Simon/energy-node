// Package settings will manage the dashboard's own settings.json
// (Health-Score threshold, runtime-cache sweep interval, and other
// operational values that are not tied to a device schema). It follows the
// same schema-validation / atomic-write / revisioning rules as the device
// configs in package config, but as its own standalone document.
//
// This package is intentionally empty in Phase 0 (Grundgerüst und Betrieb).
// It is implemented in Phase 2 - Gerätemanager und Layout.
package settings

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

//go:embed settings.schema.json
var settingsSchema []byte

//go:embed layout.schema.json
var layoutSchema []byte

//go:embed energy.schema.json
var energySchema []byte

//go:embed device-map.schema.json
var deviceMapSchema []byte

//go:embed mqtt.schema.json
var mqttSchema []byte

//go:embed bridge.schema.json
var bridgeSchema []byte

type Settings struct {
	HealthScoreThreshold         int      `json:"health_score_threshold"`
	SweepIntervalSeconds         int      `json:"sweep_interval_seconds"`
	ShowRuntimeStatus            bool     `json:"show_runtime_status"`
	DeviceViewMode               string   `json:"device_view_mode"`
	Theme                        string   `json:"theme"`
	ShowConfigEntitiesOnTile     bool     `json:"show_config_entities_on_tile"`
	ShowDiagnosticEntitiesOnTile bool     `json:"show_diagnostic_entities_on_tile"`
	LiveUpdateIntervalSeconds    int      `json:"live_update_interval_seconds"`
	WidePanels                   []string `json:"wide_panels"`
	StatusBarItems               []string `json:"status_bar_items"`

	// Verlaufs-Historie. Die Messwerte selbst liegen ausschliesslich in der
	// IndexedDB des Browsers - hier steht nur, wie aufgezeichnet und wie
	// lange aufbewahrt wird, damit die Konfiguration nicht pro Geraet
	// auseinanderlaeuft. Der Go-Prozess speichert weiterhin keine Historie.
	HistorySampleIntervalSeconds int      `json:"history_sample_interval_seconds"`
	HistoryRetentionMode         string   `json:"history_retention_mode"`
	HistoryRetentionHours        int      `json:"history_retention_hours"`
	HistoryBudgetMB              int      `json:"history_budget_mb"`
	HistoryRawWindowHours        int      `json:"history_raw_window_hours"`
	HistoryMinuteWindowDays      int      `json:"history_minute_window_days"`
	HistoryExtraEntities         []string `json:"history_extra_entities"`
	// Invertiert benannt mit Absicht: normalizeSettings erkennt "nicht
	// gesetzt" am Nullwert, und bei einem bool ist das false. Hiesse das
	// Feld ...Enabled mit Vorgabe true, waere ein bewusstes Abschalten von
	// "nicht gesetzt" nicht zu unterscheiden und wuerde jedesmal
	// zurueckgedreht.
	HistoryExchangeDisabled bool          `json:"history_exchange_disabled"`
	HistoryViews            []HistoryView `json:"history_views"`
}

// HistoryView ist eine gespeicherte Verlaufssicht. Sie liegt serverseitig,
// damit eine am Rechner angelegte Sicht auch am Telefon aufgeht - die Daten
// dahinter sind aber pro Browser, eine Sicht kann dort also leer sein.
type HistoryView struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Series     []string `json:"series"`
	RangeHours int      `json:"range_hours"`
	Aggregate  string   `json:"aggregate"`
}

const (
	DeviceViewModeControl = "control"
	DeviceViewModeCompact = "compact"

	ThemeMint       = "mint"
	ThemeStromblau  = "stromblau"
	ThemeSignalgelb = "signalgelb"
	ThemeTageslicht = "tageslicht"

	EnergyFlowSpeedReferenceModeRelative = "relative"
	EnergyFlowSpeedReferenceModeFixed    = "fixed"

	HistoryRetentionModeTime = "time"
	HistoryRetentionModeSize = "size"
)

type EnergyConfig struct {
	Assignments    map[string]energy.Assignment `json:"assignments"`
	Interpretation energy.Interpretation        `json:"interpretation"`
}

type Layout struct {
	Version int    `json:"version"`
	Pages   []Page `json:"pages"`
}

type Page struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Order  int     `json:"order"`
	Groups []Group `json:"groups"`
}

type Group struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Items     []Item   `json:"items"`
	EntityIDs []string `json:"entity_ids,omitempty"`
}

type Item struct {
	ID                string   `json:"id"`
	Type              string   `json:"type"`
	Ref               string   `json:"ref,omitempty"`
	Span              string   `json:"span"` // "1" ... "6" | "full"
	Visible           bool     `json:"visible"`
	VisibleCategories []string `json:"visible_categories,omitempty"`
	Height            int      `json:"height,omitempty"` // Zwangshoehe in 7-rem-Einheiten, 0 = keine
	FlowScale         string   `json:"flow_scale,omitempty"`

	// Bezugsleistung fuer FlowScale "speed" (Kartenmodus
	// "Animationsgeschwindigkeit", siehe energy-flow.js' flowDuration()).
	// "relative" spiegelt den Breiten-Modus: Referenz ist der jeweils
	// groesste aktive Fluss der Karte, mindestens SpeedReferenceWatts -
	// kleine Anlagen bleiben so lesbar. "fixed" nutzt immer den
	// eingestellten Wert, unabhaengig von der Anlagengroesse. Nur bei
	// FlowScale == "speed" im Editor sichtbar, aber wie FlowScale selbst
	// unabhaengig davon gespeichert.
	SpeedReferenceMode  string `json:"speed_reference_mode,omitempty"`
	SpeedReferenceWatts int    `json:"speed_reference_watts,omitempty"`

	// Konfigurationsoptionen der sechs Energiegrafiken-Alternativen (Port
	// der sechs-Varianten-Exploration, siehe
	// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md). Jedes
	// Feld ist wie FlowScale ein String-Enum mit "" als Sentinel fuer
	// "nicht gesetzt" - normalizeLayout fuellt den Typ-Default, sobald es
	// den passenden item.Type sieht, und leert das Feld sonst.
	HeightReference string `json:"height_reference,omitempty"` // energy_band: "fill" | "abs"
	ScaleMode       string `json:"scale_mode,omitempty"`       // energy_band: "linear" | "sqrt"
	Unit            string `json:"unit,omitempty"`             // energy_band: "auto" | "w" | "kw"
	BundleThreshold string `json:"bundle_threshold,omitempty"` // energy_band: "0" | "0.03" | "0.08"
	Animate         string `json:"animate,omitempty"`          // energy_band, energy_ring: "on" | "off"
	KPI             string `json:"kpi,omitempty"`              // energy_ring: "autarkie" | "eigen" | "netz" | "last"
	LabelMode       string `json:"label_mode,omitempty"`       // energy_ring: "both" | "pct" | "abs"
	Sort            string `json:"sort,omitempty"`             // energy_board: "fixed" | "power"
	SparkWindow     string `json:"spark_window,omitempty"`     // energy_board: "15" | "60" | "off"
	Dense           string `json:"dense,omitempty"`            // energy_board: "on" | "off"
	ShowInactive    string `json:"show_inactive,omitempty"`    // energy_board: "on" | "off"
	MeasuredSplit   string `json:"measured_split,omitempty"`   // energy_band, energy_ring, energy_board: "sum" | "entities"
	DisplayMode     string `json:"display_mode,omitempty"`     // energy_day: "mirror" | "supply" | "demand"
	ShowNow         string `json:"show_now,omitempty"`         // energy_day: "on" | "off"
	StrokeMode      string `json:"stroke_mode,omitempty"`      // energy_schema: "power" | "const"
	EntityLabels    string `json:"entity_labels,omitempty"`    // energy_schema: "power" | "entity"
	HideInactive    string `json:"hide_inactive,omitempty"`    // energy_schema / energy_flow: "on" | "off"
	DisplaySize     string `json:"display_size,omitempty"`     // energy_schema: "xs" | "s" | "m" | "l" | "xl"
	BeamSpan        string `json:"beam_span,omitempty"`        // energy_status: "3000" | "6000" | "11000"
	ShowAdvice      string `json:"show_advice,omitempty"`      // energy_status: "on" | "off"

	// battery_status (nur Display "trajectory"): das Zeitfenster der
	// Trajektorie in Stunden. BatteryWindow gilt fuer Verlauf und
	// Fortschreibung, BatteryProjectionWindow ueberschreibt nur die
	// Fortschreibung - "" heisst hier "folgt BatteryWindow" (echter Wert,
	// kein Sentinel, siehe normalizeEnergyGraphicOptions).
	BatteryWindow           string `json:"battery_window,omitempty"`            // "3" | "6" | "12" | "24"
	BatteryProjectionWindow string `json:"battery_projection_window,omitempty"` // "" | "3" | "6" | "12" | "24"

	// entity_group: frei zusammengestellte Liste an Entitaeten - anders als
	// Ref (ein einzelner Bezug) braucht dieser Typ mehrere. Title ist der
	// einzige Item-Typ mit freiem Anzeigetext statt eines vom Ref
	// abgeleiteten Namens.
	EntityRefs []string `json:"entity_refs,omitempty"` // entity_group
	Title      string   `json:"title,omitempty"`       // entity_group

	// device: welche der beiden Geraetekacheln die Uebersicht rendert -
	// "detail" ist die device-tile mit allen Entitaeten, "compact" die
	// compact-card des Geraete-Tabs mit bis zu drei Werten. Wie FlowScale
	// ein String-Enum mit "" als Sentinel: normalizeLayout fuellt den
	// Default, sobald es den Typ sieht, und leert das Feld sonst.
	Display string `json:"display,omitempty"` // device: "detail" | "compact"

	// v2-Altlast, ausschliesslich Eingabe. migrateToFlow liest die vier
	// Felder einmal, um Reihenfolge und Groessenklasse abzuleiten, und setzt
	// sie danach auf 0 - omitempty haelt sie damit aus jedem geschriebenen
	// v3-Dokument heraus. Nicht anfassen ausser in der Migration.
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`
	W int `json:"w,omitempty"`
	H int `json:"h,omitempty"`
}

type DeviceMap struct {
	Version int                `json:"version"`
	Nodes   []DeviceMapNode    `json:"nodes"`
	Edges   []RelationOverride `json:"edges,omitempty"`
	View    DeviceMapView      `json:"view"`
}

type DeviceMapNode struct {
	DeviceID string  `json:"device_id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
}

type DeviceMapView struct {
	SnapToGrid bool   `json:"snap_to_grid"`
	ShowGrid   bool   `json:"show_grid"`
	GridSize   int    `json:"grid_size"`
	EdgeStyle  string `json:"edge_style"`
}

type RelationOverride struct {
	ID        string    `json:"id"`
	ChildID   string    `json:"child_id"`
	ParentID  string    `json:"parent_id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

// MQTTConfig is the dashboard's own MQTT broker connection, stored in
// mqtt.json as an alternative to MQTT_* environment variables. The broker
// password is deliberately not part of this struct - it lives in
// mqtt_credentials.json (mode 0600) so it never ends up in a revision copy.
type MQTTConfig struct {
	Enabled           bool   `json:"enabled"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	ClientID          string `json:"client_id"`
	Username          string `json:"username,omitempty"`
	TLS               bool   `json:"tls"`
	TLSInsecure       bool   `json:"tls_insecure"`
	KeepaliveSeconds  int    `json:"keepalive_seconds"`
	CleanSession      bool   `json:"clean_session"`
	DiscoveryPrefix   string `json:"discovery_prefix"`
	ConnectTimeoutSec int    `json:"connect_timeout_seconds"`
	// PublishEnergyDevice steuert, ob das Dashboard seine Energiewerte per
	// HA-MQTT-Discovery als eigenes Gerät anbietet (siehe
	// internal/energydiscovery). Default true; bei false setzt der
	// Connect-Publisher retained Removals auf die Discovery-Topics.
	PublishEnergyDevice bool `json:"publish_energy_device"`
	// Metrics schaltet je Systemmetrik (nodeagent.Metrics) das Publizieren
	// der Node-Diagnose ab. Ein fehlender Schluessel bedeutet an - die
	// Bedeutung von "nicht gesetzt" traegt dieser Default, nicht das Schema
	// (Spec V2).
	Metrics map[string]bool `json:"metrics,omitempty"`
}

// MetricEnabled liefert true, solange die Metrik nicht ausdruecklich auf
// false gesetzt ist.
func (m MQTTConfig) MetricEnabled(metric string) bool {
	if m.Metrics == nil {
		return true
	}
	v, ok := m.Metrics[metric]
	return !ok || v
}

// BridgeTopic is one bridged topic pattern in a BridgeConnection, rendered
// as a single `topic <pattern> <direction> <qos>` line.
type BridgeTopic struct {
	Pattern   string `json:"pattern"`
	Direction string `json:"direction"`
	QoS       int    `json:"qos"`
	Comment   string `json:"comment,omitempty"`
}

// BridgeConnection is the typed model of one Mosquitto bridge `connection`
// block. It is rendered server-side into /etc/mosquitto/conf.d/bridge.conf
// by internal/mqttbridge - the browser never submits configuration text.
// The remote password is deliberately not part of this struct; it lives in
// mqtt_bridge_credentials.json (mode 0600), same reasoning as MQTTConfig.
type BridgeConnection struct {
	Enabled          bool          `json:"enabled"`
	Name             string        `json:"name"`
	Address          string        `json:"address"`
	Port             int           `json:"port"`
	RemoteClientID   string        `json:"remote_client_id"`
	RemoteUsername   string        `json:"remote_username,omitempty"`
	Topics           []BridgeTopic `json:"topics"`
	TryPrivate       bool          `json:"try_private"`
	StartTypeAuto    bool          `json:"start_type_auto"`
	RestartTimeout   int           `json:"restart_timeout"`
	KeepaliveSeconds int           `json:"keepalive_seconds"`
	CleanSession     bool          `json:"cleansession"`
}

// BridgeConfig is stored as bridge.json. It models a list of connections so
// a second bridge can be added later without a schema break, but today
// exactly one entry is supported - see "Offene Punkte" in
// knowhow/dashboard/dashboard-mqtt-setup.md.
type BridgeConfig struct {
	Connections []BridgeConnection `json:"connections"`
}

type Revision struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Checksum  string    `json:"checksum"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	mu              sync.Mutex
	dir             string
	sweepDefault    int
	settingsLoaded  bool
	settingsValue   Settings
	energyLoaded    bool
	energyValue     EnergyConfig
	layoutLoaded    bool
	layoutValue     Layout
	deviceMapLoaded bool
	deviceMapValue  DeviceMap
	mqttLoaded      bool
	mqttValue       MQTTConfig
	bridgeLoaded    bool
	bridgeValue     BridgeConfig
}

func NewStore(dir string) *Store { return &Store{dir: filepath.Clean(dir)} }

func Default() Settings {
	return Settings{
		HealthScoreThreshold: 3, SweepIntervalSeconds: 300, ShowRuntimeStatus: true,
		DeviceViewMode: DeviceViewModeCompact, Theme: ThemeMint,
		ShowConfigEntitiesOnTile: false, ShowDiagnosticEntitiesOnTile: false,
		LiveUpdateIntervalSeconds: 3, WidePanels: []string{"overview", "devices", "history", "layout"},
		StatusBarItems:               []string{"mqtt", "storage", "uptime", "version"},
		HistorySampleIntervalSeconds: 10,
		HistoryRetentionMode:         HistoryRetentionModeTime,
		HistoryRetentionHours:        6,
		HistoryBudgetMB:              512,
		HistoryRawWindowHours:        24,
		HistoryMinuteWindowDays:      7,
		HistoryExtraEntities:         []string{},
		HistoryViews:                 []HistoryView{},
	}
}

func DefaultMQTT() MQTTConfig {
	return MQTTConfig{
		Port:              1883,
		ClientID:          "energy-node-dashboard",
		KeepaliveSeconds:  30,
		CleanSession:      true,
		DiscoveryPrefix:   "homeassistant",
		ConnectTimeoutSec: 10,

		PublishEnergyDevice: true,
	}
}

func (m *MQTTConfig) UnmarshalJSON(data []byte) error {
	type mqttConfigAlias MQTTConfig
	value := DefaultMQTT()
	if err := json.Unmarshal(data, (*mqttConfigAlias)(&value)); err != nil {
		return err
	}
	*m = value
	return nil
}

// DefaultBridgeConnection mirrors DefaultMQTT: it is what a field absent
// from a hand-edited or pre-upgrade bridge.json falls back to, and the
// starting point a fresh connection in the UI is created with.
func DefaultBridgeConnection() BridgeConnection {
	return BridgeConnection{
		Port:             1883,
		TryPrivate:       true,
		StartTypeAuto:    true,
		RestartTimeout:   30,
		KeepaliveSeconds: 60,
	}
}

func (b *BridgeConnection) UnmarshalJSON(data []byte) error {
	type bridgeConnectionAlias BridgeConnection
	value := DefaultBridgeConnection()
	if err := json.Unmarshal(data, (*bridgeConnectionAlias)(&value)); err != nil {
		return err
	}
	*b = value
	return nil
}

func (s *Settings) UnmarshalJSON(data []byte) error {
	type settingsAlias Settings
	value := Default()
	if err := json.Unmarshal(data, (*settingsAlias)(&value)); err != nil {
		return err
	}
	*s = value
	return nil
}

// SetSweepIntervalDefault hinterlegt den Startwert, den config.json fuer
// sweep_interval_seconds liefert. Ein in settings.json ausdruecklich
// gesetzter Wert gewinnt - deshalb wird die Schluessel-Anwesenheit am
// Rohdokument geprueft und nicht am entpackten Settings-Wert:
// Settings.UnmarshalJSON startet von Default() und macht einen fehlenden
// Schluessel sonst ununterscheidbar von einem gesetzten Standardwert.
func (s *Store) SetSweepIntervalDefault(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepDefault = seconds
	s.settingsLoaded = false
}

func (s *Store) LoadSettings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingsLoaded {
		return s.settingsValue, nil
	}
	var value Settings
	data, err := os.ReadFile(filepath.Join(s.dir, "settings.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		value = Default()
		data = nil
	case err != nil:
		return Settings{}, err
	default:
		if err := json.Unmarshal(data, &value); err != nil {
			return Settings{}, fmt.Errorf("settings.json: invalid JSON: %w", err)
		}
	}
	if s.sweepDefault > 0 && !hasJSONKey(data, "sweep_interval_seconds") {
		value.SweepIntervalSeconds = s.sweepDefault
	}
	value = normalizeSettings(value)
	if err := validateSettings(value); err != nil {
		return Settings{}, err
	}
	s.settingsValue = value
	s.settingsLoaded = true
	return value, nil
}

// hasJSONKey meldet, ob das Rohdokument den Schluessel auf oberster Ebene
// ueberhaupt enthaelt. Fehlende Datei (data == nil) zaehlt als "nicht
// gesetzt".
func hasJSONKey(data []byte, key string) bool {
	if len(data) == 0 {
		return false
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return false
	}
	_, ok := document[key]
	return ok
}

func (s *Store) SaveSettings(value Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = normalizeSettings(value)
	if err := validateSettings(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("settings.json", value); err != nil {
		return err
	}
	s.settingsValue = value
	s.settingsLoaded = true
	return nil
}

func (s *Store) SettingsRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("settings")
}

func (s *Store) ReadSettingsRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("settings", revision)
}

func (s *Store) RestoreSettings(revision string) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("settings", revision)
	if err != nil {
		return Settings{}, err
	}
	var value Settings
	if err := json.Unmarshal(data, &value); err != nil {
		return Settings{}, fmt.Errorf("settings revision %q: invalid JSON: %w", revision, err)
	}
	value = normalizeSettings(value)
	if err := validateSettings(value); err != nil {
		return Settings{}, err
	}
	if err := s.saveJSONLocked("settings.json", value); err != nil {
		return Settings{}, err
	}
	s.settingsValue = value
	s.settingsLoaded = true
	return value, nil
}

func (s *Store) LoadEnergy() (EnergyConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.energyLoaded {
		return cloneEnergyConfig(s.energyValue), nil
	}
	var value EnergyConfig
	if err := s.loadJSONLocked("energy.json", &value); errors.Is(err, os.ErrNotExist) {
		value = EnergyConfig{Assignments: map[string]energy.Assignment{}}
	} else if err != nil {
		return EnergyConfig{}, err
	}
	value = normalizeEnergy(value)
	if err := validateEnergy(value); err != nil {
		return EnergyConfig{}, err
	}
	s.energyValue = cloneEnergyConfig(value)
	s.energyLoaded = true
	return cloneEnergyConfig(value), nil
}

func (s *Store) SaveEnergy(value EnergyConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = normalizeEnergy(value)
	if err := validateEnergy(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("energy.json", value); err != nil {
		return err
	}
	s.energyValue = cloneEnergyConfig(value)
	s.energyLoaded = true
	return nil
}

func (s *Store) EnergyRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("energy")
}

func (s *Store) ReadEnergyRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("energy", revision)
}

func (s *Store) RestoreEnergy(revision string) (EnergyConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("energy", revision)
	if err != nil {
		return EnergyConfig{}, err
	}
	var value EnergyConfig
	if err := json.Unmarshal(data, &value); err != nil {
		return EnergyConfig{}, fmt.Errorf("energy revision %q: invalid JSON: %w", revision, err)
	}
	value = normalizeEnergy(value)
	if err := validateEnergy(value); err != nil {
		return EnergyConfig{}, err
	}
	if err := s.saveJSONLocked("energy.json", value); err != nil {
		return EnergyConfig{}, err
	}
	s.energyValue = cloneEnergyConfig(value)
	s.energyLoaded = true
	return cloneEnergyConfig(value), nil
}

func cloneEnergyConfig(value EnergyConfig) EnergyConfig {
	assignments := make(map[string]energy.Assignment, len(value.Assignments))
	for id, assignment := range value.Assignments {
		assignments[id] = assignment
	}
	value.Assignments = assignments
	return value
}

// cloneLayout deep-copies a Layout so LoadLayout can hand out a cached value
// without callers being able to mutate the Store's copy through slices.
func cloneLayout(value Layout) Layout {
	clone := Layout{Version: value.Version, Pages: make([]Page, len(value.Pages))}
	for pageIndex, page := range value.Pages {
		clonedPage := Page{ID: page.ID, Name: page.Name, Order: page.Order, Groups: make([]Group, len(page.Groups))}
		for groupIndex, group := range page.Groups {
			clonedGroup := Group{
				ID:        group.ID,
				Name:      group.Name,
				Items:     make([]Item, len(group.Items)),
				EntityIDs: append([]string(nil), group.EntityIDs...),
			}
			for itemIndex, item := range group.Items {
				clonedGroup.Items[itemIndex] = Item{
					ID:                      item.ID,
					Type:                    item.Type,
					Ref:                     item.Ref,
					Span:                    item.Span,
					Visible:                 item.Visible,
					VisibleCategories:       append([]string(nil), item.VisibleCategories...),
					Height:                  item.Height,
					X:                       item.X,
					Y:                       item.Y,
					W:                       item.W,
					H:                       item.H,
					FlowScale:               item.FlowScale,
					SpeedReferenceMode:      item.SpeedReferenceMode,
					SpeedReferenceWatts:     item.SpeedReferenceWatts,
					HeightReference:         item.HeightReference,
					ScaleMode:               item.ScaleMode,
					Unit:                    item.Unit,
					BundleThreshold:         item.BundleThreshold,
					Animate:                 item.Animate,
					KPI:                     item.KPI,
					LabelMode:               item.LabelMode,
					Sort:                    item.Sort,
					SparkWindow:             item.SparkWindow,
					Dense:                   item.Dense,
					ShowInactive:            item.ShowInactive,
					MeasuredSplit:           item.MeasuredSplit,
					DisplayMode:             item.DisplayMode,
					ShowNow:                 item.ShowNow,
					StrokeMode:              item.StrokeMode,
					EntityLabels:            item.EntityLabels,
					HideInactive:            item.HideInactive,
					DisplaySize:             item.DisplaySize,
					BeamSpan:                item.BeamSpan,
					ShowAdvice:              item.ShowAdvice,
					BatteryWindow:           item.BatteryWindow,
					BatteryProjectionWindow: item.BatteryProjectionWindow,
					EntityRefs:              append([]string(nil), item.EntityRefs...),
					Title:                   item.Title,
					Display:                 item.Display,
				}
			}
			clonedPage.Groups[groupIndex] = clonedGroup
		}
		clone.Pages[pageIndex] = clonedPage
	}
	return clone
}

// LayoutConfigured meldet, ob je ein Layout gespeichert wurde (layout.json
// existiert). Der Overview-Handler unterscheidet damit die frische
// Installation (kein Layout -> Standardraster) vom bewusst leer gespeicherten
// Layout (-> Leerzustand mit Editieren-Knopf, Spec 4.3).
func (s *Store) LayoutConfigured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(filepath.Join(s.dir, "layout.json")); err == nil {
		return true
	}
	return false
}

func (s *Store) LoadLayout() (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.layoutLoaded {
		return cloneLayout(s.layoutValue), nil
	}
	var value Layout
	if err := s.loadJSONLocked("layout.json", &value); errors.Is(err, os.ErrNotExist) {
		value = Layout{}
	} else if err != nil {
		return Layout{}, err
	} else {
		value = normalizeLayout(value)
		if err := validateLayout(value); err != nil {
			return Layout{}, err
		}
	}
	s.layoutValue = cloneLayout(value)
	s.layoutLoaded = true
	return cloneLayout(value), nil
}

func (s *Store) SaveLayout(value Layout) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = normalizeLayout(value)
	if err := validateLayout(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("layout.json", value); err != nil {
		return err
	}
	s.layoutValue = cloneLayout(value)
	s.layoutLoaded = true
	return nil
}

func (s *Store) LayoutRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("layout")
}

func (s *Store) ReadLayoutRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("layout", revision)
}

func (s *Store) RestoreLayout(revision string) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("layout", revision)
	if err != nil {
		return Layout{}, err
	}
	var value Layout
	if err := json.Unmarshal(data, &value); err != nil {
		return Layout{}, fmt.Errorf("layout revision %q: invalid JSON: %w", revision, err)
	}
	value = normalizeLayout(value)
	if err := validateLayout(value); err != nil {
		return Layout{}, err
	}
	if err := s.saveJSONLocked("layout.json", value); err != nil {
		return Layout{}, err
	}
	s.layoutValue = cloneLayout(value)
	s.layoutLoaded = true
	return value, nil
}

// cloneDeviceMap deep-copies a DeviceMap so LoadDeviceMap can hand out a
// cached value without callers being able to mutate the Store's copy
// through slices.
func cloneDeviceMap(value DeviceMap) DeviceMap {
	return DeviceMap{
		Version: value.Version,
		Nodes:   append([]DeviceMapNode(nil), value.Nodes...),
		Edges:   append([]RelationOverride(nil), value.Edges...),
		View:    value.View,
	}
}

func (s *Store) LoadDeviceMap() (DeviceMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deviceMapLoaded {
		return cloneDeviceMap(s.deviceMapValue), nil
	}
	var value DeviceMap
	if err := s.loadJSONLocked("device-map.json", &value); errors.Is(err, os.ErrNotExist) {
		value = DeviceMap{}
	} else if err != nil {
		return DeviceMap{}, err
	} else {
		value = normalizeDeviceMap(value)
		if err := validateDeviceMap(value); err != nil {
			return DeviceMap{}, err
		}
	}
	s.deviceMapValue = cloneDeviceMap(value)
	s.deviceMapLoaded = true
	return cloneDeviceMap(value), nil
}

func (s *Store) SaveDeviceMap(value DeviceMap) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = normalizeDeviceMap(value)
	if err := validateDeviceMap(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("device-map.json", value); err != nil {
		return err
	}
	s.deviceMapValue = cloneDeviceMap(value)
	s.deviceMapLoaded = true
	return nil
}

func (s *Store) DeviceMapRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("device-map")
}

func (s *Store) ReadDeviceMapRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("device-map", revision)
}

func (s *Store) RestoreDeviceMap(revision string) (DeviceMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("device-map", revision)
	if err != nil {
		return DeviceMap{}, err
	}
	var value DeviceMap
	if err := json.Unmarshal(data, &value); err != nil {
		return DeviceMap{}, fmt.Errorf("device-map revision %q: invalid JSON: %w", revision, err)
	}
	value = normalizeDeviceMap(value)
	if err := validateDeviceMap(value); err != nil {
		return DeviceMap{}, err
	}
	if err := s.saveJSONLocked("device-map.json", value); err != nil {
		return DeviceMap{}, err
	}
	s.deviceMapValue = cloneDeviceMap(value)
	s.deviceMapLoaded = true
	return value, nil
}

func normalizeDeviceMap(value DeviceMap) DeviceMap {
	if value.Version == 0 {
		value.Version = 1
	}
	if value.Nodes == nil {
		value.Nodes = []DeviceMapNode{}
	}
	if value.Edges == nil {
		value.Edges = []RelationOverride{}
	}
	if value.View.GridSize == 0 {
		value.View.GridSize = 40
	}
	if value.View.EdgeStyle == "" {
		value.View.EdgeStyle = "straight"
	}
	return value
}

func validateDeviceMap(value DeviceMap) error {
	value = normalizeDeviceMap(value)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := config.ValidateDocument(data, deviceMapSchema); err != nil {
		return err
	}
	seenNodes := map[string]bool{}
	for _, node := range value.Nodes {
		if seenNodes[node.DeviceID] {
			return fmt.Errorf("duplicate device-map node %q", node.DeviceID)
		}
		seenNodes[node.DeviceID] = true
	}
	seenEdges := map[string]bool{}
	for _, edge := range value.Edges {
		if seenEdges[edge.ID] {
			return fmt.Errorf("duplicate device-map edge %q", edge.ID)
		}
		seenEdges[edge.ID] = true
	}
	return nil
}

func (s *Store) LoadMQTT() (MQTTConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mqttLoaded {
		return s.mqttValue, nil
	}
	value := DefaultMQTT()
	if err := s.loadJSONLocked("mqtt.json", &value); err != nil && !errors.Is(err, os.ErrNotExist) {
		return MQTTConfig{}, err
	}
	if err := validateMQTT(value); err != nil {
		return MQTTConfig{}, err
	}
	s.mqttValue = value
	s.mqttLoaded = true
	return value, nil
}

func (s *Store) SaveMQTT(value MQTTConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateMQTT(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("mqtt.json", value); err != nil {
		return err
	}
	s.mqttValue = value
	s.mqttLoaded = true
	return nil
}

func (s *Store) MQTTRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("mqtt")
}

func (s *Store) ReadMQTTRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("mqtt", revision)
}

func (s *Store) RestoreMQTT(revision string) (MQTTConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("mqtt", revision)
	if err != nil {
		return MQTTConfig{}, err
	}
	value := DefaultMQTT()
	if err := json.Unmarshal(data, &value); err != nil {
		return MQTTConfig{}, fmt.Errorf("mqtt revision %q: invalid JSON: %w", revision, err)
	}
	if err := validateMQTT(value); err != nil {
		return MQTTConfig{}, err
	}
	if err := s.saveJSONLocked("mqtt.json", value); err != nil {
		return MQTTConfig{}, err
	}
	s.mqttValue = value
	s.mqttLoaded = true
	return value, nil
}

// validateMQTT rejects the values a JSON-schema type/range check cannot
// express: characters that could break out of the field they are stored in.
// This document itself never reaches a shell or a config file the way the
// mosquitto bridge config does, but the client_id and discovery_prefix are
// used to build MQTT topic filters, so control characters and stray '/'
// segments are rejected the same defensive way.
func validateMQTT(value MQTTConfig) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := config.ValidateDocument(data, mqttSchema); err != nil {
		return err
	}
	if value.Host != "" && !isCleanHost(value.Host) {
		return errors.New("mqtt: host must not contain whitespace, control characters or slashes")
	}
	if value.ClientID != "" && !mqttClientIDPattern.MatchString(value.ClientID) {
		return errors.New("mqtt: client_id must match ^[A-Za-z0-9._-]{1,64}$")
	}
	if hasControlChars(value.Username) {
		return errors.New("mqtt: username must not contain control characters")
	}
	if err := validateDiscoveryPrefix(value.DiscoveryPrefix); err != nil {
		return err
	}
	return nil
}

var mqttClientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func isCleanHost(value string) bool {
	if len(value) > 253 {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func hasControlChars(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validateDiscoveryPrefix(value string) error {
	if value == "" {
		return errors.New("mqtt: discovery_prefix must not be empty")
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return errors.New("mqtt: discovery_prefix must not start or end with '/'")
	}
	if strings.ContainsAny(value, "#+ \t\r\n") {
		return errors.New("mqtt: discovery_prefix must not contain '#', '+' or whitespace")
	}
	if hasControlChars(value) {
		return errors.New("mqtt: discovery_prefix must not contain control characters")
	}
	return nil
}

// cloneBridgeConfig deep-copies a BridgeConfig so LoadBridge can hand out a
// cached value without callers being able to mutate the Store's copy
// through slices, same reasoning as cloneDeviceMap.
func cloneBridgeConfig(value BridgeConfig) BridgeConfig {
	connections := make([]BridgeConnection, len(value.Connections))
	for i, connection := range value.Connections {
		connections[i] = connection
		connections[i].Topics = append([]BridgeTopic(nil), connection.Topics...)
	}
	return BridgeConfig{Connections: connections}
}

func (s *Store) LoadBridge() (BridgeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bridgeLoaded {
		return cloneBridgeConfig(s.bridgeValue), nil
	}
	var value BridgeConfig
	if err := s.loadJSONLocked("bridge.json", &value); errors.Is(err, os.ErrNotExist) {
		value = BridgeConfig{}
	} else if err != nil {
		return BridgeConfig{}, err
	}
	if value.Connections == nil {
		value.Connections = []BridgeConnection{}
	}
	if err := validateBridge(value); err != nil {
		return BridgeConfig{}, err
	}
	s.bridgeValue = cloneBridgeConfig(value)
	s.bridgeLoaded = true
	return cloneBridgeConfig(value), nil
}

func (s *Store) SaveBridge(value BridgeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value.Connections == nil {
		value.Connections = []BridgeConnection{}
	}
	if err := validateBridge(value); err != nil {
		return err
	}
	if err := s.saveJSONLocked("bridge.json", value); err != nil {
		return err
	}
	s.bridgeValue = cloneBridgeConfig(value)
	s.bridgeLoaded = true
	return nil
}

func (s *Store) BridgeRevisions() ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revisionsLocked("bridge")
}

func (s *Store) ReadBridgeRevision(revision string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked("bridge", revision)
}

func (s *Store) RestoreBridge(revision string) (BridgeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readRevisionLocked("bridge", revision)
	if err != nil {
		return BridgeConfig{}, err
	}
	var value BridgeConfig
	if err := json.Unmarshal(data, &value); err != nil {
		return BridgeConfig{}, fmt.Errorf("bridge revision %q: invalid JSON: %w", revision, err)
	}
	if value.Connections == nil {
		value.Connections = []BridgeConnection{}
	}
	if err := validateBridge(value); err != nil {
		return BridgeConfig{}, err
	}
	if err := s.saveJSONLocked("bridge.json", value); err != nil {
		return BridgeConfig{}, err
	}
	s.bridgeValue = cloneBridgeConfig(value)
	s.bridgeLoaded = true
	return value, nil
}

// bridgeNamePattern matches connection names and remote client IDs - the
// same shape as an MQTT client ID, since both end up as bare tokens in a
// Mosquitto directive line with no quoting available.
var bridgeNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// bridgeTopicPattern is intentionally narrower than a full MQTT topic
// filter: it is the actual injection defense for the rendered `topic` line,
// on top of the '#'-placement and control-character checks below.
var bridgeTopicPattern = regexp.MustCompile(`^[A-Za-z0-9/_+#.-]+$`)

const maxBridgeConnections = 1

// validateBridge rejects anything the JSON schema's type/range checks
// cannot express: the actual injection defense for the rendered
// bridge.conf. Every string field that ends up in the file is checked for
// control characters/newlines here, on top of the schema's structural
// checks - see the doc comment on validateMQTT for the same reasoning.
func validateBridge(value BridgeConfig) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := config.ValidateDocument(data, bridgeSchema); err != nil {
		return err
	}
	if len(value.Connections) > maxBridgeConnections {
		return fmt.Errorf("bridge: only %d connection(s) are supported today", maxBridgeConnections)
	}
	for _, connection := range value.Connections {
		if err := validateBridgeConnection(connection); err != nil {
			return err
		}
	}
	return nil
}

func validateBridgeConnection(value BridgeConnection) error {
	if !bridgeNamePattern.MatchString(value.Name) {
		return errors.New("bridge: name must match ^[A-Za-z0-9._-]{1,64}$")
	}
	if !bridgeNamePattern.MatchString(value.RemoteClientID) {
		return errors.New("bridge: remote_client_id must match ^[A-Za-z0-9._-]{1,64}$")
	}
	if value.Address == "" || !isCleanHost(value.Address) {
		return errors.New("bridge: address must not be empty, contain whitespace, control characters or slashes")
	}
	if hasControlChars(value.RemoteUsername) {
		return errors.New("bridge: remote_username must not contain control characters")
	}
	if len(value.Topics) == 0 {
		return errors.New("bridge: at least one topic is required")
	}
	if len(value.Topics) > 32 {
		return errors.New("bridge: at most 32 topics are supported")
	}
	for _, topic := range value.Topics {
		if err := validateBridgeTopic(topic); err != nil {
			return err
		}
	}
	return nil
}

func validateBridgeTopic(value BridgeTopic) error {
	if value.Pattern == "" || !bridgeTopicPattern.MatchString(value.Pattern) {
		return errors.New("bridge: topic pattern must only contain [A-Za-z0-9/_+#.-] and must not be empty")
	}
	if value.Pattern == "#" {
		return errors.New("bridge: topic pattern must not be the bare broker-wide wildcard '#'")
	}
	for i, segment := range strings.Split(value.Pattern, "/") {
		if strings.Contains(segment, "#") && (segment != "#" || i != len(strings.Split(value.Pattern, "/"))-1) {
			return errors.New("bridge: '#' is only allowed as the last topic segment")
		}
	}
	switch value.Direction {
	case "in", "out", "both":
	default:
		return errors.New("bridge: direction must be one of in, out, both")
	}
	if value.QoS < 0 || value.QoS > 2 {
		return errors.New("bridge: qos must be between 0 and 2")
	}
	if hasControlChars(value.Comment) {
		return errors.New("bridge: comment must not contain control characters")
	}
	return nil
}

// ValidateBridgeConnection re-runs the field-level BridgeConnection checks
// outside of Store.SaveBridge/LoadBridge. internal/mqttbridge calls this as
// defense in depth immediately before rendering, on top of the validation
// already performed on every path that reads or writes bridge.json.
func ValidateBridgeConnection(value BridgeConnection) error {
	return validateBridgeConnection(value)
}

// BridgeAddressWarning returns a non-fatal hint (not a validation error)
// when an address is neither a Tailscale CGNAT address (100.64.0.0/10) nor
// an RFC1918 private address - the bridge is meant to run inside the
// Tailnet. Empty means no warning.
func BridgeAddressWarning(address string) string {
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return "Adresse konnte nicht als IPv4-Adresse erkannt werden - die Warnung bezieht sich nur auf Tailscale-/private Adressen."
	}
	v4 := ip.To4()
	if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return ""
	}
	if ip.IsPrivate() {
		return ""
	}
	return "Adresse liegt weder im Tailscale-Bereich (100.64.0.0/10) noch in einem privaten Netz (RFC 1918) - die Bridge erwartet eine Verbindung im Tailnet."
}

func (s *Store) loadJSONLocked(name string, target any) error {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", name, err)
	}
	return nil
}

// revisionLimit begrenzt, wie viele Revisionen je Dokument aufbewahrt
// werden. Das Zielgeraet ist ein Raspberry Pi mit SD-Karte; ohne Grenze
// wuechse revisions/ mit jedem Speichervorgang unbegrenzt weiter.
const revisionLimit = 20

// pruneRevisionsLocked entfernt die aeltesten Revisionen, bis hoechstens
// revisionLimit uebrig sind. revisionsLocked liefert nach Namen sortiert,
// und die Namen sind UTC-Zeitstempel - die aeltesten stehen also vorn.
func (s *Store) pruneRevisionsLocked(name string) error {
	revisions, err := s.revisionsLocked(name)
	if err != nil {
		return err
	}
	for i := 0; i+revisionLimit < len(revisions); i++ {
		if err := os.Remove(revisions[i].Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) saveJSONLocked(name string, value any) error {
	path := filepath.Join(s.dir, name)
	old, err := os.ReadFile(path)
	if err == nil {
		revisionDir := filepath.Join(s.dir, "revisions", name[:len(name)-len(filepath.Ext(name))])
		if err := os.MkdirAll(revisionDir, 0750); err != nil {
			return err
		}
		revision := filepath.Join(revisionDir, time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")+".json")
		if err := atomicWrite(revision, old); err != nil {
			return err
		}
		if err := s.pruneRevisionsLocked(name[:len(name)-len(filepath.Ext(name))]); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}

func validateSettings(value Settings) error {
	value = normalizeSettings(value)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return config.ValidateDocument(data, settingsSchema)
}

func normalizeSettings(value Settings) Settings {
	// Legacy stores may still hold the retired "analysis" view. The device
	// modal now covers what that view did, so fold it into the control tiles
	// before the schema (enum: control|compact) rejects it.
	if value.DeviceViewMode == "analysis" {
		value.DeviceViewMode = DeviceViewModeControl
	}
	if value.DeviceViewMode == "" {
		value.DeviceViewMode = DeviceViewModeCompact
	}
	if value.Theme == "" {
		value.Theme = ThemeMint
	}
	if value.LiveUpdateIntervalSeconds == 0 {
		value.LiveUpdateIntervalSeconds = Default().LiveUpdateIntervalSeconds
	}
	if value.WidePanels == nil {
		value.WidePanels = Default().WidePanels
	}
	if value.StatusBarItems == nil {
		value.StatusBarItems = Default().StatusBarItems
	}
	if value.HistorySampleIntervalSeconds == 0 {
		value.HistorySampleIntervalSeconds = Default().HistorySampleIntervalSeconds
	}
	if value.HistoryRetentionMode == "" {
		value.HistoryRetentionMode = HistoryRetentionModeTime
	}
	if value.HistoryRetentionHours == 0 {
		value.HistoryRetentionHours = Default().HistoryRetentionHours
	}
	if value.HistoryBudgetMB == 0 {
		value.HistoryBudgetMB = Default().HistoryBudgetMB
	}
	if value.HistoryRawWindowHours == 0 {
		value.HistoryRawWindowHours = Default().HistoryRawWindowHours
	}
	if value.HistoryMinuteWindowDays == 0 {
		value.HistoryMinuteWindowDays = Default().HistoryMinuteWindowDays
	}
	if value.HistoryExtraEntities == nil {
		value.HistoryExtraEntities = []string{}
	}
	if value.HistoryViews == nil {
		value.HistoryViews = []HistoryView{}
	}
	return value
}

func normalizeEnergy(value EnergyConfig) EnergyConfig {
	if value.Assignments == nil {
		value.Assignments = map[string]energy.Assignment{}
	}
	value.Interpretation = value.Interpretation.Normalized()
	return value
}

func validateEnergy(value EnergyConfig) error {
	value = normalizeEnergy(value)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := config.ValidateDocument(data, energySchema); err != nil {
		return err
	}
	return value.Interpretation.Validate()
}
func validateLayout(value Layout) error {
	value = normalizeLayout(value)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := config.ValidateDocument(data, layoutSchema); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, page := range value.Pages {
		if page.ID == "" || page.Name == "" {
			return errors.New("layout pages require id and name")
		}
		if seen[page.ID] {
			return fmt.Errorf("duplicate layout page %q", page.ID)
		}
		seen[page.ID] = true
		for _, group := range page.Groups {
			if group.ID == "" || group.Name == "" {
				return errors.New("layout groups require id and name")
			}
			for _, item := range group.Items {
				if item.ID == "" || item.Type == "" {
					return errors.New("layout items require id and type")
				}
				if seen[item.ID] {
					return fmt.Errorf("duplicate layout item %q", item.ID)
				}
				seen[item.ID] = true
				card, known := CardTypeFor(item.Type)
				if !known {
					return fmt.Errorf("layout item %q has unknown type %q", item.ID, item.Type)
				}
				if spanTracks(item.Span) < card.MinSpan {
					return fmt.Errorf("layout item %q (%s) needs span %d or more, got %q",
						item.ID, item.Type, card.MinSpan, item.Span)
				}
			}
		}
	}
	return nil
}

func normalizeLayout(value Layout) Layout {
	migrate := value.Version < 3
	value.Version = 3
	if value.Pages == nil {
		value.Pages = []Page{}
	}
	for pageIndex := range value.Pages {
		if value.Pages[pageIndex].Groups == nil {
			value.Pages[pageIndex].Groups = []Group{}
		}
		for groupIndex := range value.Pages[pageIndex].Groups {
			group := &value.Pages[pageIndex].Groups[groupIndex]
			if group.Items == nil {
				group.Items = []Item{}
			}
			for _, entityID := range group.EntityIDs {
				entityCard, _ := CardTypeFor("entity_value")
				group.Items = append(group.Items, Item{
					ID:      "entity:" + entityID,
					Type:    "entity_value",
					Ref:     entityID,
					Span:    entityCard.DefaultSpan,
					Visible: true,
				})
			}
			group.EntityIDs = nil
			for itemIndex := range group.Items {
				item := &group.Items[itemIndex]
				// Der Kartentyp "entity" ist abgeschafft (Spec 2026-08-23,
				// Abschnitt 4): auf dem Dashboard zeigte er Quelle,
				// Freshness und Zuletzt-gesehen - Diagnosefelder an der
				// Werkstattwand -, und er war die letzte Karte, deren Wert
				// nur der volle Fragment-Tausch nachzog. entity_value
				// leistet auf demselben Ref strikt dasselbe besser. Die
				// Umschrift steht hier und nur hier: LoadLayout, SaveLayout,
				// RestoreLayout und validateLayout laufen alle durch
				// normalizeLayout, also erreicht der alte Typ weder das
				// Schema-Enum noch ein Template.
				if item.Type == "entity" {
					item.Type = "entity_value"
				}
				if item.ID == "" {
					item.ID = item.Type + ":" + item.Ref
				}
				if item.Span == "" {
					card, _ := CardTypeFor(item.Type)
					item.Span = card.DefaultSpan
				}
				if item.Type == "energy_flow" {
					if item.FlowScale == "" {
						item.FlowScale = "width"
					}
					// Wie FlowScale: nur Leerstrings (fehlendes Feld) werden
					// befuellt, ein ungueltiger Wert bleibt stehen und faellt
					// validateLayout()'s Schema-Enum zum Opfer statt still
					// korrigiert zu werden.
					if item.SpeedReferenceMode == "" {
						item.SpeedReferenceMode = EnergyFlowSpeedReferenceModeRelative
					}
					if item.SpeedReferenceWatts <= 0 {
						item.SpeedReferenceWatts = 1000
					}
				} else {
					item.FlowScale = ""
					item.SpeedReferenceMode = ""
					item.SpeedReferenceWatts = 0
				}
				if item.Type == "entity_group" {
					if item.Title == "" {
						item.Title = "Entitäten"
					}
					if item.EntityRefs == nil {
						item.EntityRefs = []string{}
					}
				} else {
					item.Title = ""
					// device: die kompakte Kachel darf bis zu drei Entitaeten
					// ihres Geraets fest zeigen (entity_refs, dasselbe Feld wie
					// entity_group). Leer heisst "priorityEntities-Automatik" -
					// deshalb kein []string{} wie bei entity_group, sondern nil.
					// Jeder andere Typ und die Detailkachel tragen die Auswahl
					// nicht, also weg damit.
					if item.Type == "device" && item.Display == "compact" && len(item.EntityRefs) > 0 {
						if len(item.EntityRefs) > 3 {
							item.EntityRefs = item.EntityRefs[:3]
						}
					} else {
						item.EntityRefs = nil
					}
				}
				// Zwei Typen mit Darstellungsvarianten, als eine if/else-if/else-Kette:
				// nur der "sonst"-Zweig leert Display, und der steht damit fuer
				// battery_status gar nicht erst zur Wahl. Eine fruehere Fassung
				// setzte beide als getrennte if-Bloecke - der device-Block hat dann
				// jedes fremde Display schon geleert, bevor der battery_status-Block
				// seinen eigenen Default darauf setzen konnte, und verlor damit eine
				// bereits gewaehlte "trajectory".
				if item.Type == "device" {
					if item.Display == "" {
						item.Display = "detail"
					}
				} else if item.Type == "battery_status" {
					defaultString(&item.Display, []string{"column", "trajectory"}, "column")
				} else {
					item.Display = ""
				}
				normalizeEnergyGraphicOptions(item)
			}
			if migrate {
				migrateToFlow(group.Items)
			}
		}
	}
	return value
}

// defaultString setzt *field auf def, wenn es leer oder nicht der erlaubten
// Menge zugehoerig ist - ein unbekannter Wert (z.B. aus einem aelteren
// Enum-Stand) faellt damit auf den Default zurueck statt einen ungueltigen
// Wert durchzureichen.
func defaultString(field *string, allowed []string, def string) {
	for _, v := range allowed {
		if *field == v {
			return
		}
	}
	*field = def
}

// normalizeEnergyGraphicOptions setzt die Konfigurationsoptionen der sechs
// Energiegrafiken-Alternativen auf ihren Typ-Default, sobald item.Type
// passt, und leert sie sonst - dasselbe Muster wie FlowScale fuer
// energy_flow. Siehe knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
func normalizeEnergyGraphicOptions(item *Item) {
	if item.Type == "energy_band" {
		defaultString(&item.HeightReference, []string{"fill", "abs"}, "fill")
		defaultString(&item.ScaleMode, []string{"linear", "sqrt"}, "linear")
		defaultString(&item.Unit, []string{"auto", "w", "kw"}, "auto")
		defaultString(&item.BundleThreshold, []string{"0", "0.03", "0.08"}, "0")
	} else {
		item.HeightReference, item.ScaleMode, item.Unit, item.BundleThreshold = "", "", "", ""
	}

	if item.Type == "energy_band" || item.Type == "energy_ring" {
		defaultString(&item.Animate, []string{"on", "off"}, "on")
	} else if item.Type == "energy_schema" {
		defaultString(&item.Animate, []string{"on", "off"}, "off")
	} else {
		item.Animate = ""
	}

	if item.Type == "energy_ring" {
		defaultString(&item.KPI, []string{"autarkie", "eigen", "netz", "last"}, "autarkie")
		defaultString(&item.LabelMode, []string{"both", "pct", "abs"}, "both")
	} else {
		item.KPI, item.LabelMode = "", ""
	}

	if item.Type == "energy_board" {
		defaultString(&item.Sort, []string{"fixed", "power"}, "fixed")
		defaultString(&item.SparkWindow, []string{"15", "60", "off"}, "15")
		defaultString(&item.Dense, []string{"on", "off"}, "off")
		defaultString(&item.ShowInactive, []string{"on", "off"}, "on")
	} else {
		item.Sort, item.SparkWindow, item.Dense, item.ShowInactive = "", "", "", ""
	}

	// measured_split teilt die im Kombiniert-Modus abgetrennten gemessenen
	// Verbraucher auf (siehe energy-model.js' measuredLoadFlows). Nur die drei
	// Karten mit variabler Positionsliste koennen das; energy_day,
	// energy_schema und energy_flow zeigen immer die Sammelposition.
	if item.Type == "energy_band" || item.Type == "energy_ring" || item.Type == "energy_board" {
		defaultString(&item.MeasuredSplit, []string{"sum", "entities"}, "sum")
	} else {
		item.MeasuredSplit = ""
	}

	if item.Type == "energy_day" {
		defaultString(&item.DisplayMode, []string{"mirror", "supply", "demand"}, "mirror")
		defaultString(&item.ShowNow, []string{"on", "off"}, "on")
	} else {
		item.DisplayMode, item.ShowNow = "", ""
	}

	if item.Type == "energy_schema" {
		defaultString(&item.StrokeMode, []string{"power", "const"}, "power")
		defaultString(&item.EntityLabels, []string{"power", "entity"}, "power")
		defaultString(&item.DisplaySize, []string{"xs", "s", "m", "l", "xl"}, "m")
	} else {
		item.StrokeMode, item.EntityLabels, item.DisplaySize = "", "", ""
	}

	// hide_inactive: energy_schema blendet inaktive Abzweige aus, energy_flow
	// inaktive Verbraucher in der Lasten-Kachel (siehe energy-flow.js
	// loadConsumers()).
	if item.Type == "energy_schema" || item.Type == "energy_flow" {
		defaultString(&item.HideInactive, []string{"on", "off"}, "off")
	} else {
		item.HideInactive = ""
	}

	if item.Type == "energy_status" {
		defaultString(&item.BeamSpan, []string{"3000", "6000", "11000"}, "6000")
		defaultString(&item.ShowAdvice, []string{"on", "off"}, "on")
	} else {
		item.BeamSpan, item.ShowAdvice = "", ""
	}

	// Nur die Trajektorie hat eine Zeitachse. BatteryWindow faellt bei
	// Unbekanntem auf "6", BatteryProjectionWindow auf "" (folgt dem
	// Fenster) - anders als die uebrigen Optionen ist "" hier ein gueltiger
	// Wert und kein "noch nicht gesetzt".
	if item.Type == "battery_status" && item.Display == "trajectory" {
		defaultString(&item.BatteryWindow, []string{"3", "6", "12", "24"}, "6")
		if item.BatteryProjectionWindow != "" {
			defaultString(&item.BatteryProjectionWindow, []string{"3", "6", "12", "24"}, "")
		}
	} else {
		item.BatteryWindow, item.BatteryProjectionWindow = "", ""
	}
}

// migrateToFlow hebt eine Gruppe vom v2-Koordinatenraster auf das v3-
// Fliessraster: aus (y, x) wird Reihenfolge, aus w die Groessenklasse, h
// faellt weg. Loest migrateItemGeometry ab, das den umgekehrten Weg ging.
//
// Zu h: die heutigen Werte stammen fast ausnahmslos aus defaultItemHeight() -
// eine automatische Vergabe, keine Nutzerentscheidung. Sie mitzuschleppen
// wuerde den Ist-Zustand einfrieren und die inhaltsgetriebene Hoehe genau
// dort aushebeln, wo sie wirken soll. Der Preis ist, dass die Uebersicht nach
// der Migration anders aussieht als davor. Das ist gewollt (Spec F).
func migrateToFlow(items []Item) {
	// Stabil, damit v1-Dokumente - alle Items auf (0,0) - ihre
	// Dokumentreihenfolge behalten.
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Y != items[b].Y {
			return items[a].Y < items[b].Y
		}
		return items[a].X < items[b].X
	})
	for itemIndex := range items {
		item := &items[itemIndex]
		if item.W != 0 {
			item.Span = spanForWidth(item.W)
		}
		card, _ := CardTypeFor(item.Type)
		if spanTracks(item.Span) < card.MinSpan {
			item.Span = strconv.Itoa(card.MinSpan)
		}
		item.X, item.Y, item.W, item.H = 0, 0, 0, 0
	}
}

// spanForWidth uebersetzt eine v2-Breite in eine v3-Groessenklasse. 4 wird
// bewusst zu "full" und nicht zu "4": im v2-Raster *bedeutete* es "volle
// Breite", und die Absicht zu erhalten ist richtiger als die Zahl.
func spanForWidth(width int) string {
	if width >= 4 {
		return "full"
	}
	return strconv.Itoa(width)
}

// spanTracks liefert die Spurenzahl einer Groessenklasse fuer den Vergleich
// mit MinSpan. "full" belegt immer alle Spuren und erfuellt damit jedes
// Minimum - math.MaxInt waere ehrlicher, 99 ist lesbarer und liegt weit
// oberhalb jeder denkbaren Spurenzahl.
func spanTracks(span string) int {
	if span == "full" {
		return 99
	}
	tracks, err := strconv.Atoi(span)
	if err != nil {
		return 1
	}
	return tracks
}

func (s *Store) revisionsLocked(name string) ([]Revision, error) {
	dir := filepath.Join(s.dir, "revisions", name)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Revision{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]Revision, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		result = append(result, Revision{Name: entry.Name(), Path: path, Checksum: checksum(data), CreatedAt: info.ModTime()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *Store) readRevisionLocked(name, revision string) (json.RawMessage, error) {
	if revision == "" || filepath.Base(revision) != revision || !strings.HasSuffix(revision, ".json") {
		return nil, errors.New("invalid revision name")
	}
	return os.ReadFile(filepath.Join(s.dir, "revisions", name, revision))
}
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dashboard-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func checksum(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
