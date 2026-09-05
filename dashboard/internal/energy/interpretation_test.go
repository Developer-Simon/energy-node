package energy

import (
	"encoding/json"
	"testing"
)

// TestInterpretationNormalizedDefaultsThresholds covers the two Statuskarte
// thresholds added alongside the layout-editor options for the six
// alternative energy graphics (see
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md): they live on
// Interpretation, not on a layout.Item, because they are fachliche
// Schwellwerte rather than Darstellungsparameter.
func TestInterpretationNormalizedDefaultsThresholds(t *testing.T) {
	zero := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute}
	got := zero.Normalized()
	if got.SurplusThresholdW != 800 {
		t.Fatalf("surplus_threshold_w = %v, want 800", got.SurplusThresholdW)
	}
	if got.ImportThresholdW != 1500 {
		t.Fatalf("import_threshold_w = %v, want 1500", got.ImportThresholdW)
	}
}

func TestInterpretationNormalizedKeepsAnExplicitThreshold(t *testing.T) {
	cfg := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, SurplusThresholdW: 300, ImportThresholdW: 900}
	got := cfg.Normalized()
	if got.SurplusThresholdW != 300 {
		t.Fatalf("surplus_threshold_w = %v, want 300 (explicit value must survive normalization)", got.SurplusThresholdW)
	}
	if got.ImportThresholdW != 900 {
		t.Fatalf("import_threshold_w = %v, want 900 (explicit value must survive normalization)", got.ImportThresholdW)
	}
}

func TestInterpretationValidateRejectsNegativeThresholds(t *testing.T) {
	base := DefaultInterpretation()

	surplus := base
	surplus.SurplusThresholdW = -1
	if err := surplus.Validate(); err == nil {
		t.Fatal("expected an error for a negative surplus_threshold_w")
	}

	imp := base
	imp.ImportThresholdW = -1
	if err := imp.Validate(); err == nil {
		t.Fatal("expected an error for a negative import_threshold_w")
	}
}

func TestValidateAcceptsCombinedLoadMode(t *testing.T) {
	cfg := DefaultInterpretation()
	cfg.LoadMode = LoadModeCombined
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(load_mode=combined) = %v, want nil", err)
	}
}

func TestInterpretationDefaultsBatteryReserve(t *testing.T) {
	if got := DefaultInterpretation().BatteryReservePercent; got != 10 {
		t.Fatalf("BatteryReservePercent = %v, want 10", got)
	}
}

// Eine PUT-Nutzlast ohne das Feld darf die Reserve nicht abschalten - sie
// hat sie nie erwaehnt.
func TestInterpretationKeepsReserveWhenFieldAbsent(t *testing.T) {
	var got Interpretation
	if err := json.Unmarshal([]byte(`{"gap_mode":"diagnostic"}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.BatteryReservePercent != 10 {
		t.Fatalf("BatteryReservePercent = %v, want 10", got.BatteryReservePercent)
	}
}

// Eine ausdrueckliche 0 ist das Abschalten und muss stehenbleiben. Genau
// hier scheitert jede Loesung, die "nicht gesetzt" am Nullwert erkennt.
func TestInterpretationKeepsExplicitZeroReserve(t *testing.T) {
	var got Interpretation
	if err := json.Unmarshal([]byte(`{"battery_reserve_percent":0}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.BatteryReservePercent != 0 {
		t.Fatalf("BatteryReservePercent = %v, want 0", got.BatteryReservePercent)
	}
}
