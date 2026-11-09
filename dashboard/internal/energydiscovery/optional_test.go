package energydiscovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOptionalSensorsOrderKeysAndTemplates(t *testing.T) {
	got := OptionalSensors(false, true, false, []CategoryInput{{ID: "sauna", Label: "Sauna"}, {ID: "akku", Label: "Akku", Storage: true}})
	keys := []string{}
	for _, s := range got {
		keys = append(keys, s.Key)
	}
	if want := []string{"wallbox", "custom_akku", "custom_sauna"}; !equal(keys, want) {
		t.Fatalf("keys %v", keys)
	}
	msgs, _ := LoadAnnounced(filepath.Join(t.TempDir(), "a.json")).Plan("homeassistant", "1.0", "", got, false)
	var payload map[string]any
	json.Unmarshal(msgs[2].Payload, &payload)
	if msgs[2].Topic != "homeassistant/sensor/energy_node/custom_sauna/config" ||
		payload["unique_id"] != "energy_node_custom_sauna" || payload["name"] != "Sauna" ||
		payload["value_template"] != "{{ value_json.categories['sauna'] }}" {
		t.Fatalf("%s %v", msgs[2].Topic, payload)
	}
}

func TestFilterTreatsMissingKeyAsEnabled(t *testing.T) {
	got := Filter(OptionalSensors(false, true, true, nil), map[string]bool{"wallbox": false})
	if len(got) != 1 || got[0].Key != "heat_pump" {
		t.Fatalf("%v", got)
	}
}

func TestAnnouncedRemovesVanishedSensorsAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "announced.json")
	first := LoadAnnounced(path)
	_, commit := first.Plan("homeassistant", "1.0", "", OptionalSensors(false, false, false, []CategoryInput{{ID: "sauna", Label: "Sauna"}}), false)
	commit()
	// Neustart, Kategorie inzwischen geloescht.
	msgs, commit2 := LoadAnnounced(path).Plan("homeassistant", "1.0", "", nil, true)
	if len(msgs) != 1 || msgs[0].Topic != "homeassistant/sensor/energy_node/custom_sauna/config" || len(msgs[0].Payload) != 0 {
		t.Fatalf("%+v", msgs)
	}
	commit2()
	if msgs, _ := LoadAnnounced(path).Plan("homeassistant", "1.0", "", nil, true); len(msgs) != 0 {
		t.Fatalf("%+v", msgs)
	}
}

func TestAnnouncedRepublishesOnlyChanges(t *testing.T) {
	a := LoadAnnounced(filepath.Join(t.TempDir(), "a.json"))
	sensors := OptionalSensors(false, false, false, []CategoryInput{{ID: "sauna", Label: "Sauna"}})
	_, commit := a.Plan("homeassistant", "1.0", "", sensors, false)
	commit()
	if msgs, _ := a.Plan("homeassistant", "1.0", "", sensors, false); len(msgs) != 0 {
		t.Fatalf("unveraendert: %+v", msgs)
	}
	renamed := OptionalSensors(false, false, false, []CategoryInput{{ID: "sauna", Label: "Dampfbad"}})
	if msgs, _ := a.Plan("homeassistant", "1.0", "", renamed, false); len(msgs) != 1 || len(msgs[0].Payload) == 0 {
		t.Fatalf("umbenannt: %+v", msgs)
	}
}

