package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
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
