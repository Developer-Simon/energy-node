package energy

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestDeriveBalanceUnknownConsumerModeFoldsTheGapIntoHouseLoadAndAutarky(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 3100, RoleLoad: 3100, RoleHeatPump: 620, RoleGrid: 940}}
	cfg := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.LoadTotal != 4040 {
		t.Fatalf("load_total = %v, want 4040", balance.LoadTotal)
	}
	if balance.Base != 3420 {
		t.Fatalf("base = %v, want 3420", balance.Base)
	}
	if balance.GapAbsorbed != 940 {
		t.Fatalf("gap_absorbed = %v, want 940", balance.GapAbsorbed)
	}
	if balance.Unbalanced {
		t.Fatalf("unbalanced = true, want false")
	}
}

func TestDeriveBalanceDiagnosticModeKeepsTheGapSeparate(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 3100, RoleLoad: 3100, RoleHeatPump: 620, RoleGrid: 940}}
	cfg := Interpretation{GapMode: GapModeDiagnostic, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.LoadTotal != 3100 {
		t.Fatalf("load_total = %v, want 3100", balance.LoadTotal)
	}
	if balance.GapApplied != 940 {
		t.Fatalf("gap_applied = %v, want 940", balance.GapApplied)
	}
	if !balance.Unbalanced {
		t.Fatalf("unbalanced = false, want true")
	}
}

func TestDeriveBalanceIgnoresAGapBelowTheAbsoluteTolerance(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 1000, RoleLoad: 990}}
	cfg := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.GapRaw != 10 {
		t.Fatalf("gap_raw = %v, want 10", balance.GapRaw)
	}
	if !balance.GapIgnored || balance.GapApplied != 0 {
		t.Fatalf("gap should be ignored below tolerance, got %#v", balance)
	}
}

func TestDeriveBalanceScalesTheToleranceWithTotalPowerInPercentMode(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 5000, RoleLoad: 4900}}
	cfg := Interpretation{GapMode: GapModeDiagnostic, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModePercent, GapTolerancePercent: 2}
	balance := DeriveBalance(snapshot, cfg)
	if balance.GapToleranceW != 100 {
		t.Fatalf("gap_tolerance_w = %v, want 100 (2%% of 5000)", balance.GapToleranceW)
	}
	if !balance.GapIgnored {
		t.Fatalf("gap of 100 at the tolerance boundary should be ignored")
	}
}

func TestDeriveBalanceInCalculatedLoadModeProducesNoGap(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 2000, RoleGridImport: 500, RoleWallbox: 300, RoleHeatPump: 200}}
	cfg := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeCalculated, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.GapRaw != 0 {
		t.Fatalf("gap_raw = %v, want 0 (algebraically zero in calculated load mode)", balance.GapRaw)
	}
	if balance.LoadSource != "calculated" {
		t.Fatalf("load_source = %q, want calculated", balance.LoadSource)
	}
}

func TestDeriveBalanceInCalculatedModeClampsBaseAndReportsANegativeGap(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 1000, RoleWallbox: 2000}}
	cfg := Interpretation{GapMode: GapModeDiagnostic, LoadMode: LoadModeCalculated, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.Base != 0 {
		t.Fatalf("base = %v, want 0 (clamped)", balance.Base)
	}
	if balance.GapRaw >= 0 {
		t.Fatalf("gap_raw = %v, want negative", balance.GapRaw)
	}
}

func TestDeriveBalanceAutoModePrefersTheMeasuredLoadRole(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 800, RoleGridImport: 200, RoleLoad: 750}}
	cfg := Interpretation{GapMode: GapModeDiagnostic, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.LoadSource != "measured" || balance.LoadTotal != 750 {
		t.Fatalf("auto mode with a load role present = %#v, want measured/750", balance)
	}
}

func TestDeriveBalanceAutoModeFallsBackToCalculated(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 800, RoleGridImport: 200}}
	cfg := Interpretation{GapMode: GapModeDiagnostic, LoadMode: LoadModeAuto, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.LoadSource != "calculated" || balance.LoadTotal != 1000 {
		t.Fatalf("auto mode without a load role = %#v, want calculated/1000", balance)
	}
}

