// Package webui holds the server-side html/template views. Phase 1 adds the
// first one: a plain overview page listing every device/entity found via
// MQTT Discovery, with live value/availability. Templates are embedded into
// the binary via go:embed - no separate codegen toolchain, no CDN
// dependency.
//
// HTMX handles live overview fragments and Alpine.js handles local panel state.
// Both assets are embedded locally so the dashboard has no CDN dependency.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"hash/fnv"
	"html/template"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/basepath"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/devicefilter"
	"github.com/Developer-Simon/energy-node-dashboard/internal/diagnostics"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

//go:embed templates/*.html static/js/*.js static/js-deps/*.js static/css/*.css static/img/*
var templateFS embed.FS

var staticFS, _ = fs.Sub(templateFS, "static")

// cardStyle baut den Inline-Stil einer Rasterzelle: die Typ-Mindesthoehe
// immer, die Zwangshoehe nur wenn gesetzt. Rueckgabetyp template.CSS, weil
// html/template Werte im style-Attribut sonst durch seinen CSS-Filter
// schickt - fuer Custom-Properties ist das Ergebnis unzuverlaessig. Der
// Inhalt stammt hier ausschliesslich aus dem Katalog und einer
// schema-validierten Ganzzahl, ist also nie nutzerkontrolliert.
func cardStyle(item settings.Item) template.CSS {
	// CardTypeForItem statt CardTypeFor: die kompakte Geraetekachel hat eine
	// eigene Mindesthoehe (siehe cardVariants in settings/cardcatalog.go).
	card := settings.CardTypeForItem(item)
	style := "--card-min-height: " + card.MinHeight + ";"
	if item.Height > 0 {
		style += " --card-height: " + forcedHeightRem(item.Height) + ";"
	}
	return template.CSS(style)
}

// forcedHeightRem rechnet Hoeheneinheiten in eine rem-Laenge um - dieselbe
// Rechnung, die Gridstack im Editor mit cellHeight 7rem und .8rem Abstand
// anstellt, damit Editorhoehe und Uebersicht deckungsgleich bleiben.
func forcedHeightRem(units int) string {
	value := float64(units)*7 + float64(units-1)*0.8
	return strconv.FormatFloat(value, 'f', -1, 64) + "rem"
}

func cardFillsHeight(itemType string) bool {
	card, _ := settings.CardTypeFor(itemType)
	return card.FillsHeight
}

// stripDeviceName strips a leading device-name prefix from an entity name
// for display on the device tile, where the device name is already shown in
// the tile header and repeating it on every entity just wastes space. Falls
// back to the full name whenever it doesn't start with the device name,
// since not every MQTT bridge prefixes entity names with the device name.
func stripDeviceName(deviceName, entityName string) string {
	name := strings.TrimSpace(entityName)
	prefix := strings.TrimSpace(deviceName)
	if prefix == "" || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
		return name
	}
	if rest := strings.TrimSpace(name[len(prefix):]); rest != "" {
		return rest
	}
	return name
}

