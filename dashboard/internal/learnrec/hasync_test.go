package learnrec

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

var hsLoc = time.FixedZone("CET", 3600)

// hsNow ist der Stand der Uhr im Test, veraenderbar ueber hsClock.
var hsNow = time.Date(2026, 11, 20, 12, 0, 0, 0, hsLoc)

func hsClock() time.Time { return hsNow }

func hsLimits(ram int64) func() Limits {
	return func() Limits { return Limits{RAMMinuteBytes: ram} }
}

// hsRequest ist eine Nachfrage, wie sie an den Companion geht.
type hsRequest struct {
	Peer   string     `json:"peer"`
	ReqID  string     `json:"req_id"`
	Tier   string     `json:"tier"`
	Series string     `json:"series"`
	Ranges [][2]int64 `json:"ranges"`
}

type hsCompanion struct {
	sent []hsRequest
}

func (c *hsCompanion) send(_ string, payload []byte) bool {
	var r hsRequest
	if err := json.Unmarshal(payload, &r); err != nil {
		return false
	}
	c.sent = append(c.sent, r)
	return true
}

// hsDayStart liefert den lokalen Tagesbeginn eines Tages im Format 2006-01-02.
func hsDayStart(t *testing.T, day string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02", day, hsLoc)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// hsFull liefert ein Deckungsraster, das jeden Bucket mit 60 Saetzen belegt.
func hsFull(from time.Time, step int64, buckets int) historyexchange.Census {
	n := make([]int, buckets)
	for i := range n {
		n[i] = 60
	}
	return historyexchange.Census{From: from.UnixMilli(), Step: step, N: n}
}

// hsSetup legt einen HASync mit einem gestellten Companion an.
func hsSetup(t *testing.T, ram int64) (*HASync, *hsCompanion) {
	t.Helper()
	companion := &hsCompanion{}
	s := NewHASync(hsClock, hsLimits(ram), hsLoc)
	s.Attach(companion.send, func(id string) bool { return id == "companion" })
	return s, companion
}

// hsAnswer beantwortet die zuletzt gesendete Nachfrage.
func hsAnswer(s *HASync, c *hsCompanion, rows []historyexchange.Row, final bool) {
	if len(c.sent) == 0 {
		return
	}
	last := c.sent[len(c.sent)-1]
	s.OnDeliver("companion", last.ReqID, last.Tier, rows, final)
}

// hsDrain beantwortet jede Nachfrage leer und endgueltig, bis keine neue mehr kommt.
func hsDrain(s *HASync, c *hsCompanion) {
	for n := 0; n < len(c.sent); n++ {
		s.OnDeliver("companion", c.sent[n].ReqID, c.sent[n].Tier, nil, true)
	}
}

func TestHASyncFresh1mNeedsRecentOfferWithMinuteCoverage(t *testing.T) {
	s, _ := hsSetup(t, 1<<30)
	// Stunden-Raster bis 13:00 Uhr: der Bucket mit 11:55 Uhr hat Saetze.
	from := time.Date(2026, 11, 20, 9, 0, 0, 0, hsLoc)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"1m": {"pv": hsFull(from, 3600000, 4)},
	})
	if !s.Fresh1m(hsNow) {
		t.Fatal("Angebot mit 1m-Deckung bis jetzt: Fresh1m muss wahr sein")
	}
	if s.Fresh1m(hsNow.Add(11 * time.Minute)) {
		t.Fatal("11 Minuten ohne neues Angebot: Fresh1m muss falsch sein")
	}
}

func TestHASyncFresh1mFalseWithoutMinuteTier(t *testing.T) {
	s, _ := hsSetup(t, 1<<30)
	from := time.Date(2026, 11, 20, 9, 0, 0, 0, hsLoc)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"5m": {"pv": hsFull(from, 21600000, 1)},
	})
	if s.Fresh1m(hsNow) {
		t.Fatal("nur 5m angeboten: Fresh1m muss falsch sein")
	}
}

