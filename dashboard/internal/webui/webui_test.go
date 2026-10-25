package webui

import (
	"bytes"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestOverviewRendersManagerControls(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, panel := range []string{"config", "energy"} {
		panelRecorder := httptest.NewRecorder()
		Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(panelRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel="+panel, nil))
		if panelRecorder.Code != 200 {
			t.Fatalf("%s panel got status %d", panel, panelRecorder.Code)
		}
		body += panelRecorder.Body.String()
	}
	historyRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(historyRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=history", nil))
	body += historyRecorder.Body.String()
	devicesRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(devicesRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=devices", nil))
	body += devicesRecorder.Body.String()
	diagnosticsRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(diagnosticsRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=diagnostics", nil))
	body += diagnosticsRecorder.Body.String()
	settingsRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(settingsRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=settings", nil))
	body += settingsRecorder.Body.String()
	markers := []string{
		"Energie", "Aktualität", "Quelle", "energy-panel", "energy-roles", "dashboard.js",
		"Energie-Verläufe", "history-state", "history-series-picker", "historyPanel", "tab-history", "history-panel", "energy-history-chart", "history-aggregate", "history-range", "toggleSeries", "/static/js/history.js", "/static/js-deps/htmx.min.js", "/static/js-deps/alpine.min.js", "/static/js/dashboard.js",
		"id=\"overview-live\"", "dashboardShell", "setActivePanel('history-panel')", "x-on:click", "x-bind:class", "x-show=\"activePanel === 'config-panel'\"", "role=\"tablist\"", "role=\"tab\"", "role=\"tabpanel\"", "aria-controls=\"overview-panel\"",
		"id=\"overview-panel\" class=\"panel\" role=\"tabpanel\"", "x-bind:class=\"{ active: activePanel === 'overview-panel' }\"",
		"id=\"devices-live\"", "hx-get=\"/?fragment=devices-live\"",
		"id=\"runtime-status\"", "runtimeStatusPanel", "data-runtime-status-enabled=\"true\"", "data-status-bar-items=\"mqtt,storage,uptime,version\"", "aria-live=\"polite\"",
		"device-detail", "device-modal-warning", "discovery-diagnostics", "discovery_errors", "duplicateIDs", "discovery-error",
		"Konfiguration", "Einstellungen", "Diagnose", "license-footer", "(0BSD)", "(MIT, Copyright Caleb Porzio)", "ApexCharts 4.7.0", "(MIT, Copyright ApexCharts)", "ApexCharts-Lizenz", "v2.0.6/LICENSE", "v3.14.9/README.md", "configPanel", "x-model=\"selectedName\"", "reloadService()", "show-runtime-status", "showRuntimeStatus", "role=\"switch\"", "settings-toggle-track", "id=\"config-panel\"", "id=\"energy-panel\"", "data-panel-script=\"/static/js/revisions.js?v=4,/static/js/schema-form.js?v=2,/static/js/config-status.js?v=4,/static/js/config.page.js?v=10\"", "data-panel-script=\"/static/js-deps/cytoscape.min.js,/static/js/revisions.js?v=4,/static/js/devicemap-model.js?v=14,/static/js/devicemap-subdevices.js?v=1,/static/js/devicemap-graph.js?v=9,/static/js/devicemap-wiring.js?v=3,/static/js/devicemap-dataflow.js?v=5,/static/js/devicemap-node-svg.js?v=7,/static/js/devicemap-labels.js?v=4,/static/js/energy-plant.js?v=6,/static/js/device-picker.js?v=2,/static/js/devicemap-switch.js?v=2,/static/js/energy.page.js?v=13\"", "data-panel-css=\"/static/css/manager.css?v=48\"",
		"schema-form", "revision-preview", "config-presets-error",
		"config-actionbar-dock", "initActionBar()", "actionStatusText", "expandActions()", "id=\"config-form-save\"", "x-on:input=\"formDirty = true\"", "config-json", "resetEditor()", "id=\"config-save\"", "config-meta",
		"revision-diff", "revisionPanel(revisionConfig())", "setRevisionView('diff')",
		"diagnosticsPanel", "diagnostics-health-heading", "sortedHealthScores", "diagnostics-health-status", "sort('entity_id')", "settingsPanel", "x-model.number=\"healthScoreThreshold\"", "item.entity_id || '-'", "storage-health-heading", "Speicherzustand", "Geschätzte Restlaufzeit", "loadStorageHealth()",
		"settings-wide-panels", "Tabs ohne Breitendeckelung", "widePanelOptions", "wide-panels-select", "initChoices()",
		"settings-status-bar-items", "Angaben im Systemstatus", "statusBarItemOptions", "status-bar-items-select",
	}
	for _, marker := range markers {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q", marker)
		}
	}
	if strings.Index(body, ">Einstellungen</button>") < strings.Index(body, ">Diagnose</button>") {
		t.Fatal("Einstellungen tab is not last in the navigation")
	}
	if strings.Index(body, "/static/js/dashboard.js") > strings.Index(body, "/static/js-deps/alpine.min.js") {
		t.Fatal("dashboard.js must load before Alpine.js component initialization")
	}
	if strings.Index(body, "/static/js/history.js") > strings.Index(body, "/static/js-deps/alpine.min.js") {
		t.Fatal("history.js must load before Alpine.js component initialization")
	}
}

func TestDiagnosticsAssetLoadsHealthScores(t *testing.T) {
	recorder := httptest.NewRecorder()
	Static().ServeHTTP(recorder, httptest.NewRequest("GET", "/static/js/dashboard.js", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, marker := range []string{"warningGroups", "healthScores", "/api/v1/diagnostics/health", "sortedHealthScores", "runtimeStatusPanel", "/api/v1/health", "/api/v1/discovery", "selectDevice", "Promise.allSettled", "runtime-status-setting-changed", "registry-updated", "dashboard-panel-changed", "setActivePanel"} {
		if !strings.Contains(recorder.Body.String(), marker) {
			t.Fatalf("diagnostics asset does not contain %q", marker)
		}
	}
}

// Das Geraete-Modal laedt devicesPanel erst beim ersten Oeffnen nach.
func TestDeviceModalAssetCarriesTheModal(t *testing.T) {
	recorder := httptest.NewRecorder()
	Static().ServeHTTP(recorder, httptest.NewRequest("GET", "/static/js/device-modal.js", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, marker := range []string{"deviceModalMixin", "openDevice", "reloadDevice", "measurementEntities", "controlEntities", "configDiagEntities", "configEntities", "diagnosticEntities", "sortedWarnings", "groupedWarnings", "affectedEntityIds", "entityLabel", "activityLog"} {
		if !strings.Contains(recorder.Body.String(), marker) {
			t.Fatalf("device modal asset does not contain %q", marker)
		}
	}
}

func TestManagerAssetsLoadIndependently(t *testing.T) {
	assets := map[string][]string{
		"/static/js/config.page.js":    {"configPanel", "/api/v1/configurations", "/api/v1/topics", "/api/v1/shelly/presets", "applyShellyPreset", "shelly_devices", "confirmSave", "initActionBar", "editorDirty"},
		"/static/js/revisions.js":      {"revisionPanel", "diff-cropped"},
		"/static/js/energy.page.js":    {"energyRolesPanel", "/api/v1/energy/roles", "/api/v1/energy"},
		"/static/js/layout-editor.js":  {"layoutEditor", "/api/v1/layout", "newID"},
		"/static/js/tailscale.page.js": {"tailscalePanel", "/api/v1/tailscale/status", "/api/v1/tailscale/prereqs", "/api/v1/tailscale/login", "/api/v1/tailscale/logout", "/api/v1/tailscale/restart"},
		"/static/js/versions.page.js":  {"versionsPanel", "/api/v1/versions", "/api/v1/changelog"},
		"/static/css/manager.css":      {".schema-form", ".energy-role-row", ".energy-actionbar-dock", ".checkbox-list", ".config-actionbar", ".config-json"},
	}
	for path, markers := range assets {
		recorder := httptest.NewRecorder()
		Static().ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 200 {
			t.Fatalf("%s got status %d: %s", path, recorder.Code, recorder.Body.String())
		}
		for _, marker := range markers {
			if !strings.Contains(recorder.Body.String(), marker) {
				t.Fatalf("%s does not contain %q", path, marker)
			}
		}
	}
}

func TestOverviewDoesNotLoadManagerAssetsInitially(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`<script src="/static/js/config.page.js"`,
		`<script src="/static/js/energy.page.js"`,
		`<script src="/static/js/layout-editor.js"`,
		`<link rel="stylesheet" href="/static/css/manager.css"`,
		`<script src="/static/js/history.js"`,
		`<script src="/static/js/settings.page.js"`,
		`<script src="/static/js-deps/choices.min.js"`,
	} {
		if strings.Contains(body, marker) {
			t.Fatalf("initial page eagerly loads manager asset %q", marker)
		}
	}
	for path, script := range map[string]string{
		"history-panel":  "/static/js-deps/apexcharts.min.js,/static/js-deps/flatpickr.min.js?v=1,/static/js-deps/flatpickr-l10n-de.js?v=1,/static/js/history-export.js?v=2,/static/js/energy-model.js?v=4,/static/js/history-chart.js?v=5,/static/js/history.js?v=21",
		"settings-panel": "/static/js-deps/choices.min.js,/static/js/revisions.js?v=4,/static/js/schema-form.js?v=2,/static/js/settings-masonry.js?v=1,/static/js/settings.page.js?v=28,/static/js/mqtt.page.js?v=10,/static/js/tailscale.page.js?v=5,/static/js/versions.page.js?v=8,/static/js/systemconfig.page.js?v=5,/static/js/users-model.js?v=1,/static/js/users.page.js?v=1",
	} {
		if !strings.Contains(body, `id="`+path+`"`) {
			t.Fatalf("page is missing %s", path)
		}
		if !strings.Contains(body, `data-panel-script="`+script+`"`) {
			t.Fatalf("%s does not declare lazy script %q", path, script)
		}
	}
	if !strings.Contains(body, `data-panel-css="/static/css/choices.min.css,/static/css/choices.css?v=2,/static/css/manager.css?v=48,/static/css/settings-controls.css?v=18"`) {
		t.Fatal("settings-panel does not declare lazy choices.css + manager.css + settings-controls.css")
	}
	if strings.Contains(body, `<link rel="stylesheet" href="/static/css/choices.min.css"`) {
		t.Fatal("initial page eagerly loads choices.min.css")
	}
}

func TestOverviewNoLongerShipsTippyOrPopper(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := strings.ToLower(recorder.Body.String())
	for _, marker := range []string{"tippy", "popper"} {
		if strings.Contains(body, marker) {
			t.Fatalf("overview still references %q", marker)
		}
	}
	start := strings.Index(body, `id="devices-panel"`)
	if start < 0 {
		t.Fatal("page is missing devices-panel")
	}
	tag := body[start : start+strings.Index(body[start:], ">")]
	if strings.Contains(tag, "data-panel-script") || strings.Contains(tag, "data-panel-css") {
		t.Fatalf("devices-panel still declares lazy assets: %s", tag)
	}
	for _, name := range []string{"js-deps/popper.min.js", "js-deps/tippy.umd.min.js", "css/tippy.css"} {
		if _, err := fs.Stat(templateFS, "static/"+name); err == nil {
			t.Fatalf("embedded static still contains %s", name)
		}
	}
}

func TestStaticSetsCacheHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	Static().ServeHTTP(recorder, httptest.NewRequest("GET", "/static/js/dashboard.js", nil))
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=86400" {
		t.Fatalf("Cache-Control = %q, want public, max-age=86400", got)
	}
}

func TestOverviewIncludesReducedMotionRule(t *testing.T) {
	pageRecorder := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).ServeHTTP(pageRecorder, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(pageRecorder.Body.String(), `<link rel="stylesheet" href="/static/css/base.css`) {
		t.Fatal("overview does not link the base stylesheet")
	}
	cssRecorder := httptest.NewRecorder()
	Static().ServeHTTP(cssRecorder, httptest.NewRequest("GET", "/static/css/base.css", nil))
	if cssRecorder.Code != 200 {
		t.Fatalf("base.css got status %d", cssRecorder.Code)
	}
	css := cssRecorder.Body.String()
	for _, marker := range []string{"prefers-reduced-motion: reduce", ".energy-flow-line.is-active { animation: none; }"} {
		if !strings.Contains(css, marker) {
			t.Fatalf("base.css does not contain %q", marker)
		}
	}
}

func TestOverviewRendersTinyTuyaSettingsWorkflow(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	settingsRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(settingsRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=settings", nil))
	body += settingsRecorder.Body.String()
	for _, marker := range []string{
		"/static/js/settings.page.js", "tinyTuyaPanel", "https://iot.tuya.com", "https://github.com/jasonacox/tinytuya", "https://pypi.org/project/tinytuya/", "Create Cloud Project", "Link Devices by App Account", "Access Secret", "Local Key", "Rohe Statusantwort", "Technische Toolausgabe", "saveCredentials",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q", marker)
		}
	}
	if strings.Index(body, "/static/js/settings.page.js") > strings.Index(body, "/static/js-deps/alpine.min.js") {
		t.Fatal("settings.page.js must load before Alpine.js component initialization")
	}
	staticRecorder := httptest.NewRecorder()
	Static().ServeHTTP(staticRecorder, httptest.NewRequest("GET", "/static/js/settings.page.js", nil))
	for _, marker := range []string{"/api/v1/settings", "/api/v1/health/storage", "/api/v1/tiny-tuya/devices", "/api/v1/tiny-tuya/status", "/api/v1/tiny-tuya/configure", "/api/v1/tiny-tuya/credentials"} {
		if !strings.Contains(staticRecorder.Body.String(), marker) {
			t.Fatalf("settings asset does not contain %q", marker)
		}
	}
}

func TestOverviewRendersTailscaleSettingsWorkflow(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	settingsRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(settingsRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel=settings", nil))
	body += settingsRecorder.Body.String()
	for _, marker := range []string{
		"/static/js/tailscale.page.js", "tailscalePanel", "Voraussetzungen prüfen", "Anmeldung starten", "Ergebnis prüfen", "Aktiv ist reiner Gerätezugang", "knowhow/tailscale-setup.md", "Kein Auth-Key nötig",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q", marker)
		}
	}
}

func TestOverviewDisablesRuntimeStatusFromSettings(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{HealthScoreThreshold: 60, SweepIntervalSeconds: 300, ShowRuntimeStatus: false}); err != nil {
		t.Fatal(err)
	}

	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device:  registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity:  registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temperature", Name: "Temperatur"},
		RawJSON: `{"name":"Temperatur"}`,
	})
	recorder := httptest.NewRecorder()
	Overview(reg, config.NewManager(t.TempDir()), store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-runtime-status-enabled="false"`) {
		t.Fatal("runtime status setting was not passed to Alpine")
	}
}

func TestOverviewRendersDevicesLiveFragment(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/?fragment=devices-live", nil)
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `id="devices-live"`) || !strings.Contains(body, `hx-swap="outerHTML"`) {
		t.Fatalf("devices live fragment is missing HTMX attributes: %s", body)
	}
	if strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "tab-devices") {
		t.Fatalf("devices live fragment rendered the full page: %s", body)
	}
	if strings.Contains(body, `id="device-detail"`) {
		t.Fatalf("device detail dialog must not be part of the live fragment: %s", body)
	}
}

func TestOverviewRendersLiveFragment(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/?fragment=overview-live", nil)
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `id="overview-live"`) {
		t.Fatalf("live fragment is missing its target id: %s", body)
	}
	if strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "Browser-Historie") {
		t.Fatalf("live fragment rendered the full page: %s", body)
	}
	if strings.Contains(body, "energy-role-summary") {
		t.Fatalf("live fragment rendered the energy role overview: %s", body)
	}
}

func renderEnergyPanel(t *testing.T) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=energy", nil))
	if recorder.Code != 200 {
		t.Fatalf("energy panel got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

func renderOverviewWithItems(t *testing.T, items []settings.Item) string {
	t.Helper()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: items}},
	}}}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

// The panel used to carry two identical save buttons ("Interpretation
// speichern" and "Rollen speichern") that both call save(); they are replaced
// by one primary action inside a sticky material bar.
func TestEnergyPanelConsolidatesSaveIntoStickyBar(t *testing.T) {
	body := renderEnergyPanel(t)
	if !strings.Contains(body, "energy-actionbar-dock") {
		t.Fatalf("energy panel is missing the sticky action bar: %s", body)
	}
	for _, gone := range []string{`id="energy-interpretation-save"`, `id="energy-roles-save"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("energy panel still renders the retired save button %q", gone)
		}
	}
	if n := strings.Count(body, `id="energy-save"`); n != 1 {
		t.Fatalf("energy panel must have exactly one save action, found %d", n)
	}
}

// The settings sit in an accordion, so a long role list stays reachable; the
// conditional warnings are hoisted out so collapsing never hides one.
func TestEnergyPanelHoistsNoticesAboveCollapsibleInterpretation(t *testing.T) {
	body := renderEnergyPanel(t)
	notices := strings.Index(body, `class="energy-notices"`)
	interpretation := strings.Index(body, `data-energy-section="interpretation"`)
	if notices < 0 {
		t.Fatalf("energy panel is missing the hoisted notices strip: %s", body)
	}
	if interpretation < 0 {
		t.Fatalf("interpretation is not an accordion section: %s", body)
	}
	if notices > interpretation {
		t.Fatal("notices strip must render before the collapsible accordion")
	}
}

// Each role row carries its list position so the stylesheet can stagger the
// entry animation without any per-row JavaScript.
func TestEnergyRoleRowsCarryStaggerIndex(t *testing.T) {
	body := renderEnergyPanel(t)
	if !strings.Contains(body, "--row-index:") {
		t.Fatalf("role rows do not expose a --row-index custom property: %s", body)
	}
}

// The grey explanatory sub-texts under the interpretation controls move into
// the same icon-triggered tooltip component the automations page uses
// (.field-help). The radio cards keep their descriptions inline.
func TestEnergyInterpretationExplainsViaFieldHelpTooltips(t *testing.T) {
	body := renderEnergyPanel(t)
	if strings.Contains(body, `class="energy-interpretation-hint"`) {
		t.Fatalf("interpretation still renders a grey sub-text instead of a .field-help tooltip")
	}
	for _, want := range []string{`class="field-help"`, `class="field-help-trigger"`, `class="field-help-text"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("energy panel does not use the automations-style tooltip component %q: %s", want, body)
		}
	}
	if strings.Contains(body, `<label class="radio-card" title=`) {
		t.Fatalf("radio cards must keep their description inline, not as a native title tooltip")
	}
	if !strings.Contains(body, `class="radio-card-desc"`) {
		t.Fatalf("radio cards dropped their inline descriptions: %s", body)
	}
}

// The Hausverbrauch tiles come first, the Bilanzlücke toggle sits beneath them.
func TestEnergyInterpretationOrdersHausverbrauchBeforeBilanzluecke(t *testing.T) {
	body := renderEnergyPanel(t)
	hausverbrauch := strings.Index(body, `>Hausverbrauch<`)
	bilanzluecke := strings.Index(body, `>Bilanzlücke<`)
	if hausverbrauch < 0 || bilanzluecke < 0 {
		t.Fatalf("interpretation labels not found (haus=%d bilanz=%d)", hausverbrauch, bilanzluecke)
	}
	if hausverbrauch > bilanzluecke {
		t.Fatal("Hausverbrauch must render before Bilanzlücke")
	}
}

func TestOverviewUsesSavedLayoutItems(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true}}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `data-layout-item-kind="device"`) || !strings.Contains(body, `class="device-tile"`) {
		t.Fatalf("saved device layout item was not rendered: %s", body)
	}
	if strings.Contains(body, `id="energy-flow-card"`) {
		t.Fatal("energy flow was rendered although it was not selected in the saved layout")
	}
}

// TestOverviewSkipsEnergySummaryItemEntirely covers design.md section 1: the
// energy_summary card type is removed, but its cardCatalog entry stays so a
// layout saved before the removal still loads (settings.go rejects unknown
// types). Rendering must skip the item without a trace, not leave an empty
// grid cell - a neighboring device item confirms the rest of the row still
// renders normally around the gap.
func TestOverviewSkipsEnergySummaryItemEntirely(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "energy-summary", Type: "energy_summary", Span: "full", Visible: true},
			{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, `data-layout-item-kind="energy_summary"`) {
		t.Fatalf("energy_summary item must not produce a grid cell at all: %s", body)
	}
	if !strings.Contains(body, `data-layout-item-kind="device"`) {
		t.Fatalf("the device item after the skipped energy_summary item must still render: %s", body)
	}
}

// TestOverviewRendersEntityValueCard prueft die reduzierte Einzelwert-Karte:
// eine reine Sensor-Entitaet rendert als <article> (nur lesend), eine
// schaltbare (nicht-number) Entitaet als <button> - die ganze Karte ist dann
// das Tap-Ziel fuer sendCommand(), wie bei device-tile-entity/entity-list-
// item, statt eines separaten Steuerungs-Icons.
func TestOverviewRendersEntityValueCard(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_soc", ObjectID: "soc", Component: "sensor", Name: "Batterie SOC", UnitOfMeasurement: "%"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_relay", ObjectID: "relay", Component: "switch", Name: "Relais", CommandTopic: "state/node/relay/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "value:node_soc", Type: "entity_value", Ref: "node_soc", Span: "1", Visible: true},
			{ID: "value:node_relay", Type: "entity_value", Ref: "node_relay", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`data-layout-item-kind="entity_value"`,
		`<article class="entity-value-card" data-entity-id="node_soc"`,
		`Batterie SOC`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("entity_value card does not contain %q: %s", marker, body)
		}
	}
	if !strings.Contains(body, `<button type="button" class="entity-value-card" data-entity-id="node_relay"`) {
		t.Fatalf("commandable entity_value did not render as a button: %s", body)
	}
	if !strings.Contains(body, `x-on:click="sendCommand($event)" aria-label="Relais schalten"`) {
		t.Fatalf("commandable entity_value did not wire sendCommand: %s", body)
	}
}

// TestOverviewRendersEntityGroupCard prueft die frei zusammengestellte
// Entitaetenliste: der Titel kommt vom Item, nicht von einer Entitaet, jeder
// gueltige Ref wird zu einer Chip-Zeile, und ein Ref ohne Treffer (geloeschte
// Entitaet) wird still uebersprungen - dieselbe Semantik wie ein einzelner
// entity-Ref, der ins Leere zeigt.
func TestOverviewRendersEntityGroupCard(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_temp_werkstatt", ObjectID: "temp_werkstatt", Component: "sensor", Name: "Werkstatt", UnitOfMeasurement: "°C"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_temp_lager", ObjectID: "temp_lager", Component: "sensor", Name: "Lager", UnitOfMeasurement: "°C"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{
				ID: "group", Type: "entity_group", Span: "1", Visible: true, Title: "Temperatursensoren",
				EntityRefs: []string{"node_temp_werkstatt", "node_temp_lager", "node_temp_geloescht"},
			},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`data-layout-item-kind="entity_group"`,
		`class="entity-group-card" aria-label="Temperatursensoren"`,
		`<h4>Temperatursensoren</h4><span class="entity-group-count">2</span>`,
		`Werkstatt`,
		`Lager`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("entity_group card does not contain %q: %s", marker, body)
		}
	}
	if strings.Contains(body, "node_temp_geloescht") {
		t.Fatalf("entity_group rendered a ref with no matching entity: %s", body)
	}
	if strings.Count(body, "entity-group-chip-label") != 2 {
		t.Fatalf("entity_group rendered %d chips, want exactly the 2 resolvable refs: %s", strings.Count(body, "entity-group-chip-label"), body)
	}
}

// TestOverviewRendersCardStyleAndFlowScale prueft Spec F: die Rasterzelle
// traegt ihre Geometrie in der Klasse layout-grid-item-{span}, ihre Hoehen in
// zwei Custom-Properties und ihre Fuellbereitschaft in data-fills-height.
// grid-column/grid-row stehen nicht mehr im Inline-Stil - Zeilen sind
// inhaltshoch, Spalten kommen aus der Klasse.
func TestOverviewRendersCardStyleAndFlowScale(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "energy-flow", Type: "energy_flow", Span: "3", Visible: true, Height: 3, FlowScale: "speed"},
			{ID: "diagnostics", Type: "diagnostics", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`class="layout-grid-item layout-grid-item-3"`,
		`--card-min-height: 25rem;`,
		// 3 * 7rem + 2 * .8rem, in Go ausgerechnet statt als calc() -
		// html/template filtert Klammern im style-Attribut.
		`--card-height: 22.6rem;`,
		`data-fills-height="true"`,
		`data-flow-scale="speed"`,
		`class="layout-grid-item layout-grid-item-1"`,
		`--card-min-height: 6rem;`,
		`data-fills-height="false"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
	// Die Karte ohne Zwangshoehe bekommt kein --card-height.
	if strings.Count(body, "--card-height") != 1 {
		t.Fatalf("--card-height steht %d mal in der Seite, want 1", strings.Count(body, "--card-height"))
	}
	// Die beiden Bruchstuecke des alten Inline-Stils, wortgenau - eine Suche
	// nach "grid-column:" allein wuerde auch in einer Kartenvorlage anschlagen.
	for _, gone := range []string{`style="grid-column:`, "; grid-row: span "} {
		if strings.Contains(body, gone) {
			t.Fatalf("Inline-Stil traegt noch %q: %s", gone, body)
		}
	}
}

// TestOverviewRendersSelectedEnergyGraphicCards covers the six alternative
// energy-flow cards (docs/dashboard/overview-cards.md): each is a
// singleton layout item type, same as energy_flow, and each card wrapper
// renders only when it is the selected item - the dispatch chain in
// overview.html's "overview-live" template, plus the corresponding
// "energy-<type>-card" named template.
func TestOverviewRendersSelectedEnergyGraphicCards(t *testing.T) {
	cases := []struct {
		itemType string
		cardID   string
	}{
		{"energy_band", "energy-band-card"},
		{"energy_ring", "energy-ring-card"},
		{"energy_board", "energy-board-card"},
		{"energy_day", "energy-day-card"},
		{"energy_schema", "energy-schema-card"},
		{"energy_status", "energy-status-card"},
	}

	for _, testCase := range cases {
		t.Run(testCase.itemType, func(t *testing.T) {
			reg := registry.New()
			reg.UpsertEntity(registry.Discovery{
				Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
				Entity: registry.EntityInfo{UniqueID: "inverter_pv", ObjectID: "pv_power", Component: "sensor", Name: "PV", StateTopic: "state/pv", UnitOfMeasurement: "W"},
			})
			reg.UpdateState("state/pv", []byte("1200"), false, time.Now())
			store := settings.NewStore(t.TempDir())
			if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
				ID: "overview", Name: "Overview", Order: 0,
				Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "item", Type: testCase.itemType, Span: "full", Visible: true}}}},
			}}}); err != nil {
				t.Fatal(err)
			}

			recorder := httptest.NewRecorder()
			Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
			if recorder.Code != 200 {
				t.Fatalf("got status %d", recorder.Code)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, `id="`+testCase.cardID+`"`) {
				t.Fatalf("%s: card was not rendered: %s", testCase.itemType, body)
			}
			// Der Schnappschuss steht seit dem Serverlast-Spec einmal fuer
			// alle Karten in #overview-live, nicht mehr je Karte.
			if !strings.Contains(body, `id="energy-snapshot-initial"`) {
				t.Fatalf("%s: shared snapshot script was not rendered: %s", testCase.itemType, body)
			}
			if !strings.Contains(body, `data-layout-item-kind="`+testCase.itemType+`"`) {
				t.Fatalf("%s: layout grid item did not carry its type: %s", testCase.itemType, body)
			}

			// Absent from the layout -> absent from the rendered body.
			emptyStore := settings.NewStore(t.TempDir())
			if err := emptyStore.SaveLayout(settings.Layout{Pages: []settings.Page{{
				ID: "overview", Name: "Overview", Order: 0,
				Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "flow", Type: "energy_flow", Span: "full", Visible: true}}}},
			}}}); err != nil {
				t.Fatal(err)
			}
			recorder = httptest.NewRecorder()
			Overview(reg, nil, emptyStore).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
			if strings.Contains(recorder.Body.String(), `id="`+testCase.cardID+`"`) {
				t.Fatalf("%s: card was rendered although it was not selected in the saved layout", testCase.itemType)
			}
		})
	}
}

