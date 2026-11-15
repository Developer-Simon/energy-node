package nodeagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ServiceManifest ist der Teil eines deployten Dienst-Manifests, den die
// Messung braucht. Unit ist der systemd-Unitname aus dem Feld "unit" des
// Manifests (etwa "shelly-rpc.service"). PollFloorS ist die kleinste
// Abfrageperiode in Sekunden, auf die der Dienst herunterregeln kann; 0
// heisst: das Manifest nennt keine.
type ServiceManifest struct {
	ServiceID  string
	Unit       string
	PollFloorS float64
}

// LoadServiceManifests liest <dir>/*.json (die deployten Manifeste unter
// /etc/energy-node/manifests/) und liefert die Dienste sortiert nach
// ServiceID. Ein fehlendes Verzeichnis ist kein Fehler - die
// Entwicklungsmaschine hat es nicht.
func LoadServiceManifests(dir string) ([]ServiceManifest, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifests []ServiceManifest
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m struct {
			ServiceID  string  `json:"service_id"`
			Unit       string  `json:"unit"`
			PollFloorS float64 `json:"poll_floor_s"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.ServiceID != "" {
			manifests = append(manifests, ServiceManifest{
				ServiceID:  m.ServiceID,
				Unit:       m.Unit,
				PollFloorS: m.PollFloorS,
			})
		}
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].ServiceID < manifests[j].ServiceID
	})
	return manifests, nil
}

// LoadServiceIDs liefert die sortierten service_id-Werte aus
// LoadServiceManifests.
func LoadServiceIDs(dir string) ([]string, error) {
	manifests, err := LoadServiceManifests(dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range manifests {
		ids = append(ids, m.ServiceID)
	}
	return ids, nil
}
