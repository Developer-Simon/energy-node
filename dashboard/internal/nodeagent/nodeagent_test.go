package nodeagent

import "testing"

// Die Statusleiste liest nur diesen Ausschnitt; jede Angabe, die sie zeigen
// kann, muss hier durchgereicht werden.
func TestTelemetryCarriesTheStatusBarValues(t *testing.T) {
	agent := New(Options{})
	if _, ok := agent.Telemetry(); ok {
		t.Fatal("before the first sample there must be no telemetry")
	}
	temp, load, ram := 51.5, 23.4, 60.0
	agent.fast = FastState{CPUTempC: &temp, CPULoadPct: &load, RAMUsedPct: &ram, UndervoltageNow: true}
	agent.seen = true
	tel, ok := agent.Telemetry()
	if !ok {
		t.Fatal("telemetry missing after a sample")
	}
	if tel.CPUTempC == nil || *tel.CPUTempC != temp || tel.CPULoadPct == nil || *tel.CPULoadPct != load ||
		tel.RAMUsedPct == nil || *tel.RAMUsedPct != ram || !tel.UndervoltageNow {
		t.Fatalf("telemetry = %+v, want the sampled values", tel)
	}
}
