package diagnostics

import (
	"encoding/json"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestSummarizeCountsSeveritiesAndPicksStatusClass(t *testing.T) {
	cases := []struct {
		name     string
		warnings []Warning
		want     string
	}{
		{"leer ist ok", nil, "ok"},
		{"nur Hinweise", []Warning{{Severity: SeverityInfo}}, "info"},
		{"eine Warnung schlaegt Hinweise", []Warning{{Severity: SeverityInfo}, {Severity: SeverityWarning}}, "warn"},
		{"kritisch schlaegt alles", []Warning{{Severity: SeverityInfo}, {Severity: SeverityWarning}, {Severity: SeverityCritical}}, "bad"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.warnings, nil, nil).StatusClass; got != tc.want {
				t.Errorf("StatusClass = %q, erwartet %q", got, tc.want)
			}
		})
	}

	summary := Summarize([]Warning{{Severity: SeverityInfo}, {Severity: SeverityInfo}, {Severity: SeverityCritical}}, nil, nil)
	if summary.Info != 2 || summary.Critical != 1 || summary.Warning != 0 {
		t.Errorf("Zaehler falsch: %+v", summary)
	}
}

func TestSummarizeNamesTheWorstDevice(t *testing.T) {
	health := []DeviceHealth{
		{DeviceID: "dev-a", Score: 90, Status: "healthy"},
		{DeviceID: "dev-b", Score: 40, Status: "degraded"},
	}
	devices := []registry.DeviceView{{ID: "dev-b", Name: "Speicher"}}

	summary := Summarize(nil, health, devices)

	if summary.WorstDevice == nil {
		t.Fatal("kein schlechtestes Geraet gemeldet")
	}
	if summary.WorstDevice.Name != "Speicher" || summary.WorstDevice.Score != 40 || summary.WorstDevice.StatusClass != "warn" {
		t.Errorf("falsches Geraet: %+v", *summary.WorstDevice)
	}
}

// Ohne passenden Registry-Eintrag bleibt die ID stehen - eine leere Zeile
// waere auf der Kachel schlimmer als eine technische ID.
func TestSummarizeFallsBackToDeviceID(t *testing.T) {
	summary := Summarize(nil, []DeviceHealth{{DeviceID: "dev-x", Score: 10, Status: "critical"}}, nil)
	if summary.WorstDevice == nil || summary.WorstDevice.Name != "dev-x" || summary.WorstDevice.StatusClass != "bad" {
		t.Errorf("Rueckfall auf die DeviceID fehlt: %+v", summary.WorstDevice)
	}
}

// Der SSE-Rumpf traegt die Zusammenfassung als JSON; ohne Warnungen und ohne
// Health darf kein "worst_device": null darin stehen.
func TestSummaryJSONOmitsEmptyWorstDevice(t *testing.T) {
	data, err := json.Marshal(Summarize(nil, nil, nil))
	if err != nil {
		t.Fatalf("Marshal fehlgeschlagen: %v", err)
	}
	want := `{"critical":0,"warning":0,"info":0,"status_class":"ok"}`
	if string(data) != want {
		t.Errorf("JSON = %s, erwartet %s", data, want)
	}
}