func TestAnnouncedWithoutCommitKeepsOldState(t *testing.T) {
	a := LoadAnnounced(filepath.Join(t.TempDir(), "a.json"))
	sensors := OptionalSensors(false, true, false, nil)
	a.Plan("homeassistant", "1.0", "", sensors, false) // Veroeffentlichen fehlgeschlagen
	if msgs, _ := a.Plan("homeassistant", "1.0", "", sensors, false); len(msgs) != 1 {
		t.Fatalf("%+v", msgs)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOptionalSensorsOfferBothHouseLoadsWithAMeter(t *testing.T) {
	got := OptionalSensors(true, true, false, nil)
	keys := []string{}
	for _, s := range got {
		keys = append(keys, s.Key)
	}
	if want := []string{"house_load_measured", "house_load_calculated", "wallbox"}; !equal(keys, want) {
		t.Fatalf("keys %v", keys)
	}
	msgs, _ := LoadAnnounced(filepath.Join(t.TempDir(), "a.json")).Plan("homeassistant", "1.0", "", got[:2], false)
	var measured, calculated map[string]any
	json.Unmarshal(msgs[0].Payload, &measured)
	json.Unmarshal(msgs[1].Payload, &calculated)
	if msgs[0].Topic != "homeassistant/sensor/energy_node/house_load_measured/config" ||
		measured["unique_id"] != "energy_node_house_load_measured" || measured["name"] != "Hauslast gemessen" ||
		measured["value_template"] != "{{ value_json.balance['load_meter'] }}" {
		t.Fatalf("measured %+v", measured)
	}
	if calculated["unique_id"] != "energy_node_house_load_calculated" || calculated["name"] != "Hauslast berechnet" ||
		calculated["value_template"] != "{{ value_json.balance['load_calculated'] }}" {
		t.Fatalf("calculated %+v", calculated)
	}
}

func TestChainSensors(t *testing.T) {
	got := ChainSensors([]ChainInput{{ID: "b", Label: "Balkon"}, {ID: "a"}})
	keys := []string{}
	for _, s := range got {
		keys = append(keys, s.Key)
	}
	want := []string{"chain_a_efficiency", "chain_a_loss", "chain_a_loss_energy", "chain_b_efficiency", "chain_b_loss", "chain_b_loss_energy"}
	if !equal(keys, want) {
		t.Fatalf("keys %v", keys)
	}
	names := []string{}
	for _, s := range got {
		names = append(names, s.Name)
	}
	wantNames := []string{"a Wirkungsgrad", "a Verlust", "a Verlustenergie", "Balkon Wirkungsgrad", "Balkon Verlust", "Balkon Verlustenergie"}
	if !equal(names, wantNames) {
		t.Fatalf("names %v", names)
	}
	if got[3].Kind != "chain_efficiency" || got[3].Chain != "b" || got[3].Label != "Balkon" {
		t.Fatalf("balkon efficiency %+v", got[3])
	}
}

func TestPlanChainSensorPayloads(t *testing.T) {
	msgs, _ := LoadAnnounced(filepath.Join(t.TempDir(), "a.json")).Plan("homeassistant", "1.0", "", ChainSensors([]ChainInput{{ID: "a", Label: "Balkon"}}), false)
	if len(msgs) != 3 {
		t.Fatalf("%+v", msgs)
	}
	decode := func(m Message) map[string]any {
		var p map[string]any
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	eff, loss, energy := decode(msgs[0]), decode(msgs[1]), decode(msgs[2])
	if _, ok := eff["device_class"]; ok {
		t.Fatalf("efficiency has device_class: %v", eff)
	}
	if eff["unit_of_measurement"] != "%" || eff["state_class"] != "measurement" || eff["state_topic"] != ChainsTopic ||
		eff["value_template"] != "{{ value_json.chains['a']['efficiency_pct'] }}" {
		t.Fatalf("efficiency %v", eff)
	}
	if loss["device_class"] != "power" || loss["unit_of_measurement"] != "W" || loss["state_class"] != "measurement" ||
		loss["state_topic"] != ChainsTopic || loss["value_template"] != "{{ value_json.chains['a']['loss_w'] }}" {
		t.Fatalf("loss %v", loss)
	}
	if energy["device_class"] != "energy" || energy["unit_of_measurement"] != "kWh" || energy["state_class"] != "total_increasing" ||
		energy["state_topic"] != ChainsTopic || energy["value_template"] != "{{ value_json.chains['a']['loss_kwh_total'] }}" {
		t.Fatalf("loss energy %v", energy)
	}
	if msgs[2].Topic != "homeassistant/sensor/energy_node/chain_a_loss_energy/config" || energy["unique_id"] != "energy_node_chain_a_loss_energy" {
		t.Fatalf("%s %v", msgs[2].Topic, energy)
	}
}

// Die optionalen Sensoren, die es schon vor den Messketten gab, muessen
// byte-gleich zu den Golden-Dateien bleiben.
func TestOptionalSensorPayloadsMatchGolden(t *testing.T) {
	sensors := OptionalSensors(true, true, false, []CategoryInput{{ID: "sauna", Label: "Sauna"}})
	msgs, _ := LoadAnnounced(filepath.Join(t.TempDir(), "a.json")).Plan("homeassistant", "1.2.3-test", "", sensors, true)
	golden := map[string]string{
		"homeassistant/sensor/energy_node/wallbox/config":             "optional_wallbox.config.json",
		"homeassistant/sensor/energy_node/custom_sauna/config":        "optional_custom_sauna.config.json",
		"homeassistant/sensor/energy_node/house_load_measured/config": "optional_house_load_measured.config.json",
	}
	seen := 0
	for _, m := range msgs {
		file, ok := golden[m.Topic]
		if !ok {
			continue
		}
		seen++
		want, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		if string(m.Payload) != string(want) {
			t.Fatalf("%s:\n got: %s\nwant: %s", m.Topic, m.Payload, want)
		}
	}
	if seen != len(golden) {
		t.Fatalf("saw %d of %d golden topics: %+v", seen, len(golden), msgs)
	}
}

func TestPlanRemovesChainSensorsOfDeletedChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "announced.json")
	_, commit := LoadAnnounced(path).Plan("homeassistant", "1.0", "", ChainSensors([]ChainInput{{ID: "a", Label: "Balkon"}}), false)
	commit()
	msgs, _ := LoadAnnounced(path).Plan("homeassistant", "1.0", "", nil, false)
	want := []string{
		"homeassistant/sensor/energy_node/chain_a_efficiency/config",
		"homeassistant/sensor/energy_node/chain_a_loss/config",
		"homeassistant/sensor/energy_node/chain_a_loss_energy/config",
	}
	if len(msgs) != len(want) {
		t.Fatalf("%+v", msgs)
	}
	for i, m := range msgs {
		if m.Topic != want[i] || len(m.Payload) != 0 {
			t.Fatalf("%d: %s %q", i, m.Topic, m.Payload)
		}
	}
}
