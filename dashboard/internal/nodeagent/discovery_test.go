package nodeagent

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func testAgent() *Agent {
	return New(Options{
		NodeID: "energy_node", NodeName: "Energy Node",
		DiscoveryPrefix: "homeassistant", PollIntervalS: 60, DiagnosticPollMultiplier: 10,
	})
}

func TestDiscoveryMessagesFrozen(t *testing.T) {
	msgs := testAgent().DiscoveryMessages(nil)
	got, _ := json.MarshalIndent(msgs, "", "  ")
	got = append(got, '\n')
	golden := filepath.Join("testdata", "discovery.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("discovery messages drifted from testdata/discovery.json; re-run with -update if intended\n--- got ---\n%s", got)
	}
}

func TestDiscoveryHonoursDisabledMetric(t *testing.T) {
	enabled := func(m string) bool { return m != metricCPUTemp }
	for _, msg := range testAgent().DiscoveryMessages(enabled) {
		if msg.Topic == "homeassistant/sensor/energy_node/cpu_temp/config" {
			if msg.Payload != "" || !msg.Retain {
				t.Fatalf("disabled cpu_temp: got payload %q retain %v, want empty retained", msg.Payload, msg.Retain)
			}
			return
		}
	}
	t.Fatal("cpu_temp config topic not found")
}

func TestStateMessagesOmitDisabledMetric(t *testing.T) {
	a := testAgent()
	a.reader = fakeReader(
		map[string]string{"/proc/meminfo": "MemTotal: 100 kB\nMemAvailable: 40 kB\n"},
		map[string]string{"vcgencmd get_throttled": "throttled=0x0"},
	)
	msgs := a.StateMessages(context.Background(), func(m string) bool { return m != metricRAM })
	var state map[string]any
	if err := json.Unmarshal([]byte(msgs[0].Payload), &state); err != nil {
		t.Fatal(err)
	}
	if _, present := state["ram_used_pct"]; present {
		t.Fatal("ram_used_pct present although metric disabled")
	}
	if msgs[0].Topic != "outstation/energy_node/state" || msgs[1].Topic != "outstation/energy_node/diagnostics" {
		t.Fatalf("state topics = %q / %q", msgs[0].Topic, msgs[1].Topic)
	}
}

func TestLegacyCleanupIsEmptyRetained(t *testing.T) {
	msgs := testAgent().LegacyCleanupMessages()
	seenState := false
	for _, m := range msgs {
		if m.Payload != "" || !m.Retain {
			t.Fatalf("cleanup msg %q: payload %q retain %v, want empty retained", m.Topic, m.Payload, m.Retain)
		}
		if m.Topic == "outstation/energy-node/state" {
			seenState = true
		}
	}
	if !seenState {
		t.Fatal("legacy state topic not cleaned")
	}
}
