package hostapi

import (
	"strconv"
	"strings"
	"time"
)

// ParseLogLine splits one line of a job log file into its original publish
// time and payload. Plan D's updater prefixes every line it appends to
// job/log with "<unix-millis> " before the raw ##STEP marker or human text
// -- the same line a live SSH run would produce, just delayed through a
// file instead of stdout. A line with no parseable prefix (a malformed or
// hand-edited log) keeps the whole line as rest and reports at as 0;
// ReplayEvents then falls back to "now" rather than dropping it.
func ParseLogLine(raw string) (at int64, rest string) {
	head, tail, found := strings.Cut(raw, " ")
	if !found {
		return 0, raw
	}
	parsed, err := strconv.ParseInt(head, 10, 64)
	if err != nil {
		return 0, raw
	}
	return parsed, tail
}

// ReplayEvents turns a job log's lines into the same Event shape busSink
// publishes live, numbered from 1 in order. A restarted process (Plan D:
// the updater outlives the dashboard's own self-update) seeds a fresh Bus
// with this slice via RestoreBus, so a reconnecting SSE client's ?since=
// still lines up with what it already saw before the restart.
func ReplayEvents(lines []string) []Event {
	events := make([]Event, 0, len(lines))
	var seq int64
	currentStep := ""
	for _, raw := range lines {
		at, rest := ParseLogLine(raw)
		if at == 0 {
			at = time.Now().UnixMilli()
		}
		seq++
		if stepID, state, detail, ok := parseMarkerLine(rest); ok {
			currentStep = stepID
			events = append(events, Event{Seq: seq, Type: "step", At: at,
				Data: map[string]string{"id": stepID, "state": state, "detail": detail}})
			continue
		}
		events = append(events, Event{Seq: seq, Type: "log", At: at,
			Data: map[string]string{"step_id": currentStep, "line": rest}})
	}
	return events
}

// parseMarkerLine recognizes "##STEP <id> <state> [detail]", the same
// grammar installer/internal/steps.ParseMarker parses from SSH stdout.
// Duplicated rather than imported: this module must not depend on the
// installer module (Komponente C keeps SSH out of the dashboard binary),
// and the grammar itself is a stable, documented contract (E9).
func parseMarkerLine(line string) (stepID, state, detail string, ok bool) {
	const prefix = "##STEP "
	if !strings.HasPrefix(line, prefix) {
		return "", "", "", false
	}
	fields := strings.SplitN(strings.TrimPrefix(line, prefix), " ", 3)
	if len(fields) < 2 {
		return "", "", "", false
	}
	switch fields[1] {
	case "begin", "ok", "skip", "fail":
	default:
		return "", "", "", false
	}
	if len(fields) == 3 {
		detail = fields[2]
	}
	return fields[0], fields[1], detail, true
}
