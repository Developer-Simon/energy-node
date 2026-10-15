package energydiscovery

import (
	"encoding/json"
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
