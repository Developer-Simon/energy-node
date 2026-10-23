package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestExchangeSeriesFolgtDenRollenDesRecorders(t *testing.T) {
	// grid_import allein reicht fuer role:grid, genau wie has('grid',
	// 'grid_import', 'grid_export') in history-recorder.js.
	snapshot := energy.Snapshot{Values: map[energy.Role]float64{
		energy.RolePV:         1200,
		energy.RoleGridImport: 300,
		energy.RoleBatterySoC: 55,
	}}
	extras := []registry.EntityValue{{UniqueID: "shelly_temp", Unit: "°C"}}

	got := exchangeSeriesFrom(snapshot, extras)

	want := []exchangeSeries{
		{ID: "role:pv", Unit: "W"},
		{ID: "role:grid", Unit: "W"},
		{ID: "role:battery_soc", Unit: "%"},
		{ID: "shelly_temp", Unit: "°C"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("series = %+v, want %+v", got, want)
	}
}

func TestExchangeSeriesOhneRollenIstLeerUndNichtNil(t *testing.T) {
	got := exchangeSeriesFrom(energy.Snapshot{}, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("series = %#v, want leere Liste", got)
	}
}

func TestAnkuendigungNenntDieAufgezeichnetenSerien(t *testing.T) {
	mux := http.NewServeMux()
	newHistoryExchange(func() []exchangeSeries {
		return []exchangeSeries{{ID: "role:pv", Unit: "W"}}
	}).routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/history/exchange")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var announced struct {
		Series []exchangeSeries `json:"series"`
	}
	if err := json.NewDecoder(response.Body).Decode(&announced); err != nil {
		t.Fatal(err)
	}
	if len(announced.Series) != 1 || announced.Series[0].ID != "role:pv" || announced.Series[0].Unit != "W" {
		t.Fatalf("series = %+v", announced.Series)
	}
}

func TestAnkuendigungOhneSerienquelleNenntLeereListe(t *testing.T) {
	server := exchangeServer(t)
	response, err := http.Get(server.URL + "/api/v1/history/exchange")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	// Ein Fremd-Peer unterscheidet "nichts aufgezeichnet" ([]) von "zu
	// altes Dashboard" (Feld fehlt) - null waere keins von beiden.
	if !strings.Contains(string(raw), `"series":[]`) {
		t.Fatalf("Ankuendigung ohne series-Liste: %s", raw)
	}
}

func TestExchangeSeriesKuendigtEigeneKategorienMitBasisAn(t *testing.T) {
	snapshot := energy.Snapshot{
		Values:     map[energy.Role]float64{"custom:werkstatt": 350},
		Categories: map[string]energy.Category{"werkstatt": {Base: energy.CategoryConsumer}},
	}

	got := exchangeSeriesFrom(snapshot, nil)

	want := []exchangeSeries{{ID: "role:custom:consumer:werkstatt", Unit: "W"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("series = %+v, want %+v", got, want)
	}
}

func writeScenarioRules(t *testing.T, rules string) *config.Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "automation_rules.json"), []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "automation_rules.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.NewManager(dir)
}

const threeScenarioRules = `{"version":1,"settings":{},"rules":[],"scenarios":[` +
	`{"id":"a","name":"A","enabled":true,"when":{"conditions":[]},"on_start":{"actions":[]},"while":{"mode":"regulate"},"on_end":{"actions":[]}},` +
	`{"id":"b","name":"B","enabled":true,"when":{"conditions":[]},"on_start":{"actions":[]},"while":{"mode":"hold"},"on_end":{"actions":[]}},` +
	`{"id":"c","name":"C","enabled":true,"when":{"conditions":[]},"on_start":{"actions":[]},"while":{"mode":"none"},"on_end":{"actions":[]}}]}`

func TestScenarioOutputEntityIDsListsOnlyRegulatingScenarios(t *testing.T) {
	got := scenarioOutputEntityIDs(writeScenarioRules(t, threeScenarioRules))
	if !reflect.DeepEqual(got, []string{"automation_scenario_a_output"}) {
		t.Fatalf("ids = %#v, want [automation_scenario_a_output]", got)
	}
}

func TestScenarioOutputEntityIDsToleratesAMissingOrBrokenFile(t *testing.T) {
	if got := scenarioOutputEntityIDs(config.NewManager(t.TempDir())); got != nil {
		t.Fatalf("empty manager: ids = %#v, want nil", got)
	}
	if got := scenarioOutputEntityIDs(writeScenarioRules(t, "{")); got != nil {
		t.Fatalf("broken file: ids = %#v, want nil", got)
	}
	if got := scenarioOutputEntityIDs(nil); got != nil {
		t.Fatalf("nil manager: ids = %#v, want nil", got)
	}
}

