package media

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Phase is a step of an imager write run.
type Phase string

const (
	PhasePrepare Phase = "prepare"
	PhaseWrite   Phase = "write"
	PhaseVerify  Phase = "verify"
	PhaseDone    Phase = "done"
	PhaseError   Phase = "error"
)

// Progress is one event read from the imager output.
type Progress struct {
	Phase   Phase
	Percent int
	Message string
}

var (
	imagerProgressRe = regexp.MustCompile(`^(Writing|Verifying): \[[^\]]*\]\s+(\d+)\s*%`)
	imagerIgnored    = []string{"Platform:", "Starting background", "CacheManager", "Optimal async", "FileOperations", "WARNING:"}
	vtCSIRe          = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	vtOSCRe          = regexp.MustCompile(`\x1b\][^\x07]*\x07`)
)

// stripVT removes the terminal control sequences (CSI and OSC) that the Windows
// console writes into the imager output.
func stripVT(s string) string {
	return vtOSCRe.ReplaceAllString(vtCSIRe.ReplaceAllString(s, ""), "")
}

// splitLines splits on both \r and \n, so progress updates that overwrite a line count as lines.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ParseImagerOutput reads the imager output line by line (\r and \n both separate lines,
// control sequences are removed) and calls emit for every recognized line.
func ParseImagerOutput(r io.Reader, emit func(Progress)) error {
	sc := bufio.NewScanner(r)
	sc.Split(splitLines)
	for sc.Scan() {
		raw := stripVT(sc.Text())
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if p, ok := parseImagerLine(raw); ok {
			emit(p)
		}
	}
	return sc.Err()
}

// parseImagerLine classifies one output line. raw keeps its indentation, because
// indented lines are the preparation steps.
func parseImagerLine(raw string) (Progress, bool) {
	line := strings.TrimSpace(raw)
	if m := imagerProgressRe.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[2])
		phase := PhaseWrite
		if m[1] == "Verifying" {
			phase = PhaseVerify
		}
		return Progress{Phase: phase, Percent: n}, true
	}
	if strings.HasPrefix(line, "Write successful.") {
		return Progress{Phase: PhaseDone}, true
	}
	for _, prefix := range []string{"Error:", "ERROR:"} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return Progress{Phase: PhaseError, Message: strings.TrimSpace(rest)}, true
		}
	}
	for _, prefix := range imagerIgnored {
		if strings.HasPrefix(line, prefix) {
			return Progress{}, false
		}
	}
	if strings.HasPrefix(raw, "  ") {
		return Progress{Phase: PhasePrepare, Message: line}, true
	}
	return Progress{}, false
}
