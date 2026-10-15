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

// OptionalSensor ist ein Sensor, der nur angeboten wird, wenn die Rolle
// vorhanden ist (Wallbox, Waermepumpe) oder eine Kategorie existiert. Key
// ist die object_id und zugleich der Teil nach "energy_node_" in der
// unique_id. Name ist der Entitaetsname, template das value_template.
type OptionalSensor struct {
	Key      string `json:"key"`
	Role     string `json:"role,omitempty"`
	Label    string `json:"label,omitempty"`
	Name     string `json:"-"`
	template string
}

// OptionalSensors liefert die gewuenschten optionalen Sensoren in fester
// Reihenfolge: Wallbox, Waermepumpe, dann die Kategorien nach ID sortiert.
// Eine leere Kategorie-Bezeichnung nimmt die ID als Namen.
func OptionalSensors(wallbox, heatPump bool, categories []CategoryInput) []OptionalSensor {
	var out []OptionalSensor
	if wallbox {
		out = append(out, OptionalSensor{
			Key:      "wallbox",
			Role:     "wallbox",
			Name:     "Wallbox-Leistung",
			template: "{{ value_json.balance['wallbox'] }}",
		})
	}
	if heatPump {
		out = append(out, OptionalSensor{
			Key:      "heat_pump",
			Role:     "heat_pump",
			Name:     "Wärmepumpen-Leistung",
			template: "{{ value_json.balance['heat_pump'] }}",
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
			Key:      "custom_" + c.ID,
			Role:     "category",
			Label:    c.Label,
			Name:     name,
			template: "{{ value_json.categories['" + c.ID + "'] }}",
		})
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
		msgs = append(msgs, Message{
			Topic:   DiscoveryTopic(prefix, s.Key),
			Payload: sensorPayload(s.Name, DeviceID+"_"+s.Key, s.template, "power", "W", device),
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
