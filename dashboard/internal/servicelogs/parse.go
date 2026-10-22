package servicelogs

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// journalLine holds the journalctl --output=json fields used by the reader.
type journalLine struct {
	Cursor   string          `json:"__CURSOR"`
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
	Priority string          `json:"PRIORITY"`
	Unit     string          `json:"_SYSTEMD_UNIT"`
	Message  json.RawMessage `json:"MESSAGE"`
}

// parseLine decodes one journal JSON line. It returns the entry, its cursor
// and false if the line is not a usable journal entry.
func parseLine(line []byte) (Entry, string, bool) {
	var j journalLine
	if err := json.Unmarshal(line, &j); err != nil {
		return Entry{}, "", false
	}
	if j.Cursor == "" {
		return Entry{}, "", false
	}
	usec, err := strconv.ParseInt(j.Realtime, 10, 64)
	if err != nil {
		return Entry{}, "", false
	}

	prio := 6
	if p, err := strconv.Atoi(j.Priority); err == nil && p >= 0 && p <= 7 {
		prio = p
	}

	msg := decodeMessage(j.Message)
	truncated := false
	if utf8.RuneCountInString(msg) > MaxMessageRunes {
		msg = string([]rune(msg)[:MaxMessageRunes])
		truncated = true
	}

	return Entry{
		Time:      time.UnixMicro(usec),
		Priority:  prio,
		Unit:      j.Unit,
		Message:   msg,
		Truncated: truncated,
	}, j.Cursor, true
}

// decodeMessage accepts MESSAGE as a JSON string or as a byte array, which
// journalctl uses for non-UTF-8 content. Invalid bytes become U+FFFD.
func decodeMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '[' {
		var nums []int
		if json.Unmarshal(raw, &nums) != nil {
			return ""
		}
		var b strings.Builder
		for _, n := range nums {
			b.WriteByte(byte(n))
		}
		return strings.ToValidUTF8(b.String(), "�")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.ToValidUTF8(s, "�")
}
