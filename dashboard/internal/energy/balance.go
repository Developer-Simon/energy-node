package energy

import "math"

// Balance is the canonical energy-balance derivation, computed server-side
// so the automation engine (Teil B) and all six dashboard tiles agree on
// exactly the same numbers. See knowhow/dashboard/energie-interpretation.md
// for the full semantics; this struct and DeriveBalance are its
// implementation. JS keeps a parametrized mirror (energy-model.js
// deriveBalance()) only for history points reconstructed client-side from
// IndexedDB - internal/energy/testdata/balance-cases.json is what keeps the
// two from silently drifting apart.
type Balance struct {
	PV               float64 `json:"pv"`
	GridImport       float64 `json:"grid_import"`
	GridExport       float64 `json:"grid_export"`
	BatteryCharge    float64 `json:"battery_charge"`
	BatteryDischarge float64 `json:"battery_discharge"`
	Wallbox          float64 `json:"wallbox"`
	HeatPump         float64 `json:"heat_pump"`
	LoadTotal        float64 `json:"load_total"`
	LoadSource       string  `json:"load_source"`
	// LoadMeasured ist der im Modus LoadModeCombined aus dem "Uebrigen
	// Verbrauch" herausgeloeste, ueber die Rolle "load" gemessene Anteil.
	// In jedem anderen load_mode ist er 0 - dort ist "load" entweder der
	// Gesamtverbrauch selbst oder gar nicht beteiligt.
	LoadMeasured   float64 `json:"load_measured"`
	Base           float64 `json:"base"`
	GapRaw         float64 `json:"gap_raw"`
	GapApplied     float64 `json:"gap_applied"`
	GapAbsorbed    float64 `json:"gap_absorbed"`
	GapToleranceW  float64 `json:"gap_tolerance_w"`
	GapIgnored     bool    `json:"gap_ignored"`
	Unbalanced     bool    `json:"unbalanced"`
	Autarkie       float64 `json:"autarkie"`
	Eigenverbrauch float64 `json:"eigenverbrauch"`
	Netz           float64 `json:"netz"`
	Total          float64 `json:"total"`

	// Reine Durchreichungen aus Snapshot.Values, keine Ableitungen: die
	// Lueckenrechnung, Total, Autarkie, Eigenverbrauch und Unbalanced
	// rechnen ausschliesslich mit den Leistungsrollen und bleiben von
	// diesen drei Feldern unberuehrt.
	BatterySoC         float64 `json:"battery_soc"`
	BatteryCapacityKWh float64 `json:"battery_capacity_kwh"`
	BatteryEnergyKWh   float64 `json:"battery_energy_kwh"`
}

// DeriveBalance turns a Snapshot's raw role values into the balance the
// dashboard tiles render, following cfg's gap/load-mode choice. cfg is
// normalized first so a zero-value Interpretation behaves like
// DefaultInterpretation.
func DeriveBalance(s Snapshot, cfg Interpretation) Balance {
	cfg = cfg.Normalized()

	pv := s.Value("pv")
	charge := s.BatteryChargePower()
	discharge := s.BatteryDischargePower()
	gridImport := s.GridImportPower()
	gridExport := s.GridExportPower()
	wallbox := math.Max(s.Value("wallbox"), 0)
	heatPump := math.Max(s.Value("heat_pump"), 0)

	calculated := math.Max(pv+gridImport+discharge-gridExport-charge, 0)
	hasMeasured := s.HasValue("load")
	measured := 0.0
	if hasMeasured {
		measured = math.Max(s.Value("load"), 0)
	}

	var loadTotal float64
	var loadSource string
	loadMeasured := 0.0
	switch cfg.LoadMode {
	case LoadModeCalculated:
		loadTotal, loadSource = calculated, "calculated"
	case LoadModeCombined:
		// Kein "missing"-Zweig: fehlt die Rolle "load", faellt der Modus
		// stillschweigend auf das Verhalten von LoadModeCalculated zurueck
		// (loadMeasured bleibt 0). Die Energie-Seite weist darauf hin.
		loadTotal, loadSource, loadMeasured = calculated, "combined", measured
	case LoadModeMeasured:
		if hasMeasured {
			loadTotal, loadSource = measured, "measured"
		} else {
			loadTotal, loadSource = 0, "missing"
		}
	default: // LoadModeAuto
		if hasMeasured {
			loadTotal, loadSource = measured, "measured"
		} else {
			loadTotal, loadSource = calculated, "calculated"
		}
	}

	base := math.Max(loadTotal-wallbox-heatPump-loadMeasured, 0)
	gapRaw := (pv + gridImport + discharge) - (base + loadMeasured + wallbox + heatPump + gridExport + charge)

	sumSources := pv + gridImport + discharge
	sumSinks := base + loadMeasured + wallbox + heatPump + gridExport + charge
	total := math.Max(sumSources, sumSinks)

	toleranceW := cfg.GapToleranceW
	if cfg.GapToleranceMode == ToleranceModePercent {
		toleranceW = cfg.GapTolerancePercent / 100 * total
	}

	gapIgnored := math.Abs(gapRaw) <= toleranceW
	gapApplied := gapRaw
	if gapIgnored {
		gapApplied = 0
	}

	balance := Balance{
		PV: pv, GridImport: gridImport, GridExport: gridExport,
		BatteryCharge: charge, BatteryDischarge: discharge,
		Wallbox: wallbox, HeatPump: heatPump,
		LoadTotal: loadTotal, LoadSource: loadSource, LoadMeasured: loadMeasured, Base: base,
		GapRaw: gapRaw, GapApplied: gapApplied, GapToleranceW: toleranceW, GapIgnored: gapIgnored,
		Total:              total,
		BatterySoC:         s.Value("battery_soc"),
		BatteryCapacityKWh: s.Value("battery_capacity_kwh"),
		BatteryEnergyKWh:   s.Value("battery_energy_kwh"),
	}

	switch cfg.GapMode {
	case GapModeDiagnostic:
		balance.GapAbsorbed = 0
		balance.Unbalanced = gapApplied != 0
	default: // GapModeUnknownConsumer
		balance.Unbalanced = false
		balance.GapAbsorbed = gapApplied
		if gapApplied > 0 {
			balance.LoadTotal += gapApplied
			balance.Base += gapApplied
		}
	}

	balance.Autarkie = 0
	if balance.LoadTotal > 0 {
		balance.Autarkie = clamp01(1 - gridImport/balance.LoadTotal)
	}
	balance.Eigenverbrauch = 0
	if pv > 0 {
		balance.Eigenverbrauch = clamp01(1 - gridExport/pv)
	}
	balance.Netz = gridImport - gridExport

	return balance
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
