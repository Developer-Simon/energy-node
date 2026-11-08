package energydiscovery

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// CategoryInput beschreibt eine Verbrauchskategorie, die einen optionalen
// Sensor bekommt. Storage ist nur fuer die Doku gedacht: alle Basen
// bekommen einen Sensor.
type CategoryInput struct {
	ID      string
	Label   string
	Storage bool
}

// ChainInput beschreibt eine Messkette, die drei Sensoren bekommt. Eine
// leere Bezeichnung nimmt die ID als Namen.
type ChainInput struct {
	ID    string
	Label string
}

// OptionalSensor ist ein Sensor, der nur angeboten wird, wenn die Rolle
// vorhanden ist (Wallbox, Waermepumpe), eine Kategorie existiert oder eine
// Messkette angelegt ist. Key ist die object_id und zugleich der Teil nach
// "energy_node_" in der unique_id. Name ist der Entitaetsname, template das
// value_template. Kind und Chain kennzeichnen Messketten-Sensoren, Label
// traegt die Kettenbezeichnung.
//
// deviceClass und unit setzen die Constructoren immer selbst. Ein leeres
// deviceClass wird nicht veroeffentlicht (Wirkungsgrad). Leeres stateClass
// heisst "measurement", leeres stateTopic heisst StateTopic.
type OptionalSensor struct {
	Key   string `json:"key"`
	Role  string `json:"role,omitempty"`
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind,omitempty"`  // "chain_efficiency" | "chain_loss" | "chain_loss_energy"
	Chain string `json:"chain,omitempty"` // Ketten-ID
	Name  string `json:"-"`

	template    string
	deviceClass string
	unit        string
	stateClass  string
	stateTopic  string
}

// OptionalSensors liefert die gewuenschten optionalen Sensoren in fester
// Reihenfolge: gemessene und berechnete Hauslast (nur mit Hauszaehler,
// ohne ihn waeren beide gleich der Hauslast), Wallbox, Waermepumpe, dann
// die Kategorien nach ID sortiert. Eine leere Kategorie-Bezeichnung nimmt
// die ID als Namen.
func OptionalSensors(houseMeter, wallbox, heatPump bool, categories []CategoryInput) []OptionalSensor {
	var out []OptionalSensor
	if houseMeter {
		out = append(out,
			OptionalSensor{
				Key:         "house_load_measured",
				Role:        "house_load_measured",
				Name:        "Hauslast gemessen",
				template:    "{{ value_json.balance['load_meter'] }}",
				deviceClass: "power",
				unit:        "W",
			},
			OptionalSensor{
				Key:         "house_load_calculated",
				Role:        "house_load_calculated",
				Name:        "Hauslast berechnet",
				template:    "{{ value_json.balance['load_calculated'] }}",
				deviceClass: "power",
				unit:        "W",
			},
		)
	}
	if wallbox {
		out = append(out, OptionalSensor{
			Key:         "wallbox",
			Role:        "wallbox",
			Name:        "Wallbox-Leistung",
			template:    "{{ value_json.balance['wallbox'] }}",
			deviceClass: "power",
			unit:        "W",
		})
	}
	if heatPump {
		out = append(out, OptionalSensor{
			Key:         "heat_pump",
			Role:        "heat_pump",
			Name:        "Wärmepumpen-Leistung",
			template:    "{{ value_json.balance['heat_pump'] }}",
			deviceClass: "power",
			unit:        "W",
		})
	}
	sorted := make([]CategoryInput, len(categories))
	copy(sorted, categories)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, c := range sorted {
		name := c.Label
		if name == "" {
			name = c.ID
		}
		out = append(out, OptionalSensor{
			Key:         "custom_" + c.ID,
			Label:       c.Label,
			Name:        name,
			template:    "{{ value_json.categories['" + c.ID + "'] }}",
			deviceClass: "power",
			unit:        "W",
		})
	}
	return out
}