var overviewTmpl = template.Must(template.New("base.html").Funcs(template.FuncMap{
	"iconFor":            iconFor,
	"add":                func(a, b int) int { return a + b },
	"energySnapshotJSON": energySnapshotJSON,
	"defaultHiddenCount": defaultHiddenCount,
	"deviceAvailability": deviceAvailability,
	"commandableCount":   commandableCount,
	"localTimestamp":     localTimestamp,
	"localDisplay":       localDisplay,
	"prettyJSON":         prettyJSON,
	"priorityEntities":   priorityEntities,
	"compactCardAuto":    compactCardAuto,
	"compactCardForItem": compactCardForItem,
	"deviceTileForItem":  deviceTileForItem,
	"entityGroupForItem": entityGroupForItem,
	"cardStyle":          cardStyle,
	"cardFillsHeight":    cardFillsHeight,
	"stripDeviceName":    stripDeviceName,
	"activePage": func(layout settings.Layout, id string) *settings.Page {
		// Die Uebersicht rendert genau eine Seite. Ohne Treffer die erste, damit
		// ein unbekannter Seitenname (alter Link, geloeschte Seite) nicht auf eine
		// leere Uebersicht fuehrt.
		for i := range layout.Pages {
			if layout.Pages[i].Name == id {
				return &layout.Pages[i]
			}
		}
		if len(layout.Pages) > 0 {
			return &layout.Pages[0]
		}
		return nil
	},
}).ParseFS(templateFS, "templates/base.html", "templates/overview.html", "templates/devices.html", "templates/device-tile.html", "templates/config.html", "templates/revisions.html", "templates/energy.html", "templates/layout-editor.html", "templates/devicemap.html", "templates/settings.html", "templates/automations.html", "templates/tiny-tuya.html", "templates/mqtt.html", "templates/tailscale.html", "templates/settings-stepper.html", "templates/diagnostics.html"))

// diagnosticTextPattern and configTextPattern mirror the fallback heuristics
// in dashboard.js's entityCategory() for entities whose discovery payload
// didn't set entity_category - the two implementations must be kept in sync.
var diagnosticTextPattern = regexp.MustCompile(`(?i)diagnos|diagnostic|error|fault|rssi|signal|uptime|firmware|update|version|status`)
var configTextPattern = regexp.MustCompile(`(?i)config|configuration|setting|einstellung|option|mode|modus`)

// classifyEntityCategory buckets an entity into the same four categories the
// device-detail modal uses (dashboard.js:entityCategory()), so per-tile
// category filtering in the Layout editor can agree with the modal.
func classifyEntityCategory(e registry.EntityView) string {
	switch strings.ToLower(e.EntityCategory) {
	case "diagnostic":
		return "diagnostics"
	case "config":
		return "configuration"
	}
	component := strings.ToLower(e.Component)
	if e.Commandable || component == "button" || component == "number" || component == "select" || component == "text" {
		return "controls"
	}
	text := strings.ToLower(e.Name + " " + e.ObjectID + " " + e.DeviceClass)
	if e.DefaultHidden || diagnosticTextPattern.MatchString(text) {
		return "diagnostics"
	}
	if configTextPattern.MatchString(text) {
		return "configuration"
	}
	return "measurements"
}

// deviceAvailability rolls a device's per-entity availability up into one
// state, mirroring dashboard.js's deviceAvailability getter: offline only
// when availability is known everywhere it is reported and nothing is
// currently available; unknown when no entity reports availability at all.
func deviceAvailability(entities []registry.EntityView) string {
	known := false
	for _, e := range entities {
		if !e.HasAvailability {
			continue
		}
		known = true
		if e.Available {
			return "online"
		}
	}
	if !known {
		return "unknown"
	}
	return "offline"
}

// priorityEntities picks up to three entities to surface on a compact device
// card: measurement entities that currently carry a value first (registry
// order), then any other non-hidden entity that carries a value, so a device
// whose only live entities are controls still shows something. Values only —
// the template never renders an input for these.
func priorityEntities(dev registry.DeviceView) []registry.EntityView {
	const limit = 3
	picked := make([]registry.EntityView, 0, limit)
	seen := make(map[string]bool)
	take := func(pred func(registry.EntityView) bool) {
		for _, e := range dev.Entities {
			if len(picked) == limit {
				return
			}
			if seen[e.UniqueID] || e.DefaultHidden || !e.HasValue || !pred(e) {
				continue
			}
			seen[e.UniqueID] = true
			picked = append(picked, e)
		}
	}
	take(func(e registry.EntityView) bool { return classifyEntityCategory(e) == "measurements" })
	take(func(registry.EntityView) bool { return true })
	return picked
}