// TestOverviewOnlyLoadsSelectedEnergyGraphicScripts covers the resource-
// conservation counterpart to TestOverviewRendersSelectedEnergyGraphicCards:
// the six alternative energy-graphic cards each fully re-mount on every
// #overview-live refresh (see the comment atop energy-band.js), so their
// script has to be present in the initial page - but base.html previously
// loaded all six energy-<type>.js files plus their shared energy-model.js
// on every single page load regardless of the saved layout. Now the server
// only emits <script> tags for the types actually selected, mirroring how
// the card HTML itself is already gated by the Type dispatch in
// "overview-live".
func TestOverviewOnlyLoadsSelectedEnergyGraphicScripts(t *testing.T) {
	allScripts := []string{
		"/static/js/energy-model.js?v=4",
		"/static/js/energy-band.js?v=2", "/static/js/energy-ring.js?v=2", "/static/js/energy-board.js?v=2",
		"/static/js/energy-day.js?v=3", "/static/js/energy-schema.js?v=2", "/static/js/energy-status.js?v=1",
	}

	t.Run("default layout loads none of the six card scripts", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		Overview(registry.New(), nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
		body := recorder.Body.String()
		for _, script := range allScripts {
			if strings.Contains(body, `<script src="`+script+`"`) {
				t.Fatalf("default layout eagerly loads unused energy graphic script %q", script)
			}
		}
	})

	cases := []struct {
		itemType string
		script   string
	}{
		{"energy_band", "/static/js/energy-band.js?v=2"},
		{"energy_ring", "/static/js/energy-ring.js?v=2"},
		{"energy_board", "/static/js/energy-board.js?v=2"},
		{"energy_day", "/static/js/energy-day.js?v=3"},
		{"energy_schema", "/static/js/energy-schema.js?v=2"},
		{"energy_status", "/static/js/energy-status.js?v=1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.itemType, func(t *testing.T) {
			store := settings.NewStore(t.TempDir())
			if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
				ID: "overview", Name: "Overview", Order: 0,
				Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "item", Type: testCase.itemType, Span: "full", Visible: true}}}},
			}}}); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			Overview(registry.New(), nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
			body := recorder.Body.String()
			if !strings.Contains(body, `<script src="/static/js/energy-model.js?v=4"`) {
				t.Fatalf("%s: page does not load the shared energy-model.js", testCase.itemType)
			}
			if !strings.Contains(body, `<script src="`+testCase.script+`"`) {
				t.Fatalf("%s: page does not load %q", testCase.itemType, testCase.script)
			}
			for _, script := range allScripts {
				if script == testCase.script || script == "/static/js/energy-model.js?v=4" {
					continue
				}
				if strings.Contains(body, `<script src="`+script+`"`) {
					t.Fatalf("%s: page also loads unrelated energy graphic script %q", testCase.itemType, script)
				}
			}
		})
	}

	t.Run("an invisible item does not pull in its script", func(t *testing.T) {
		store := settings.NewStore(t.TempDir())
		if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
			ID: "overview", Name: "Overview", Order: 0,
			Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "item", Type: "energy_band", Span: "full", Visible: false}}}},
		}}}); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		Overview(registry.New(), nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
		body := recorder.Body.String()
		if strings.Contains(body, `<script src="/static/js/energy-band.js?v=2"`) {
			t.Fatal("invisible energy_band item still loaded energy-band.js")
		}
		if strings.Contains(body, `<script src="/static/js/energy-model.js?v=4"`) {
			t.Fatal("invisible energy_band item still loaded energy-model.js")
		}
	})
}

func TestOverviewDefaultsToCompactCards(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", UnitOfMeasurement: "W", StateTopic: "node/power"},
	})
	reg.UpdateState("node/power", []byte("42"), false, time.Now())
	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{`data-device-view-mode="compact"`, `class="device-compact-grid"`, `class="compact-card is-unknown"`, `class="compact-card-row-value">42 W</span>`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("compact fragment does not contain %q: %s", marker, body)
		}
	}
	for _, forbidden := range []string{`class="device-tile-grid"`, `class="device-overview"`, `entity-command-button`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("compact fragment unexpectedly contains %q: %s", forbidden, body)
		}
	}
}

func TestDevicesLiveFragmentHonorsRequestedViewMode(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_relay", ObjectID: "relay", Component: "switch", Name: "Relais", CommandTopic: "node/relay/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})

	compact := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(compact, httptest.NewRequest("GET", "/?fragment=devices-live", nil))
	if b := compact.Body.String(); !strings.Contains(b, `data-device-view-mode="compact"`) || !strings.Contains(b, `class="compact-card`) {
		t.Fatalf("default fragment is not the compact view: %s", b)
	}

	control := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(control, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	b := control.Body.String()
	for _, marker := range []string{`data-device-view-mode="control"`, `class="device-tile-grid"`, `class="device-tile"`, `data-entity-id="node_relay"`, `class="entity-command-button"`} {
		if !strings.Contains(b, marker) {
			t.Fatalf("view_mode=control fragment does not contain %q: %s", marker, b)
		}
	}
	if strings.Contains(b, `class="compact-card `) {
		t.Fatalf("view_mode=control must not render compact cards: %s", b)
	}
}

func TestOverviewRendersNumberSliderInControlTiles(t *testing.T) {
	minimum, maximum, step := 10.0, 60.0, 5.0
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_interval", ObjectID: "interval", Component: "number", Name: "Intervall", CommandTopic: "node/interval/set", MinValue: &minimum, MaxValue: &maximum, Step: &step},
	})
	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{`data-command-type="number"`, `class="entity-number-slider"`, `min="10"`, `max="60"`, `step="5"`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("control fragment does not contain %q: %s", marker, body)
		}
	}
}

// TestOverviewRendersEnergyFlowCard checks the always-on client-rendered
// energy-flow-card: the Alpine component wiring, the embedded initial
// snapshot energy-flow.js reads on mount to draw the graphic, and the five
// server-rendered node articles that give it a no-JS/first-paint fallback.
// The card is a normal part of #overview-live/the layout grid - see the
// comment atop energy-flow.js for why it deliberately has no hx-preserve or
// client-owned live-update path of its own. The graphic's geometry/scaling
// itself is covered client-side in test/energyflow.page.test.mjs.
func TestOverviewRendersEnergyFlowCard(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
		Entity: registry.EntityInfo{UniqueID: "pv_power", ObjectID: "pv_power", Component: "sensor", Name: "PV Leistung", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("420"), false, time.Now())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`id="energy-flow-card" class="energy-flow-card" aria-labelledby="energy-flow-title" x-data="energyFlowCard()" x-init="init()"`,
		`id="energy-snapshot-initial"`,
		`"pv":420`,
		`class="energy-flow-node energy-flow-node-home"`,
		`x-ref="map"`,
		`x-ref="svg"`,
		`data-node="pv"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
	if strings.Contains(body, `energy-flow-arrow`) {
		t.Fatalf("arrow marker should be gone - flow direction is shown purely via animation direction now: %s", body)
	}
	// The card was aligned with the other energy tiles: no "Live" badge, no
	// footer legend, no timestamp - the coloured stripe on each node carries
	// the legend now (see base.css .energy-flow-node).
	for _, gone := range []string{`energy-flow-status`, `energy-flow-footer`, `energy-flow-node-kind`} {
		if strings.Contains(body, gone) {
			t.Fatalf("energy-flow chrome %q should be removed: %s", gone, body)
		}
	}
	if strings.Contains(body, `toggleMode`) {
		t.Fatalf("the legacy/live mode toggle was retired - the client-rendered graphic is the only one now: %s", body)
	}
}

// TestOverviewRendersEnergyFlowSpeedReferenceSettings checks that a layout
// item's per-tile SpeedReferenceMode/-Watts reach energy-flow.js's render()
// as data attributes on the [data-layout-item-id] wrapper - the same element
// that already carries the per-tile FlowScale - see flowDuration()'s
// reference parameter in energy-flow.js.
func TestOverviewRendersEnergyFlowSpeedReferenceSettings(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "energy-flow", Type: "energy_flow", Span: "full", Visible: true,
				FlowScale: "speed", SpeedReferenceMode: settings.EnergyFlowSpeedReferenceModeFixed, SpeedReferenceWatts: 2500},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`data-speed-reference-mode="fixed"`,
		`data-speed-reference-watts="2500"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
}

// TestOverviewGreysStaleEnergyFlowValueWithoutDisablingPath checks that
// staleness reaches the client-rendered graphic, which computes it from the
// embedded roles array (see energy-flow.js's isStale()/nodeText()).
func TestOverviewGreysStaleEnergyFlowValueWithoutDisablingPath(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
		Entity: registry.EntityInfo{UniqueID: "pv_power", ObjectID: "pv_power", Name: "PV Leistung", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("420"), true, time.Now().UTC())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`"pv":420`,
		`"freshness":"stale"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
}

func TestOverviewGreysOfflineEnergyFlowValueWithoutDisablingPath(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
		Entity: registry.EntityInfo{UniqueID: "pv_power", ObjectID: "pv_power", Name: "PV Leistung", StateTopic: "state/pv", AvailabilityTopic: "status/inverter", PayloadAvailable: "online", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("420"), false, time.Now().UTC())
	reg.UpdateAvailability("status/inverter", []byte("offline"), false, time.Now().UTC())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`"pv":420`,
		`"freshness":"stale"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
}

