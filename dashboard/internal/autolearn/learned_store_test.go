package autolearn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lampValue is the learned usual_off of the device stehlampe as the learner gives it.
func lampValue(text string, n int) []Value {
	return []Value{{Key: "device/stehlampe/usual_off", Device: "stehlampe", Kind: ValueTime,
		Group: "habit", Text: text, N: n}}
}

// everyDevice says that all devices exist.
func everyDevice(string) bool { return true }

// payloadOf decodes a message payload into its value, n and at.
func payloadOf(t *testing.T, m Message) (value any, n int, at int64) {
	t.Helper()
	var p struct {
		Value any   `json:"value"`
		N     int   `json:"n"`
		At    int64 `json:"at"`
	}
	if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
		t.Fatalf("payload %q: %v", m.Payload, err)
	}
	return p.Value, p.N, p.At
}

func TestLearnedStoreFirstValuePublishesOnce(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	msgs := s.Update(lampValue("22:30", 12), now, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v, want one", msgs)
	}
	if msgs[0].Topic != "outstation/energy_node/learned/device/stehlampe/usual_off" {
		t.Errorf("topic = %q", msgs[0].Topic)
	}
	value, n, at := payloadOf(t, msgs[0])
	if value != "22:30" || n != 12 || at != now.Unix() {
		t.Errorf("payload = %s, want value 22:30, n 12, at %d", msgs[0].Payload, now.Unix())
	}
}

func TestLearnedStoreSameValueRepublishedOnlyAfterSixtyMinutes(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Update(lampValue("22:30", 12), now, everyDevice)
	if msgs := s.Update(lampValue("22:30", 12), now.Add(10*time.Minute), everyDevice); len(msgs) != 0 {
		t.Fatalf("after 10 min: %+v, want nothing", msgs)
	}
	later := now.Add(61 * time.Minute)
	msgs := s.Update(lampValue("22:30", 12), later, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("after 61 min: %+v, want one message", msgs)
	}
	if _, _, at := payloadOf(t, msgs[0]); at != later.Unix() {
		t.Errorf("at = %d, want the new time %d", at, later.Unix())
	}
}

func TestLearnedStoreChangedValueWaitsFifteenMinutes(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Update(lampValue("22:30", 12), now, everyDevice)
	if msgs := s.Update(lampValue("22:45", 12), now.Add(5*time.Minute), everyDevice); len(msgs) != 0 {
		t.Fatalf("change after 5 min: %+v, want nothing", msgs)
	}
	msgs := s.Update(lampValue("22:45", 12), now.Add(16*time.Minute), everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("change after 16 min: %+v, want one message", msgs)
	}
	if value, _, _ := payloadOf(t, msgs[0]); value != "22:45" {
		t.Errorf("value = %v, want 22:45", value)
	}
	entries := s.Entries()
	if len(entries) != 1 || entries[0].Prev != "22:30" || entries[0].Value != "22:45" {
		t.Errorf("entries = %+v, want prev 22:30 and value 22:45", entries)
	}
}

func TestLearnedStoreDeletedDeviceClearsTopic(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Update(lampValue("22:30", 12), now, everyDevice)
	msgs := s.Update(nil, now.Add(time.Hour), func(string) bool { return false })
	if len(msgs) != 1 || msgs[0].Payload != "" {
		t.Fatalf("messages = %+v, want one empty payload", msgs)
	}
	if got := s.Entries(); len(got) != 0 {
		t.Errorf("entries = %+v, want none", got)
	}
}

func TestLearnedStoreResetEmptiesAllTopics(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	vals := append(lampValue("22:30", 12), Value{Key: "site/house_state", Kind: ValueState,
		Group: "habit", Text: "quiet", N: 9})
	s.Update(vals, now, everyDevice)
	msgs := s.Reset()
	if len(msgs) != 2 {
		t.Fatalf("reset messages = %+v, want two", msgs)
	}
	for _, m := range msgs {
		if m.Payload != "" {
			t.Errorf("reset payload on %s = %q, want empty", m.Topic, m.Payload)
		}
	}
	if got := s.Entries(); len(got) != 0 {
		t.Errorf("entries after reset = %+v, want none", got)
	}
}