// compactCardView ist die Render-Form der Kompakt-Karte: das Geraet plus die
// bis zu drei Zeilen, die sie zeigt. Vor 2026-09 waehlte das Template die
// Zeilen selbst (priorityEntities); jetzt entscheidet der Aufrufer, ob die
// Automatik greift (Geraete-Tab, compactCardAuto) oder eine feste Auswahl aus
// dem Layout (Uebersichtskachel, compactCardForItem).
type compactCardView struct {
	Device registry.DeviceView
	Rows   []registry.EntityView
}

// compactCardAuto ist der unveraenderte Weg: die Zeilen kommen aus
// priorityEntities. Das nutzt der Geraete-Tab (devices-compact).
func compactCardAuto(dev registry.DeviceView) compactCardView {
	return compactCardView{Device: dev, Rows: priorityEntities(dev)}
}

// compactCardForItem beruecksichtigt die feste Zeilenauswahl eines
// Layout-Items. Ist item.EntityRefs gesetzt, sind das genau die Zeilen - in
// der gewaehlten Reihenfolge, ein Ref ohne Treffer faellt still weg (wie ein
// verwaister entity_value-Ref). Leer heisst: zurueck zur Automatik.
func compactCardForItem(dev registry.DeviceView, item settings.Item) compactCardView {
	if len(item.EntityRefs) == 0 {
		return compactCardAuto(dev)
	}
	byID := make(map[string]registry.EntityView, len(dev.Entities))
	for _, entity := range dev.Entities {
		byID[entity.UniqueID] = entity
	}
	rows := make([]registry.EntityView, 0, len(item.EntityRefs))
	for _, ref := range item.EntityRefs {
		if entity, ok := byID[ref]; ok {
			rows = append(rows, entity)
		}
		if len(rows) == 3 {
			break
		}
	}
	return compactCardView{Device: dev, Rows: rows}
}

// CompactStructureFingerprint verdichtet das, was die Kompakt-Karte
// (compact-card in devices.html) *strukturell* zeigt und was der geteilte
// registry.StructureFingerprint nicht abdeckt: welche bis zu drei Entitaeten
// priorityEntities je Geraet auswaehlt - diese Auswahl haengt an HasValue,
// also an einem Messwert - und die zu einer Ampel verdichtete
// Geraete-Verfuegbarkeit, die den linken Kartenrand und die Status-Pille
// faerbt.
//
// dashboard.js vergleicht ihn (SSE-Feld structure_compact gegen
// data-structure-compact) *zusaetzlich* zum geteilten Fingerabdruck. Stehen
// beide, hat sich nur ein Wert innerhalb derselben Auswahl bewegt und
// compact-card-values.js zieht ihn nach. Wechselt einer, tauscht
// #devices-live wie frueher.
//
// Als Hex-String und nicht als Zahl - dieselbe Begruendung wie bei
// registry.StructureFingerprint: ein uint64 jenseits 2^53 verliert beim
// JSON-Parsen in JavaScript Stellen.
//
// Die Reihenfolge ist verlaesslich, weil snapshotLocked() Geraete nach ID
// und Entitaeten nach ObjectID sortiert und priorityEntities die
// Snapshot-Reihenfolge beibehaelt.
func CompactStructureFingerprint(devices []registry.DeviceView) string {
	sum := fnv.New64a()
	writeField := func(value string) {
		sum.Write([]byte(value))
		// Das Nullbyte trennt die Felder: ohne es ergaeben "ab"+"" und
		// "a"+"b" denselben Hash.
		sum.Write([]byte{0})
	}
	for _, device := range devices {
		writeField(device.ID)
		writeField(deviceAvailability(device.Entities))
		for _, entity := range priorityEntities(device) {
			writeField(entity.UniqueID)
		}
		// Geraete-Trenner, damit zwei Geraete mit den Auswahl-IDs [a] und
		// [a b] nicht denselben Hash ergeben wie [a b] und [a].
		writeField("|")
	}
	return strconv.FormatUint(sum.Sum64(), 16)
}