// ChainSensors liefert je Messkette den Wirkungsgrad, den Verlust in W und
// die Verlustenergie in kWh. Die Ketten sind nach ID sortiert, je Kette
// kommen die drei Sensoren in dieser Reihenfolge. Sie lesen aus dem
// ChainsTopic, nicht aus StateTopic.
func ChainSensors(chains []ChainInput) []OptionalSensor {
	sorted := make([]ChainInput, len(chains))
	copy(sorted, chains)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var out []OptionalSensor
	for _, c := range sorted {
		name := c.Label
		if name == "" {
			name = c.ID
		}
		tmpl := func(field string) string {
			return "{{ value_json.chains['" + c.ID + "']['" + field + "'] }}"
		}
		out = append(out,
			OptionalSensor{
				Key:        "chain_" + c.ID + "_efficiency",
				Kind:       "chain_efficiency",
				Chain:      c.ID,
				Label:      c.Label,
				Name:       name + " Wirkungsgrad",
				template:   tmpl("efficiency_pct"),
				unit:       "%",
				stateClass: "measurement",
				stateTopic: ChainsTopic,
			},
			OptionalSensor{
				Key:         "chain_" + c.ID + "_loss",
				Kind:        "chain_loss",
				Chain:       c.ID,
				Label:       c.Label,
				Name:        name + " Verlust",
				template:    tmpl("loss_w"),
				deviceClass: "power",
				unit:        "W",
				stateClass:  "measurement",
				stateTopic:  ChainsTopic,
			},
			OptionalSensor{
				Key:         "chain_" + c.ID + "_loss_energy",
				Kind:        "chain_loss_energy",
				Chain:       c.ID,
				Label:       c.Label,
				Name:        name + " Verlustenergie",
				template:    tmpl("loss_kwh_total"),
				deviceClass: "energy",
				unit:        "kWh",
				stateClass:  "total_increasing",
				stateTopic:  ChainsTopic,
			},
		)
	}
	return out
}

// Filter entfernt die Sensoren, deren Schluessel in enabled auf false steht.
// Ein fehlender Schluessel gilt als an.
func Filter(sensors []OptionalSensor, enabled map[string]bool) []OptionalSensor {
	var out []OptionalSensor
	for _, s := range sensors {
		if on, ok := enabled[s.Key]; ok && !on {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Announced merkt sich, welche optionalen Sensoren mit welchem Namen
// angekündigt sind. Nur so kann ein Sensor, dessen Kategorie geloescht
// wurde, nach einem Neustart noch mit einer leeren Config entfernt werden.
type Announced struct {
	mu    sync.Mutex
	path  string
	known map[string]string
}

// LoadAnnounced liest den gespeicherten Stand aus path. Fehlt die Datei oder
// ist sie kaputt, gilt der Stand als leer.
func LoadAnnounced(path string) *Announced {
	a := &Announced{path: path, known: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return a
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("energydiscovery: announced state %s unreadable, starting empty: %v", path, err)
		return a
	}
	if m != nil {
		a.known = m
	}
	return a
}

// Plan liefert die Nachrichten fuer den Abgleich mit desired. Neue oder
// umbenannte Sensoren bekommen ihre Config (bei force alle gewuenschten),
// in der Reihenfolge von desired. Danach folgen leere Payloads fuer die
// gespeicherten Sensoren, die nicht mehr gewuenscht sind, nach Key sortiert.
// Die zurueckgegebene Funktion uebernimmt den neuen Stand und schreibt ihn
// atomar nach path. Der Aufrufer ruft sie erst, wenn alle Nachrichten
// veroeffentlicht wurden. Schreibfehler werden geloggt.
func (a *Announced) Plan(prefix, swVersion, configurationURL string, desired []OptionalSensor, force bool) ([]Message, func()) {
	a.mu.Lock()
	known := make(map[string]string, len(a.known))
	for k, v := range a.known {
		known[k] = v
	}
	a.mu.Unlock()

	device := deviceBlock(swVersion, configurationURL)
	want := make(map[string]string, len(desired))
	var msgs []Message
	for _, s := range desired {
		want[s.Key] = s.Name
		if !force && known[s.Key] == s.Name {
			continue
		}
		stateClass, stateTopic := s.stateClass, s.stateTopic
		if stateClass == "" {
			stateClass = "measurement"
		}
		if stateTopic == "" {
			stateTopic = StateTopic
		}
		msgs = append(msgs, Message{
			Topic:   DiscoveryTopic(prefix, s.Key),
			Payload: sensorPayload(s.Name, DeviceID+"_"+s.Key, s.template, s.deviceClass, s.unit, stateClass, stateTopic, device),
		})
	}

	var gone []string
	for key := range known {
		if _, ok := want[key]; !ok {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		msgs = append(msgs, Message{Topic: DiscoveryTopic(prefix, key)})
	}

	commit := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.known = want
		a.save()
	}
	return msgs, commit
}

// save schreibt den Stand atomar (tmp + rename). Fehler werden nur geloggt.
// Der Aufrufer haelt a.mu.
func (a *Announced) save() {
	data, err := json.Marshal(a.known)
	if err != nil {
		log.Printf("energydiscovery: marshal announced state: %v", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.path), filepath.Base(a.path)+".tmp-*")
	if err != nil {
		log.Printf("energydiscovery: write announced state %s: %v", a.path, err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		log.Printf("energydiscovery: write announced state %s: %v", a.path, err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		log.Printf("energydiscovery: write announced state %s: %v", a.path, err)
		return
	}
	if err := os.Rename(tmpName, a.path); err != nil {
		os.Remove(tmpName)
		log.Printf("energydiscovery: write announced state %s: %v", a.path, err)
	}
}