func TestHASyncPrefetchMinuteValuesFromOneMinuteTier(t *testing.T) {
	s, c := hsSetup(t, 1<<30)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"1m": {"pv": hsFull(hsDayStart(t, "2026-11-18"), 3600000, 72)},
	})
	s.Prefetch([]string{"e:pv"}, []string{"2026-11-18", "2026-11-19", "2026-11-20"}, hsNow.Add(time.Hour))
	if len(c.sent) != 1 {
		t.Fatalf("erste Nachfrage fehlt: %d gesendet", len(c.sent))
	}
	first := c.sent[0]
	day := hsDayStart(t, "2026-11-20")
	if first.Peer != historyexchange.LearnPeerID || first.Tier != "1m" || first.Series != "pv" {
		t.Fatalf("Nachfrage = %+v", first)
	}
	if len(first.Ranges) != 1 || first.Ranges[0] != [2]int64{day.UnixMilli(), day.Add(24 * time.Hour).UnixMilli()} {
		t.Fatalf("Bereich = %v", first.Ranges)
	}
	hsAnswer(s, c, []historyexchange.Row{
		{Series: "pv", TS: day.UnixMilli(), Avg: 100},
		{Series: "pv", TS: day.Add(time.Minute).UnixMilli(), Avg: 200},
	}, true)
	values, fiveMin := s.Day("2026-11-20")
	if values["e:pv"] == nil || values["e:pv"][0] != 100 || values["e:pv"][1] != 200 {
		t.Fatalf("Minutenwerte fehlen: %v", values)
	}
	if fiveMin["e:pv"] {
		t.Fatal("1m-Antwort darf nicht als 5m markiert sein")
	}
	if len(c.sent) != 2 || c.sent[1].Ranges[0][0] != hsDayStart(t, "2026-11-19").UnixMilli() {
		t.Fatalf("nach der Antwort muss die Nachfrage fuer den naechsten Tag folgen: %+v", c.sent)
	}
}

func TestHASyncFiveMinuteValuesSpreadOverFiveMinutes(t *testing.T) {
	s, c := hsSetup(t, 1<<30)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"5m": {"pv": hsFull(hsDayStart(t, "2026-11-01"), 21600000, 120)},
	})
	s.Prefetch([]string{"e:pv"}, []string{"2026-11-05"}, hsNow.Add(time.Hour))
	if len(c.sent) != 1 || c.sent[0].Tier != "5m" {
		t.Fatalf("Nachfrage fuer 15 Tage zurueck muss 5m sein: %+v", c.sent)
	}
	day := hsDayStart(t, "2026-11-05")
	hsAnswer(s, c, []historyexchange.Row{{Series: "pv", TS: day.UnixMilli(), Avg: 300}}, true)
	values, fiveMin := s.Day("2026-11-05")
	arr := values["e:pv"]
	if arr == nil {
		t.Fatal("5m-Wert fehlt")
	}
	for i := 0; i < 5; i++ {
		if arr[i] != 300 {
			t.Fatalf("Minute %d = %v, erwartet 300", i, arr[i])
		}
	}
	if !math.IsNaN(float64(arr[5])) {
		t.Fatalf("Minute 5 = %v, erwartet Luecke (NaN)", arr[5])
	}
	if !fiveMin["e:pv"] {
		t.Fatal("5m-Werte muessen als 5m markiert sein")
	}
}

func TestHASyncBudgetKeepsOnlyTheNewestDays(t *testing.T) {
	keys := make([]string, 0, 120)
	coverage := map[string]historyexchange.Census{}
	for i := 0; i < 120; i++ {
		name := "k" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		keys = append(keys, "e:"+name)
		coverage[name] = hsFull(hsDayStart(t, "2026-11-18"), 3600000, 72)
	}
	// Budget RAMMinuteBytes/2 = 120 Schluessel * 2 Tage * 1440 * 4 Byte.
	s, c := hsSetup(t, 2*120*2*1440*4)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{"1m": coverage})
	s.Prefetch(keys, []string{"2026-11-18", "2026-11-19", "2026-11-20"}, hsNow.Add(time.Hour))
	hsDrain(s, c)
	if len(c.sent) != 240 {
		t.Fatalf("Nachfragen = %d, erwartet 240 (120 Schluessel, 2 Tage)", len(c.sent))
	}
	for _, r := range c.sent {
		if r.Ranges[0][0] < hsDayStart(t, "2026-11-19").UnixMilli() {
			t.Fatalf("aelterer Tag angefragt: %v", r.Ranges)
		}
	}
}