// deviceTileForItem returns dev unchanged when visibleCategories is empty
// (the layout item has no per-tile override, so the tile follows the global
// HiddenOnTile computation like the Devices panel). Otherwise it returns a
// copy of dev whose entities are restricted to the given categories,
// overriding the global per-category tile settings for this one tile.
func deviceTileForItem(dev registry.DeviceView, visibleCategories []string) registry.DeviceView {
	if len(visibleCategories) == 0 {
		return dev
	}
	allowed := make(map[string]bool, len(visibleCategories))
	for _, category := range visibleCategories {
		allowed[category] = true
	}
	filtered := dev
	filtered.Entities = append([]registry.EntityView(nil), dev.Entities...)
	for i := range filtered.Entities {
		entity := &filtered.Entities[i]
		entity.HiddenOnTile = entity.DefaultHidden || !allowed[classifyEntityCategory(*entity)]
	}
	return filtered
}

var loginTmpl = template.Must(template.ParseFS(templateFS, "templates/login.html"))

// entityGroupView is the render-ready shape of an "entity_group" item: the
// user-chosen title plus the resolved entities, in the order they were
// picked. A ref without a match (deleted/renamed entity) is skipped
// silently - the same failure mode as a single "entity_value" item's missing Ref,
// which the "with index" lookup in overview.html already treats as absent.
type entityGroupView struct {
	Title    string
	Entities []registry.EntityView
}

func entityGroupForItem(item settings.Item, entities map[string]registry.EntityView) entityGroupView {
	view := entityGroupView{Title: item.Title, Entities: make([]registry.EntityView, 0, len(item.EntityRefs))}
	for _, ref := range item.EntityRefs {
		if entity, ok := entities[ref]; ok {
			view.Entities = append(view.Entities, entity)
		}
	}
	return view
}

func defaultHiddenCount(entities []registry.EntityView) int {
	count := 0
	for _, entity := range entities {
		if entity.DefaultHidden {
			count++
		}
	}
	return count
}

func commandableCount(entities []registry.EntityView) int {
	count := 0
	for _, entity := range entities {
		if entity.Commandable {
			count++
		}
	}
	return count
}

func localTimestamp(value time.Time) string {
	return value.Format("2006-01-02T15:04:05.999999999Z07:00")
}

func localDisplay(value time.Time) string {
	return value.Format("2006-01-02 15:04:05")
}

func prettyJSON(value string) string {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, []byte(value), "", "  "); err != nil {
		return value
	}
	return formatted.String()
}

// energySnapshotJSON embeds an energy.Snapshot as the payload of a
// <script type="application/json"> element so energy-flow.js can draw its
// first frame without a round-trip to /api/v1/energy. "</" is escaped to
// "<\/" because <script> is an HTML raw-text element - the parser ends the
// element at the first literal "</script", even inside a JSON string, and
// entity/source strings ultimately originate from MQTT discovery payloads.
func energySnapshotJSON(snapshot energy.Snapshot) template.JS {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return template.JS("{}")
	}
	return template.JS(strings.ReplaceAll(string(data), "</", "<\\/"))
}

func defaultLayout(devices []registry.DeviceView) settings.Layout {
	items := []settings.Item{
		{ID: "energy-flow", Type: "energy_flow", Span: "full", Visible: true},
		{ID: "diagnostics", Type: "diagnostics", Span: "full", Visible: true},
	}
	for _, device := range devices {
		items = append(items, settings.Item{ID: "device:" + device.ID, Type: "device", Ref: device.ID, Span: "1", Visible: true})
	}
	return settings.Layout{
		Version: 2,
		Pages: []settings.Page{{
			ID:    "overview",
			Name:  "Übersicht",
			Order: 0,
			Groups: []settings.Group{{
				ID:    "dashboard",
				Name:  "Dashboard",
				Items: items,
			}},
		}},
	}
}