func TestOverviewLoadsAlpinePluginsBeforeAlpine(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	collapse := strings.Index(body, "/static/js-deps/alpine-collapse.min.js")
	core := strings.Index(body, "/static/js-deps/alpine.min.js")
	if collapse < 0 || core < 0 || collapse > core {
		t.Fatalf("Alpine plugins must load before Alpine core: collapse=%d core=%d", collapse, core)
	}
	// alpine-mask is unused (no template applies x-mask) and is intentionally
	// not part of the eager script list.
	if strings.Contains(body, "/static/js-deps/alpine-mask.min.js") {
		t.Fatal("unused alpine-mask asset should not be eagerly loaded")
	}
}

func TestOverviewRendersDiscoveryIcons(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "battery", Name: "Battery"},
		Entity: registry.EntityInfo{UniqueID: "battery_soc", ObjectID: "soc", Component: "sensor", Name: "Battery SoC", Icon: "mdi:battery"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "battery", Name: "Battery"},
		Entity: registry.EntityInfo{UniqueID: "battery_voltage", ObjectID: "voltage", Component: "sensor", Name: "Battery Voltage", DeviceClass: "voltage"},
	})

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`class="entity-icon"`,
		`aria-label="Home Assistant icon: mdi:battery"`,
		`<svg viewBox="0 0 24 24"`,
		`aria-label="Fallback icon: mdi:sine-wave"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
}

func TestOverviewRendersTimestampValueForRelativeBrowserFormatting(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_update", ObjectID: "last_update", Component: "sensor", Name: "Letzte Aktualisierung", StateTopic: "state/update", DeviceClass: "timestamp"},
	})
	reg.UpdateState("state/update", []byte("1785538823"), false, time.Now())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-relative-timestamp="1785538823"`) {
		t.Fatalf("timestamp value was not exposed for relative browser formatting: %s", recorder.Body.String())
	}
}

func TestOverviewDoesNotRenderUnknownIconAsMarkup(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_custom", ObjectID: "custom", Component: "sensor", Name: "Custom", Icon: "mdi:<unsafe>"},
	})

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<unsafe>") {
		t.Fatalf("unknown icon was rendered as markup: %s", body)
	}
	if !strings.Contains(body, "Fallback icon: mdi:&lt;unsafe&gt;") {
		t.Fatalf("unknown icon label was not escaped: %s", body)
	}
}

// prefixedRequest builds a request as an nginx location /node/ would forward
// it: the prefix announced in X-Forwarded-Prefix, resolved by
// basepath.Middleware before the handler runs.
func prefixedRequest(target string) *http.Request {
	request := httptest.NewRequest("GET", target, nil)
	request.Header.Set("X-Forwarded-Prefix", "/node")
	return request
}

func renderWithBasePath(t *testing.T, handler http.Handler, target string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	basepath.Middleware(handler).ServeHTTP(recorder, prefixedRequest(target))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

func TestOverviewPrefixesEveryURLBehindAForwardedPrefix(t *testing.T) {
	body := renderWithBasePath(t, Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())), "/node/")
	for _, marker := range []string{
		`<html lang="de" data-base-path="/node" data-theme="mint">`,
		`href="/node/static/css/base.css?v=43"`,
		`href="/node/static/img/favicon.svg"`,
		`<script src="/node/static/js/dashboard.js`,
		`<script src="/node/static/js-deps/alpine.min.js"`,
		`data-panel-src="/node/?fragment=panel&panel=devices"`,
		`data-panel-css="/node/static/css/choices.min.css,/node/static/css/choices.css?v=2,/node/static/css/manager.css?v=48,/node/static/css/settings-controls.css?v=18"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("proxied page does not contain %q", marker)
		}
	}
	// Nothing may stay root-relative: a leftover "/static/..." would resolve
	// against the proxy host instead of the dashboard.
	if strings.Contains(body, `="/static/`) {
		t.Fatalf("proxied page still contains an unprefixed /static/ reference: %s", body)
	}
}

func TestDevicesFragmentPrefixesHXGet(t *testing.T) {
	body := renderWithBasePath(t, Overview(registry.New(), nil, nil), "/node/?fragment=devices-live")
	if !strings.Contains(body, `hx-get="/node/?fragment=devices-live"`) {
		t.Fatalf("devices fragment does not poll through the base path: %s", body)
	}
}

func TestOverviewPrefixesEnergyCardScripts(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{{ID: "item", Type: "energy_band", Span: "full", Visible: true}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	body := renderWithBasePath(t, Overview(registry.New(), nil, store), "/node/")
	if !strings.Contains(body, `<script src="/node/static/js/energy-band.js?v=2"`) {
		t.Fatalf("energy card script is not prefixed: %s", body)
	}
}

func TestOverviewRejectsHostileForwardedPrefix(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("X-Forwarded-Prefix", "//evil.com")
	basepath.Middleware(Overview(registry.New(), nil, nil)).ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if strings.Contains(body, "evil.com") {
		t.Fatalf("hostile prefix leaked into the page: %s", body)
	}
	if !strings.Contains(body, `<html lang="de" data-base-path="" data-theme="mint">`) {
		t.Fatalf("hostile prefix did not fall back to an empty base path: %s", body)
	}
}

func TestLoginPrefixesAuthEndpoints(t *testing.T) {
	body := renderWithBasePath(t, Login(false, nil, ""), "/node/login")
	if !strings.Contains(body, `<html lang="de" data-base-path="/node" data-theme="mint">`) {
		t.Fatalf("login page does not publish the base path: %s", body)
	}
	for _, marker := range []string{"${base}/api/v1/auth/login", "${base}/api/v1/auth/guest", "window.location.assign(`${base}/`)"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("login page does not contain %q: %s", marker, body)
		}
	}
}

func TestLoginShowsAdminAuthWarningWhenSet(t *testing.T) {
	for lang, texts := range readCatalogs(t) {
		request := httptest.NewRequest("GET", "/", nil)
		request.AddCookie(&http.Cookie{Name: "lang", Value: lang})
		recorder := httptest.NewRecorder()
		Login(false, nil, "login.admin_auth_unavailable").ServeHTTP(recorder, request)
		want := `<p class="admin-auth-warning" role="alert">` + html.EscapeString(texts["login.admin_auth_unavailable"]) + `</p>`
		if body := recorder.Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s login page does not show the admin auth warning %q", lang, want)
		}
	}
}

func TestLoginOmitsAdminAuthWarningByDefault(t *testing.T) {
	recorder := httptest.NewRecorder()
	Login(false, nil, "").ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	if strings.Contains(body, `<p class="admin-auth-warning"`) {
		t.Fatalf("login page shows an empty admin auth warning: %s", body)
	}
}

func TestLoginOverHTTPKeepsFormUsableAndRootPathsWithoutPrefix(t *testing.T) {
	recorder := httptest.NewRecorder()
	Login(true, nil, "").ServeHTTP(recorder, httptest.NewRequest("GET", "/login", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, "<input name=\"password\" type=\"password\" autocomplete=\"current-password\" required>") {
		t.Fatalf("login page over HTTP does not render the password form as usable: %s", body)
	}
	if strings.Contains(body, "required disabled") {
		t.Fatalf("login page over HTTP disables a form field: %s", body)
	}
	if !strings.Contains(body, `<html lang="de" data-base-path="" data-theme="mint">`) {
		t.Fatalf("unproxied login page does not fall back to an empty base path: %s", body)
	}
}

func TestOverviewRendersThemeAttribute(t *testing.T) {
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{
		HealthScoreThreshold: 60, SweepIntervalSeconds: 300,
		ShowRuntimeStatus: true,
		DeviceViewMode:    settings.DeviceViewModeControl, Theme: settings.ThemeSignalgelb,
	}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `data-theme="signalgelb"`) {
		t.Fatal("rendered page carries no data-theme attribute")
	}
}

func TestOverviewFallsBackToMintWithoutStore(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(recorder.Body.String(), `data-theme="mint"`) {
		t.Fatal("missing store did not fall back to mint")
	}
}

func TestLoginRendersThemeAttribute(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveSettings(settings.Settings{
		HealthScoreThreshold: 60, SweepIntervalSeconds: 300,
		ShowRuntimeStatus: true,
		DeviceViewMode:    settings.DeviceViewModeControl, Theme: settings.ThemeTageslicht,
	}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Login(false, store, "").ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(recorder.Body.String(), `data-theme="tageslicht"`) {
		t.Fatal("login page carries no data-theme attribute")
	}
}

func TestLoginWithoutStoreFallsBackToMint(t *testing.T) {
	recorder := httptest.NewRecorder()
	Login(true, nil, "").ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(recorder.Body.String(), `data-theme="mint"`) {
		t.Fatal("missing store did not fall back to mint")
	}
}

func TestBaseTemplateSetsTheViewport(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `name="viewport"`) {
		t.Fatal("ohne Viewport-Meta rendern mobile Browser mit ~980px und die max-width-Regeln in base.css greifen nie")
	}
	if !strings.Contains(body, `content="width=device-width, initial-scale=1"`) {
		t.Fatalf("falscher Viewport-Inhalt in:\n%s", body)
	}
}

// Das Icon-Sprite liegt seit dem Verlaeufe-Redesign in der Shell
// (base.html), nicht mehr im Automations-Fragment: Panels werden per
// outerHTML einzeln nachgeladen (loadPanel() in dashboard.js), ein Sprite im
// Fragment selbst waere fuer jedes andere Panel unsichtbar gewesen. Die
// Symbole muessen deshalb in der Vollseite stehen, das Fluss-Markup bleibt
// im Automations-Fragment.
func TestAutomationsPanelShipsTheIconSpriteAndFlowMarkup(t *testing.T) {
	pageRecorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(pageRecorder, httptest.NewRequest("GET", "/", nil))
	page := pageRecorder.Body.String()
	for _, symbol := range []string{
		"ico-sun", "ico-grid", "ico-battery", "ico-load", "ico-wallbox", "ico-heatpump",
		"ico-clock", "ico-topic", "ico-flash", "ico-switch", "ico-bell", "ico-play",
		"ico-check", "ico-warning", "ico-gate",
		// Knoepfe des Bearbeiten-Modus
		"ico-chevron", "ico-pencil", "ico-trash", "ico-plus", "ico-save",
		"ico-help", "ico-gear", "ico-power-on", "ico-power-off",
		// Fuers Verlaeufe-Panel ergaenzte Symbole
		"ico-calendar", "ico-download", "ico-close",
		// Geraete-Symbol des Wechselrichters fuer Szenarien
		"ico-inverter",
		// Benutzer-Eintrag der Einstellungen
		"ico-user",
	} {
		if !strings.Contains(page, `id="`+symbol+`"`) {
			t.Fatalf("Sprite-Symbol %s fehlt in der Shell", symbol)
		}
	}

	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, marker := range []string{`auto-segment`, `auto-scenario`, `auto-rule`, `class="auto-line`, `class="auto-pill`, `class="auto-fires`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Fluss-Markup %q fehlt im Automations-Panel", marker)
		}
	}
}

// Die Sprite-<symbol> bringen weder fill noch stroke mit (siehe base.css,
// .automation-icon). Das Icon der Update-Leiste ist deshalb ein Inline-SVG mit
// eigener Klasse, die in base.css Strich und Farbe setzt.
func TestUpdateBarIconIsStyledByItsClass(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	page := recorder.Body.String()
	if !strings.Contains(page, `<svg class="update-bar-icon" viewBox="0 0 24 24" aria-hidden="true">`) {
		t.Fatal("das Icon der Update-Leiste braucht class=\"update-bar-icon\" (fill:none, stroke aus base.css)")
	}
}

// Die Szenario-Module laden vor dem Seitenskript, sonst fehlt
// window.__automationScenario beim ersten Render der Seite.
func TestAutomationsPanelLoadsTheScenarioModules(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	const attr = `data-panel-script="`
	panel := strings.Index(body, `id="automations-panel"`)
	if panel < 0 {
		t.Fatal("das Automations-Panel fehlt in der Shell")
	}
	start := strings.Index(body[panel:], attr)
	if start < 0 {
		t.Fatal("das Automations-Panel hat keine data-panel-script-Liste")
	}
	line := body[panel+start+len(attr):]
	line = line[:strings.Index(line, `"`)]
	scenario := strings.Index(line, "automations-scenario.js?v=3")
	chart := strings.Index(line, "automations-chart.js?v=3")
	page := strings.Index(line, "automations.page.js?v=")
	wizard := strings.Index(line, "automation-wizard.js?v=2")
	templates := strings.Index(line, "automation-templates.js?v=2")
	sheet := strings.Index(line, "automation-sheet.js?v=1")
	if scenario < 0 || chart < 0 || page < 0 || wizard < 0 || templates < 0 || sheet < 0 {
		t.Fatalf("Szenario- oder Assistenten-Module fehlen in der Skriptliste: %s", line)
	}
	if scenario > page || chart > page {
		t.Fatalf("Szenario-Module laden nach automations.page.js: %s", line)
	}
	// Die Vorlagen-Engine haengt am Baustein-Modul, beide vor der Seite.
	if sheet > page {
		t.Fatalf("automation-sheet.js lädt nach automations.page.js: %s", line)
	}
	if wizard > templates || templates > page {
		t.Fatalf("Assistenten-Module in falscher Reihenfolge vor automations.page.js: %s", line)
	}
}

// Icon-only Knoepfe brauchen beides: title fuer die Maus, aria-label fuer
// Screenreader. Die Konvention stammt aus der Device-Map.
func TestAutomationsIconButtonsCarryAccessibleNames(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `class="icon-button`) {
		t.Fatal("das Automations-Panel benutzt .icon-button nicht")
	}
	if !strings.Contains(body, "aria-label") {
		t.Fatal("das Automations-Panel setzt kein aria-label an seinen Icon-Knoepfen")
	}
}

// Der Typ heisst jetzt "Energiewert", nicht mehr "PV-Ueberschuss": er deckt
// jedes Bilanzfeld ab, nicht nur die Einspeisung.
func TestAutomationsOffersEnergyValueInsteadOfPvSurplus(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	if strings.Contains(body, "PV-Überschuss") {
		t.Fatal("das Automations-Panel bietet noch 'PV-Überschuss' an")
	}
	if !strings.Contains(body, "type-tiles") {
		t.Fatal("das Automations-Panel benutzt die Typ-Kacheln nicht")
	}
}

// Der Tab beginnt mit den Regeln, nicht mit dem Tick-Intervall: der lange
// Erklaertext zu den Publish-Praefixen steckt jetzt im Hilfe-Popover.
func TestAutomationsSettingsSitBelowTheRulesInAnAdvancedSection(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()

	if !strings.Contains(body, "automations-advanced") {
		t.Fatal("die Dienst-Einstellungen stehen nicht im Fortgeschrittenen-Bereich")
	}
	rules := strings.Index(body, `x-on:click="openAssistant()"`)
	settings := strings.Index(body, "automations-advanced")
	if rules < 0 || settings < 0 || settings < rules {
		t.Fatalf("die Dienst-Einstellungen stehen nicht unter den Regeln (Regeln bei %d, Einstellungen bei %d)", rules, settings)
	}
	if strings.Contains(body, "Leer gelassen erlaubt das Dashboard beim Speichern") {
		t.Fatal("der Praefix-Erklaertext steht noch als Absatz im Markup statt im Hilfe-Popover")
	}
}

func TestAutomationsPanelLoadsItsOwnStylesheet(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, "static/css/automations.css") {
		t.Fatal("automations.css ist nicht als Panel-Stylesheet eingetragen")
	}
}

// TestAutomationsPanelShipsTheAssistantDialog haelt fest, dass der Vorlagen-
// Assistent als Dialog im Panel steht und ueber den Kopfknopf oeffnet.
func TestAutomationsPanelShipsTheAssistantDialog(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, marker := range []string{
		`<dialog`, `x-ref="assistantDialog"`, `class="auto-assist"`, `class="auto-assist-sheet"`,
		`x-ref="assistantSearch"`, `x-on:click="openAssistant()"`,
		`x-on:close="`, `closeAssistant()`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Assistenten-Markup %q fehlt im Automations-Panel", marker)
		}
	}
}

// TestAutomationsPanelShipsAssistantBlocks haelt fest, dass die Bausteine
// multi, number, time und summary samt Zusammenfassung im Assistenten-Markup stehen.
func TestAutomationsPanelShipsAssistantBlocks(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, marker := range []string{
		`assistantStep().type === 'multi'`, `assistantStep().type === 'number'`,
		`assistantStep().type === 'time'`, `assistantStep().id === 'summary'`,
		`class="settings-toggle auto-assist-check"`, `assistantToggleDay(`,
		`assistantGoTo(row.step)`, `assistantPreview()`, `$t('automations.assistant.change')`,
		`class="auto-assist-sum-row"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Assistenten-Baustein %q fehlt im Automations-Panel", marker)
		}
	}
	if strings.Contains(body, "5c: Bausteine") {
		t.Fatal("der Platzhalter fuer die Bausteine ist noch im Markup")
	}
}

// TestAutomationsAssistantShipsTheTargetBand haelt fest, dass der Schritt
// "Wie knapp soll geregelt werden?" das Zielband mit Toleranzschalter,
// Kennzahlen und Tastatur-Schieberegler ausliefert. Die Rolle des Schiebers
// setzt automation-band.js, daher wird die Quelle geprueft.
func TestAutomationsAssistantShipsTheTargetBand(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, marker := range []string{
		`x-ref="assistBand"`, `automations.assistant.band.tolerance_toggle`,
		`assistantBandToggleTolerance()`, `assistantBandStats()`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Zielband-Markup %q fehlt im Automations-Panel", marker)
		}
	}
	js, err := os.ReadFile("static/js/automation-band.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`role`, `slider`} {
		if !strings.Contains(string(js), marker) {
			t.Fatalf("automation-band.js setzt den Schieber nicht: %q fehlt", marker)
		}
	}
	shell := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(shell, httptest.NewRequest("GET", "/", nil))
	const attr = `data-panel-script="`
	panel := strings.Index(shell.Body.String(), `id="automations-panel"`)
	start := strings.Index(shell.Body.String()[panel:], attr)
	line := shell.Body.String()[panel+start+len(attr):]
	line = line[:strings.Index(line, `"`)]
	sheet := strings.Index(line, "automation-sheet.js?v=1")
	band := strings.Index(line, "automation-band.js?v=1")
	page := strings.Index(line, "automations.page.js?v=")
	if band < 0 || band < sheet || band > page {
		t.Fatalf("automation-band.js steht nicht zwischen automation-sheet.js und automations.page.js: %s", line)
	}
}

// TestOverviewEmbedsEnergySnapshotOnce haelt fest, dass der Energie-
// Schnappschuss genau einmal im Fragment steht. Vorher trug ihn jede der
// sieben Energiekarten separat - identischer Inhalt, 26,6 KB von 56,3 KB
// Fragmentgroesse.
func TestOverviewEmbedsEnergySnapshotOnce(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
		Entity: registry.EntityInfo{UniqueID: "inverter_pv", ObjectID: "pv_power", Component: "sensor", Name: "PV", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("1200"), false, time.Now())
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "band", Type: "energy_band", Span: "full", Visible: true},
			{ID: "ring", Type: "energy_ring", Span: "full", Visible: true},
			{ID: "flow", Type: "energy_flow", Span: "full", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	body := recorder.Body.String()
	if got := strings.Count(body, `id="energy-snapshot-initial"`); got != 1 {
		t.Fatalf("snapshot node appears %d times, want exactly 1: %s", got, body)
	}
	for _, gone := range []string{"energy-band-initial", "energy-ring-initial", "energy-flow-initial", "energy-board-initial", "energy-schema-initial", "energy-status-initial", "energy-day-initial"} {
		if strings.Contains(body, `id="`+gone+`"`) {
			t.Fatalf("per-card snapshot %q is still embedded: %s", gone, body)
		}
	}
	if !strings.Contains(body, `"pv":1200`) {
		t.Fatalf("shared snapshot does not carry the aggregate: %s", body)
	}
}

// TestOverviewEmbedsEnergySnapshotInsideLiveFragment sichert die Platzierung:
// der htmx-Swap ist outerHTML auf #overview-live, also wird nur getauscht,
// was *innerhalb* dieses Knotens steht. Stuende der Schnappschuss ausserhalb,
// fror er auf dem Stand des ersten Seitenaufrufs ein und alle sieben Karten
// zeichneten nach jedem Refresh veraltete Werte.
func TestOverviewEmbedsEnergySnapshotInsideLiveFragment(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "inverter", Name: "Inverter"},
		Entity: registry.EntityInfo{UniqueID: "inverter_pv", ObjectID: "pv_power", Component: "sensor", Name: "PV", StateTopic: "state/pv", UnitOfMeasurement: "W"},
	})
	reg.UpdateState("state/pv", []byte("1200"), false, time.Now())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	body := recorder.Body.String()

	open := strings.Index(body, `id="overview-live"`)
	node := strings.Index(body, `id="energy-snapshot-initial"`)
	if open < 0 || node < 0 {
		t.Fatalf("fragment or snapshot node missing: %s", body)
	}
	if node < open {
		t.Fatalf("snapshot node stands before #overview-live and would freeze on swap: %s", body)
	}
	// Das Fragment ist genau ein #overview-live-Knoten; alles nach dem
	// oeffnenden Tag und vor dem Ende der Antwort liegt darin.
	if !strings.HasSuffix(strings.TrimSpace(body), "</div>") {
		t.Fatalf("fragment does not end with the closing #overview-live div: %s", body)
	}
}

