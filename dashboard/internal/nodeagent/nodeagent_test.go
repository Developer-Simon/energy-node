package nodeagent

import (
	"testing"
	"time"
)

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

// Nach SetRates liefern die Ticker-Intervalle die neuen Werte, und ein Signal
// liegt an. Ein zweites SetRates ohne Leser blockiert nicht.
func TestSetRatesSignalsAndChangesIntervals(t *testing.T) {
	agent := New(Options{PollIntervalS: 60, DiagnosticPollMultiplier: 10})
	agent.SetRates(10, 5)
	if got := agent.PollInterval(); got != 10*time.Second {
		t.Fatalf("PollInterval() = %v, want 10s", got)
	}
	if got := agent.DiagnosticInterval(); got != 50*time.Second {
		t.Fatalf("DiagnosticInterval() = %v, want 50s", got)
	}
	select {
	case <-agent.RatesChanged():
	default:
		t.Fatal("no signal after SetRates")
	}
	agent.SetRates(20, 5)
	agent.SetRates(30, 5)
	if got := agent.PollInterval(); got != 30*time.Second {
		t.Fatalf("PollInterval() after second SetRates = %v, want 30s", got)
	}
}