// energyCardScripts maps each of the six alternative energy-graphic layout
// item types (energiegrafiken-sechs-varianten.md) to the static/js file that
// implements its Alpine component. energy_flow keeps its own always-loaded
// energy-flow.js and isn't part of this - only the six newer, singleton
// "compare a Vorschlag" cards are optional enough to be worth gating.
var energyCardScripts = map[string]string{
	"energy_band":    "/static/js/energy-band.js",
	"energy_ring":    "/static/js/energy-ring.js",
	"energy_board":   "/static/js/energy-board.js",
	"energy_day":     "/static/js/energy-day.js",
	"energy_schema":  "/static/js/energy-schema.js",
	"energy_status":  "/static/js/energy-status.js",
	"battery_status": "/static/js/battery-status.js?v=1",
}

// requiredEnergyCardScripts returns the energy-<type>.js paths for whichever
// of the six alternative energy-graphic card types are actually visible
// somewhere in layout, sorted for a stable script order. Each of those cards
// fully re-mounts on every #overview-live refresh instead of patching an
// existing SVG in place (see the comment atop energy-band.js), so its script
// has to already be loaded by the time that first mount happens - but there
// is no reason to ship all six to every browser when a given layout only
// ever selects one or two of them. basePath prefixes the returned <script src>
// values; it is "" for direct access.
func requiredEnergyCardScripts(layout settings.Layout, basePath string) []string {
	seen := map[string]bool{}
	var scripts []string
	for _, page := range layout.Pages {
		for _, group := range page.Groups {
			for _, item := range group.Items {
				if !item.Visible {
					continue
				}
				script, ok := energyCardScripts[item.Type]
				if !ok || seen[script] {
					continue
				}
				seen[script] = true
				scripts = append(scripts, basepath.Join(basePath, script))
			}
		}
	}
	sort.Strings(scripts)
	return scripts
}

func devicesByID(devices []registry.DeviceView) map[string]registry.DeviceView {
	result := make(map[string]registry.DeviceView, len(devices))
	for _, device := range devices {
		result[device.ID] = device
	}
	return result
}

func entitiesByID(devices []registry.DeviceView) map[string]registry.EntityView {
	result := map[string]registry.EntityView{}
	for _, device := range devices {
		for _, entity := range device.Entities {
			result[entity.UniqueID] = entity
		}
	}
	return result
}

// Overview renders the Phase 1 server-side overview page.
func Overview(reg *registry.Registry, configs *config.Manager, store *settings.Store) http.HandlerFunc {
	return OverviewWithDeviceFilter(reg, configs, store, nil)
}

// templateNameForRequest maps the fragment/panel query parameters to the
// template definition that serves them (see overviewTmpl's ParseFS call for
// the full set). Returning ok=false means "unknown panel", a 404.
func templateNameForRequest(r *http.Request) (name string, ok bool) {
	switch r.URL.Query().Get("fragment") {
	case "overview-live":
		return "overview-live", true
	case "devices-live":
		return "devices-live", true
	case "panel":
		switch panel := r.URL.Query().Get("panel"); panel {
		case "devices", "history", "diagnostics", "config", "energy", "devicemap", "settings", "automations", "layout-editor":
			return panel, true
		default:
			return "", false
		}
	default:
		return "base", true
	}
}

func OverviewWithDeviceFilter(reg *registry.Registry, configs *config.Manager, store *settings.Store, ignored *devicefilter.Store) http.HandlerFunc {
	return OverviewWithDeviceFilterAndEngine(reg, configs, store, ignored, diagnostics.NewEngine(reg, store))
}