// renderPanel liefert das HTML eines einzelnen, lazy geladenen Panel-
// Fragments. Die bestehenden Tests bauen diesen Aufruf jeweils inline; fuer
// die Revisions-Tests, die dasselbe fuer sechs Panels brauchen, lohnt der
// gemeinsame Helfer.
func renderPanel(t *testing.T, panel string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler := Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel="+panel, nil))
	if recorder.Code != 200 {
		t.Fatalf("panel %q got status %d", panel, recorder.Code)
	}
	return recorder.Body.String()
}

func TestConfigPanelEmbedsSharedRevisionPartial(t *testing.T) {
	body := renderPanel(t, "config")
	if !strings.Contains(body, `x-data="revisionPanel(revisionConfig())"`) {
		t.Fatalf("config panel does not embed the shared revision partial: %s", body)
	}
	// Das alte, fest verdrahtete Revisions-Markup darf nicht mehr da sein -
	// sonst wuerden zwei Revisionsansichten nebeneinander stehen.
	if strings.Contains(body, `id="revision-name"`) {
		t.Fatalf("config panel still carries the old hard-wired revision markup: %s", body)
	}
}

func TestDeviceMapPanelEmbedsRevisionPartial(t *testing.T) {
	body := renderPanel(t, "devicemap")
	if !strings.Contains(body, `x-data="revisionPanel(revisionConfig())"`) {
		t.Fatalf("panel does not embed the shared revision partial: %s", body)
	}
}

func TestDeviceMapPanelRendersTheRolePanel(t *testing.T) {
	body := renderPanel(t, "devicemap")
	for _, want := range []string{`class="devicemap-panel"`, `data-panel-save`, `x-text="panelView.title"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("device map fragment misses %q", want)
		}
	}
}

func TestSettingsPanelEmbedsRevisionPartial(t *testing.T) {
	body := renderPanel(t, "settings")
	// Nur der allgemeine Einstellungsbereich nutzt die geteilte Revisions-
	// komponente. MQTT und Bridge bekommen bewusst keine; die Systemkonfiguration
	// (config.json) hat serverseitig keine /revisions-, /revisions/{name}- und
	// /restore-Routen und zeigt daher nur eine Nur-Lese-Liste (P1.8 der
	// Dashboard-Ideenliste).
	// Die Systemkonfiguration darf die geteilte Komponente nicht mehr an ihre
	// nicht existierenden Routen haengen (sonst kommt HTML statt JSON zurueck);
	// sie zeigt stattdessen die Nur-Lese-Liste mit revisionLabel().
	if got := strings.Count(body, `x-data="revisionPanel(revisionConfig())"`); got != 1 {
		t.Fatalf("settings panel embeds the revision partial %d times, want 1", got)
	}
	if !strings.Contains(body, "revisionLabel(name)") {
		t.Fatalf("system config panel does not render the read-only revision list: %s", body)
	}
}

func TestEnergyPanelEmbedsRevisionPartial(t *testing.T) {
	body := renderPanel(t, "energy")
	if !strings.Contains(body, `x-data="revisionPanel(revisionConfig())"`) {
		t.Fatalf("energy panel does not embed the shared revision partial: %s", body)
	}
}

// Die beiden Bauteile aus base.css tragen den ganzen Automations-Tab und die
// Device-Map. Sie muessen in base.css stehen (auf jeder Seite geladen), nicht
// in einem der nachgeladenen Panel-Stylesheets.
func TestBaseCSSDefinesSharedIconButtonAndFieldHelp(t *testing.T) {
	recorder := httptest.NewRecorder()
	Static().ServeHTTP(recorder, httptest.NewRequest("GET", "/static/css/base.css", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("base.css nicht ausgeliefert: Status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, selector := range []string{
		".icon-button",
		".icon-button.labeled",
		".icon-button.danger",
		".field-help-trigger",
		".field-help-text",
	} {
		if !strings.Contains(body, selector) {
			t.Fatalf("base.css definiert %q nicht", selector)
		}
	}
}

// Der alte Name ist im Bauteil aufgegangen - bleibt er irgendwo stehen, hat
// die Device-Map ploetzlich keine Button-Styles mehr.
func TestDevicemapUsesTheSharedIconButton(t *testing.T) {
	recorder := httptest.NewRecorder()
	Static().ServeHTTP(recorder, httptest.NewRequest("GET", "/static/css/manager.css", nil))
	if strings.Contains(recorder.Body.String(), "devicemap-edit-button") {
		t.Fatal("manager.css definiert devicemap-edit-button noch - gehoert in .icon-button (base.css)")
	}
}

// renderIndexForTest rendert die Startseite zusammen mit den Fragments aller
// Panels und liefert den kompletten HTML-Body zurueck.
func renderIndexForTest(t *testing.T) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler := Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()

	// Alle Panels hinzufuegen
	for _, panel := range []string{"config", "energy", "history", "devices", "diagnostics", "settings"} {
		panelRecorder := httptest.NewRecorder()
		handler.ServeHTTP(panelRecorder, httptest.NewRequest("GET", "/?fragment=panel&panel="+panel, nil))
		if panelRecorder.Code == 200 {
			body += panelRecorder.Body.String()
		}
	}
	return body
}

func TestApexChartsIsOnlyLoadedLazily(t *testing.T) {
	body := renderIndexForTest(t)
	// Eager geladen waere die Bibliothek auf jedem Seitenaufruf dabei -
	// auf einem Pi mit ARMv6 ist das genau die Art Ballast, die die
	// Invariante in AGENTS.md ausschliesst. Erlaubt sind genau zwei
	// lazy Listen: Verlauf und Statistik, jeweils als erster Eintrag.
	if strings.Count(body, "/static/js-deps/apexcharts.min.js") != 2 {
		t.Fatalf("apexcharts.min.js must appear exactly twice, once in the history and once in the stats panel's lazy script list")
	}
	if strings.Count(body, `data-panel-script="/static/js-deps/apexcharts.min.js,`) != 2 {
		t.Fatal("apexcharts.min.js must be the first lazy script of the history and the stats panel")
	}
	if !strings.Contains(body, `<script src="/static/js-deps/apexcharts.min.js"`) {
		return // korrekt: es darf kein eager <script>-Tag geben
	}
	t.Fatal("apexcharts.min.js is loaded eagerly instead of lazily")
}

func TestStatsPanelRenders(t *testing.T) {
	rec := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(rec, httptest.NewRequest("GET", "/?fragment=panel&panel=stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `x-data="statsPanel()"`) {
		t.Fatalf("Status %d: %s", rec.Code, rec.Body.String())
	}
	full := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(full, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(full.Body.String(), `id="tab-stats"`) || !strings.Contains(full.Body.String(), `energy-stats-model.js?v=4`) {
		t.Fatal("Tab oder Panel Statistik fehlt")
	}
}

// TestOverviewRendersDiagnosticsCardAllClear covers the collapsed state: a
// registry with nothing to report renders "Alles in Ordnung" instead of a
// 0/0/0 count row (design.md section 2, Stufe B).
func TestOverviewRendersDiagnosticsCardAllClear(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `class="diagnostics-summary-card`) {
		t.Fatal("page does not contain the diagnostics-summary-card button")
	}
	if !strings.Contains(body, "Alles in Ordnung") {
		t.Fatalf("empty registry should collapse to the all-clear state: %s", body)
	}
	if strings.Contains(body, "diagnostics-summary-counts") {
		t.Fatal("all-clear state must not also render the severity count row")
	}
}

// TestOverviewRendersDiagnosticsCardWithWarningsAndWorstDevice seeds a
// device whose entity has no state topic - deliberately triggering
// MissingTopicRule (critical) among others - and checks the card surfaces
// both the severity counts and that device as the worst one, matching
// exactly what /api/v1/diagnostics and /api/v1/diagnostics/health would
// report for the same registry.
func TestOverviewRendersDiagnosticsCardWithWarningsAndWorstDevice(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "broken", Name: "Kaputte Lampe"},
		Entity: registry.EntityInfo{UniqueID: "broken_state", ObjectID: "state"},
	})

	recorder := httptest.NewRecorder()
	Overview(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`class="diagnostics-summary-card`,
		`diagnostics-summary-dot-bad`,
		`diagnostics-summary-counts`,
		`diagnostics-summary-worst`,
		"Kaputte Lampe",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page does not contain %q: %s", marker, body)
		}
	}
	if strings.Contains(body, "Alles in Ordnung") {
		t.Fatal("a device with a critical warning must not collapse to the all-clear state")
	}
}

// TestOverviewDiagnosticsCardIsTheWholeTapTarget carries Stufe A (whole-card
// button, same pattern as entity-value-card) forward into Stufe B instead of
// losing it during the rewrite.
func TestOverviewDiagnosticsCardIsTheWholeTapTarget(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `<button type="button" class="diagnostics-summary-card`) {
		t.Fatalf("diagnostics card must be a single <button> root element: %s", body)
	}
	if !strings.Contains(body, `x-on:click="setActivePanel('diagnostics-panel')"`) {
		t.Fatal("diagnostics card button must open the Diagnose tab")
	}
}

// Das Nachziehen per SSE (overview-values.js) findet eine Karte ueber
// data-entity-id und formatiert ueber data-device-class/data-unit. Fehlt
// eines der drei, bleibt die Karte beim Live-Update stehen - lautlos.
func TestEntityCardsCarryPatchAttributes(t *testing.T) {
	entity := registry.EntityView{
		UniqueID:          "sensor.leistung",
		Name:              "Leistung",
		Component:         "sensor",
		DeviceClass:       "power",
		UnitOfMeasurement: "W",
		Value:             "42",
		HasValue:          true,
	}

	var value bytes.Buffer
	if err := overviewTmpl.ExecuteTemplate(&value, "entity-value-card", entity); err != nil {
		t.Fatalf("entity-value-card: %v", err)
	}
	for _, want := range []string{`data-entity-id="sensor.leistung"`, `data-device-class="power"`, `data-unit="W"`} {
		if !strings.Contains(value.String(), want) {
			t.Errorf("entity-value-card ohne %s:\n%s", want, value.String())
		}
	}

	var group bytes.Buffer
	if err := overviewTmpl.ExecuteTemplate(&group, "entity-group-card", entityGroupView{Title: "Test", Entities: []registry.EntityView{entity}}); err != nil {
		t.Fatalf("entity-group-card: %v", err)
	}
	for _, want := range []string{`data-entity-id="sensor.leistung"`, `data-device-class="power"`, `data-unit="W"`} {
		if !strings.Contains(group.String(), want) {
			t.Errorf("entity-group-chip ohne %s:\n%s", want, group.String())
		}
	}
}

// Die nicht schaltbare Variante ist der Regelfall (jeder Sensor) - genau sie
// hatte bis 2026-08 kein data-entity-id.
func TestNonCommandableGroupChipIsAddressable(t *testing.T) {
	entity := registry.EntityView{UniqueID: "sensor.temp", Name: "Temperatur", Component: "sensor", Commandable: false}
	var group bytes.Buffer
	if err := overviewTmpl.ExecuteTemplate(&group, "entity-group-card", entityGroupView{Title: "Test", Entities: []registry.EntityView{entity}}); err != nil {
		t.Fatalf("entity-group-card: %v", err)
	}
	if !strings.Contains(group.String(), `<article class="entity-group-chip" data-entity-id="sensor.temp"`) {
		t.Errorf("nicht schaltbarer Chip ohne data-entity-id:\n%s", group.String())
	}
}

// Ohne diese vier Attribute kann device-tile-values.js eine Kachelzeile
// weder finden noch ihren Wert so formatieren, wie das Template es tut -
// und ohne data-structure weiss dashboard.js nicht, wann der Tausch faellig
// ist.
func TestDeviceTilesCarryPatchAttributes(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", DeviceClass: "power", UnitOfMeasurement: "W", StateTopic: "node/power"},
	})
	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()

	for _, marker := range []string{
		`data-entity-id="node_power"`,
		`data-component="sensor"`,
		`data-device-class="power"`,
		`data-unit="W"`,
		`class="device-tile-entity-value`,
		`data-structure="`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Steuerungs-Fragment enthaelt %q nicht: %s", marker, body)
		}
	}

	want := DeviceStructureFingerprint(reg.Snapshot()[0])
	if !strings.Contains(body, `data-structure="`+want+`"`) {
		t.Fatalf("data-structure passt nicht zu DeviceStructureFingerprint (%q): %s", want, body)
	}
}

// Dieselbe Kachel steht als "device"-Karte im Raster; auch dort braucht der
// Vergleich seinen Bezugswert.
func TestOverviewFragmentCarriesStructure(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	want := DeviceStructureFingerprint(reg.Snapshot()[0])
	if !strings.Contains(recorder.Body.String(), `data-structure="`+want+`"`) {
		t.Fatalf("#overview-live ohne passendes data-structure (%q): %s", want, recorder.Body.String())
	}
}

func TestDeviceAvailabilityRollup(t *testing.T) {
	cases := []struct {
		name string
		in   []registry.EntityView
		want string
	}{
		{"no availability anywhere", []registry.EntityView{{}, {}}, "unknown"},
		{"one available", []registry.EntityView{{HasAvailability: true, Available: false}, {HasAvailability: true, Available: true}}, "online"},
		{"all known, none available", []registry.EntityView{{HasAvailability: true, Available: false}}, "offline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deviceAvailability(tc.in); got != tc.want {
				t.Fatalf("deviceAvailability = %q, want %q", got, tc.want)
			}
		})
	}
}

// "is this device healthy?" - offline entities outrank a stale (replayed)
// value, a stale value outranks a clean roll-up, and a device that reports no
// availability at all stays neutral. Der Text kommt aus i18n (Template und
// Browser), hier nur Klasse, Rail und Zahl.
func TestDeviceTileStatus(t *testing.T) {
	cases := []struct {
		name string
		in   []registry.EntityView
		want DeviceTileStatus
	}{
		{"no availability anywhere", []registry.EntityView{{}, {}}, DeviceTileStatus{Class: "unknown"}},
		{"all available", []registry.EntityView{{HasAvailability: true, Available: true}, {HasAvailability: true, Available: true}}, DeviceTileStatus{Class: "ok"}},
		{"two offline", []registry.EntityView{{HasAvailability: true, Available: false}, {HasAvailability: true, Available: false}, {HasAvailability: true, Available: true}}, DeviceTileStatus{Class: "bad", Rail: "is-offline", Offline: 2}},
		{"stale value, all available", []registry.EntityView{{HasAvailability: true, Available: true, Stale: true}}, DeviceTileStatus{Class: "warn", Rail: "is-degraded"}},
		{"offline outranks stale", []registry.EntityView{{HasAvailability: true, Available: false}, {HasAvailability: true, Available: true, Stale: true}}, DeviceTileStatus{Class: "bad", Rail: "is-offline", Offline: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deviceTileStatus(tc.in); got != tc.want {
				t.Fatalf("deviceTileStatus = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDeviceTileHeaderNachzug covers Spec 2026-08-23 Abschnitt 3.2/3.3/3.4:
// the flat accent stripe becomes a header status dot, the raw device ID drops
// out of the subtitle (it is in the detail dialog), and the "Entitäten N
// Steuerungen M" baseline row becomes two count chips.
func TestDeviceTileHeaderNachzug(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node-1", Name: "Node One", Model: "Widget 9000"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "device:node-1", Type: "device", Ref: "node-1", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()

	if !strings.Contains(body, `class="device-tile-dot device-tile-dot-`) {
		t.Fatalf("device tile is missing the header status dot: %s", body)
	}
	if strings.Contains(body, `class="device-tile-status-label"`) {
		t.Fatalf("device tile still renders the old baseline count row instead of chips: %s", body)
	}
	if !strings.Contains(body, `class="device-tile-chip"`) {
		t.Fatalf("device tile is missing the count chips: %s", body)
	}
	if strings.Contains(body, `<small>node-1`) {
		t.Fatalf("device tile subtitle still leads with the raw device ID: %s", body)
	}
	if !strings.Contains(body, `<small>Widget 9000</small>`) {
		t.Fatalf("device tile subtitle dropped the model: %s", body)
	}
}

// TestDeviceTileAccentRailReflectsState covers the follow-up to Abschnitt 3.2:
// the theme-coloured side rail stays (styled like .compact-card) but recolours
// with the device roll-up - an offline entity marks the whole tile is-offline.
func TestDeviceTileAccentRailReflectsState(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node-1", Name: "Node One"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "state/p", AvailabilityTopic: "status/n", PayloadAvailable: "online"},
	})
	reg.UpdateAvailability("status/n", []byte("offline"), false, time.Now().UTC())
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "device:node-1", Type: "device", Ref: "node-1", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `class="device-tile is-offline"`) {
		t.Fatalf("offline device tile must carry the is-offline rail modifier: %s", body)
	}
}

// TestEntityValueCardAccentRailReflectsState: same follow-up for the value
// card - the accent rail stays and an unavailable entity marks it is-offline.
func TestEntityValueCardAccentRailReflectsState(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node-1", Name: "Node One"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "state/p", AvailabilityTopic: "status/n", PayloadAvailable: "online"},
	})
	reg.UpdateAvailability("status/n", []byte("offline"), false, time.Now().UTC())
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "entity-value:node_power", Type: "entity_value", Ref: "node_power", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `entity-value-card is-offline`) {
		t.Fatalf("offline entity value card must carry the is-offline rail modifier: %s", body)
	}
}