func TestLearnedStoreMissingValueKeepsEntry(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Update(lampValue("22:30", 12), now, everyDevice)
	if msgs := s.Update(nil, now.Add(2*time.Hour), everyDevice); len(msgs) != 0 {
		t.Fatalf("missing value published %+v, want nothing", msgs)
	}
	if got := s.Entries(); len(got) != 1 || got[0].Value != "22:30" {
		t.Errorf("entries = %+v, want the old entry kept", got)
	}
}

func TestLearnedStoreFailedPublishIsSentAgain(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	first := s.Update(lampValue("22:30", 12), now, everyDevice)
	s.Revert(first)
	if msgs := s.Update(lampValue("22:30", 12), now.Add(time.Minute), everyDevice); len(msgs) != 1 {
		t.Fatalf("after failed publish: %+v, want the message sent again", msgs)
	}
}

func TestLearnedStoreSaveAndLoadKeepEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autolearn", "learned.json")
	s := LoadLearnedStore(path, nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Update(append(lampValue("22:30", 12), Value{Key: "device/heater/usual_runtime_s",
		Device: "heater", Kind: ValueDuration, Group: "habit", Num: 2400, N: 12}), now, everyDevice)
	s.Update(lampValue("22:45", 12), now.Add(16*time.Minute), everyDevice)
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	loaded := LoadLearnedStore(path, nil)
	want, got := s.Entries(), loaded.Entries()
	if len(got) != len(want) {
		t.Fatalf("loaded %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLearnedStoreCorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logged int
	s := LoadLearnedStore(path, func(string, ...any) { logged++ })
	if got := s.Entries(); len(got) != 0 || logged != 1 {
		t.Errorf("entries = %+v, logged %d, want none and one log line", got, logged)
	}
}

func TestLearnedStoreNumberKindPublishesJSONNumber(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	msgs := s.Update([]Value{{Key: "site/pv_reserve_w", Kind: ValueNumber, Group: "habit",
		Unit: "W", Num: 1700, N: 9}}, now, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v, want one", msgs)
	}
	want := `{"value":1700,"n":9,"at":` + strconv.FormatInt(now.Unix(), 10) + `}`
	if msgs[0].Payload != want {
		t.Errorf("payload = %s, want %s", msgs[0].Payload, want)
	}
	if e := s.Entries()[0]; e.Unit != "W" || e.Estimate {
		t.Errorf("entry = %+v, want unit W and no estimate", e)
	}
}

func TestLearnedStoreEstimatePayloadCarriesFlag(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	msgs := s.Update([]Value{{Key: "site/pv_reserve_w", Kind: ValueNumber, Unit: "W",
		Num: 850, N: 4, Estimate: true}}, now, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v, want one", msgs)
	}
	want := `{"value":850,"n":4,"at":` + strconv.FormatInt(now.Unix(), 10) + `,"est":true}`
	if msgs[0].Payload != want {
		t.Errorf("payload = %s, want %s", msgs[0].Payload, want)
	}
	if e := s.Entries()[0]; !e.Estimate || e.Unit != "W" {
		t.Errorf("entry = %+v, want estimate and unit W", e)
	}
}

func TestLearnedStoreEstimateFlipRepublishesAfterChangeGap(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	measured := []Value{{Key: "site/pv_reserve_w", Kind: ValueNumber, Unit: "W", Num: 1700, N: 9}}
	estimated := []Value{{Key: "site/pv_reserve_w", Kind: ValueNumber, Unit: "W", Num: 1700, N: 9, Estimate: true}}
	s.Update(measured, now, everyDevice)
	if msgs := s.Update(estimated, now.Add(5*time.Minute), everyDevice); len(msgs) != 0 {
		t.Fatalf("flip after 5 min: %+v, want nothing", msgs)
	}
	msgs := s.Update(estimated, now.Add(16*time.Minute), everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("flip after 16 min: %+v, want one message", msgs)
	}
	if !strings.Contains(msgs[0].Payload, `"est":true`) {
		t.Errorf("payload = %s, want est true", msgs[0].Payload)
	}
	back := s.Update(measured, now.Add(40*time.Minute), everyDevice)
	if len(back) != 1 || strings.Contains(back[0].Payload, "est") {
		t.Fatalf("flip back: %+v, want one message without est", back)
	}
}

func TestLearnedStoreOldStateFileLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	old := `{"version":1,"entries":[{"key":"device/stehlampe/usual_off","device":"stehlampe",` +
		`"kind":"time","value":"22:30","n":12,"at":1763632800,"changed_at":1763632800,"published_at":1763632800}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadLearnedStore(path, nil)
	got := s.Entries()
	if len(got) != 1 || got[0].Value != "22:30" || got[0].Unit != "" || got[0].Estimate {
		t.Fatalf("entries = %+v, want the old entry without unit or estimate", got)
	}
	if msgs := s.Update(lampValue("22:30", 12), time.Unix(1763632800, 0).Add(time.Minute), everyDevice); len(msgs) != 0 {
		t.Errorf("unchanged old entry published: %+v", msgs)
	}
}

func TestLearnedStorePrograms(t *testing.T) {
	s := LoadLearnedStore(filepath.Join(t.TempDir(), "learned.json"), nil)
	now := time.Date(2026, 3, 20, 3, 30, 0, 0, time.UTC)
	vals := []Value{{Key: "device/geschirr/cycle_wh", Device: "geschirr", Kind: ValueNumber,
		Unit: "Wh", Group: "energy", Num: 900, N: 12, Programs: []float64{500}}}
	msgs := s.Update(vals, now, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if strings.Contains(msgs[0].Payload, "programs") {
		t.Errorf("payload %s carries programs, want none", msgs[0].Payload)
	}
	entries := s.Entries()
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].Programs, []float64{500}) {
		t.Errorf("entries = %+v, want programs [500]", entries)
	}
}

// A refresh of an unchanged value must still reach the file with the new
// programs, the unit and the estimate flag, so a restart keeps them.
func TestLearnedStoreRefreshSavesProgramsUnitAndEstimate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	s := LoadLearnedStore(path, nil)
	now := time.Date(2026, 3, 20, 3, 30, 0, 0, time.UTC)
	first := []Value{{Key: "device/geschirr/cycle_wh", Device: "geschirr", Kind: ValueNumber,
		Unit: "Wh", Group: "energy", Num: 900, N: 12, Estimate: true, Programs: []float64{500}}}
	s.Update(first, now, everyDevice)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	later := now.Add(learnedRefreshGap + time.Minute)
	refresh := []Value{{Key: "device/geschirr/cycle_wh", Device: "geschirr", Kind: ValueNumber,
		Unit: "Wh", Group: "energy", Num: 900, N: 13, Estimate: true, Programs: []float64{420}}}
	msgs := s.Update(refresh, later, everyDevice)
	if len(msgs) != 1 {
		t.Fatalf("refresh sent %d messages, want 1", len(msgs))
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got := LoadLearnedStore(path, nil).Entries()
	if len(got) != 1 {
		t.Fatalf("reloaded %d entries, want 1", len(got))
	}
	e := got[0]
	if !reflect.DeepEqual(e.Programs, []float64{420}) {
		t.Errorf("programs after reload = %v, want [420]", e.Programs)
	}
	if e.Unit != "Wh" || !e.Estimate || e.N != 13 {
		t.Errorf("unit %q estimate %v n %d after reload, want Wh true 13", e.Unit, e.Estimate, e.N)
	}
}