// OverviewWithDeviceFilterAndEngine is OverviewWithDeviceFilter with an
// explicit diagnostics.Engine instead of a freshly constructed one. The
// engine used for /api/v1/diagnostics and /api/v1/diagnostics/health already
// carries SetIgnoredStore/SetConfigManager/SetStartedAt (see
// httpapi.NewRouterWithDependencies) - reusing that same instance for the
// "diagnostics" layout card is what keeps its counts from ever disagreeing
// with the Diagnose tab it summarizes. OverviewWithDeviceFilter builds its
// own bare engine because most of its ~50 call sites are tests that don't
// exercise the diagnostics card at all.
func OverviewWithDeviceFilterAndEngine(reg *registry.Registry, configs *config.Manager, store *settings.Store, ignored *devicefilter.Store, engine *diagnostics.Engine) http.HandlerFunc {
	resolver := energy.NewResolver(nil)
	var resolverMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		templateName, ok := templateNameForRequest(r)
		if !ok {
			http.Error(w, "unknown panel", http.StatusNotFound)
			return
		}
		// Der Bearbeitungsmodus haengt an der Rolle edit_layout. Ist die
		// Authentifizierung ganz aus (kein Nutzer im Context, z.B. Tests oder
		// eine Instanz ohne auth.Manager), bleibt er offen wie bisher; sonst
		// entscheidet die Rolle - die jeder Gast mitbekommt (siehe
		// auth.ContinueAsGuest), also aendert sich fuer heutige Instanzen
		// nichts. .Manager (configs+store vorhanden) bleibt die zusaetzliche
		// Deployment-Voraussetzung.
		user, hasUser := auth.UserFromContext(r.Context())
		canEditLayout := configs != nil && store != nil && (!hasUser || auth.HasRole(user, auth.RoleEditLayout))

		// Das Editor-Fragment ist die einzige layout-veraendernde Oberflaeche
		// und darf ohne die Rolle gar nicht erst rendern.
		if templateName == "layout-editor" && !canEditLayout {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Every fragment/panel is rendered through the same handler, but most
		// of them only need a slice of the full page's data: "devices-live"
		// never touches .Energy or .Layout, "energy" never touches .Devices'
		// tile-visibility flags, and panels like "settings" or "history" are
		// entirely client-rendered and need none of it. Loading/aggregating
		// only what the requested template actually reads keeps SSE-driven
		// live-fragment requests (up to 1/s) from paying for a full registry
		// snapshot, layout load and energy aggregation every time.
		needsTiles := templateName == "base" || templateName == "overview-live" || templateName == "devices-live" || templateName == "devices"
		needsEnergyAggregate := templateName == "base" || templateName == "overview-live" || templateName == "energy"
		needsLayout := templateName == "base" || templateName == "overview-live"
		needsDevices := needsTiles || needsEnergyAggregate

		showDiscoveryTooltips := true
		showRuntimeStatus := true
		deviceViewMode := settings.DeviceViewModeCompact
		theme := settings.ThemeMint
		showConfigEntitiesOnTile := false
		showDiagnosticEntitiesOnTile := false
		widePanels := settings.Default().WidePanels
		statusBarItems := settings.Default().StatusBarItems
		if store != nil {
			if value, err := store.LoadSettings(); err == nil {
				showDiscoveryTooltips = value.ShowDiscoveryTooltips
				showRuntimeStatus = value.ShowRuntimeStatus
				deviceViewMode = value.DeviceViewMode
				theme = value.Theme
				showConfigEntitiesOnTile = value.ShowConfigEntitiesOnTile
				showDiagnosticEntitiesOnTile = value.ShowDiagnosticEntitiesOnTile
				widePanels = value.WidePanels
				statusBarItems = value.StatusBarItems
			}
		}
		if requestedMode := r.URL.Query().Get("view_mode"); requestedMode == settings.DeviceViewModeControl || requestedMode == settings.DeviceViewModeCompact {
			deviceViewMode = requestedMode
		}
		if theme == "" {
			theme = settings.ThemeMint
		}

		var devices []registry.DeviceView
		if needsDevices {
			devices = reg.Snapshot()
		}
		if needsTiles {
			for i := range devices {
				for j := range devices[i].Entities {
					entity := &devices[i].Entities[j]
					entity.HiddenOnTile = entity.DefaultHidden ||
						(entity.EntityCategory == "config" && !showConfigEntitiesOnTile) ||
						(entity.EntityCategory == "diagnostic" && !showDiagnosticEntitiesOnTile)
				}
			}
		}

		view := map[string]any{
			"BasePath":              basepath.From(r),
			"Manager":               configs != nil && store != nil,
			"CanEditLayout":         canEditLayout,
			"ShowDiscoveryTooltips": showDiscoveryTooltips,
			"ShowRuntimeStatus":     showRuntimeStatus,
			"DeviceViewMode":        deviceViewMode,
			"Theme":                 theme,
			"IgnoredDevices":        []devicefilter.Summary{},
			"WidePanels":            strings.Join(widePanels, ","),
			"StatusBarItems":        strings.Join(statusBarItems, ","),
		}
		if needsDevices {
			view["Devices"] = devices
		}
		// Dasselbe Feld, das der SSE-Rumpf mitschickt (eventBody.Structure).
		// Der Browser vergleicht beide und tauscht sein Fragment nur, wenn
		// sie auseinanderlaufen - siehe deviceTilesPushCovers in dashboard.js.
		if needsTiles {
			view["Structure"] = registry.StructureFingerprint(devices)
		}
		// Dasselbe Feld, das der SSE-Rumpf als structure_compact mitschickt
		// (eventBody.StructureCompact). Die HiddenOnTile-Schleife oben aendert
		// nur Felder, die weder priorityEntities noch deviceAvailability
		// liest - der Wert hier ist also deckungsgleich mit dem, den bodies()
		// aus reg.Snapshot() rechnet.
		if needsTiles {
			view["StructureCompact"] = CompactStructureFingerprint(devices)
		}
		if needsEnergyAggregate {
			if store != nil {
				if value, err := store.LoadEnergy(); err == nil {
					resolverMu.Lock()
					resolver.SetOverrides(value.Assignments)
					resolver.SetInterpretation(value.Interpretation)
					resolverMu.Unlock()
				}
			}
			resolverMu.Lock()
			interpretation := resolver.Interpretation()
			resolverMu.Unlock()
			snapshot := energy.Aggregate(devices, resolver, time.Now().UTC())
			view["Energy"] = snapshot.WithInterpretation(interpretation)
		}
		if needsLayout {
			// Frische Installation ohne layout.json: Standardraster. Ein
			// bewusst leer gespeichertes Layout dagegen fuehrt in den
			// Leerzustand mit Editieren-Knopf (Spec 4.3) - darum kein
			// len(Pages) > 0 mehr, sondern die Existenzpruefung.
			layout := defaultLayout(devices)
			if store != nil && store.LayoutConfigured() {
				if value, err := store.LoadLayout(); err == nil {
					layout = value
				}
			}
			view["Layout"] = layout
			view["ActivePage"] = r.URL.Query().Get("page")
			view["LayoutDevices"] = devicesByID(devices)
			view["LayoutEntities"] = entitiesByID(devices)
			view["EnergyCardScripts"] = requiredEnergyCardScripts(layout, basepath.From(r))
			now := time.Now().UTC()
			view["Diagnostics"] = diagnostics.Summarize(engine.Evaluate(now), engine.Health(now), devices)
		}
		if ignored != nil {
			view["IgnoredDevices"] = ignored.Summaries()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := overviewTmpl.ExecuteTemplate(w, templateName, view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func Static() http.Handler {
	files := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}

// Die Anmeldeseite rendert ohne base.html und braucht das Theme deshalb
// selbst - der Store ist hier der einzige Weg daran, weil noch keine
// Sitzung existiert.
func Login(guestOnly bool, store *settings.Store, adminAuthWarning string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		theme := settings.ThemeMint
		if store != nil {
			if value, err := store.LoadSettings(); err == nil && value.Theme != "" {
				theme = value.Theme
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"GuestOnly": guestOnly, "BasePath": basepath.From(r), "Theme": theme, "AdminAuthWarning": adminAuthWarning}
		if err := loginTmpl.ExecuteTemplate(w, "login", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