func TestPriorityEntitiesPrefersMeasurementsWithValueCappedAtThree(t *testing.T) {
	dev := registry.DeviceView{Entities: []registry.EntityView{
		{UniqueID: "hidden", Name: "Hidden", HasValue: true, DefaultHidden: true},
		{UniqueID: "relay", Name: "Relay", Component: "switch", Commandable: true, HasValue: true, Value: "on"},
		{UniqueID: "power", Name: "Power", Component: "sensor", HasValue: true, Value: "42", UnitOfMeasurement: "W"},
		{UniqueID: "temp", Name: "Temp", Component: "sensor", HasValue: true, Value: "21", UnitOfMeasurement: "C"},
		{UniqueID: "novalue", Name: "NoValue", Component: "sensor", HasValue: false},
		{UniqueID: "volt", Name: "Volt", Component: "sensor", HasValue: true, Value: "230", UnitOfMeasurement: "V"},
	}}
	got := priorityEntities(dev)
	if len(got) != 3 {
		t.Fatalf("want 3 entities, got %d (%v)", len(got), got)
	}
	if got[0].UniqueID != "power" || got[1].UniqueID != "temp" || got[2].UniqueID != "volt" {
		t.Fatalf("measurement-with-value entities in registry order expected, got %s,%s,%s", got[0].UniqueID, got[1].UniqueID, got[2].UniqueID)
	}
}

func TestPriorityEntitiesFallsBackToControlsWithValue(t *testing.T) {
	dev := registry.DeviceView{Entities: []registry.EntityView{
		{UniqueID: "relay", Name: "Relay", Component: "switch", Commandable: true, HasValue: true, Value: "on"},
		{UniqueID: "cfg", Name: "Cfg", Component: "number", Commandable: true, HasValue: true, Value: "5"},
	}}
	got := priorityEntities(dev)
	if len(got) != 2 || got[0].UniqueID != "relay" || got[1].UniqueID != "cfg" {
		t.Fatalf("fallback should surface with-value controls in order, got %v", got)
	}
}

func TestCompactCardTemplateRendersStatusAndPriorityRows(t *testing.T) {
	dev := registry.DeviceView{ID: "node", Name: "Node", Model: "X1", Entities: []registry.EntityView{
		{UniqueID: "node_power", Name: "Node Leistung", Component: "sensor", HasValue: true, Value: "42", UnitOfMeasurement: "W", HasAvailability: true, Available: true},
	}}
	var buf bytes.Buffer
	if err := overviewTmpl.ExecuteTemplate(&buf, "compact-card", compactCardAuto(dev)); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, marker := range []string{
		`class="compact-card is-online"`,
		`data-device-id="node"`,
		`x-on:click="selectDevice($el.dataset.deviceId)"`,
		`<span class="compact-card-row-label">Leistung</span>`,
		`class="compact-card-row-value">42 W</span>`,
		`compact-card-status-online`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("compact-card missing %q:\n%s", marker, body)
		}
	}
	for _, forbidden := range []string{`entity-command-button`, `type="range"`, `sendCommand`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("compact-card must be read-only, found %q", forbidden)
		}
	}
}

// Die Zeilen der Kompaktkarte tragen die Attribute, an denen der Push und
// device-structure.js sie wiedererkennen: die fertige Zeile und die Vorlage
// <template data-compact-row>, aus der neue Zeilen gebaut werden.
func TestCompactCardRowsCarryPatchAttributes(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", DeviceClass: "power", UnitOfMeasurement: "W", StateTopic: "node/power"},
	})
	reg.UpdateTopicWithQoS("node/power", []byte("42"), false, 0, time.Now().UTC())

	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=compact", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()

	for _, marker := range []string{
		`class="compact-card-row"`,
		`data-entity-id="node_power"`,
		`data-device-class="power"`,
		`data-unit="W"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("Kompakt-Fragment enthaelt %q nicht: %s", marker, body)
		}
	}

	start := strings.Index(body, "<template data-compact-row>")
	if start < 0 {
		t.Fatalf("Kompakt-Fragment hat keine Zeilenvorlage: %s", body)
	}
	end := strings.Index(body[start:], "</template>")
	if end < 0 {
		t.Fatalf("Zeilenvorlage ist nicht geschlossen: %s", body)
	}
	tmpl := body[start : start+end]
	for _, marker := range []string{
		`class="compact-card-row"`,
		`data-entity-id="`,
		`data-device-class="`,
		`data-unit="`,
		`class="compact-card-row-label"`,
		`class="compact-card-row-value"`,
	} {
		if !strings.Contains(tmpl, marker) {
			t.Fatalf("Zeilenvorlage enthaelt %q nicht: %s", marker, tmpl)
		}
	}
}

// layoutWithPages erzeugt ein Testlayout mit den gegebenen Seitennamen.
func layoutWithPages(names ...string) settings.Layout {
	pages := make([]settings.Page, len(names))
	for i, name := range names {
		pages[i] = settings.Page{
			ID:    "page_" + name,
			Name:  name,
			Order: i,
			Groups: []settings.Group{{
				ID:   "main",
				Name: "Main",
				Items: []settings.Item{{
					ID:      "test_item_" + name,
					Type:    "device",
					Ref:     "test_device",
					Span:    "1",
					Visible: true,
				}},
			}},
		}
	}
	layout := settings.Layout{Pages: pages}
	layout.Version = 1
	return layout
}

// renderOverview rendert das Overview-Fragment mit dem gegebenen Layout und
// der aktiven Seite.
func renderOverview(t *testing.T, layout settings.Layout, activePage string) string {
	t.Helper()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "test_device", Name: "Test Device"},
		Entity: registry.EntityInfo{UniqueID: "test_entity", ObjectID: "test", Component: "sensor", Name: "Test"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(layout); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?fragment=overview-live&page="+activePage, nil)
	// Mit Manager, damit der Editieren-Knopf (Werkzeugleiste wie Leerzustand,
	// beide {{if .Manager}}) im gerenderten HTML auftaucht.
	Overview(reg, config.NewManager(t.TempDir()), store).ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

// renderBase rendert die volle Base-Seite mit dem gegebenen Layout.
func renderBase(t *testing.T, layout settings.Layout) string {
	t.Helper()
	reg := registry.New()
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(layout); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, config.NewManager(t.TempDir()), store).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

func TestUebersichtRendertNurDieAktiveSeite(t *testing.T) {
	// Seiten sind jetzt Navigationsziele. Die Uebersicht haengt sie nicht mehr
	// aneinander, sonst zeigte der Bearbeitungsmodus ein Raster, das es so
	// nirgends gibt.
	html := renderOverview(t, layoutWithPages("Zuhause", "Werkstatt"), "Werkstatt")
	if strings.Contains(html, "data-layout-page=\"page_Zuhause\"") {
		t.Fatal("die inaktive Seite darf nicht mitgerendert werden")
	}
	if !strings.Contains(html, "data-layout-page=\"page_Werkstatt\"") {
		t.Fatal("die aktive Seite fehlt")
	}
}

func TestUebersichtStartetDieZeilenfuellung(t *testing.T) {
	// Ohne den x-init laeuft applyRowFill() nie an und die Ansicht behaelt die
	// Luecken rechts, die nur der Editor zeigen soll.
	html := renderOverview(t, layoutWithPages("Zuhause"), "Zuhause")
	if !strings.Contains(html, `x-init="watchRowFill()"`) {
		t.Fatal("die Zeilenfuellung wird nie gestartet")
	}
}

func TestLayoutSeiteTraegtKeineFesteUeberschrift(t *testing.T) {
	// Die Seite hat einen Namen, und der steht im Navigations-Tab. Eine feste
	// Ueberschrift "Energie" darueber widerspricht jeder anders benannten Seite.
	html := renderOverview(t, layoutWithPages("Werkstatt"), "Werkstatt")
	if strings.Contains(html, "<h2>Energie</h2>") {
		t.Fatal("die feste Ueberschrift steht noch ueber dem Raster")
	}
}

func TestEditierenKnopfHatEinIcon(t *testing.T) {
	html := renderOverview(t, layoutWithPages("Zuhause"), "Zuhause")
	i := strings.Index(html, "data-mode-edit")
	if i < 0 {
		t.Fatal("kein Editieren-Knopf gerendert")
	}
	if !strings.Contains(html[i:min(len(html), i+400)], "<svg") {
		t.Fatal("dem Editieren-Knopf fehlt das Icon")
	}
}

func TestEditorLaedtKeinGridstack(t *testing.T) {
	// Der Bearbeitungsmodus arbeitet auf dem echten CSS-Raster. Wuerde
	// Gridstack wieder mitgeladen, kaeme ein zweites Layoutmodell zurueck und
	// der Moduswechsel veraenderte die Ansicht.
	html := renderBase(t, layoutWithPages("Zuhause"))
	if strings.Contains(html, "gridstack") {
		t.Fatal("gridstack steht wieder in den Editor-Assets")
	}
}

func TestUebersichtRendertDieZielbreitenBuehne(t *testing.T) {
	// applyTargetWidth() schreibt --target-w/--target-zoom auf diesen Knoten.
	// Fehlt er, sind Handy/Tablet/Monitor wirkungslos.
	html := renderOverview(t, layoutWithPages("Zuhause"), "Zuhause")
	if !strings.Contains(html, `class="layout-canvas-stage"`) {
		t.Fatal("die Zielbreiten-Buehne fehlt")
	}
}

func TestUebersichtsPanelHatEinLabelOhneFestenTab(t *testing.T) {
	// base.html rendert #tab-overview nur, solange es keine Layout-Seiten
	// gibt - ein aria-labelledby darauf zeigte sonst ins Leere.
	html := renderBase(t, layoutWithPages("Zuhause"))
	if strings.Contains(html, `aria-labelledby="tab-overview"`) {
		t.Fatal("das Panel verweist auf einen Tab, den es nicht immer gibt")
	}
}

func TestJedeLayoutSeiteWirdEinTab(t *testing.T) {
	html := renderBase(t, layoutWithPages("Zuhause", "Werkstatt"))
	for _, name := range []string{"Zuhause", "Werkstatt"} {
		if !strings.Contains(html, ">"+name+"</button>") {
			t.Fatalf("Tab fuer Seite %q fehlt", name)
		}
	}
	if strings.Contains(html, `id="tab-layout"`) {
		t.Fatal("der eigene Layout-Tab muss entfallen")
	}
	if !strings.Contains(html, `class="tab-divider"`) {
		t.Fatal("der Trennstrich zwischen Dashboard-Seiten und festen Tabs fehlt")
	}
	// Neue, noch nicht gespeicherte Editor-Seiten rendern zur Laufzeit aus
	// dashboardShell.extraPages.
	if !strings.Contains(html, `x-for="page in extraPages"`) {
		t.Fatal("die Laufzeit-Vorlage fuer neue Seiten-Tabs fehlt")
	}
}

func TestOhneSeiteBleibtDerWegInDenEditorOffen(t *testing.T) {
	// Ein bewusst leer gespeichertes Layout fuehrt in den Leerzustand mit
	// Editieren-Knopf (Spec 4.3). Der Knopf muss im overviewShell()-Scope
	// haengen, sonst greift x-on:click="enterEdit()" ins Leere.
	html := renderOverview(t, layoutWithPages(), "")
	if !strings.Contains(html, "data-mode-edit") {
		t.Fatal("der leere Zustand muss einen Editieren-Knopf anbieten")
	}
	if !strings.Contains(html, `x-on:click="enterEdit()"`) {
		t.Fatal("der Editieren-Knopf des Leerzustands muss enterEdit() rufen")
	}
	if !strings.Contains(html, "panel-empty") {
		t.Fatal("der Leerzustand-Text fehlt")
	}
}

func TestEditorFragmentEnthaeltWerkzeugToolboxUndRevisionen(t *testing.T) {
	html := renderFragment(t, "layout-editor")
	for _, marker := range []string{"layout-toolbox", "layout-options-modal", "revision-panel", "data-addpage"} {
		if !strings.Contains(html, marker) {
			t.Fatalf("Fragment fehlt %q", marker)
		}
	}
}

func TestEditorFragmentIstManagernVorbehalten(t *testing.T) {
	if status := fragmentStatusAsGuest(t, "layout-editor"); status != http.StatusForbidden {
		t.Fatalf("Gast bekam %d statt 403", status)
	}
}

func TestBearbeitungsmodusHaengtAnDerRolleEditLayout(t *testing.T) {
	// Steht ein Nutzer im Context, entscheidet die Rolle edit_layout - sonst
	// (Tests, Instanz ohne auth.Manager) bleibt der Modus offen wie bisher.
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(layoutWithPages("Zuhause")); err != nil {
		t.Fatal(err)
	}
	handler := Overview(registry.New(), config.NewManager(t.TempDir()), store)

	withUser := func(roles ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/?fragment=overview-live&page=Zuhause", nil)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{Username: "u", Roles: roles}))
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := withUser(auth.RoleViewOverview, auth.RoleEditLayout); !strings.Contains(rec.Body.String(), "data-mode-edit") {
		t.Fatal("mit edit_layout muss der Editieren-Knopf erscheinen")
	}
	if rec := withUser(); strings.Contains(rec.Body.String(), "data-mode-edit") {
		t.Fatal("ohne edit_layout darf der Editieren-Knopf nicht erscheinen")
	}

	fragmentRec := httptest.NewRecorder()
	fragmentReq := httptest.NewRequest("GET", "/?fragment=panel&panel=layout-editor", nil)
	fragmentReq = fragmentReq.WithContext(auth.WithUser(fragmentReq.Context(), auth.User{Username: "u", Roles: []string{}}))
	handler.ServeHTTP(fragmentRec, fragmentReq)
	if fragmentRec.Code != http.StatusForbidden {
		t.Fatalf("ohne edit_layout muss das Fragment 403 liefern, war %d", fragmentRec.Code)
	}
}

func TestEditorFragmentModalbodyIstLeereHuelle(t *testing.T) {
	html := renderFragment(t, "layout-editor")
	if !strings.Contains(html, `data-modal-body`) {
		t.Fatal("die Optionen-Huelle data-modal-body fehlt - optionsSheetHTML() fuellt sie zur Laufzeit")
	}
	if strings.Contains(html, "mini-toggle") {
		t.Fatal("mini-toggle ist eine Prototyp-Silhouette (Spec 3.2) und darf nicht im Fragment stehen")
	}
	if strings.Contains(html, ">1 Spalte</option>") || strings.Contains(html, ">Ladestand</option>") {
		t.Fatal("das statische Options-Mockup muss raus - der Body wird zur Laufzeit gefuellt")
	}
}

func renderFragment(t *testing.T, panel string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(rec, httptest.NewRequest("GET", "/?fragment=panel&panel="+panel, nil))
	if rec.Code != 200 {
		t.Fatalf("Fragment %q: Status %d", panel, rec.Code)
	}
	return rec.Body.String()
}

func fragmentStatusAsGuest(t *testing.T, panel string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).
		ServeHTTP(rec, httptest.NewRequest("GET", "/?fragment=panel&panel="+panel, nil))
	return rec.Code
}

// TestOverviewRendersStoredEntityCardAsValueCard belegt das Migrationsversprechen
// aus Abschnitt 4 der Spec vom 2026-08-23: ein bereits gespeichertes Layout mit
// dem abgeschafften Kartentyp "entity" laedt weiter, aber die Kachel steht danach
// als Wert-Karte im Raster - derselbe Ref, kein Verlust, und ohne die
// Diagnose-Metazeile (Quelle/Freshness/Zuletzt gesehen), die auf dem Dashboard
// nie hingehoerte.
func TestOverviewRendersStoredEntityCardAsValueCard(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_soc", ObjectID: "soc", Component: "sensor", Name: "Batterie SOC", UnitOfMeasurement: "%"},
	})
	dir := t.TempDir()
	stored := `{"version":3,"pages":[{"id":"overview","name":"Overview","order":0,"groups":[{"id":"main","name":"Main","items":[
		{"id":"entity:node_soc","type":"entity","ref":"node_soc","span":"1","visible":true}
	]}]}]}`
	if err := os.WriteFile(dir+"/layout.json", []byte(stored), 0600); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, settings.NewStore(dir)).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, `data-layout-item-kind="entity"`) {
		t.Fatalf("der abgeschaffte Kartentyp steht noch im Raster: %s", body)
	}
	if !strings.Contains(body, `<article class="entity-value-card" data-entity-id="node_soc"`) {
		t.Fatalf("die gespeicherte entity-Karte rendert nicht als Wert-Karte: %s", body)
	}
	if strings.Contains(body, "entity-list-meta") {
		t.Fatalf("die Diagnose-Metazeile steht weiterhin auf der Uebersicht: %s", body)
	}
}

// Die kompakte Kachel bekommt ihre kleinere Mindesthoehe aus dem Katalog
// gestellt - nicht aus einer zweiten Zahl hier im Renderer.
func TestCardStyleUsesCompactMinHeight(t *testing.T) {
	compact := string(cardStyle(settings.Item{Type: "device", Display: "compact"}))
	if !strings.Contains(compact, "--card-min-height: 7rem;") {
		t.Errorf("cardStyle(kompakt) = %q, want 7rem", compact)
	}
	detail := string(cardStyle(settings.Item{Type: "device", Display: "detail"}))
	if !strings.Contains(detail, "--card-min-height: 10rem;") {
		t.Errorf("cardStyle(detail) = %q, want 10rem", detail)
	}
}

// Baut Registry, Store und Handler wie TestOverviewUsesSavedLayoutItems und
// gibt den gerenderten Fragment-Body zurueck - ein Ort fuer die Tests, die
// sich nur im gespeicherten Layout unterscheiden.
func renderOverviewWithLayout(t *testing.T, layout settings.Layout) string {
	t.Helper()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(layout); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

// Ein device-Item mit display "compact" rendert die compact-card des
// Geraete-Tabs statt der device-tile - dasselbe Template, damit es die
// Kachel nicht zweimal gibt.
func TestOverviewRendersCompactDeviceCard(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "Dashboard", Items: []settings.Item{
			{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact"},
		}}},
	}}}
	body := renderOverviewWithLayout(t, layout)
	if !strings.Contains(body, `class="compact-card`) {
		t.Error("die kompakte Kachel wurde nicht gerendert")
	}
	if strings.Contains(body, `class="device-tile"`) {
		t.Error("die Detailkachel wurde zusaetzlich gerendert")
	}
	if !strings.Contains(body, `data-display="compact"`) {
		t.Error("data-display fehlt an der Rasterzelle")
	}
}

