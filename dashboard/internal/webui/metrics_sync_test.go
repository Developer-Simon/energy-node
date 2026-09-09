package webui

import (
	"os"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
)

// TestMetricKeysMatchNodeagent keeps the metricKeys array in mqtt.page.js
// word-for-word in sync with nodeagent.Metrics - the MQTT tab renders one
// per-metric publish toggle per entry, and a drifted list would silently
// drop or invent switches.
func TestMetricKeysMatchNodeagent(t *testing.T) {
	js, err := os.ReadFile("static/js/mqtt.page.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range nodeagent.Metrics {
		if !strings.Contains(string(js), "'"+m+"'") {
			t.Errorf("mqtt.page.js metricKeys is missing %q", m)
		}
	}
}
