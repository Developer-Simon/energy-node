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
	// LoadSource sagt, woher LoadTotal stammt: "calculated", "measured"
	// oder "missing" (Gemessen erzwungen, aber kein Hauszaehler).
	LoadSource string `json:"load_source"`
	// LoadFallback nennt den Grund, wenn Automatisch den Hauszaehler statt
	// der Bilanz nimmt. Derzeit nur "no_grid", sonst "".
	LoadFallback string `json:"load_fallback"`
	// LoadCalculated ist der aus der Bilanz berechnete Hausverbrauch vor
	// jeder Lueckenverrechnung, in jedem Modus.
	LoadCalculated float64 `json:"load_calculated"`
	// LoadMeter ist der Wert des Hauszaehlers (Rolle "load"), 0 ohne
	// Zaehler. HasLoadMeter trennt 0 W von "kein Zaehler".
	LoadMeter    float64 `json:"load_meter"`
	HasLoadMeter bool    `json:"has_load_meter"`
	// MeterDeviation ist Zaehler minus Berechnung, nur wenn ein Zaehler und
	// eine Netzrolle vorliegen, sonst 0. MeterCheck ist dann "ok" (innerhalb
	// der Lueckentoleranz) oder "deviates", ohne Vergleich "".
	MeterDeviation float64 `json:"meter_deviation"`
	MeterCheck     string  `json:"meter_check"`
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

	// Categories holds each custom:<id> value as its category's base counts
	// it: consumers clamped at 0, producers and storage with their raw sign.
	// Never nil.
	Categories map[string]float64 `json:"categories"`
}

// categorySums folds every custom:<id> value by its category's base. A
// value whose category is unknown is ignored rather than guessed.
type categorySums struct {
	consumers, producers, charge, discharge float64
	perCategory                             map[string]float64
}

func sumCategories(s Snapshot) categorySums {
	sums := categorySums{perCategory: map[string]float64{}}
	for role, value := range s.Values {
		id, ok := role.CategoryID()
		if !ok {
			continue
		}
		category, known := s.Categories[id]
		if !known {
			continue
		}
		switch category.Base {
		case CategoryConsumer:
			v := math.Max(value, 0)
			sums.consumers += v
			sums.perCategory[id] = v
		case CategoryProducer:
			sums.producers += value
			sums.perCategory[id] = value
		case CategoryStorage:
			if value > 0 {
				sums.charge += value
			} else {
				sums.discharge -= value
			}
			sums.perCategory[id] = value
		}
	}
	return sums
}

// DeriveBalance turns a Snapshot's raw role values into the balance the
// dashboard tiles render, following cfg's gap/load-mode choice. cfg is
// normalized first so a zero-value Interpretation behaves like
// DefaultInterpretation.
func DeriveBalance(s Snapshot, cfg Interpretation) Balance {
	cfg = cfg.Normalized()

	cats := sumCategories(s)
	pv := s.Value("pv") + cats.producers
	charge := s.BatteryChargePower() + cats.charge
	discharge := s.BatteryDischargePower() + cats.discharge
	gridImport := s.GridImportPower()
	gridExport := s.GridExportPower()
	wallbox := math.Max(s.Value("wallbox"), 0)
	heatPump := math.Max(s.Value("heat_pump"), 0)

	calculated := math.Max(pv+gridImport+discharge-gridExport-charge, 0)
	hasMeter := s.HasValue("load")
	meter := 0.0
	if hasMeter {
		meter = math.Max(s.Value("load"), 0)
	}
	hasGrid := s.HasValue("grid") || s.HasValue("grid_import") || s.HasValue("grid_export")

	var loadTotal float64
	var loadSource, loadFallback string
	switch cfg.LoadMode {
	case LoadModeCalculated:
		loadTotal, loadSource = calculated, "calculated"
	case LoadModeMeasured:
		if hasMeter {
			loadTotal, loadSource = meter, "measured"
		} else {
			loadTotal, loadSource = 0, "missing"
		}
	default: // LoadModeAuto
		// Ohne Netzrolle fehlt der Bilanz der Netzbezug. Dann ist der
		// Zaehler die bessere Quelle. Ohne beides bleibt die Rechnung, die
		// fuer eine Inselanlage (PV plus Speicher) auch stimmt.
		if hasMeter && !hasGrid {
			loadTotal, loadSource, loadFallback = meter, "measured", "no_grid"
		} else {
			loadTotal, loadSource = calculated, "calculated"
		}
	}

	base := math.Max(loadTotal-wallbox-heatPump-cats.consumers, 0)
	gapRaw := (pv + gridImport + discharge) - (base + wallbox + heatPump + cats.consumers + gridExport + charge)

	sumSources := pv + gridImport + discharge
	sumSinks := base + wallbox + heatPump + cats.consumers + gridExport + charge
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

	meterDeviation, meterCheck := 0.0, ""
	if hasMeter && hasGrid {
		meterDeviation = meter - calculated
		meterCheck = "ok"
		if math.Abs(meterDeviation) > toleranceW {
			meterCheck = "deviates"
		}
	}

	balance := Balance{
		PV: pv, GridImport: gridImport, GridExport: gridExport,
		BatteryCharge: charge, BatteryDischarge: discharge,
		Wallbox: wallbox, HeatPump: heatPump,
		LoadTotal: loadTotal, LoadSource: loadSource, LoadFallback: loadFallback, LoadCalculated: calculated,
		LoadMeter: meter, HasLoadMeter: hasMeter, MeterDeviation: meterDeviation, MeterCheck: meterCheck, Base: base,
		GapRaw: gapRaw, GapApplied: gapApplied, GapToleranceW: toleranceW, GapIgnored: gapIgnored,
		Total:              total,
		BatterySoC:         s.Value("battery_soc"),
		BatteryCapacityKWh: s.Value("battery_capacity_kwh"),
		BatteryEnergyKWh:   s.Value("battery_energy_kwh"),
		Categories:         cats.perCategory,
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
