package webui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func decodeHistoryViewConfig(t *testing.T, raw string) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("ConfigJSON ist kein JSON: %v (%s)", err, raw)
	}
	return config
}

func TestHistoryViewCardResolvesABoundView(t *testing.T) {
	views := []settings.HistoryView{{ID: "v1", Name: "PV und Netz", Series: []string{"role:pv", "role:grid"}, RangeHours: 6, RangeMode: "custom", RangeFrom: 1000, RangeTo: 5000, Aggregate: "max"}}
	card := historyViewCard(settings.Item{ID: "item-1", Type: "history_view", Ref: "v1"}, views)
	if card.Missing || card.Title != "PV und Netz" {
		t.Fatalf("gebundene Kachel: %#v", card)
	}
	config := decodeHistoryViewConfig(t, card.ConfigJSON)
	if config["range_mode"] != "custom" || config["range_from"] != float64(1000) || config["range_to"] != float64(5000) || config["aggregate"] != "max" {
		t.Fatalf("Konfiguration: %v", config)
	}
	if len(config["series"].([]any)) != 2 {
		t.Fatalf("Serien: %v", config["series"])
	}
	if !strings.HasPrefix(card.DOMID, "hv-") || len(card.DOMID) != len("hv-")+12 {
		t.Fatalf("DOMID = %q", card.DOMID)
	}
}

func TestHistoryViewCardMissingView(t *testing.T) {
	card := historyViewCard(settings.Item{ID: "item-1", Type: "history_view", Ref: "weg"}, nil)
	if !card.Missing || card.Title != "" {
		t.Fatalf("verschwundene Sicht: %#v", card)
	}
}

func TestHistoryViewCardOwnSettings(t *testing.T) {
	card := historyViewCard(settings.Item{ID: "item-1", Type: "history_view", Title: "Netz", HistoryRangeHours: "168", HistoryAggregate: "min"}, nil)
	if card.Missing || card.Title != "Netz" {
		t.Fatalf("eigene Einstellung: %#v", card)
	}
	config := decodeHistoryViewConfig(t, card.ConfigJSON)
	if config["range_mode"] != "relative" || config["range_hours"] != float64(168) || config["aggregate"] != "min" {
		t.Fatalf("Konfiguration: %v", config)
	}
	// leere Serienliste ist [] und nicht null - der Client prueft .length
	if series, ok := config["series"].([]any); !ok || len(series) != 0 {
		t.Fatalf("series = %#v", config["series"])
	}
}

func TestHistoryViewCardDOMIDTracksConfig(t *testing.T) {
	item := settings.Item{ID: "item-1", Type: "history_view", Ref: "v1"}
	views := []settings.HistoryView{{ID: "v1", Name: "A", Series: []string{"role:pv"}, RangeHours: 6, Aggregate: "avg"}}
	first := historyViewCard(item, views).DOMID
	if again := historyViewCard(item, views).DOMID; again != first {
		t.Fatalf("DOMID nicht stabil: %q != %q", again, first)
	}
	changed := []settings.HistoryView{{ID: "v1", Name: "A", Series: []string{"role:pv"}, RangeHours: 24, Aggregate: "avg"}}
	if historyViewCard(item, changed).DOMID == first {
		t.Fatal("geaenderter Zeitraum behaelt die DOMID - htmx wuerde den alten Knoten behalten")
	}
	renamed := []settings.HistoryView{{ID: "v1", Name: "B", Series: []string{"role:pv"}, RangeHours: 6, Aggregate: "avg"}}
	if historyViewCard(item, renamed).DOMID == first {
		t.Fatal("geaenderter Name behaelt die DOMID")
	}
	other := settings.Item{ID: "item-2", Type: "history_view", Ref: "v1"}
	if historyViewCard(other, views).DOMID == first {
		t.Fatal("zwei Kacheln mit derselben Sicht teilen sich eine DOMID")
	}
}

func saveHistoryViewFixture(t *testing.T, visible bool) *settings.Store {
	t.Helper()
	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.HistoryViews = []settings.HistoryView{{ID: "v1", Name: "PV", Series: []string{"role:pv"}, RangeHours: 6, Aggregate: "avg"}}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLayout(settings.Layout{Version: 3, Pages: []settings.Page{{ID: "p", Name: "P", Groups: []settings.Group{{ID: "g", Name: "G", Items: []settings.Item{
		{ID: "bound", Type: "history_view", Ref: "v1", Span: "2", Visible: visible},
		{ID: "own", Type: "history_view", Span: "2", Visible: visible},
		{ID: "gone", Type: "history_view", Ref: "weg", Span: "2", Visible: visible},
	}}}}}}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOverviewRendersHistoryViewCards(t *testing.T) {
	body := renderWithBasePath(t, Overview(registry.New(), nil, saveHistoryViewFixture(t, true)), "/node/")
	if strings.Count(body, `class="history-view-card"`) != 3 {
		t.Fatalf("erwartet drei Kacheln: %s", body)
	}
	if !strings.Contains(body, "hx-preserve") || !strings.Contains(body, "x-ignore") {
		t.Fatal("Kachel ohne hx-preserve/x-ignore")
	}
	if !strings.Contains(body, "data-history-view-missing") {
		t.Fatal("verschwundene Sicht nicht markiert")
	}
	if !strings.Contains(body, ">PV</h3>") {
		t.Fatal("Name der gebundenen Sicht fehlt im Kopf")
	}
	apex := strings.Index(body, `src="/node/static/js-deps/apexcharts.min.js"`)
	chart := strings.Index(body, `src="/node/static/js/history-chart.js?v=1"`)
	card := strings.Index(body, `src="/node/static/js/history-view-card.js?v=1"`)
	if apex < 0 || chart < 0 || card < 0 || !(apex < chart && chart < card) {
		t.Fatalf("Skripte fehlen oder falsche Reihenfolge: %d %d %d", apex, chart, card)
	}
	if strings.Count(body, "history-view-card.js") != 1 {
		t.Fatal("Skript fuer drei Kacheln mehrfach eingebunden")
	}
}

func TestOverviewSkipsHistoryScriptsForHiddenCards(t *testing.T) {
	body := renderWithBasePath(t, Overview(registry.New(), nil, saveHistoryViewFixture(t, false)), "/node/")
	if strings.Contains(body, "history-view-card.js") || strings.Contains(body, "js-deps/apexcharts.min.js\" defer") {
		t.Fatal("Skripte trotz unsichtbarer Kachel ausgeliefert")
	}
}
