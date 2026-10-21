// Package servicelogs reads journal entries of allowlisted services through
// the system action helper.
package servicelogs

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
