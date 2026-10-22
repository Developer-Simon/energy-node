package servicelogs

import (
	"strings"
	"testing"
	"time"
)

func TestParseEntry(t *testing.T) {
	line := []byte(`{"__CURSOR":"s=1;i=2","__REALTIME_TIMESTAMP":"1761040800123456","PRIORITY":"3","_SYSTEMD_UNIT":"tuya.service","MESSAGE":"boom"}`)
	e, cursor, ok := parseLine(line)
	if !ok {
		t.Fatal("parseLine rejected a valid line")
	}
	want := Entry{Time: time.UnixMicro(1761040800123456), Priority: 3, Unit: "tuya.service", Message: "boom"}
	if !e.Time.Equal(want.Time) || e.Priority != want.Priority || e.Unit != want.Unit || e.Message != want.Message || e.Truncated {
		t.Fatalf("entry = %+v, want %+v", e, want)
	}
	if cursor != "s=1;i=2" {
		t.Fatalf("cursor = %q, want s=1;i=2", cursor)
	}
}

func TestParseMessageByteArray(t *testing.T) {
	line := []byte(`{"__CURSOR":"s=1","__REALTIME_TIMESTAMP":"1761040800123456","PRIORITY":"6","_SYSTEMD_UNIT":"tuya.service","MESSAGE":[104,105,255]}`)
	e, _, ok := parseLine(line)
	if !ok {
		t.Fatal("parseLine rejected a valid line")
	}
	if e.Message != "hi�" {
		t.Fatalf("message = %q, want %q", e.Message, "hi�")
	}
}

func TestParseMissingPriority(t *testing.T) {
	line := []byte(`{"__CURSOR":"s=1","__REALTIME_TIMESTAMP":"1761040800123456","_SYSTEMD_UNIT":"tuya.service","MESSAGE":"ohne"}`)
	e, _, ok := parseLine(line)
	if !ok {
		t.Fatal("parseLine rejected a valid line")
	}
	if e.Priority != 6 {
		t.Fatalf("priority = %d, want 6", e.Priority)
	}
}

func TestParseTruncatesLongMessage(t *testing.T) {
	msg := strings.Repeat("ä", 2500)
	line := []byte(`{"__CURSOR":"s=1","__REALTIME_TIMESTAMP":"1761040800123456","PRIORITY":"6","_SYSTEMD_UNIT":"tuya.service","MESSAGE":"` + msg + `"}`)
	e, _, ok := parseLine(line)
	if !ok {
		t.Fatal("parseLine rejected a valid line")
	}
	if n := len([]rune(e.Message)); n != MaxMessageRunes {
		t.Fatalf("message runes = %d, want %d", n, MaxMessageRunes)
	}
	if !e.Truncated {
		t.Fatal("Truncated = false, want true")
	}
}

func TestParseSkipsBrokenLine(t *testing.T) {
	good1 := []byte(`{"__CURSOR":"s=1","__REALTIME_TIMESTAMP":"1761040800123456","PRIORITY":"6","_SYSTEMD_UNIT":"tuya.service","MESSAGE":"eins"}`)
	broken := []byte(`{"__CURSOR":"s=2","MESSAGE":`)
	good2 := []byte(`{"__CURSOR":"s=3","__REALTIME_TIMESTAMP":"1761040800223456","PRIORITY":"6","_SYSTEMD_UNIT":"tuya.service","MESSAGE":"zwei"}`)
	if _, _, ok := parseLine(good1); !ok {
		t.Fatal("first valid line rejected")
	}
	if _, _, ok := parseLine(broken); ok {
		t.Fatal("broken line accepted")
	}
	if _, _, ok := parseLine(good2); !ok {
		t.Fatal("second valid line rejected")
	}
}
