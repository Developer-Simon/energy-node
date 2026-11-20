package learnrec

import (
	"math"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// Frame ist der abgeschlossene Minutenrahmen aller Geraete.
type Frame struct {
	Minute  time.Time      // lokaler Minutenbeginn
	Devices []DeviceMinute // nach ID sortiert
	Balance map[string]float64
}

// DeviceMinute enthaelt die Minutenwerte eines Geraets.
type DeviceMinute struct {
	ID        string
	Power     map[string]float64 // Minutenmittel |W| je Entitaet
	Switch    map[string]bool    // Zustand am Minutenende je Schalt-Entitaet
	Reachable bool
}

// PowerOf liefert den Betrag der Leistung in W. Veraltete, nicht verfuegbare,
// fehlende oder nicht als Leistung lesbare Werte sowie NaN und Inf fallen weg.
func PowerOf(e registry.EntityView) (float64, bool) {
	if e.Stale || (e.HasAvailability && !e.Available) || !e.HasValue {
		return 0, false
	}
	w, ok := energy.PowerW(e.Value, e.UnitOfMeasurement)
	if !ok || math.IsNaN(w) || math.IsInf(w, 0) {
		return 0, false
	}
	return math.Abs(w), true
}

// SwitchOf liest den Schaltzustand von switch, light und fan. Andere
// Komponenten und unbekannte Werte liefern ok=false.
func SwitchOf(e registry.EntityView) (on bool, ok bool) {
	switch e.Component {
	case "switch", "light", "fan":
	default:
		return false, false
	}
	if !e.HasValue {
		return false, false
	}
	if e.PayloadOn != "" && e.Value == e.PayloadOn {
		return true, true
	}
	if e.PayloadOff != "" && e.Value == e.PayloadOff {
		return false, true
	}
	switch strings.ToLower(e.Value) {
	case "on", "true", "1":
		return true, true
	case "off", "false", "0":
		return false, true
	}
	return false, false
}