func TestOverviewRendersDetailDeviceTileByDefault(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "Dashboard", Items: []settings.Item{
			{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "detail"},
		}}},
	}}}
	body := renderOverviewWithLayout(t, layout)
	if !strings.Contains(body, `class="device-tile"`) {
		t.Error("die Detailkachel fehlt")
	}
	if strings.Contains(body, `class="compact-card `) {
		t.Error("die kompakte Kachel wurde faelschlich gerendert")
	}
}

func TestOverviewRendersBatteryColumnCard(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "overview", Name: "Overview", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: []settings.Item{
			{ID: "bat", Type: "battery_status", Display: "column", Span: "1", Visible: true},
		}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=overview-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `x-data="batteryColumnCard()"`) {
		t.Fatal("Uebersicht rendert die Saeulen-Karte nicht")
	}

	// Check for scripts in the full page
	fullRecorder := httptest.NewRecorder()
	Overview(registry.New(), nil, store).ServeHTTP(fullRecorder, httptest.NewRequest("GET", "/", nil))
	if fullRecorder.Code != 200 {
		t.Fatalf("got status %d", fullRecorder.Code)
	}
	fullBody := fullRecorder.Body.String()
	if !strings.Contains(fullBody, "/static/js/battery-status.js") {
		t.Fatal("battery-status.js wird nicht geladen")
	}
	if !strings.Contains(fullBody, "/static/js/battery-card-core.js") {
		t.Fatal("battery-card-core.js wird nicht geladen")
	}
}

func TestOverviewRendersBatteryTrajectoryCard(t *testing.T) {
	body := renderOverviewWithItems(t, []settings.Item{
		{ID: "bat", Type: "battery_status", Display: "trajectory", Span: "2", Visible: true, BatteryWindow: "12", BatteryProjectionWindow: "3"},
	})
	if !strings.Contains(body, `x-data="batteryTrajectoryCard()"`) {
		t.Fatal("Uebersicht rendert die Trajektorie-Karte nicht")
	}
	if strings.Contains(body, `x-data="batteryColumnCard()"`) {
		t.Fatal("Trajektorie-Item rendert zusaetzlich die Saeule")
	}
	// Das Zeitfenster reist als data-Attribut zur Karte, die es im Browser liest.
	if !strings.Contains(body, `data-battery-window="12"`) || !strings.Contains(body, `data-battery-projection-window="3"`) {
		t.Fatalf("Zeitfenster-Attribute fehlen am Layout-Item:\n%s", body)
	}
}

func TestOverviewRendersEnergyStatsOptions(t *testing.T) {
	body := renderOverviewWithItems(t, []settings.Item{
		{ID: "stats", Type: "energy_stats", Span: "1", Visible: true, StatsPeriod: "month", StatsKPIs: []string{"autarky", "pv"}, StatsChart: "on"},
	})
	// Die Optionen reisen als data-Attribute zur Kachel, die sie im Browser liest.
	for _, want := range []string{`data-stats-period="month"`, `data-stats-kpis="autarky,pv"`, `data-stats-chart="on"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s fehlt:\n%s", want, body)
		}
	}
	// Das optionale Diagramm braucht ApexCharts vor dem Kachel-Skript.
	layout := settings.Layout{Version: 3, Pages: []settings.Page{{ID: "p", Groups: []settings.Group{{ID: "g", Items: []settings.Item{{ID: "stats", Type: "energy_stats", Visible: true}}}}}}}
	scripts := requiredEnergyCardScripts(layout, "")
	if len(scripts) != 3 || scripts[0] != "/static/js-deps/apexcharts.min.js" {
		t.Fatalf("energy_stats-Skripte = %v, want ApexCharts zuerst", scripts)
	}
}

// compactCardForItem ist die eine Stelle, an der sich entscheidet, welche
// Zeilen eine kompakte Kachel zeigt: die fest gewaehlten (in genau ihrer
// Reihenfolge), sonst die Automatik. Ein Ref ohne Treffer faellt still weg -
// dieselbe Fehlerbehandlung wie beim einzelnen entity_value-Ref.
func TestCompactCardForItemHonoursEntityRefsOrderAndFallback(t *testing.T) {
	dev := registry.DeviceView{ID: "node", Name: "Node", Entities: []registry.EntityView{
		{UniqueID: "node_power", Name: "Leistung", Component: "sensor", HasValue: true, Value: "42", UnitOfMeasurement: "W"},
		{UniqueID: "node_temp", Name: "Temperatur", Component: "sensor", HasValue: true, Value: "21", UnitOfMeasurement: "C"},
		{UniqueID: "node_relay", Name: "Relais", Component: "switch", HasValue: true, Value: "on"},
	}}

	picked := compactCardForItem(dev, settings.Item{Type: "device", Display: "compact",
		Ref: "node", EntityRefs: []string{"node_temp", "missing", "node_relay"}})
	if len(picked.Rows) != 2 || picked.Rows[0].UniqueID != "node_temp" || picked.Rows[1].UniqueID != "node_relay" {
		t.Fatalf("Rows = %v, want [node_temp node_relay] in dieser Reihenfolge", picked.Rows)
	}
	if picked.Device.ID != "node" {
		t.Errorf("Device.ID = %q, want node", picked.Device.ID)
	}

	auto := compactCardForItem(dev, settings.Item{Type: "device", Display: "compact", Ref: "node"})
	want := priorityEntities(dev)
	if len(auto.Rows) != len(want) {
		t.Fatalf("ohne entity_refs = %d Rows, want %d (priorityEntities)", len(auto.Rows), len(want))
	}
}

// Das compact-card-Template rendert jetzt aus compactCardView, nicht mehr aus
// einer nackten DeviceView - die feste Auswahl schlaegt dabei auf die
// sichtbaren Zeilen durch.
func TestCompactCardTemplateRendersConfiguredRows(t *testing.T) {
	dev := registry.DeviceView{ID: "node", Name: "Node", Entities: []registry.EntityView{
		{UniqueID: "node_power", Name: "Node Leistung", Component: "sensor", HasValue: true, Value: "42", UnitOfMeasurement: "W"},
		{UniqueID: "node_temp", Name: "Node Temperatur", Component: "sensor", HasValue: true, Value: "21", UnitOfMeasurement: "C"},
	}}
	view := compactCardForItem(dev, settings.Item{Type: "device", Display: "compact", Ref: "node",
		EntityRefs: []string{"node_temp"}})
	var buf bytes.Buffer
	if err := overviewTmpl.ExecuteTemplate(&buf, "compact-card", view); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, `data-entity-id="node_temp"`) {
		t.Errorf("die gewaehlte Zeile fehlt:\n%s", body)
	}
	if strings.Contains(body, `data-entity-id="node_power"`) {
		t.Errorf("eine nicht gewaehlte Zeile wurde gerendert:\n%s", body)
	}
}

// Die Uebersicht markiert jede kompakte Geraetekachel danach, ob sie eine
// feste Zeilenauswahl hat - daran entscheidet dashboard.js, welcher
// Fingerabdruck die Kachel deckt.
func TestOverviewMarksConfiguredCompactCards(t *testing.T) {
	configured := settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "Dashboard", Items: []settings.Item{
			{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact",
				EntityRefs: []string{"node_power"}},
		}}},
	}}}
	body := renderOverviewWithLayout(t, configured)
	if !strings.Contains(body, `data-compact-configured="true"`) {
		t.Errorf("die konfigurierte Kachel traegt data-compact-configured=\"true\" nicht:\n%s", body)
	}

	auto := settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "Start", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "Dashboard", Items: []settings.Item{
			{ID: "device:node", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact"},
		}}},
	}}}
	if strings.Contains(renderOverviewWithLayout(t, auto), `data-compact-configured="true"`) {
		t.Error("eine Kachel ohne feste Auswahl gilt faelschlich als konfiguriert")
	}
}

func TestOverviewHidesDeselectedTabsAndSubpages(t *testing.T) {
	reg := registry.New()
	installed := map[string]bool{"automation": false, "tailscale": false, "tuya": true}
	handler := OverviewWithDeviceFilterAndEngine(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, diagnostics.NewEngine(reg, nil), installed)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()

	// Settings panel is lazy-loaded, so fetch it separately
	settingsRec := httptest.NewRecorder()
	OverviewWithDeviceFilterAndEngine(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, diagnostics.NewEngine(reg, nil), installed).
		ServeHTTP(settingsRec, httptest.NewRequest(http.MethodGet, "/?fragment=panel&panel=settings", nil))
	body += settingsRec.Body.String()

	for _, gone := range []string{`id="tab-automations"`, `aria-controls="settings-tailscale"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("erwartet, dass %q fehlt, aber es steht im Body", gone)
		}
	}
	if !strings.Contains(body, `aria-controls="settings-tiny-tuya"`) {
		t.Fatal("tuya ist installiert - die TinyTuya-Unterseite sollte da sein")
	}
}

