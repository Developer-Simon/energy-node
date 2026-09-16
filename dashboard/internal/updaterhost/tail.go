// dashboard/internal/updaterhost/tail.go
package updaterhost

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/updaterjob"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// tailJobLog polls logPath for lines past fromOffset, translating each one
// through sink exactly like installer/internal/steps does for an SSH
// stdout stream, until statusPath exists. It is the one function both
// Host.Run (a fresh job, tailed live) and Host.TailInFlight (resuming a
// job the previous dashboard process instance started) use -- the only
// difference between the two callers is where the Sink implementation
// ends up publishing (see host.go).
//
// maxWait bounds the whole poll. The updater writes status.json even when
// it is interrupted (its EXIT trap), but a process killed hard enough
// never runs that trap, and a dashboard that resumed into such a job would
// otherwise poll a file that will never appear, forever and silently. Zero
// means no bound; tests use it.
func tailJobLog(ctx context.Context, logPath, statusPath string, fromOffset int64, sink hostapi.Sink, pollEvery, maxWait time.Duration) error {
	offset := fromOffset
	currentStep := ""
	started := time.Now()
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		info, err := os.Stat(logPath)
		if err == nil && info.Size() > offset {
			f, err := os.Open(logPath)
			if err != nil {
				return fmt.Errorf("updaterhost: opening log: %w", err)
			}
			if _, err := f.Seek(offset, 0); err != nil {
				f.Close()
				return fmt.Errorf("updaterhost: seeking log: %w", err)
			}
			lines, newOffset, err := readCompleteLines(f)
			f.Close()
			if err != nil {
				return err
			}
			offset = newOffset
			for _, raw := range lines {
				_, rest := hostapi.ParseLogLine(raw)
				if stepID, state, detail, ok := parseMarkerLine(rest); ok {
					currentStep = stepID
					sink.Marker(stepID, state, detail)
					continue
				}
				sink.Log(currentStep, rest)
			}
		}

		if status, done, err := updaterjob.ReadStatus(dirOf(statusPath)); err != nil {
			return fmt.Errorf("updaterhost: reading status: %w", err)
		} else if done {
			if status.Result == "ok" {
				return nil
			}
			// A rejection carries no step -- "Schritt " with nothing
			// after it would be the worst of both.
			detail := "Bundle abgelehnt"
			if status.Step != "" {
				detail = "Schritt " + status.Step
			}
			return &hostapi.Error{Code: status.Code, Detail: detail}
		}

		if maxWait > 0 && time.Since(started) > maxWait {
			return &hostapi.Error{Code: "UPDATER_TIMEOUT", Detail: "Der Auftrag hat sich nicht mehr gemeldet."}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func dirOf(statusPath string) string {
	for i := len(statusPath) - 1; i >= 0; i-- {
		if statusPath[i] == '/' {
			return statusPath[:i]
		}
	}
	return "."
}

// readCompleteLines reads whatever f offers from its current position and
// returns only whole lines, plus the byte offset immediately after the
// last one returned -- a trailing partial line (the updater script mid
// os.WriteFile append) is left for the next poll rather than split wrong.
func readCompleteLines(f *os.File) (lines []string, newOffset int64, err error) {
	start, err := f.Seek(0, 1)
	if err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, readErr := f.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if readErr != nil {
			break
		}
	}
	consumed := 0
	lineStart := 0
	for i, b := range buf {
		if b == '\n' {
			lines = append(lines, string(buf[lineStart:i]))
			consumed = i + 1
			lineStart = i + 1
		}
	}
	return lines, start + int64(consumed), nil
}

// parseMarkerLine mirrors hostapi's own (unexported) parser exactly --
// duplicated for the same reason replay.go duplicates it there: this
// module must not import installer/internal/steps, and the grammar is a
// stable, documented contract (E9). Kept here rather than calling into
// hostapi a second time because hostapi does not export it -- only
// ReplayEvents and ParseLogLine, which parse whole lines at once rather
// than handing back the pieces this loop needs to call sink.Marker/Log.
func parseMarkerLine(line string) (stepID, state, detail string, ok bool) {
	const prefix = "##STEP "
	if len(line) < len(prefix) || line[:len(prefix)] != prefix {
		return "", "", "", false
	}
	rest := line[len(prefix):]
	fields := splitN(rest, ' ', 3)
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

func splitN(s string, sep byte, n int) []string {
	var out []string
	start := 0
	for i := 0; i < len(s) && len(out) < n-1; i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
