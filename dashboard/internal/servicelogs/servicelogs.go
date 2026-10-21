// Package servicelogs reads journal entries of allowlisted services through
// the system action helper.
package servicelogs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var Units = []string{"dashboard", "updater", "automation", "battery-soc", "shelly",
	"trucki", "tuya", "apsystems", "mosquitto", "caddy", "tailscale"}
var SinceMinutes = []int{15, 60, 360, 1440, 10080}
var LineLimits = []int{100, 200, 500, 1000}

const (
	DefaultUnit         = "dashboard"
	DefaultSinceMinutes = 60
	DefaultLines        = 200
	MaxMessageRunes     = 2000
	MaxConcurrent       = 2
	QueryTimeout        = 10 * time.Second
)

const (
	maxCursorLen = 256
	maxFilterLen = 200
)

type Query struct {
	Unit         string
	SinceMinutes int
	Lines        int
	Cursor       string // leer = erste Abfrage
	Filter       string // Teilstring, Groß-/Kleinschreibung egal, max. 200 Runes
}

type Entry struct {
	Time      time.Time `json:"time"`
	Priority  int       `json:"priority"` // 0..7, fehlt im Journal → 6
	Unit      string    `json:"unit"`     // _SYSTEMD_UNIT
	Message   string    `json:"message"`
	Truncated bool      `json:"truncated,omitempty"`
}

type Result struct {
	Entries []Entry `json:"entries"` // nie nil, älteste zuerst
	Cursor  string  `json:"cursor"`  // __CURSOR der letzten GEHOLTEN Zeile, sonst Query.Cursor
	Limited bool    `json:"limited"` // Anzahl geholter Zeilen == Query.Lines
}

var (
	ErrInvalidQuery = errors.New("invalid log query")
	ErrNotPermitted = errors.New("journal access not permitted")
	ErrJournal      = errors.New("journal read failed")
	ErrTimeout      = errors.New("journal read timed out")
	ErrBusy         = errors.New("too many journal reads")
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Validate checks every field against the allowlists. The cursor must match
// what the helper accepts (prefix "s=", at most 256 characters, only
// [A-Za-z0-9=;]), so an invalid cursor is rejected before any sudo call.
func (q Query) Validate() error {
	if !slices.Contains(Units, q.Unit) {
		return fmt.Errorf("%w: unit %q", ErrInvalidQuery, q.Unit)
	}
	if !slices.Contains(SinceMinutes, q.SinceMinutes) {
		return fmt.Errorf("%w: since %d", ErrInvalidQuery, q.SinceMinutes)
	}
	if !slices.Contains(LineLimits, q.Lines) {
		return fmt.Errorf("%w: lines %d", ErrInvalidQuery, q.Lines)
	}
	if q.Cursor != "" {
		if !strings.HasPrefix(q.Cursor, "s=") || len(q.Cursor) > maxCursorLen {
			return fmt.Errorf("%w: cursor", ErrInvalidQuery)
		}
		for _, c := range q.Cursor {
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '=' || c == ';') {
				return fmt.Errorf("%w: cursor", ErrInvalidQuery)
			}
		}
	}
	if utf8.RuneCountInString(q.Filter) > maxFilterLen {
		return fmt.Errorf("%w: filter longer than %d characters", ErrInvalidQuery, maxFilterLen)
	}
	return nil
}

const maxScanLine = 1 << 20

// Reader runs the system action helper's read-journal action for one query at
// a time per slot, and never more than MaxConcurrent at once.
type Reader struct {
	runner  Runner
	helper  string
	timeout time.Duration
	sem     chan struct{}
}

// NewReader returns a Reader that calls helper through sudo. A nil runner
// executes the real commands.
func NewReader(runner Runner, helper string) *Reader {
	if runner == nil {
		runner = execRunner{}
	}
	return &Reader{
		runner:  runner,
		helper:  helper,
		timeout: QueryTimeout,
		sem:     make(chan struct{}, MaxConcurrent),
	}
}

// Read validates the query, runs the helper and returns the matching entries.
// The filter is applied here, the cursor advances past every fetched line.
func (r *Reader) Read(ctx context.Context, q Query) (Result, error) {
	if err := q.Validate(); err != nil {
		return Result{}, err
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	default:
		return Result{}, ErrBusy
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []string{"-n", r.helper, "read-journal", q.Unit, strconv.Itoa(q.SinceMinutes), strconv.Itoa(q.Lines)}
	if q.Cursor != "" {
		args = append(args, q.Cursor)
	}
	stdout, stderr, err := r.runner.Run(ctx, "sudo", args...)
	if err != nil {
		return Result{}, mapRunError(ctx, err, stderr)
	}

	result := Result{Entries: []Entry{}, Cursor: q.Cursor}
	needle := strings.ToLower(q.Filter)
	fetched := 0
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanLine)
	for scanner.Scan() {
		entry, cursor, ok := parseLine(scanner.Bytes())
		if !ok {
			continue
		}
		fetched++
		result.Cursor = cursor
		if needle == "" || strings.Contains(strings.ToLower(entry.Message), needle) {
			result.Entries = append(result.Entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("%w: %s", ErrJournal, err)
	}
	result.Limited = fetched == q.Lines
	return result, nil
}

// mapRunError turns a failed helper call into one of the package errors. The
// checks run in this order: deadline, sudo refusal, helper rejection, rest.
func mapRunError(ctx context.Context, err error, stderr []byte) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	detail := firstStderrLine(stderr)
	if strings.HasPrefix(detail, "sudo:") {
		return fmt.Errorf("%w: %s", ErrNotPermitted, detail)
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && (exit.ExitCode() == 64 || exit.ExitCode() == 65) {
		return fmt.Errorf("%w: helper rejected the query", ErrInvalidQuery)
	}
	if detail == "" {
		return ErrJournal
	}
	return fmt.Errorf("%w: %s", ErrJournal, detail)
}

// firstStderrLine returns the first non-empty stderr line, cut to 200 characters.
func firstStderrLine(stderr []byte) string {
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 200 {
			line = string([]rune(line)[:200])
		}
		return line
	}
	return ""
}

// execRunner runs real commands and collects their output.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