func TestOverviewShowsEverythingWithoutInstalledServices(t *testing.T) {
	reg := registry.New()
	handler := OverviewWithDeviceFilterAndEngine(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, diagnostics.NewEngine(reg, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()

	// Settings panel is lazy-loaded, so fetch it separately
	settingsRec := httptest.NewRecorder()
	OverviewWithDeviceFilterAndEngine(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, diagnostics.NewEngine(reg, nil), nil).
		ServeHTTP(settingsRec, httptest.NewRequest(http.MethodGet, "/?fragment=panel&panel=settings", nil))
	body += settingsRec.Body.String()

	for _, want := range []string{`id="tab-automations"`, `aria-controls="settings-tailscale"`, `aria-controls="settings-tiny-tuya"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("ohne installed_services sollte %q vorhanden sein", want)
		}
	}
}

// Anders als die tailscale/tuya-Unterseiten (nur ueber den Tab-Knopf im
// bereits geladenen settings-Fragment erreichbar) haengt der
// Automations-Inhalt an seiner eigenen URL (?fragment=panel&panel=automations).
// Das Gating am Tab-Knopf/an der Ladeplatzhalter-Section in base.html reicht
// dafuer nicht: dieser Test fragt das Fragment direkt ab und stellt sicher,
// dass es bei abgewaehltem Dienst nicht mehr den echten Panel-Inhalt liefert
// (leere Huelle, Status 200 - wie es die tailscale/tuya-Unterseiten fuer
// ihren eigenen, ungegateten Fall bereits vorleben).
func TestOverviewAutomationsFragmentGatedWhenDeselected(t *testing.T) {
	reg := registry.New()
	installed := map[string]bool{"automation": false, "tailscale": true, "tuya": true}
	handler := OverviewWithDeviceFilterAndEngine(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, diagnostics.NewEngine(reg, nil), installed)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?fragment=panel&panel=automations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, erwartet 200 (das Template-Gate liefert eine leere Huelle, keinen 404)", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `x-data="automationsPanel()"`) {
		t.Fatalf("automation ist abgewaehlt, trotzdem liefert die eigene Fragment-URL den echten Panel-Inhalt:\n%s", body)
	}
}

func devicePrefsTestDevice() registry.DeviceView {
	return registry.DeviceView{ID: "node", Name: "Node", Entities: []registry.EntityView{
		{UniqueID: "node_temp", ObjectID: "temp", Name: "Temperatur", Component: "sensor",
			DeviceClass: "temperature", UnitOfMeasurement: "°C", HasValue: true, Value: "21"},
		{UniqueID: "node_relay", ObjectID: "relay", Name: "Relais", Component: "switch",
			HasValue: true, Value: "ON", Commandable: true},
		{UniqueID: "node_uptime", ObjectID: "uptime", Name: "Laufzeit", Component: "sensor",
			HasValue: true, Value: "12"},
		{UniqueID: "node_pending", ObjectID: "pending", Name: "Ohne Wert", Component: "sensor"},
	}}
}

func TestApplyDevicePrefsStampsSnapshot(t *testing.T) {
	devices := []registry.DeviceView{devicePrefsTestDevice(), {ID: "other"}}

	ApplyDevicePrefs(devices, map[string]settings.DevicePrefsEntry{
		"node": {DeviceID: "node", Icon: "mdi:raspberry-pi", FavoriteRefs: []string{"node_relay"}, PinFavorites: true},
	})

	if devices[0].IconName != "mdi:raspberry-pi" || !devices[0].PinFavorites {
		t.Errorf("devices[0] = %#v, want the stamped icon and pin flag", devices[0])
	}
	if len(devices[0].FavoriteRefs) != 1 || devices[0].FavoriteRefs[0] != "node_relay" {
		t.Errorf("FavoriteRefs = %#v, want the stored ref", devices[0].FavoriteRefs)
	}
	if devices[1].IconName != "" || devices[1].FavoriteRefs != nil {
		t.Errorf("devices[1] = %#v, want a device without a record left untouched", devices[1])
	}
}

func TestPriorityEntitiesUsesFavouritesInOrder(t *testing.T) {
	dev := devicePrefsTestDevice()
	dev.FavoriteRefs = []string{"node_relay", "node_temp"}

	picked := priorityEntities(dev)

	if len(picked) != 2 || picked[0].UniqueID != "node_relay" || picked[1].UniqueID != "node_temp" {
		t.Fatalf("priorityEntities = %v, want the favourites in the picked order", picked)
	}
}

func TestPriorityEntitiesKeepsFavouriteWithoutValue(t *testing.T) {
	dev := devicePrefsTestDevice()
	dev.FavoriteRefs = []string{"node_pending"}

	picked := priorityEntities(dev)

	if len(picked) != 1 || picked[0].UniqueID != "node_pending" {
		t.Fatalf("priorityEntities = %v, want an explicitly picked entity even without a value", picked)
	}
}

func TestPriorityEntitiesSkipsOrphanFavouriteAndFallsBackWhenEmpty(t *testing.T) {
	dev := devicePrefsTestDevice()
	dev.FavoriteRefs = []string{"node_gone", "node_temp"}

	picked := priorityEntities(dev)
	if len(picked) != 1 || picked[0].UniqueID != "node_temp" {
		t.Fatalf("priorityEntities = %v, want the orphan ref skipped silently", picked)
	}

	dev.FavoriteRefs = nil
	if automatic := priorityEntities(dev); len(automatic) == 0 {
		t.Fatal("priorityEntities returned nothing for a device without favourites, want the automatic selection")
	}
}

func TestCompactRowsFollowFavourites(t *testing.T) {
	plain := devicePrefsTestDevice()
	favoured := devicePrefsTestDevice()
	favoured.FavoriteRefs = []string{"node_uptime"}

	ids := func(rows []CompactRow) string {
		var out []string
		for _, row := range rows {
			out = append(out, row.ID)
		}
		return strings.Join(out, ",")
	}
	if ids(CompactRows(plain)) == ids(CompactRows(favoured)) {
		t.Fatal("CompactRows ignored the favourite selection - the live patcher would build the wrong rows")
	}
}

// TestOverviewPageStampsDevicePrefs is the one render path Overview() itself
// takes (the other two - SSE event cache, device-detail JSON - already have
// their own tests in internal/httpapi). Deleting the ApplyDevicePrefs call
// in OverviewWithDeviceFilterAndEngine leaves every other test in this
// package and internal/httpapi green, which is exactly the silent-regression
// this plan warns about: the compact card would keep its automatic rows and
// default icon while the SSE stream and device-detail JSON already reflect
// the saved preference.
func TestOverviewPageStampsDevicePrefs(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung"},
	})
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_temp", ObjectID: "temp", Component: "sensor", Name: "Temperatur"},
	})
	reg.UpdateState("node/power", []byte("42"), false, time.Now())
	reg.UpdateState("node/temp", []byte("21"), false, time.Now())

	store := settings.NewStore(t.TempDir())
	if _, err := store.SaveDevicePrefsEntry(settings.DevicePrefsEntry{
		DeviceID: "node", Icon: "mdi:raspberry-pi", FavoriteRefs: []string{"node_power"},
	}); err != nil {
		t.Fatalf("SaveDevicePrefsEntry: %v", err)
	}

	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live", nil))
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	body := recorder.Body.String()

	if !strings.Contains(body, `cx="11.7" cy="12.2"`) {
		t.Errorf("compact card does not carry the saved mdi:raspberry-pi markup - ApplyDevicePrefs was not applied on the page render path: %s", body)
	}
	if !strings.Contains(body, "Leistung") {
		t.Errorf("compact card is missing the favourite row (Leistung): %s", body)
	}
	if strings.Contains(body, "Temperatur") {
		t.Errorf("compact card shows the non-favourite entity (Temperatur) - favourite selection did not replace the automatic rows: %s", body)
	}
}

func TestApplyDevicePrefsStampsSuggestedIconWithoutPrefs(t *testing.T) {
	devices := []registry.DeviceView{{ID: "ez1", Manufacturer: "APsystems", Model: "EZ1"}, {ID: "x"}}

	ApplyDevicePrefs(devices, nil)

	if devices[0].SuggestedIcon != "energy-node:solar-panel" {
		t.Errorf("SuggestedIcon = %q, want energy-node:solar-panel even without any stored prefs", devices[0].SuggestedIcon)
	}
	if devices[0].IconName != "" || devices[1].SuggestedIcon != "" {
		t.Errorf("devices = %#v, want no saved icon and no suggestion for an unknown device", devices)
	}
}

func TestOverviewEntityGroupCardFallsBackToDefaultTitleWhenStoredEmpty(t *testing.T) {
	empty := settings.Layout{Version: 3, Pages: []settings.Page{{
		ID: "p", Name: "P", Order: 0,
		Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{
			{ID: "group", Type: "entity_group", Span: "1", Visible: true},
		}}},
	}}}
	body := renderOverviewWithLayout(t, empty)
	if !strings.Contains(body, `<h4>Entitäten</h4>`) {
		t.Fatalf("entity_group card without a stored title does not show the catalog fallback \"Entitäten\":\n%s", body)
	}

	named := empty
	named.Pages[0].Groups[0].Items[0].Title = "Mein Haus"
	body = renderOverviewWithLayout(t, named)
	if !strings.Contains(body, `<h4>Mein Haus</h4>`) {
		t.Fatalf("entity_group card with a stored title does not show it:\n%s", body)
	}
}

func TestActivePageFindsByID(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{
		{ID: "overview", Name: "", Order: 0, Groups: []settings.Group{}},
		{ID: "rooms", Name: "Räume", Order: 1, Groups: []settings.Group{}},
	}}

	// Render base to get the activePage function set up
	html := renderBase(t, layout)

	// Test that the page with ID "rooms" is rendered
	if !strings.Contains(html, `data-page-id="rooms"`) {
		t.Fatalf("page with ID \"rooms\" not rendered with correct data-page-id:\n%s", html)
	}
	if !strings.Contains(html, `data-page-id="overview"`) {
		t.Fatalf("page with ID \"overview\" not rendered with correct data-page-id:\n%s", html)
	}
}

func TestDefaultPageNameRendersLocalization(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{
		{ID: "overview", Name: "", Order: 0, Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{}}}},
	}}

	html := renderBase(t, layout)

	// The template should render the German translation for the default page name when Name is empty
	if !strings.Contains(html, `data-page-id="overview"`) || !strings.Contains(html, `>Übersicht</button>`) {
		t.Fatalf("default page name should render as Übersicht, got:\n%s", html)
	}
}

func TestCustomPageNameRendersAsLabel(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{
		{ID: "p1", Name: "Wohnzimmer", Order: 0, Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{}}}},
	}}

	html := renderBase(t, layout)

	// The template should render the custom page name directly
	if !strings.Contains(html, `>Wohnzimmer</button>`) {
		t.Fatalf("custom page name should be rendered, got:\n%s", html)
	}
}

func TestActivePageRendersFirstPageWhenNoQueryAndPageHasEmptyName(t *testing.T) {
	layout := settings.Layout{Version: 3, Pages: []settings.Page{
		{ID: "first", Name: "Foo", Order: 0, Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{{ID: "i", Type: "device", Ref: "test_device", Span: "1", Visible: true}}}}},
		{ID: "second", Name: "", Order: 1, Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{}}}},
	}}

	html := renderOverview(t, layout, "")

	// Without a ?page= query, the first page should render, not the one with empty name
	if !strings.Contains(html, `data-layout-page="first"`) {
		t.Fatalf("first page with non-empty name should render, got:\n%s", html)
	}
	if strings.Contains(html, `data-layout-page="second"`) {
		t.Fatalf("second page with empty name should not render when no query specified:\n%s", html)
	}
}

func TestSettingsPanelRendersTheVersionsPage(t *testing.T) {
	newHandler := func() http.Handler {
		return Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))
	}
	panel := httptest.NewRecorder()
	newHandler().ServeHTTP(panel, httptest.NewRequest("GET", "/?fragment=panel&panel=settings", nil))
	body := panel.Body.String()
	for _, marker := range []string{
		`aria-controls="settings-versions"`, `versionsPanel()`, "Installiertes Paket",
		"checkForUpdates()", "Nach Updates suchen",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("settings panel does not contain %q", marker)
		}
	}
	if strings.Contains(body, "Services-Version") || strings.Contains(body, "servicesVersion") || strings.Contains(body, "Dashboard-Version") {
		t.Fatal("the System panel shows no versions any more: they live on the Versionen page")
	}

	page := httptest.NewRecorder()
	newHandler().ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(page.Body.String(), "/static/js/versions.page.js?v=8") {
		t.Fatal("base.html must lazy-load versions.page.js with the settings panel")
	}
}

func TestSpriteDrawsDeviceGlyphsFromTheCatalogue(t *testing.T) {
	body := renderOverviewForSpriteTest(t)
	for id, name := range map[string]string{
		"ico-sun": "energy-node:sun", "ico-battery": "energy-node:battery", "ico-thermo": "energy-node:thermometer",
		"ico-load": "energy-node:home", "ico-wallbox": "energy-node:wallbox-compact", "ico-heatpump": "energy-node:heat-pump",
		"ico-grid": "energy-node:grid", "ico-switch": "energy-node:switch", "ico-clock": "energy-node:timer",
	} {
		if !strings.Contains(body, string(deviceIconSymbol(id, name))) {
			t.Errorf("sprite symbol %s is not drawn from %s", id, name)
		}
	}
	if !strings.Contains(body, `<symbol id="ico-flash" viewBox="0 0 24 24"><path d="M13.8 3 7.05 13.13 11.33 13.13 10.2 21 16.95 10.88 12.68 10.88Z"/></symbol>`) {
		t.Error("ico-flash must be the catalogue bolt at height 18")
	}
	if strings.Contains(body, `<symbol id="ico-gear" viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M12 2v3`) {
		t.Error("ico-gear still looks like the sun")
	}
}

func renderOverviewForSpriteTest(t *testing.T) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	return recorder.Body.String()
}

func liveTestDevice(id string, entities ...registry.EntityView) registry.DeviceView {
	return registry.DeviceView{ID: id, Name: "Node", Entities: entities}
}

func TestDeviceStructureFingerprintIsPerDevice(t *testing.T) {
	a := liveTestDevice("a", registry.EntityView{UniqueID: "a1", Name: "Leistung", Component: "sensor"})
	b := liveTestDevice("b", registry.EntityView{UniqueID: "b1", Name: "Leistung", Component: "sensor"})
	before := DeviceStructureFingerprint(a)
	b.Entities = append(b.Entities, registry.EntityView{UniqueID: "b2", Name: "Neu", Component: "sensor"})
	if DeviceStructureFingerprint(a) != before {
		t.Fatal("Fingerabdruck von a aendert sich mit b")
	}
	if DeviceStructureFingerprint(b) == DeviceStructureFingerprint(liveTestDevice("b", registry.EntityView{UniqueID: "b1", Name: "Leistung", Component: "sensor"})) {
		t.Fatal("neue Entitaet aendert den Fingerabdruck von b nicht")
	}
}

func TestDeviceStructureFingerprintCoversHeaderAndIgnoresValues(t *testing.T) {
	base := liveTestDevice("a", registry.EntityView{UniqueID: "a1", Name: "Leistung", Component: "sensor", Value: "1", HasValue: true})
	tick := base
	tick.Entities = []registry.EntityView{{UniqueID: "a1", Name: "Leistung", Component: "sensor", Value: "2", HasValue: true}}
	if DeviceStructureFingerprint(base) != DeviceStructureFingerprint(tick) {
		t.Error("ein Messwert aendert den Fingerabdruck")
	}
	for _, change := range []func(*registry.DeviceView){
		func(d *registry.DeviceView) { d.Name = "Anders" },
		func(d *registry.DeviceView) { d.Model = "X1" },
		func(d *registry.DeviceView) { d.IconName = "battery" },
	} {
		changed := base
		change(&changed)
		if DeviceStructureFingerprint(changed) == DeviceStructureFingerprint(base) {
			t.Errorf("Kopfaenderung %+v bleibt unsichtbar", changed)
		}
	}
	if DeviceStructureFingerprint(registry.DeviceView{}) != "" {
		t.Error("ein Geraet ohne ID muss die Struktur \"\" haben")
	}
}

func TestNewDeviceLive(t *testing.T) {
	dev := liveTestDevice("a", registry.EntityView{UniqueID: "a1", HasAvailability: true, Available: false})
	live := NewDeviceLive(dev)
	if live.Structure != DeviceStructureFingerprint(dev) || live.Availability != "offline" || live.Status != (DeviceTileStatus{Class: "bad", Rail: "is-offline", Offline: 1}) {
		t.Fatalf("live = %+v", live)
	}
}

func TestCompactRowsFollowPriorityAndCarryTheLabel(t *testing.T) {
	dev := registry.DeviceView{ID: "a", Name: "Shelly", Entities: []registry.EntityView{
		{UniqueID: "a1", Name: "Shelly Leistung", Component: "sensor", DeviceClass: "power", UnitOfMeasurement: "W", HasValue: true, Value: "5"},
		{UniqueID: "a2", Name: "Shelly Energie", Component: "sensor", HasValue: false},
	}}
	rows := CompactRows(dev)
	if len(rows) != 1 || rows[0] != (CompactRow{ID: "a1", Label: stripDeviceName("Shelly", "Shelly Leistung"), Unit: "W", DeviceClass: "power"}) {
		t.Fatalf("rows = %+v", rows)
	}
	if got := CompactRows(registry.DeviceView{ID: "leer"}); got == nil || len(got) != 0 {
		t.Fatalf("leere Auswahl = %#v, want leeres Slice", got)
	}
	dev.FavoriteRefs = []string{"a2"}
	if rows := CompactRows(dev); len(rows) != 1 || rows[0].ID != "a2" {
		t.Fatalf("Favoriten ignoriert: %+v", rows)
	}
}

func TestDeviceTileStatusTitleComesFromTheCatalog(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", StateTopic: "node/power", AvailabilityTopic: "node/status"},
	})
	reg.UpdateTopicWithQoS("node/status", []byte("offline"), false, 0, time.Now().UTC())
	recorder := httptest.NewRecorder()
	Overview(reg, nil, nil).ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=devices-live&view_mode=control", nil))
	if body := recorder.Body.String(); !strings.Contains(body, `title="1 offline"`) {
		t.Fatalf("Status-Titel fehlt: %s", body)
	}
}

func fragmentTestSetup(t *testing.T, items ...settings.Item) (*registry.Registry, *settings.Store) {
	t.Helper()
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_power", ObjectID: "power", Component: "sensor", Name: "Leistung", DeviceClass: "power", UnitOfMeasurement: "W", StateTopic: "node/power"},
	})
	reg.UpdateTopicWithQoS("node/power", []byte("12"), false, 0, time.Now().UTC())
	store := settings.NewStore(t.TempDir())
	if err := store.SaveLayout(settings.Layout{Pages: []settings.Page{{
		ID: "p1", Name: "Seite", Order: 0,
		Groups: []settings.Group{{ID: "main", Name: "Main", Items: items}},
	}}}); err != nil {
		t.Fatal(err)
	}
	return reg, store
}

func render(t *testing.T, reg *registry.Registry, store *settings.Store, url string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	Overview(reg, nil, store).ServeHTTP(recorder, httptest.NewRequest("GET", url, nil))
	return recorder.Code, recorder.Body.String()
}

// Spec 8: das Fragment ist byte-gleich zum selben Ausschnitt im vollen
// Raster. Sonst saehe eine nachgeladene Kachel anders aus als ihre
// Nachbarn.
func TestLayoutItemFragmentMatchesTheGrid(t *testing.T) {
	for _, item := range []settings.Item{
		{ID: "d1", Type: "device", Ref: "node", Span: "1", Visible: true},
		{ID: "d2", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact"},
		{ID: "e1", Type: "entity_value", Ref: "node_power", Span: "1", Visible: true},
	} {
		t.Run(item.ID, func(t *testing.T) {
			reg, store := fragmentTestSetup(t, item)
			code, grid := render(t, reg, store, "/?fragment=overview-live&page=p1")
			if code != 200 {
				t.Fatalf("Raster: %d", code)
			}
			code, fragment := render(t, reg, store, "/?fragment=layout-item&page=p1&item="+item.ID)
			if code != 200 {
				t.Fatalf("Fragment: %d %s", code, fragment)
			}
			fragment = strings.TrimSpace(fragment)
			if !strings.HasPrefix(fragment, `<div class="layout-grid-item`) || !strings.Contains(fragment, `data-layout-item-id="`+item.ID+`"`) {
				t.Fatalf("Fragment ist kein Rasterfeld: %s", fragment)
			}
			if !strings.Contains(grid, fragment) {
				t.Fatalf("Fragment weicht vom Raster ab:\n%s\n---\n%s", fragment, grid)
			}
		})
	}
}

func TestLayoutItemFragmentUnknownItemIs404(t *testing.T) {
	reg, store := fragmentTestSetup(t, settings.Item{ID: "d1", Type: "device", Ref: "node", Span: "1", Visible: true})
	if code, _ := render(t, reg, store, "/?fragment=layout-item&page=p1&item=fehlt"); code != 404 {
		t.Fatalf("Status %d, want 404", code)
	}
}

func TestDeviceCardFragmentMatchesTheDevicesTab(t *testing.T) {
	reg, store := fragmentTestSetup(t)
	for _, mode := range []string{"control", "compact"} {
		t.Run(mode, func(t *testing.T) {
			code, tab := render(t, reg, store, "/?fragment=devices-live&view_mode="+mode)
			if code != 200 {
				t.Fatalf("Tab: %d", code)
			}
			code, fragment := render(t, reg, store, "/?fragment=device-card&id=node&mode="+mode)
			if code != 200 {
				t.Fatalf("Fragment: %d %s", code, fragment)
			}
			fragment = strings.TrimSpace(fragment)
			if fragment == "" || !strings.Contains(tab, fragment) {
				t.Fatalf("Fragment weicht vom Tab ab:\n%s\n---\n%s", fragment, tab)
			}
		})
	}
	if code, _ := render(t, reg, store, "/?fragment=device-card&id=fehlt&mode=control"); code != 404 {
		t.Fatalf("unbekanntes Geraet: %d, want 404", code)
	}
}

func TestDeviceTilesCarryTheirOwnStructure(t *testing.T) {
	reg, store := fragmentTestSetup(t,
		settings.Item{ID: "d1", Type: "device", Ref: "node", Span: "1", Visible: true},
		settings.Item{ID: "d2", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact"},
		settings.Item{ID: "d3", Type: "device", Ref: "node", Span: "1", Visible: true, Display: "compact", EntityRefs: []string{"node_power"}},
		settings.Item{ID: "gone", Type: "device", Ref: "fehlt", Span: "1", Visible: true},
	)
	_, grid := render(t, reg, store, "/?fragment=overview-live&page=p1")
	devices := reg.Snapshot()
	want := DeviceStructureFingerprint(devices[0])
	for _, id := range []string{"d1", "d2", "d3"} {
		if !strings.Contains(grid, `data-layout-item-id="`+id+`"`) {
			t.Fatalf("Item %s fehlt", id)
		}
	}
	if got := strings.Count(grid, `data-structure="`+want+`"`); got < 6 {
		t.Errorf("data-structure %d-mal, want mindestens 6 (3 Rasterfelder + 3 Karten): %s", got, grid)
	}
	if !strings.Contains(grid, `data-layout-item-ref="fehlt"`) {
		t.Fatal("Item mit fehlendem Geraet fehlt")
	}
	if !strings.Contains(grid, `data-structure=""`) {
		t.Errorf("fehlendes Geraet meldet nicht die leere Struktur: %s", grid)
	}
	if strings.Count(grid, `data-compact-auto="true"`) != 1 {
		t.Errorf("genau die automatische Kompaktkarte (d2) traegt data-compact-auto: %s", grid)
	}
	if !strings.Contains(grid, `data-rows="node_power"`) {
		t.Errorf("data-rows fehlt: %s", grid)
	}
	if strings.Count(grid, "<template data-compact-row>") != 1 {
		t.Errorf("Zeilenvorlage nicht genau einmal im Raster: %s", grid)
	}
}

func TestDevicesTabCardsCarryStructureAndRowTemplate(t *testing.T) {
	reg, store := fragmentTestSetup(t)
	want := DeviceStructureFingerprint(reg.Snapshot()[0])
	for _, mode := range []string{"control", "compact"} {
		_, tab := render(t, reg, store, "/?fragment=devices-live&view_mode="+mode)
		if !strings.Contains(tab, `data-structure="`+want+`"`) {
			t.Errorf("%s: Karte ohne eigene Struktur: %s", mode, tab)
		}
		if strings.Count(tab, "<template data-compact-row>") != 1 {
			t.Errorf("%s: Zeilenvorlage nicht genau einmal", mode)
		}
	}
	_, compact := render(t, reg, store, "/?fragment=devices-live&view_mode=compact")
	if !strings.Contains(compact, `data-compact-auto="true"`) {
		t.Error("Kompaktkarte im Geraete-Tab ist nicht automatisch")
	}
}

// Die Szenario-Karte: die zugeklappte Zeile ist per Tastatur bedienbar, der
// Schalter ist ein <label>, Werte und Diagramm gibt es nur beim Regler.
func TestScenarioCardMarkupCarriesRowChartAndPhases(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`class="auto-scenario-row" role="button" tabindex="0"`,
		`x-on:keydown.enter.prevent="toggleScenarioExpanded(scenario)"`,
		`x-on:keydown.space.prevent="toggleScenarioExpanded(scenario)"`,
		`x-if="scenario.while && scenario.while.mode === 'regulate'"`,
		`class="auto-figures"`,
		`Heute eingespeist`,
		`viewBox="0 0 600 170"`,
		`class="auto-chart-band"`,
		`class="auto-chart-grid"`,
		`class="auto-chart-output"`,
		`class="auto-chart-legend"`,
		`class="auto-phases"`,
		`'is-active': activePhaseIndex(scenario) === index`,
		`automations.scenario.chart.empty`,
		`<label class="settings-toggle" x-on:click.stop`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Szenario-Karte: %q fehlt im Automations-Panel", want)
		}
	}
	// Der Schalter der zugeklappten Zeile ist ein <label>, damit er auch per
	// Klick auf den Schalter schaltet; ein <span> ließe sich nicht umlegen.
	if !strings.Contains(body, `<label class="settings-toggle" x-on:click.stop x-on:keydown.stop`) {
		t.Fatal("der Szenario-Schalter der zugeklappten Zeile ist kein <label class=\"settings-toggle\">")
	}
}

// Ein Verbraucher-Regler faerbt Stellwert, Verlauf und Legende in der
// Verbraucherfarbe. Die Klasse setzt die Karte, das CSS die Farben.
func TestConsumerRegulatorCardUsesTheConsumerColour(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tpl), `'is-consumer': regulatorFigures(scenario).direction === 'consumer'`) {
		t.Fatal("der Regler-Wrapper setzt die Klasse is-consumer nicht")
	}
	css, err := os.ReadFile("static/css/automations.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		`.auto-regulator.is-consumer .auto-figure-value.is-output { color: var(--flow-wallbox); }`,
		`.auto-regulator.is-consumer .auto-chart-output { stroke: var(--flow-wallbox); }`,
		`.auto-regulator.is-consumer .auto-swatch.is-output { background: var(--flow-wallbox); }`,
	} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("CSS-Regel fehlt: %s", rule)
		}
	}
}

// Zugeklappt zeigt ein Regler-Szenario Netz und Stellwert neben der Pill,
// solange kein Fortschritt zur Startschwelle an ihrer Stelle steht.
func TestCollapsedRegulatorRowShowsNetAndOutput(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	collapsed, _, found := strings.Cut(string(tpl), `<template x-if="scenarioExpanded[scenario.id]">`)
	if !found {
		t.Fatal("aufgeklappte Karte nicht gefunden")
	}
	for _, marker := range []string{
		`x-if="scenario.while && scenario.while.mode === 'regulate' && !scenarioProgress(scenario)"`,
		`class="auto-mini-figures"`,
		`x-text="gridNowText(scenario)"`,
		`x-text="regulatorFigures(scenario).output"`,
	} {
		if !strings.Contains(collapsed, marker) {
			t.Fatalf("zugeklappte Zeile ohne %s", marker)
		}
	}
}

