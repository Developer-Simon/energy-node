package hostapi_test

import (
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestParseLogLineSplitsTheTimestampPrefix(t *testing.T) {
	at, rest := hostapi.ParseLogLine("1737000000123 ##STEP 60 begin")
	if at != 1737000000123 || rest != "##STEP 60 begin" {
		t.Fatalf("got (%d, %q)", at, rest)
	}
}

func TestParseLogLineFallsBackOnAMalformedLine(t *testing.T) {
	at, rest := hostapi.ParseLogLine("not a timestamp at all")
	if at != 0 || rest != "not a timestamp at all" {
		t.Fatalf("got (%d, %q)", at, rest)
	}
}

func TestReplayEventsNumbersFromOneAndKeepsOriginalTimestamps(t *testing.T) {
	lines := []string{
		"1000 ##STEP 60 begin",
		"1002 energy-node-dashboard 1.4.1 -> 1.4.2",
		"1005 ##STEP 60 ok",
	}
	events := hostapi.ReplayEvents(lines)
	if len(events) != 3 {
		t.Fatalf("len = %d, want 3", len(events))
	}
	if events[0].Seq != 1 || events[0].Type != "step" || events[0].At != 1000 {
		t.Fatalf("event 0 = %+v", events[0])
	}
	data0 := events[0].Data.(map[string]string)
	if data0["id"] != "60" || data0["state"] != "begin" {
		t.Fatalf("event 0 data = %+v", data0)
	}
	if events[1].Type != "log" || events[1].At != 1002 {
		t.Fatalf("event 1 = %+v", events[1])
	}
	data1 := events[1].Data.(map[string]string)
	if data1["step_id"] != "60" || data1["line"] != "energy-node-dashboard 1.4.1 -> 1.4.2" {
		t.Fatalf("event 1 data = %+v", data1)
	}
	if events[2].Seq != 3 || events[2].Data.(map[string]string)["state"] != "ok" {
		t.Fatalf("event 2 = %+v", events[2])
	}
}

func TestReplayEventsFallsBackToNowForAnUnparseableTimestamp(t *testing.T) {
	events := hostapi.ReplayEvents([]string{"garbage line"})
	if len(events) != 1 || events[0].At == 0 {
		t.Fatalf("events = %+v", events)
	}
}

func TestReplayRunStartsWithTheOriginalRunStarted(t *testing.T) {
	events := hostapi.ReplayRun("run-7", "redeploy", "", []string{
		"1000 ##STEP 60 begin",
		"1005 ##STEP 60 ok",
	})
	if len(events) != 3 {
		t.Fatalf("len = %d, want run-started plus 2", len(events))
	}
	first := events[0]
	if first.Seq != 1 || first.Type != "run-started" || first.At != 1000 {
		t.Fatalf("event 0 = %+v", first)
	}
	data := first.Data.(map[string]any)
	if data["run_id"] != "run-7" || data["mode"] != "redeploy" || data["only"] != "" {
		t.Fatalf("run-started data = %+v", data)
	}
	if events[1].Seq != 2 || events[1].Type != "step" || events[2].Seq != 3 {
		t.Fatalf("replayed events not shifted behind run-started: %+v", events[1:])
	}
}
