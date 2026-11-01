package unitlogs

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// maxMessageRunes begrenzt eine Journal-Nachricht. Laengere bekommen ein
// angehaengtes Auslassungszeichen.
const maxMessageRunes = 2000

// defaultPriority ist die journald-Vorgabe, wenn PRIORITY fehlt.
const defaultPriority = 6

// journalEntry ist der Teil eines `journalctl --output=json`-Eintrags, den
// der Strom braucht. MESSAGE bleibt roh, weil es String oder Byte-Array sein kann.
type journalEntry struct {
	Cursor    string          `json:"__CURSOR"`
	Realtime  string          `json:"__REALTIME_TIMESTAMP"`
	Priority  string          `json:"PRIORITY"`
	Unit      string          `json:"_SYSTEMD_UNIT"`
	UnitAbout string          `json:"UNIT"`
	Ident     string          `json:"SYSLOG_IDENTIFIER"`
	PID       string          `json:"_PID"`
	Message   json.RawMessage `json:"MESSAGE"`
}

// ParseJournalLine wandelt einen JSON-Eintrag von journalctl in eine
// Logzeile. Die Unit ist _SYSTEMD_UNIT, wenn sie in units steht, sonst UNIT
// (systemd berichtet ueber den Dienst). Alles andere wird verworfen (ok false).
func ParseJournalLine(raw []byte, units []string) (hostapi.UnitLogLine, bool) {
	var e journalEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return hostapi.UnitLogLine{}, false
	}
	if e.Cursor == "" || e.Realtime == "" {
		return hostapi.UnitLogLine{}, false
	}
	micros, err := strconv.ParseInt(e.Realtime, 10, 64)
	if err != nil {
		return hostapi.UnitLogLine{}, false
	}

	unit := ""
	switch {
	case containsString(units, e.Unit):
		unit = e.Unit
	case containsString(units, e.UnitAbout):
		unit = e.UnitAbout
	default:
		return hostapi.UnitLogLine{}, false
	}

	prio := defaultPriority
	if p, err := strconv.Atoi(e.Priority); err == nil {
		prio = p
	}

	return hostapi.UnitLogLine{
		Unit:     unit,
		At:       micros / 1000,
		Priority: prio,
		Ident:    e.Ident,
		PID:      e.PID,
		Message:  truncateRunes(decodeMessage(e.Message), maxMessageRunes),
		Cursor:   e.Cursor,
	}, true
}

// ParseStatus liest die Key=Value-Zeilen von `systemctl show`. Fehlende oder
// unbekannte Felder bleiben Nullwerte.
func ParseStatus(unit string, out string) hostapi.UnitStatus {
	st := hostapi.UnitStatus{Unit: unit}
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "LoadState":
			st.Load = value
		case "ActiveState":
			st.Active = value
		case "SubState":
			st.Sub = value
		case "Result":
			st.Result = value
		case "ExecMainStatus":
			st.ExitStatus = atoiOrZero(value)
		case "NRestarts":
			st.Restarts = atoiOrZero(value)
		case "MainPID":
			st.PID = atoiOrZero(value)
		case "ActiveEnterTimestamp":
			st.Since = parseSince(value)
		}
	}
	return st
}

// parseSince nimmt "@<Sekunden>" aus --timestamp=unix und liefert Unix-ms.
// Leer, "n/a" oder kaputt ergibt 0.
func parseSince(value string) int64 {
	secs, ok := strings.CutPrefix(value, "@")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return 0
	}
	return n * 1000
}

func atoiOrZero(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

// decodeMessage liest MESSAGE als String oder Byte-Array. Ungueltiges UTF-8
// wird zu U+FFFD.
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

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
