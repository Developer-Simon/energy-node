package energy

import (
	"encoding/json"
	"errors"
	"fmt"
)

// GapMode controls how DeriveBalance treats a gap between sources and sinks
// that survives GapToleranceW/GapTolerancePercent.
type GapMode string

// LoadMode controls whether Balance.LoadTotal comes from a measured "load"
// role, is calculated from the other roles, or picks whichever is available.
type LoadMode string

// ToleranceMode selects whether GapToleranceW is read directly or derived
// from GapTolerancePercent of Balance.Total.
type ToleranceMode string

const (
	GapModeUnknownConsumer GapMode = "unknown_consumer"
	GapModeDiagnostic      GapMode = "diagnostic"

	LoadModeMeasured   LoadMode = "measured"
	LoadModeCalculated LoadMode = "calculated"
	LoadModeAuto       LoadMode = "auto"
	// LoadModeCombined rechnet den Hausverbrauch wie LoadModeCalculated aus
	// der Bilanz, trennt aber die ueber die Rolle "load" gemessenen
	// Teilverbraucher als eigene Position (Balance.LoadMeasured) vom
	// "Uebrigen Verbrauch" ab. Die Rolle "load" bedeutet in diesem Modus
	// *gemessener Teilverbraucher*, nicht *Hausverbrauch gesamt* - ein
	// Hauszaehler dort wuerde doppelt zaehlen.
	LoadModeCombined LoadMode = "combined"

	ToleranceModeAbsolute ToleranceMode = "absolute"
	ToleranceModePercent  ToleranceMode = "percent"
)

// Interpretation is the user-configurable half of DeriveBalance: it decides
// how an unassigned power gap and the household load are read, on top of
// the raw role values a Snapshot already carries. See
// knowhow/dashboard/energie-interpretation.md for the full semantics.
//
// SurplusThresholdW/ImportThresholdW belong to the Statuskarte
// (energy_status layout item, see energy-status.js) rather than to
// DeriveBalance - they live here anyway because they are fachliche
// Schwellwerte ("what do the roles mean"), not Darstellungsparameter ("how
// does the tile look"), the same distinction that keeps GapToleranceW out of
// the layout editor. See knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
type Interpretation struct {
	GapMode             GapMode       `json:"gap_mode"`
	LoadMode            LoadMode      `json:"load_mode"`
	GapToleranceMode    ToleranceMode `json:"gap_tolerance_mode"`
	GapToleranceW       float64       `json:"gap_tolerance_w"`
	GapTolerancePercent float64       `json:"gap_tolerance_percent"`
	SurplusThresholdW   float64       `json:"surplus_threshold_w"`
	ImportThresholdW    float64       `json:"import_threshold_w"`
	// BatteryReservePercent ist der Ladestand, unterhalb dessen der
	// Speicher als aufgebraucht gilt - die Notreserve fuer den
	// Netzausfall. Die Batteriekarten rechnen ihre Restlaufzeit gegen
	// diese Grenze statt gegen 0 %.
	//
	// 0 heisst: keine Reserve, der Speicher darf bis 0 % gerechnet werden.
	// Ein zweites Ein/Aus-Feld gibt es bewusst nicht - es waere eine
	// zweite Wahrheit ueber denselben Sachverhalt. Dass die 0 nicht als
	// "nicht gesetzt" missverstanden wird, traegt UnmarshalJSON: es legt
	// DefaultInterpretation() unter die Dekodierung, ein fehlendes Feld
	// bekommt also die 10 und eine gesendete 0 bleibt eine 0.
	BatteryReservePercent float64 `json:"battery_reserve_percent"`
}