func TestDeriveBalanceMeasuredModeReportsAMissingLoadRole(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{RolePV: 800, RoleGridImport: 200}}
	cfg := Interpretation{GapMode: GapModeUnknownConsumer, LoadMode: LoadModeMeasured, GapToleranceMode: ToleranceModeAbsolute, GapToleranceW: 25}
	balance := DeriveBalance(snapshot, cfg)
	if balance.LoadSource != "missing" {
		t.Fatalf("load_source = %q, want missing", balance.LoadSource)
	}
	// unknown_consumer absorbs the entire unmeasured house load as gap, so
	// the final LoadTotal ends up at the full 1000 W even though it started
	// from 0 - LoadSource is what actually records "missing".
	if balance.LoadTotal != 1000 {
		t.Fatalf("load_total = %v, want 1000 (absorbed gap)", balance.LoadTotal)
	}
}

// balanceCase mirrors one entry of testdata/balance-cases.json, the fixture
// shared with test/energy-model.test.mjs so the Go DeriveBalance and its JS
// mirror in energy-model.js cannot silently drift apart.
type balanceCase struct {
	Name           string         `json:"name"`
	Values         map[string]any `json:"values"`
	Interpretation Interpretation `json:"interpretation"`
	Expect         map[string]any `json:"expect"`
}

func TestDeriveBalanceMatchesTheSharedFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/balance-cases.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var cases []balanceCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			values := make(map[Role]float64, len(c.Values))
			for role, value := range c.Values {
				values[Role(role)] = value.(float64)
			}
			snapshot := Snapshot{Values: values}
			balance := DeriveBalance(snapshot, c.Interpretation)

			got, err := json.Marshal(balance)
			if err != nil {
				t.Fatalf("marshaling balance: %v", err)
			}
			var gotFields map[string]any
			if err := json.Unmarshal(got, &gotFields); err != nil {
				t.Fatalf("re-parsing balance: %v", err)
			}

			for key, want := range c.Expect {
				gotValue, ok := gotFields[key]
				if !ok {
					t.Fatalf("balance has no field %q", key)
				}
				switch wantValue := want.(type) {
				case float64:
					gotNumber, ok := gotValue.(float64)
					if !ok || !almostEqual(gotNumber, wantValue) {
						t.Fatalf("%s = %v, want %v", key, gotValue, wantValue)
					}
				default:
					if gotValue != want {
						t.Fatalf("%s = %v, want %v", key, gotValue, want)
					}
				}
			}
		})
	}
}

// Im Kombiniert-Modus darf der gemessene Teilverbrauch den "Uebrigen
// Verbrauch" nicht ins Negative druecken - Base klemmt bei 0 und die
// Differenz taucht als negative Luecke auf, damit eine doppelte oder falsch
// skalierte Zuordnung sichtbar bleibt statt still verrechnet zu werden.
func TestDeriveBalanceInCombinedModeSeparatesMeasuredFromBase(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{"pv": 3000, "load": 900, "wallbox": 500}}
	cfg := DefaultInterpretation()
	cfg.LoadMode = LoadModeCombined

	balance := DeriveBalance(snapshot, cfg)

	if balance.LoadSource != "combined" {
		t.Fatalf("LoadSource = %q, want %q", balance.LoadSource, "combined")
	}
	if balance.LoadMeasured != 900 {
		t.Fatalf("LoadMeasured = %v, want 900", balance.LoadMeasured)
	}
	if balance.LoadTotal != 3000 {
		t.Fatalf("LoadTotal = %v, want 3000", balance.LoadTotal)
	}
	if balance.Base != 1600 {
		t.Fatalf("Base = %v, want 1600", balance.Base)
	}
	if math.Abs(balance.GapRaw) > 1e-9 {
		t.Fatalf("GapRaw = %v, want 0", balance.GapRaw)
	}
}

func TestDeriveBalanceInCombinedModeKeepsLoadMeasuredOutOfOtherModes(t *testing.T) {
	snapshot := Snapshot{Values: map[Role]float64{"pv": 3000, "load": 900, "wallbox": 500}}
	for _, mode := range []LoadMode{LoadModeMeasured, LoadModeCalculated, LoadModeAuto} {
		cfg := DefaultInterpretation()
		cfg.LoadMode = mode
		if got := DeriveBalance(snapshot, cfg).LoadMeasured; got != 0 {
			t.Fatalf("load_mode %q: LoadMeasured = %v, want 0", mode, got)
		}
	}
}
