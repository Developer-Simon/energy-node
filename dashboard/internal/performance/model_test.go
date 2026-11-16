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
		RAMFreeKnown:  true,
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

func TestBuildRowsFlagsMatchDraft(t *testing.T) {
	rows := BuildRows(Snapshot{}, RateConfig{}, nil, map[RowID]bool{RowRegulator: true})
	type flags struct {
		right, follows  string
		source, balance bool
	}
	want := map[RowID]flags{
		RowLiveUpdate: {"edit_setup", "half", false, false},
		RowTick:       {"automations", "half", false, false},
		RowRegulator:  {"automations", "balance", false, false},
		RowNode:       {"system_actions", "", true, false},
		RowShelly:     {"system_actions", "", true, true},
		RowTrucki:     {"system_actions", "", true, false},
		RowBatterySoc: {"system_actions", "", true, false},
		RowApsystems:  {"system_actions", "", true, false},
		RowTuya:       {"system_actions", "", true, false},
	}
	if len(rows) != len(want) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(want))
	}
	for _, r := range rows {
		w, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected row %q", r.ID)
			continue
		}
		got := flags{r.Right, r.Follows, r.Source, r.Balance}
		if got != w {
			t.Errorf("%s flags = %+v, want %+v", r.ID, got, w)
		}
	}
}

// Ohne RAM-Probe im Fenster (Anlauf) ist der freie Speicher unbekannt und
// darf keinen Abschlag ausloesen.
func TestDeductionSkipsRAMWhenUnknown(t *testing.T) {
	d := DeductionFor(Snapshot{RAMFreePct: 0, RAMFreeKnown: false})
	if d.Pct != 0 || len(d.Reasons) != 0 {
		t.Fatalf("warmup DeductionFor = %+v, want no ram deduction", d)
	}
}

func TestDeductionRAMWhenKnown(t *testing.T) {
	d := DeductionFor(Snapshot{RAMFreePct: 10, RAMFreeKnown: true})
	if d.Pct != 10 || len(d.Reasons) != 1 || d.Reasons[0] != "performance.deduction.ram" {
		t.Fatalf("DeductionFor = %+v, want 10 with ram", d)
	}
}

func TestBuildRowsRealServiceIDsAndAliases(t *testing.T) {
	// The manifests name the services apsystems and tuya. The directory names
	// stay accepted as aliases and must give the same rows.
	snapFor := func(apsID, tuyaID string) Snapshot {
		return Snapshot{
			MeasuredFor: 10 * time.Minute,
			Units: []UnitStats{
				{ID: apsID, CPUP95: 2},
				{ID: tuyaID, CPUP95: 1.5},
			},
		}
	}
	cases := map[string]struct {
		snap     Snapshot
		services map[string]float64
		floors   map[string]float64
	}{
		"real ids":   {snapFor("apsystems", "tuya"), map[string]float64{"apsystems": 20, "tuya": 30}, map[string]float64{"apsystems": 12, "tuya": 10}},
		"alias keys": {snapFor("apsystems_ez1", "tuya_mqtt"), map[string]float64{"apsystems_ez1": 20, "tuya_mqtt": 30}, map[string]float64{"apsystems_ez1": 12, "tuya_mqtt": 10}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rows := BuildRows(c.snap, RateConfig{Services: c.services}, c.floors, nil)
			byID := map[RowID]Row{}
			for _, r := range rows {
				byID[r.ID] = r
			}
			aps, tuya := byID[RowApsystems], byID[RowTuya]
			if aps.Floor != 12 {
				t.Errorf("apsystems Floor = %v, want 12", aps.Floor)
			}
			if tuya.Floor != 10 {
				t.Errorf("tuya Floor = %v, want 10", tuya.Floor)
			}
			if aps.Current != 20 || tuya.Current != 30 {
				t.Errorf("Current = %v / %v, want 20 / 30", aps.Current, tuya.Current)
			}
			if aps.CostPct != 2 {
				t.Errorf("apsystems CostPct = %v, want its P95 2", aps.CostPct)
			}
			if tuya.CostPct != 1.5 {
				t.Errorf("tuya CostPct = %v, want its P95 1.5", tuya.CostPct)
			}
		})
	}
}

func TestBuildRowsUsesPushForLiveUpdate(t *testing.T) {
	// Dashboard P95 10 %, measured split: push 40 %, node 10 %, mqtt 30 %.
	// Mosquitto's 2 % and the folded mqtt share 3 % spread over 60 messages.
	shellyRound := 0.4
	snap := Snapshot{
		MeasuredFor: time.Minute,
		Units: []UnitStats{
			{ID: "shelly", CPUP95: 4, MsgPerMin: 60, RoundS: &shellyRound},
			{ID: mosquittoUnit, CPUP95: 2, Fixed: true},
			{ID: DashboardUnit, CPUP95: 10, Fixed: true},
		},
		Dashboard: &DashboardSplit{Shares: map[Part]float64{PartPush: 0.4, PartNode: 0.1, PartMQTT: 0.3}},
	}
	cur := RateConfig{LiveUpdateS: 3, TickS: 10, RegulatorS: 10, NodeS: 60, Services: map[string]float64{"shelly": 5}}
	rows := BuildRows(snap, cur, nil, map[RowID]bool{RowRegulator: false})

	byID := map[RowID]Row{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if got := byID[RowLiveUpdate].CostPct; math.Abs(got-4) > 1e-9 {
		t.Errorf("live_update CostPct = %v, want fold(push) 4", got)
	}
	if got := byID[RowNode].CostPct; math.Abs(got-1) > 1e-9 {
		t.Errorf("node CostPct = %v, want fold(node) 1", got)
	}
	// shelly: 4 own + (2 mosquitto + 3 folded mqtt) / 60 messages * 60 messages.
	if got := byID[RowShelly].CostPct; math.Abs(got-9) > 1e-9 {
		t.Errorf("shelly CostPct = %v, want 4 + (2 + 3) = 9", got)
	}
}

func TestBuildRowsWithoutSplitKeepsOldModel(t *testing.T) {
	shellyRound := 0.4
	snap := Snapshot{
		MeasuredFor: time.Minute,
		Units: []UnitStats{
			{ID: "shelly", CPUP95: 4, MsgPerMin: 60, RoundS: &shellyRound},
			{ID: mosquittoUnit, CPUP95: 2, Fixed: true},
			{ID: DashboardUnit, CPUP95: 10, Fixed: true},
		},
	}
	cur := RateConfig{LiveUpdateS: 3, TickS: 10, RegulatorS: 10, NodeS: 60, Services: map[string]float64{"shelly": 5}}
	rows := BuildRows(snap, cur, nil, map[RowID]bool{RowRegulator: false})

	byID := map[RowID]Row{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if got := byID[RowLiveUpdate].CostPct; got != 10 {
		t.Errorf("live_update CostPct = %v, want the dashboard P95 10", got)
	}
	if got := byID[RowNode].CostPct; got != 0 {
		t.Errorf("node CostPct = %v, want 0 without split", got)
	}
	// shelly: 4 own + mosquitto 2 over 60 messages, all 60 on shelly.
	if got := byID[RowShelly].CostPct; math.Abs(got-6) > 1e-9 {
		t.Errorf("shelly CostPct = %v, want 4 + 2 = 6", got)
	}
}