func TestHASyncAnswerAfterDeadlineIsDiscarded(t *testing.T) {
	s, c := hsSetup(t, 1<<30)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"1m": {"pv": hsFull(hsDayStart(t, "2026-11-18"), 3600000, 72)},
	})
	day := hsDayStart(t, "2026-11-20")
	s.Prefetch([]string{"e:pv"}, []string{"2026-11-20"}, hsNow.Add(time.Minute))
	if len(c.sent) != 1 {
		t.Fatalf("Nachfrage vor der Frist fehlt: %d gesendet", len(c.sent))
	}
	late := hsNow
	hsNow = hsNow.Add(2 * time.Minute)
	defer func() { hsNow = late }()
	hsAnswer(s, c, []historyexchange.Row{{Series: "pv", TS: day.UnixMilli(), Avg: 100}}, true)
	values, _ := s.Day("2026-11-20")
	if len(values) != 0 {
		t.Fatalf("Antwort nach der Frist wurde uebernommen: %v", values)
	}
}

func TestHASyncDropClearsAndIgnoresOldAnswers(t *testing.T) {
	s, c := hsSetup(t, 1<<30)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"1m": {"pv": hsFull(hsDayStart(t, "2026-11-18"), 3600000, 72)},
	})
	day := hsDayStart(t, "2026-11-20")
	s.Prefetch([]string{"e:pv"}, []string{"2026-11-20", "2026-11-19"}, hsNow.Add(time.Hour))
	hsAnswer(s, c, []historyexchange.Row{{Series: "pv", TS: day.UnixMilli(), Avg: 100}}, false)
	values, _ := s.Day("2026-11-20")
	if len(values) == 0 {
		t.Fatal("vor Drop fehlt der Wert")
	}
	s.Drop()
	values, fiveMin := s.Day("2026-11-20")
	if len(values) != 0 || len(fiveMin) != 0 {
		t.Fatalf("Drop hat den Zwischenspeicher nicht geleert: %v", values)
	}
	sent := len(c.sent)
	hsAnswer(s, c, []historyexchange.Row{{Series: "pv", TS: day.UnixMilli(), Avg: 5}}, true)
	if len(c.sent) != sent {
		t.Fatal("nach Drop darf keine Nachfrage mehr gehen")
	}
	values, _ = s.Day("2026-11-20")
	if len(values) != 0 {
		t.Fatalf("Antwort nach Drop wurde gespeichert: %v", values)
	}
}

func TestHASyncAnswerWithOtherTierIsDiscarded(t *testing.T) {
	s, c := hsSetup(t, 1<<30)
	s.OnOffer("companion", map[string]map[string]historyexchange.Census{
		"1m": {"pv": hsFull(hsDayStart(t, "2026-11-18"), 3600000, 72)},
	})
	day := hsDayStart(t, "2026-11-20")
	s.Prefetch([]string{"e:pv"}, []string{"2026-11-20"}, hsNow.Add(time.Hour))
	last := c.sent[0]
	s.OnDeliver("companion", last.ReqID, haTierFive, []historyexchange.Row{{Series: "pv", TS: day.UnixMilli(), Avg: 100}}, true)
	values, _ := s.Day("2026-11-20")
	if len(values) != 0 || len(c.sent) != 1 {
		t.Fatalf("Lieferung mit anderer Stufe wurde uebernommen: %v, gesendet %d", values, len(c.sent))
	}
}
