package autolearn

import (
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

func unthrottledFrame(balance map[string]float64) learnrec.Frame {
	return learnrec.Frame{Balance: balance}
}

func TestUnthrottledWithoutRegulatorCountsPVMinutes(t *testing.T) {
	f := unthrottledFrame(map[string]float64{"pv": 800})
	if !Unthrottled(f, nil) {
		t.Error("nil regulator: want unthrottled")
	}
	if !Unthrottled(f, &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 800, Present: false}) {
		t.Error("regulator not present: want unthrottled")
	}
}

func TestUnthrottledRegulatorFeedInBelowDeadbandIsThrottled(t *testing.T) {
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 100, Present: true}
	f := unthrottledFrame(map[string]float64{"pv": 800, "grid_export": 5})
	if Unthrottled(f, reg) {
		t.Error("export 5 W with deadband 30: want throttled")
	}
}

func TestUnthrottledRegulatorFeedInAboveDeadbandPlusMarginIsUnthrottled(t *testing.T) {
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 100, Present: true}
	f := unthrottledFrame(map[string]float64{"pv": 800, "grid_export": 60})
	if !Unthrottled(f, reg) {
		t.Error("export 60 W with deadband 30: want unthrottled")
	}
}

func TestUnthrottledImportAtFullOutput(t *testing.T) {
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 0.99 * 800, Present: true}
	f := unthrottledFrame(map[string]float64{"pv": 800, "grid_import": 200})
	if !Unthrottled(f, reg) {
		t.Error("output 99 % of max and import 200 W: want unthrottled")
	}
	reg.OutputW = 0.5 * 800
	if Unthrottled(f, reg) {
		t.Error("output 50 % of max and import 200 W: want throttled")
	}
}

func TestUnthrottledBatteryChargeBelowFullSOC(t *testing.T) {
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 100, Present: true}
	charging := map[string]float64{"pv": 800, "battery_charge": 300, "battery_soc": 60}
	if !Unthrottled(unthrottledFrame(charging), reg) {
		t.Error("charge 300 W at soc 60 %: want unthrottled")
	}
	charging["battery_soc"] = 96
	if Unthrottled(unthrottledFrame(charging), reg) {
		t.Error("charge 300 W at soc 96 %: want throttled")
	}
	delete(charging, "battery_soc")
	if Unthrottled(unthrottledFrame(charging), reg) {
		t.Error("charge 300 W without soc value: want throttled")
	}
}

func TestUnthrottledNeedsPVAboveFiftyWatts(t *testing.T) {
	cases := []map[string]float64{
		{"pv": 50},
		{"pv": 0, "grid_export": 500},
		{"pv": 40, "grid_export": 500, "battery_charge": 300, "battery_soc": 10},
	}
	reg := &RegulatorView{DeadbandW: 30, MaxW: 800, OutputW: 800, Present: true}
	for _, balance := range cases {
		if Unthrottled(unthrottledFrame(balance), nil) || Unthrottled(unthrottledFrame(balance), reg) {
			t.Errorf("pv <= 50 for %v: want never unthrottled", balance)
		}
	}
}
