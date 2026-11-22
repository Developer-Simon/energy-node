package learnrec

import (
	"math"
	"strconv"
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
	// OutdoorC ist das Minutenmittel der Aussentemperatur in Grad Celsius.
	// nil, wenn keine Entitaet gewaehlt ist oder sie keinen Wert liefert.
	OutdoorC *float64
	// Replayed ist gesetzt, wenn der Rahmen aus der Aufzeichnung kommt (Hub.Replay).
	// Dort ist jede Entitaet ein eigenes Geraet mit ID = unique_id.
	Replayed bool
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

// TempOf liefert die Temperatur in Grad Celsius. Veraltete, nicht verfuegbare,
// fehlende oder nicht numerische Werte sowie NaN und Inf fallen weg. Fahrenheit
// wird umgerechnet. Andere Einheiten ergeben ok=false.
func TempOf(e registry.EntityView) (float64, bool) {
	if e.Stale || (e.HasAvailability && !e.Available) || !e.HasValue {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(e.Value), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	switch strings.TrimSpace(e.UnitOfMeasurement) {
	case "", "°C", "C":
		return v, true
	case "°F", "F":
		return (v - 32) * 5 / 9, true
	}
	return 0, false
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
