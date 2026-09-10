package nodeagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// LoadServiceIDs liest <dir>/*.json (die deployten Manifeste unter
// /etc/energy-node/manifests/) und liefert die sortierten service_id-Werte.
// Ein fehlendes Verzeichnis ist kein Fehler - die Entwicklungsmaschine hat
// es nicht.
func LoadServiceIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m struct {
			ServiceID string `json:"service_id"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.ServiceID != "" {
			ids = append(ids, m.ServiceID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