func TestRecordedEntityIDsMergesExtrasAndScenarioOutputs(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.HistoryExtraEntities = []string{"x"}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	configs := writeScenarioRules(t, `{"version":1,"settings":{},"rules":[],"scenarios":[`+
		`{"id":"a","name":"A","enabled":true,"when":{"conditions":[]},"on_start":{"actions":[]},"while":{"mode":"regulate"},"on_end":{"actions":[]}}]}`)

	got := recordedEntityIDs(store, configs)
	if !reflect.DeepEqual(got, []string{"x", "automation_scenario_a_output"}) {
		t.Fatalf("ids = %#v", got)
	}

	value.HistoryExtraEntities = []string{"automation_scenario_a_output"}
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	got = recordedEntityIDs(store, configs)
	if !reflect.DeepEqual(got, []string{"automation_scenario_a_output"}) {
		t.Fatalf("duplicate ids = %#v, want one entry", got)
	}
}

func TestRecordedExchangeSeriesAnnouncesScenarioOutputs(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "automation"},
		Entity: registry.EntityInfo{
			UniqueID: "automation_scenario_a_output", Component: "sensor", ObjectID: "scenario_a_output",
			Name: "A Ausgabe", StateTopic: "automation/scenario_a_output", UnitOfMeasurement: "W",
		},
	})
	configs := writeScenarioRules(t, `{"version":1,"settings":{},"rules":[],"scenarios":[`+
		`{"id":"a","name":"A","enabled":true,"when":{"conditions":[]},"on_start":{"actions":[]},"while":{"mode":"regulate"},"on_end":{"actions":[]}}]}`)

	series := recordedExchangeSeries(reg, settings.NewStore(t.TempDir()), configs, nil)()
	found := false
	for _, s := range series {
		if s.ID == "automation_scenario_a_output" && s.Unit == "W" {
			found = true
		}
	}
	if !found {
		t.Fatalf("series = %+v, want automation_scenario_a_output with unit W", series)
	}
}

func TestHistorySamplerValuesIsEmptyWithoutRegulatingScenarios(t *testing.T) {
	snapshot := energy.Snapshot{Values: map[energy.Role]float64{energy.RoleGrid: -300}}
	configs := writeScenarioRules(t, `{"version":1,"settings":{},"rules":[],"scenarios":[]}`)
	if got := HistorySamplerValues(snapshot, registry.New(), configs); len(got) != 0 {
		t.Fatalf("values = %+v, want empty", got)
	}
}

func TestHistorySamplerValuesReadsGridAndScenarioOutputs(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "automation"},
		Entity: registry.EntityInfo{
			UniqueID: "automation_scenario_a_output", Component: "sensor", ObjectID: "scenario_a_output",
			Name: "A Ausgabe", StateTopic: "automation/scenario_a_output", UnitOfMeasurement: "W",
		},
	})
	reg.UpdateTopic("automation/scenario_a_output", []byte("615"), false, time.Now())
	snapshot := energy.Snapshot{Values: map[energy.Role]float64{
		energy.RoleGridImport: 0,
		energy.RoleGridExport: 470,
	}}
	configs := writeScenarioRules(t, threeScenarioRules)

	got := HistorySamplerValues(snapshot, reg, configs)
	if got["role:grid"] != -470 || got["automation_scenario_a_output"] != 615 || len(got) != 2 {
		t.Fatalf("values = %+v, want role:grid -470 and output 615", got)
	}
}

func TestHistorySamplerValuesSkipsANonNumericOutput(t *testing.T) {
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "automation"},
		Entity: registry.EntityInfo{
			UniqueID: "automation_scenario_a_output", Component: "sensor", ObjectID: "scenario_a_output",
			Name: "A Ausgabe", StateTopic: "automation/scenario_a_output", UnitOfMeasurement: "W",
		},
	})
	reg.UpdateTopic("automation/scenario_a_output", []byte("kaputt"), false, time.Now())
	configs := writeScenarioRules(t, threeScenarioRules)

	got := HistorySamplerValues(energy.Snapshot{}, reg, configs)
	if _, ok := got["automation_scenario_a_output"]; ok || len(got) != 0 {
		t.Fatalf("values = %+v, want no entry for a non-numeric output", got)
	}
}