// DefaultInterpretation is both the zero-config default and what a field
// absent from a hand-edited or pre-upgrade energy.json falls back to - same
// role as DefaultMQTT/DefaultBridgeConnection in package settings.
func DefaultInterpretation() Interpretation {
	return Interpretation{
		GapMode:               GapModeUnknownConsumer,
		LoadMode:              LoadModeAuto,
		GapToleranceMode:      ToleranceModeAbsolute,
		GapToleranceW:         25,
		GapTolerancePercent:   2,
		SurplusThresholdW:     800,
		ImportThresholdW:      1500,
		BatteryReservePercent: 10,
	}
}

// UnmarshalJSON applies DefaultInterpretation() to any field absent from
// data before decoding, the same alias trick as settings.MQTTConfig -
// otherwise a PUT that only sends {"gap_mode": "diagnostic"} would zero out
// every other field instead of defaulting it.
func (i *Interpretation) UnmarshalJSON(data []byte) error {
	type interpretationAlias Interpretation
	value := DefaultInterpretation()
	if err := json.Unmarshal(data, (*interpretationAlias)(&value)); err != nil {
		return err
	}
	*i = value
	return nil
}

// Normalized fills in defaults for an Interpretation that never went through
// UnmarshalJSON at all - the "interpretation" key entirely absent from a
// hand-written or pre-upgrade energy.json leaves the Go zero value instead
// of invoking the alias-default trick above.
func (i Interpretation) Normalized() Interpretation {
	d := DefaultInterpretation()
	// Check if UnmarshalJSON was called by looking for empty enum fields
	// (UnmarshalJSON always fills these in).
	unmarshalCalled := i.GapMode != ""

	if i.GapMode == "" {
		i.GapMode = d.GapMode
	}
	if i.LoadMode == "" {
		i.LoadMode = d.LoadMode
	}
	if i.GapToleranceMode == "" {
		i.GapToleranceMode = d.GapToleranceMode
	}
	if i.GapToleranceW == 0 && i.GapTolerancePercent == 0 {
		i.GapToleranceW = d.GapToleranceW
		i.GapTolerancePercent = d.GapTolerancePercent
	}
	if i.SurplusThresholdW == 0 {
		i.SurplusThresholdW = d.SurplusThresholdW
	}
	if i.ImportThresholdW == 0 {
		i.ImportThresholdW = d.ImportThresholdW
	}
	// BatteryReservePercent is only filled with a default if UnmarshalJSON was
	// definitely not called. When UnmarshalJSON was called, it already applied
	// the default for absent fields (leaving 10) or preserved explicitly-sent 0.
	if !unmarshalCalled && i.BatteryReservePercent == 0 {
		i.BatteryReservePercent = d.BatteryReservePercent
	}
	return i
}

// Validate rejects values the flat JSON schema (config.validateValue has no
// oneOf/pattern/$ref) cannot express on its own - see settings.validateEnergy.
func (i Interpretation) Validate() error {
	switch i.GapMode {
	case GapModeUnknownConsumer, GapModeDiagnostic:
	default:
		return fmt.Errorf("energy: unknown gap_mode %q", i.GapMode)
	}
	switch i.LoadMode {
	case LoadModeMeasured, LoadModeCalculated, LoadModeAuto, LoadModeCombined:
	default:
		return fmt.Errorf("energy: unknown load_mode %q", i.LoadMode)
	}
	switch i.GapToleranceMode {
	case ToleranceModeAbsolute, ToleranceModePercent:
	default:
		return fmt.Errorf("energy: unknown gap_tolerance_mode %q", i.GapToleranceMode)
	}
	if i.GapToleranceW < 0 {
		return errors.New("energy: gap_tolerance_w must not be negative")
	}
	if i.GapTolerancePercent < 0 || i.GapTolerancePercent > 100 {
		return errors.New("energy: gap_tolerance_percent must be between 0 and 100")
	}
	if i.SurplusThresholdW < 0 {
		return errors.New("energy: surplus_threshold_w must not be negative")
	}
	if i.ImportThresholdW < 0 {
		return errors.New("energy: import_threshold_w must not be negative")
	}
	return nil
}
