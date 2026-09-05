package diagnostics

import "github.com/Developer-Simon/energy-node-dashboard/internal/registry"

// WorstDevice ist das Geraet mit dem niedrigsten Gesundheitswert - die eine
// Zeile unter den Schweregrad-Zahlen auf der Diagnose-Kachel.
type WorstDevice struct {
	Name        string `json:"name"`
	Score       int    `json:"score"`
	StatusClass string `json:"status_class"` // "bad"/"warn"/"ok"/"unknown"
}

// Summary ist die Diagnose-Kachel in Zahlen. Sie lag bis 2026-08 privat in
// webui.go und war damit fuer den SSE-Handler unerreichbar; seit dem
// Uebersichts-Push braucht httpapi sie ebenfalls. Gerechnet wird sie einmal
// je Registry-Aenderung statt einmal je Client - das war der Grund, sie hier
// zu behalten statt sie nach JavaScript zu portieren.
//
// Die Go-Feldnamen sind der Vertrag mit "diagnostics-summary-card" in
// overview.html, die JSON-Namen der mit overview-values.js.
type Summary struct {
	Critical    int          `json:"critical"`
	Warning     int          `json:"warning"`
	Info        int          `json:"info"`
	StatusClass string       `json:"status_class"` // Kopfpunkt: "bad"/"warn"/"info"/"ok"
	WorstDevice *WorstDevice `json:"worst_device,omitempty"`
}

// Summarize zaehlt Schweregrade, leitet daraus die Farbe des Kopfpunkts ab
// und sucht das schlechteste Geraet. Warnungen und Health kommen aus
// derselben Engine wie /api/v1/diagnostics und /api/v1/diagnostics/health -
// die Kachel kann deshalb nicht von der Diagnose-Ansicht abweichen.
func Summarize(warnings []Warning, health []DeviceHealth, devices []registry.DeviceView) Summary {
	var summary Summary
	for _, item := range warnings {
		switch item.Severity {
		case SeverityCritical:
			summary.Critical++
		case SeverityWarning:
			summary.Warning++
		case SeverityInfo:
			summary.Info++
		}
	}
	switch {
	case summary.Critical > 0:
		summary.StatusClass = "bad"
	case summary.Warning > 0:
		summary.StatusClass = "warn"
	case summary.Info > 0:
		summary.StatusClass = "info"
	default:
		summary.StatusClass = "ok"
	}

	var worst *DeviceHealth
	for i := range health {
		if worst == nil || health[i].Score < worst.Score {
			worst = &health[i]
		}
	}
	if worst != nil {
		name := worst.DeviceID
		for _, device := range devices {
			if device.ID == worst.DeviceID && device.Name != "" {
				name = device.Name
				break
			}
		}
		summary.WorstDevice = &WorstDevice{Name: name, Score: worst.Score, StatusClass: statusClass(worst.Status)}
	}
	return summary
}

// statusClass bildet DeviceHealth.Status (healthStatus() plus die
// "critical"/"unknown"-Zustaende, die Health() ebenfalls setzt) auf die drei
// Statusfarben ab, die das Dashboard sonst ueberall benutzt.
func statusClass(status string) string {
	switch status {
	case "healthy":
		return "ok"
	case "degraded":
		return "warn"
	case "unhealthy", "critical":
		return "bad"
	}
	return "unknown"
}