// Der Schalter "Verlaeufe anzeigen" sitzt in der Kopfleiste zwischen Abstandhalter
// und Speichern und ist ein <label class="settings-toggle">.
func TestHeaderOffersTheHistorySwitch(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	bar := strings.Index(source, `class="auto-bar"`)
	if bar < 0 {
		t.Fatal("Kopfleiste .auto-bar nicht gefunden")
	}
	header := source[bar:]
	save := strings.Index(header, `x-on:click="save()"`)
	// Der Schalter steckt in einem Label mit Text: .settings-toggle selbst hat
	// eine feste Breite, Text darin lief ueber den Speichern-Knopf.
	label := strings.Index(header, `<label class="auto-bar-switch"`)
	if label < 0 {
		t.Fatal("Verlaufsschalter ist kein <label class=\"auto-bar-switch\"> in der Kopfleiste")
	}
	for _, want := range []string{`class="settings-toggle"`, `setShowHistory(`, `automations.show_history`} {
		if !strings.Contains(header[label:], want) {
			t.Fatalf("Verlaufsschalter ohne %s", want)
		}
	}
	if save < 0 || label > save {
		t.Fatal("Verlaufsschalter steht nicht vor dem Speichern-Knopf")
	}
}

// Zugeklappte Regler-Zeilen zeigen bei eingeschaltetem Verlauf die Sparkline,
// sonst weiter Netz und Stellwert.
func TestCollapsedRowSwitchesToSparkline(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	collapsed, _, found := strings.Cut(string(tpl), `<template x-if="scenarioExpanded[scenario.id]">`)
	if !found {
		t.Fatal("aufgeklappte Karte nicht gefunden")
	}
	for _, marker := range []string{`sparklineFor(scenario)`, `auto-spark`, `showHistory`} {
		if !strings.Contains(collapsed, marker) {
			t.Fatalf("zugeklappte Zeile ohne %s", marker)
		}
	}
}

// Die Regelkarte gliedert sich in Wenn, Dann und Ablauf, mit Live-Balken oben
// und Zeitleiste unten. Die alten Spalten Ursache, Tor und Wirkung sind weg.
func TestAutomationsRuleCardGroupsWhenThenAndFlow(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		"Wenn alles zutrifft",
		"auto-fires",
		"auto-live",
		"auto-blocks",
		`<template x-if="!editing[rule.id]">`,
		`<template x-if="editing[rule.id]">`,
		`x-on:click="testRule(rule)"`,
		`x-on:click="startEditing(rule)"`,
		"auto-cond-state",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Regelkarte: %q fehlt im Automations-Panel", want)
		}
	}
	// Der Kopf der Regel hat keinen Papierkorb mehr, Loeschen gibt es nur im Bearbeiten.
	rule, err := fs.ReadFile(templateFS, "templates/automations-rule.html")
	if err != nil {
		t.Fatal(err)
	}
	header, _, found := strings.Cut(string(rule), `<div class="auto-rule-body"`)
	if !found {
		t.Fatal("Regelkarte: Kopf und Ansicht nicht getrennt")
	}
	if strings.Contains(header, "removeRule(") {
		t.Fatal("Regelkarte: der Kopf enthaelt noch removeRule")
	}
	// Die Zustandsklassen entstehen im Skript, die Vorlage zeigt nur die Klasse.
	script, err := fs.ReadFile(templateFS, "static/js/automations.page.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"'is-met'", "'is-pending'", "'is-open'"} {
		if !strings.Contains(string(script), marker) {
			t.Fatalf("Regelkarte: Zustandsklasse %s fehlt im Skript", marker)
		}
	}
	for _, gone := range []string{`class="gate"`, "mini-chain", "rule-column", "rule-arrow"} {
		if strings.Contains(body, gone) {
			t.Fatalf("Regelkarte: %q gibt es nicht mehr", gone)
		}
	}
}

// Die Werkzeugleiste hat mobil einen Verlauf-Knopf mit Zustand, der den Schalter ersetzt.
func TestAutomationsToolbarHasHistoryIconButton(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`x-on:click="setShowHistory(!showHistory)"`,
		`x-bind:aria-pressed`,
		"auto-history-toggle",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Werkzeugleiste: %q fehlt im Automations-Panel", want)
		}
	}
}

// Der Bearbeiten-Zweig der Regel hat eine Fussleiste mit Loeschen, Verwerfen und
// Fertig, und die Sperrzeit ist ein Stepper statt eines Zahlenfelds.
func TestAutomationsRuleEditorHasFooterActions(t *testing.T) {
	tpl, err := fs.ReadFile(templateFS, "templates/automations-rule.html")
	if err != nil {
		t.Fatal(err)
	}
	_, edit, found := strings.Cut(string(tpl), `<template x-if="editing[rule.id]">`)
	if !found {
		t.Fatal("Bearbeiten-Zweig nicht gefunden")
	}
	for _, want := range []string{
		`removeRule(rule.id)`,
		`discardEditing(rule)`,
		`stopEditing(rule)`,
		`automations.rule.footer.delete`,
		`automations.rule.footer.discard`,
		`automations.rule.footer.done`,
		`auto-stepper`,
		`stepKey: 'cooldown_seconds'`,
	} {
		if !strings.Contains(edit, want) {
			t.Fatalf("Bearbeiten-Zweig: %q fehlt", want)
		}
	}
	if strings.Contains(edit, `type="number" min="0" max="86400" x-model.number="rule.cooldown_seconds"`) {
		t.Fatal("Bearbeiten-Zweig: Sperrzeit ist noch ein Zahlenfeld")
	}
}

// Im Bearbeiten-Modus sind die Zeilen editierbar wie in der Regelkarte: jeder
// Phasenblock bindet die gemeinsamen Editoren und Kacheln an seinen Besitzer.
func TestScenarioEditModeUsesTheSharedRowEditors(t *testing.T) {
	recorder := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).
		ServeHTTP(recorder, httptest.NewRequest("GET", "/?fragment=panel&panel=automations", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`<template x-if="scenarioEditing[scenario.id]">`,
		`scenarioRows(scenario, 'when', 'when')`,
		`scenarioRows(scenario, 'on_start', null)`,
		`scenarioRows(scenario, 'while', null)`,
		`scenarioRows(scenario, 'until', 'until')`,
		`scenarioRows(scenario, 'on_end', null)`,
		`in rowsOwner.conditions`,
		`in rowsOwner.actions`,
		`removeCondition(rowsOwner, index)`,
		`removeAction(rowsOwner, index)`,
		`addCondition(rowsOwner, type.key)`,
		`addAction(rowsOwner, type.key)`,
		`x-on:change="toggleUntil(scenario, $event.target.checked)"`,
		`x-on:click="changeWhileMode(scenario, 'hold')"`,
		`x-model="scenario.history_enabled"`,
		"Wenn eine Startbedingung nicht mehr erfüllt ist",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Szenario-Bearbeitung: %q fehlt im Automations-Panel", want)
		}
	}
}

func TestDevicePickerTemplateSupportsSpriteIconsAndFilters(t *testing.T) {
	source, err := fs.ReadFile(templateFS, "templates/devicemap.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`x-bind:href="'#' + item.iconRef"`, `x-on:click="pickerFilter(chip.key)"`, `x-on:keydown.enter="if (picker.mode === 'single') { $event.preventDefault(); pickerChoose(item.id) }"`, "item.subtitle", "is-dim"} {
		if !strings.Contains(string(source), marker) {
			t.Fatalf("device picker template does not contain %q", marker)
		}
	}
}

// Die Bedingungskarten nutzen weiche Felder und Stepper statt der alten
// Editor-Karte mit Linie links. Geprueft wird der Bedingungs-Block, die
// Aktions-Editoren folgen erst in einer spaeteren Aufgabe.
func TestConditionEditorsUseSoftFieldsAndSteppers(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-rows.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	block, _, found := strings.Cut(source[strings.Index(source, `{{define "automations-condition-editors"}}`):], `{{define "automations-action-editors"}}`)
	if !found {
		t.Fatal("Bedingungs-Editoren nicht gefunden")
	}
	if !strings.Contains(block, `class="auto-card`) {
		t.Fatal("Bedingungs-Editoren nutzen keine auto-card")
	}
	if !strings.Contains(block, `{{template "auto-stepper"}}`) {
		t.Fatal("Bedingungs-Editoren nutzen keinen auto-stepper")
	}
	if strings.Contains(block, `class="edit-card"`) {
		t.Fatal("Bedingungs-Editoren nutzen noch edit-card")
	}
	if !strings.Contains(source, `{{define "auto-stepper"}}`) {
		t.Fatal("Baustein auto-stepper fehlt")
	}
}

// Das Regler-Formular zeigt Geraete als Kacheln und oeffnet den Picker fuer
// "Alle anzeigen". Der Picker ist einmal im Automationen-Template eingebunden.
func TestRegulatorFormUsesDeviceChipsAndThePicker(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	for _, marker := range []string{
		`actuatorChips(scenario)`,
		`meterChips(scenario)`,
		`chooseActuator(scenario)`,
		`chooseMeter(scenario)`,
		`bandPreviewFor(scenario)`,
		`role="img"`,
		`class="auto-device-chip`,
		`is-selected`,
		`automations.devices.show_all`,
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("Regler-Formular fehlt %s", marker)
		}
	}
	page, err := os.ReadFile("templates/automations.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(page), `{{template "device-picker" .}}`) != 1 {
		t.Fatal("automations.html bindet den device-picker nicht genau einmal ein")
	}
}

// Die Feineinstellungen des Reglers sind eine Karte unter dem Regler, mit dem
// Zahnrad-Icon und vier Steppern (Verstaerkung, Takt, Schritt, Max. Aenderung).
func TestFineTuningIsACardWithSteppers(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-scenario.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	start := strings.Index(source, `class="auto-card`)
	if start < 0 {
		t.Fatal("Feineinstellungen sind keine auto-card")
	}
	card := source[start:]
	if end := strings.Index(card, `{{end}}`); end > 0 {
		card = card[:end]
	}
	if !strings.Contains(card, `ico-gear`) {
		t.Fatal("Feineinstellungen-Karte ohne ico-gear")
	}
	if n := strings.Count(card, `{{template "auto-stepper"}}`); n != 4 {
		t.Fatalf("Feineinstellungen-Karte hat %d auto-stepper, erwartet 4", n)
	}
	if !strings.Contains(source, `automations.scenario.edit.defaults_hint`) {
		t.Fatal("Hinweiszeile automations.scenario.edit.defaults_hint fehlt")
	}
}

// Die Aktionskarten zeigen je Aktion Art, Testen (nur mit rowsCanTest) und Loeschen.
// Der Editor-Block der Aktionen nutzt keine edit-card mehr.
func TestActionCardsShowKindTestAndDelete(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-rows.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	block, _, found := strings.Cut(source[strings.Index(source, `{{define "automations-action-editors"}}`):], `{{define "automations-condition-tiles"}}`)
	if !found {
		t.Fatal("Aktions-Editoren nicht gefunden")
	}
	for _, marker := range []string{
		`actionKind(action)`,
		`ico-play`,
		`ico-trash`,
		`auto-card`,
		`rowsCanTest`,
		`removeAction(rowsOwner, index)`,
		`chooseActionDevice(rowsOwner, index)`,
		`setSwitchState(rowsOwner, index,`,
		`setSetpointSource(action,`,
		`{{template "auto-stepper"}}`,
	} {
		if !strings.Contains(block, marker) {
			t.Fatalf("Aktions-Editoren ohne %q", marker)
		}
	}
	if strings.Contains(block, `class="edit-card"`) {
		t.Fatal("Aktions-Editoren nutzen noch edit-card")
	}
}

// Die Kopf-Kachel der Sollwert-Karte traegt die Rollenfarbe des Geraets.
func TestSetpointActionHeadUsesRoleTone(t *testing.T) {
	tpl, err := os.ReadFile("templates/automations-rows.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(tpl)
	block, _, found := strings.Cut(source[strings.Index(source, `{{define "automations-action-editors"}}`):], `{{define "automations-condition-tiles"}}`)
	if !found {
		t.Fatal("Aktions-Editoren nicht gefunden")
	}
	head, _, found := strings.Cut(block[strings.Index(block, `<div class="auto-card-head">`):], `auto-card-title`)
	if !found {
		t.Fatal("Kopf der Aktionskarte nicht gefunden")
	}
	if !strings.Contains(head, `actionTone(action)`) {
		t.Fatal("Kopf-Kachel der Aktionskarte ohne actionTone")
	}
	if !strings.Contains(head, `'is-tone-' + actionTone(action)`) {
		t.Fatal("Kopf-Kachel der Sollwert-Karte ohne Rollenfarbe")
	}
}

func TestSettingsShowsUsersTabOnlyWithUserManagement(t *testing.T) {
	settingsBody := func(roles []string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/?fragment=panel&panel=settings", nil)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{Username: "u", Roles: roles}))
		Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(rec, req)
		return rec.Body.String()
	}

	if body := settingsBody([]string{auth.RoleViewOverview}); strings.Contains(body, `aria-controls="settings-users"`) {
		t.Fatal("ohne user_management darf der Reiter Benutzer nicht erscheinen")
	}
	if body := settingsBody([]string{auth.RoleUserManagement}); !strings.Contains(body, `aria-controls="settings-users"`) {
		t.Fatal("mit user_management muss der Reiter Benutzer erscheinen")
	}
}

func viewPermissionRequest(target string, roles ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(auth.WithUser(req.Context(), auth.User{Username: "u", Roles: roles}))
}

func TestPanelFragmentForbiddenWithoutView(t *testing.T) {
	handler := Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, viewPermissionRequest("/?fragment=panel&panel=history", auth.RoleViewStats))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("panel=history ohne view_history muss 403 liefern, war %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, viewPermissionRequest("/?fragment=panel&panel=settings", auth.RoleViewStats))
	if rec.Code != http.StatusOK {
		t.Fatalf("panel=settings ist immer erlaubt, war %d", rec.Code)
	}
}

func TestLiveFragmentsForbiddenWithoutView(t *testing.T) {
	handler := Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, viewPermissionRequest("/?fragment=overview-live", auth.RoleViewDevices))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("overview-live ohne view_overview muss 403 liefern, war %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, viewPermissionRequest("/?fragment=devices-live", auth.RoleViewOverview))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("devices-live ohne view_devices muss 403 liefern, war %d", rec.Code)
	}
}

func TestBaseWithoutOverviewRendersWithoutTiles(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_relay", ObjectID: "relay", Component: "switch", Name: "Relais", CommandTopic: "node/relay/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	rec := httptest.NewRecorder()
	Overview(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(rec, viewPermissionRequest("/", auth.RoleViewStats))
	if rec.Code != http.StatusOK {
		t.Fatalf("base ohne view_overview muss rendern, war %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-entity-id="node_relay"`) {
		t.Fatal("ohne view_overview darf base keine Kacheln laden")
	}
}

func TestTilesWithoutOperateDevicesHaveNoCommandButtons(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "node", Name: "Node"},
		Entity: registry.EntityInfo{UniqueID: "node_relay", ObjectID: "relay", Component: "switch", Name: "Relais", CommandTopic: "node/relay/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	handler := Overview(reg, config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()))

	without := httptest.NewRecorder()
	handler.ServeHTTP(without, viewPermissionRequest("/?fragment=devices-live&view_mode=control", auth.RoleViewDevices, auth.RoleViewOverview))
	if without.Code != http.StatusOK {
		t.Fatalf("got status %d", without.Code)
	}
	if strings.Contains(without.Body.String(), `class="entity-command-button"`) {
		t.Fatal("ohne operate_devices darf keine Schaltflaeche in der Kachel stehen")
	}

	with := httptest.NewRecorder()
	handler.ServeHTTP(with, viewPermissionRequest("/?fragment=devices-live&view_mode=control", auth.RoleViewDevices, auth.RoleOperateDevices))
	if !strings.Contains(with.Body.String(), `class="entity-command-button"`) {
		t.Fatal("mit operate_devices muss die Schaltflaeche in der Kachel stehen")
	}
}

func TestInitialPanelFollowsViewPermissions(t *testing.T) {
	allowed := func(roles ...string) map[string]bool {
		can := map[string]bool{}
		for _, key := range auth.AllRoles() {
			can[key] = false
		}
		for _, role := range roles {
			can[role] = true
		}
		return can
	}

	cases := []struct {
		name                string
		can                 map[string]bool
		manager, automation bool
		want                string
	}{
		{"overview wins", allowed(auth.RoleViewOverview, auth.RoleViewStats), true, true, "overview-panel"},
		{"devices first", allowed(auth.RoleViewDevices, auth.RoleViewHistory, auth.RoleViewStats), true, true, "devices-panel"},
		{"history before stats", allowed(auth.RoleViewStats, auth.RoleViewHistory), true, true, "history-panel"},
		{"stats before config", allowed(auth.RoleViewConfig, auth.RoleViewStats), true, true, "stats-panel"},
		{"config needs manager", allowed(auth.RoleViewConfig, auth.RoleViewEnergy), false, true, "settings-panel"},
		{"config with manager", allowed(auth.RoleViewConfig, auth.RoleViewEnergy), true, true, "config-panel"},
		{"energy before devicemap", allowed(auth.RoleViewDeviceMap, auth.RoleViewEnergy), true, true, "energy-panel"},
		{"devicemap needs manager", allowed(auth.RoleViewDeviceMap, auth.RoleViewDiagnostics), false, true, "settings-panel"},
		{"diagnostics before automations", allowed(auth.RoleViewAutomations, auth.RoleViewDiagnostics), true, true, "diagnostics-panel"},
		{"automations needs service", allowed(auth.RoleViewAutomations), true, false, "settings-panel"},
		{"automations with service", allowed(auth.RoleViewAutomations), true, true, "automations-panel"},
		{"diagnostics needs manager", allowed(auth.RoleViewDiagnostics), false, true, "settings-panel"},
		{"nothing allowed falls back", allowed(), true, true, "settings-panel"},
	}
	for _, tc := range cases {
		if got := initialPanel(tc.can, tc.manager, tc.automation); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTabsFollowViewPermissions(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithUser(req.Context(), auth.User{Username: "u", Roles: []string{auth.RoleViewStats}}))
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{`id="tab-stats"`, `id="tab-settings"`, `id="stats-panel"`, `data-initial-panel="stats-panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("view_stats-Nutzer: %s fehlt", want)
		}
	}
	for _, gone := range []string{`id="tab-history"`, `id="history-panel"`, `id="tab-overview"`, `id="overview-panel"`, `id="tab-automations"`, `id="automations-panel"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("view_stats-Nutzer: %s darf nicht erscheinen", gone)
		}
	}
}

func TestNoUserShowsEveryTab(t *testing.T) {
	rec := httptest.NewRecorder()
	Overview(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir())).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="tab-devices"`, `id="tab-history"`, `id="tab-stats"`, `id="tab-config"`, `id="tab-energy"`, `id="tab-devicemap"`, `id="tab-diagnostics"`, `id="tab-automations"`, `id="tab-settings"`, `data-initial-panel="overview-panel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("ohne Nutzer fehlt %s", want)
		}
	}
}
