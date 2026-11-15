package performance

import (
	"math"
	"testing"
	"time"
)

func TestRowCostScalesWithRealPeriod(t *testing.T) {
	r := Row{Current: 20, RoundS: 0.4, CostPct: 5}
	got := RowCost(r, 5)
	want := 5 * 20.4 / 5.4
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("RowCost = %v, want %v", got, want)
	}
}

func TestDefaultTarget(t *testing.T) {
	if got := DefaultTarget(1); got != 60 {
		t.Errorf("DefaultTarget(1) = %d, want 60", got)
	}
	if got := DefaultTarget(4); got != 70 {
		t.Errorf("DefaultTarget(4) = %d, want 70", got)
	}
}

func TestDeductionCapsAtForty(t *testing.T) {
	temp := 85.0
	s := Snapshot{
		Throttled:     true,
		TempC:         &temp,
		RAMFreePct:    5,
		SwapPagesPerS: 40,
	}
	d := DeductionFor(s)
	if d.Pct != 40 {
		t.Errorf("Pct = %v, want 40 (capped)", d.Pct)
	}
	want := []string{
		"performance.deduction.throttled",
		"performance.deduction.hot",
		"performance.deduction.ram",
		"performance.deduction.swap",
	}
	if len(d.Reasons) != len(want) {
		t.Fatalf("Reasons = %v, want %v", d.Reasons, want)
	}
	for i := range want {
		if d.Reasons[i] != want[i] {
			t.Errorf("Reasons[%d] = %q, want %q", i, d.Reasons[i], want[i])
		}
	}
}

func TestDeductionWarmBand(t *testing.T) {
	temp := 76.0
	d := DeductionFor(Snapshot{TempC: &temp, RAMFreePct: 50})
	if d.Pct != 5 || len(d.Reasons) != 1 || d.Reasons[0] != "performance.deduction.warm" {
		t.Errorf("DeductionFor = %+v, want 5 with warm", d)
	}
	if d := DeductionFor(Snapshot{RAMFreePct: 50}); d.Pct != 0 || d.Reasons == nil {
		t.Errorf("no signals = %+v, want 0 with an empty, non-nil list", d)
	}
}

func TestBuildRowsCostSplit(t *testing.T) {
	// Ten minutes window. Shelly sends 120 messages per minute, so 1200 in the window.
	// Mosquitto's 2 % goes to the 1200 messages and is fully carried by Shelly.
	// The automation unit's 3 % goes entirely to tick, because regulator is absent.
	shellyRound := 0.4
	snap := Snapshot{
		MeasuredFor: 10 * time.Minute,
		CPUP95:      20,
		Units: []UnitStats{
			{ID: "shelly", CPUP95: 4, MsgPerMin: 120, RoundS: &shellyRound},
			{ID: mosquittoUnit, CPUP95: 2, Fixed: true},
			{ID: DashboardUnit, CPUP95: 5, Fixed: true},
			{ID: automationUnit, CPUP95: 3, Fixed: true},
		},
	}
	cur := RateConfig{
		LiveUpdateS: 3,
		TickS:       10,
		RegulatorS:  10,
		NodeS:       60,
		Services:    map[string]float64{"shelly": 5},
	}
	rows := BuildRows(snap, cur, map[string]float64{"shelly": 2}, map[RowID]bool{RowRegulator: false})

	byID := map[RowID]Row{}
	var sum float64
	for _, r := range rows {
		byID[r.ID] = r
		sum += r.CostPct
	}
	if len(rows) != 9 {
		t.Fatalf("len(rows) = %d, want 9", len(rows))
	}
	if sum > snap.CPUP95 {
		t.Errorf("sum of row costs = %v, want at most the measured total %v", sum, snap.CPUP95)
	}

	if reg := byID[RowRegulator]; reg.Present {
		t.Errorf("regulator Present = true, want false without a scenario")
	}
	if got := byID[RowShelly].CostPct; math.Abs(got-6) > 1e-9 {
		t.Errorf("shelly CostPct = %v, want 4 + 2 = 6", got)
	}
	if got := byID[RowShelly].RoundS; got != shellyRound {
		t.Errorf("shelly RoundS = %v, want %v", got, shellyRound)
	}
	if got := byID[RowLiveUpdate].CostPct; got != 5 {
		t.Errorf("live_update CostPct = %v, want the dashboard P95 5", got)
	}
	if got := byID[RowTick].CostPct; math.Abs(got-3) > 1e-9 {
		t.Errorf("tick CostPct = %v, want the whole automation unit 3", got)
	}
	if got := byID[RowTick].Floor; got != 5 {
		t.Errorf("tick Floor = %v, want the fixed floor 5", got)
	}
	if got := byID[RowShelly].Floor; got != 2 {
		t.Errorf("shelly Floor = %v, want 2 from floors", got)
	}
	if got := byID[RowShelly].Current; got != 5 {
		t.Errorf("shelly Current = %v, want 5", got)
	}
}
