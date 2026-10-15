package energystats

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

const testDayMs = int64(24 * time.Hour / time.Millisecond)

type sentRequest struct {
	to   string
	body map[string]any
}

type syncHarness struct {
	sync  *Sync
	store *Store
	now   time.Time
	sent  []sentRequest
	peers map[string]bool
}

func newSyncHarness(t *testing.T, now string) *syncHarness {
	t.Helper()
	h := &syncHarness{store: NewStore(t.TempDir()), now: hourAt(now), peers: map[string]bool{"p-ha": true}}
	h.sync = NewSync(h.store, func() time.Time { return h.now })
	h.sync.Attach(func(to string, payload []byte) bool {
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatalf("Nachfrage kein JSON: %v", err)
		}
		h.sent = append(h.sent, sentRequest{to, body})
		return h.peers[to]
	}, func(id string) bool { return h.peers[id] })
	return h
}

func census(from string, n ...int) historyexchange.Census {
	return historyexchange.Census{From: hourAt(from).UnixMilli(), Step: testDayMs, N: n}
}

func ms(s string) int64 { return hourAt(s).UnixMilli() }

func ranges(body map[string]any) [][2]int64 {
	var out [][2]int64
	for _, item := range body["ranges"].([]any) {
		pair := item.([]any)
		out = append(out, [2]int64{int64(pair[0].(float64)), int64(pair[1].(float64))})
	}
	return out
}

func haDay(h *syncHarness, role, day string, hours int) {
	var records []HARecord
	for i := 0; i < hours; i++ {
		records = append(records, HARecord{Hour: hourAt(day).Add(time.Duration(i) * time.Hour), Role: role, KWh: 0.1})
	}
	h.store.SetHA(records)
}

func hourRow(series, at string, avg float64) historyexchange.Row {
	return historyexchange.Row{Series: series, TS: ms(at), Min: avg, Max: avg, Avg: avg, N: 1, U: "W"}
}

func TestOfferRequestsMissingDaysInOneMergedRequest(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	haDay(h, "pv", "2026-10-09T00:00:00Z", 24)
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:pv": census("2026-10-08T00:00:00Z", 24, 24, 24, 12)})
	if len(h.sent) != 1 {
		t.Fatalf("%d Nachfragen, erwartet 1", len(h.sent))
	}
	req := h.sent[0]
	if req.to != "p-ha" || req.body["peer"] != historyexchange.StatsPeerID || req.body["tier"] != "1h" || req.body["series"] != "role:pv" {
		t.Fatalf("Nachfrage = %+v", req)
	}
	want := [][2]int64{{ms("2026-10-08T00:00:00Z"), ms("2026-10-09T00:00:00Z")}, {ms("2026-10-10T00:00:00Z"), ms("2026-10-12T00:00:00Z")}}
	if got := ranges(req.body); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ranges = %v, erwartet %v", got, want)
	}
	if st := h.sync.Status(); !st.Connected || st.MissingDays != 3 {
		t.Fatalf("Status = %+v", st)
	}
}

func TestLongBackfillIsSplitNewestFirst(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	n := make([]int, 40)
	for i := range n {
		n[i] = 24
	}
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:pv": census("2026-09-01T00:00:00Z", n...)})
	got := ranges(h.sent[0].body)
	// 40 Tage ab 01.09.: die neuesten 31 zuerst, also 10.09. bis 10.10. einschliesslich.
	if len(got) != 1 || got[0] != [2]int64{ms("2026-09-10T00:00:00Z"), ms("2026-10-11T00:00:00Z")} {
		t.Fatalf("erste Nachfrage = %v", got)
	}
}

func TestDeliveryIsStoredAndTheNextSeriesIsRequested(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"role:pv":         census("2026-10-10T00:00:00Z", 1),
		"role:load_total": census("2026-10-10T00:00:00Z", 1),
	})
	first := h.sent[0].body
	h.sync.OnDeliver("p-ha", first["req_id"].(string), []historyexchange.Row{hourRow(first["series"].(string), "2026-10-10T05:00:00Z", 1500)}, true)
	if len(h.sent) != 2 || h.sent[1].body["series"] == first["series"] {
		t.Fatalf("zweite Nachfrage fehlt: %+v", h.sent)
	}
	role := first["series"].(string)[len("role:"):]
	if source, _ := h.store.SourceAt(role, hourAt("2026-10-10T05:00:00Z")); source != SourceHA {
		t.Fatalf("Quelle = %q", source)
	}
	var kwh float64
	h.store.Each(hourAt("2026-10-10T05:00:00Z"), hourAt("2026-10-10T06:00:00Z"), func(_ time.Time, m *Month, i int) { kwh = m.Roles[role].KWh[i] })
	if math.Abs(kwh-1.5) > 1e-9 {
		t.Fatalf("kWh = %v, erwartet 1.5", kwh)
	}
}

func TestInvalidRowsAndForeignDeliveriesAreDropped(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:pv": census("2026-10-11T00:00:00Z", 12)})
	id := h.sent[0].body["req_id"].(string)

	h.sync.OnDeliver("p-other", id, []historyexchange.Row{hourRow("role:pv", "2026-10-11T01:00:00Z", 100)}, false)
	h.sync.OnDeliver("p-ha", "stats-999", []historyexchange.Row{hourRow("role:pv", "2026-10-11T02:00:00Z", 100)}, false)
	unaligned := hourRow("role:pv", "2026-10-11T03:00:00Z", 100)
	unaligned.TS += 60_000
	h.sync.OnDeliver("p-ha", id, []historyexchange.Row{
		hourRow("role:load_total", "2026-10-11T04:00:00Z", 100), // andere Serie als angefragt
		unaligned,
		hourRow("role:pv", "2026-10-11T12:00:00Z", 100),        // laufende Stunde
		hourRow("role:pv", "2026-10-11T05:00:00Z", -20),        // Zaehlerruecksetzung
		hourRow("role:pv", "2026-10-11T06:00:00Z", math.NaN()), // kein Wert
		hourRow("role:pv", "2026-10-11T07:00:00Z", 100),        // gueltig
	}, true)

	counts, _ := h.store.HADays("pv", hourAt("2026-10-11T00:00:00Z"), 1)
	if counts[0] != 1 {
		t.Fatalf("%d HA-Stunden gespeichert, erwartet 1", counts[0])
	}
	if source, _ := h.store.SourceAt("load_total", hourAt("2026-10-11T04:00:00Z")); source != SourceNone {
		t.Fatal("fremde Serie gespeichert")
	}
}

func TestDaysHAKeepsShortAreNotRequestedAgain(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	offer := map[string]historyexchange.Census{"role:pv": census("2026-10-09T00:00:00Z", 20)}
	h.sync.OnOffer("p-ha", offer)
	h.sync.OnDeliver("p-ha", h.sent[0].body["req_id"].(string), nil, true)
	h.sync.OnOffer("p-ha", offer)
	if len(h.sent) != 1 {
		t.Fatalf("%d Nachfragen, erwartet 1 (Tag bleibt kurz)", len(h.sent))
	}
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:pv": census("2026-10-09T00:00:00Z", 24)})
	if len(h.sent) != 2 {
		t.Fatal("geaenderte Stundenzahl muss neu angefragt werden")
	}
}

func TestTimeoutFreesTheQueue(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"role:pv":         census("2026-10-10T00:00:00Z", 24),
		"role:load_total": census("2026-10-10T00:00:00Z", 24),
	})
	h.sync.Tick()
	if len(h.sent) != 1 {
		t.Fatal("vor Ablauf keine zweite Nachfrage")
	}
	h.now = h.now.Add(3 * time.Minute)
	h.sync.Tick()
	if len(h.sent) != 2 {
		t.Fatalf("%d Nachfragen nach Zeitablauf, erwartet 2", len(h.sent))
	}
}

func TestNewCompanionPeerDoesNotWaitForTheOldRequest(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:pv": census("2026-10-10T00:00:00Z", 24)})
	delete(h.peers, "p-ha")
	h.peers["p-ha2"] = true
	h.sync.OnOffer("p-ha2", map[string]historyexchange.Census{"role:pv": census("2026-10-10T00:00:00Z", 24)})
	if len(h.sent) != 2 || h.sent[1].to != "p-ha2" {
		t.Fatalf("Nachfragen = %+v", h.sent)
	}
}

func TestForeignSeriesAndRastersAreIgnored(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	hourly := census("2026-10-10T00:00:00Z", 24)
	hourly.Step = 3_600_000
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"sensor.pv":  census("2026-10-10T00:00:00Z", 24),
		"role:sauna": census("2026-10-10T00:00:00Z", 24),
		"role:pv":    hourly,
	})
	if len(h.sent) != 0 {
		t.Fatalf("Nachfragen = %+v", h.sent)
	}
}

func TestStatusCurrentNeedsTheLatestHoursForEveryOfferedRole(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	if st := h.sync.Status(); st.Connected || st.Current {
		t.Fatalf("ohne Angebot: %+v", st)
	}
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"role:pv":         census("2026-10-11T00:00:00Z", 11),
		"role:load_total": census("2026-10-11T00:00:00Z", 11),
	})
	h.store.SetHA([]HARecord{{Hour: hourAt("2026-10-11T10:00:00Z"), Role: "pv", KWh: 1}})
	if h.sync.Status().Current {
		t.Fatal("load_total fehlt noch")
	}
	h.store.SetHA([]HARecord{{Hour: hourAt("2026-10-11T11:00:00Z"), Role: "load_total", KWh: 1}})
	if !h.sync.Status().Current {
		t.Fatal("letzte (load_total) und vorletzte Stunde (pv) vorhanden: aktuell")
	}
	h.now = h.now.Add(2 * time.Hour)
	if h.sync.Status().Current {
		t.Fatal("zwei Stunden spaeter nicht mehr aktuell")
	}
	delete(h.peers, "p-ha")
	if st := h.sync.Status(); st.Connected || st.Current {
		t.Fatalf("Peer weg: %+v", st)
	}
}

func TestStatusReportsTheOldestMissingDay(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	from := hourAt("2026-10-01T00:00:00Z").UnixMilli()
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"role:pv": {From: from, Step: dayMs, N: []int{24, 24, 24, 24, 24, 24, 24, 24, 24, 24}},
	})
	st := h.sync.Status()
	if st.MissingDays == 0 || st.BackfillFrom == nil || !st.BackfillFrom.Equal(hourAt("2026-10-01T00:00:00Z")) {
		t.Fatalf("Status = %+v", st)
	}
}

func TestStatusHasNoBackfillDateWhenNothingIsMissing(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	if st := h.sync.Status(); st.BackfillFrom != nil {
		t.Fatalf("BackfillFrom = %v", st.BackfillFrom)
	}
}

func TestOfferAcceptsOnlyConsumerAndProducerCategories(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.SetCategories(func() map[string]energy.Category {
		return map[string]energy.Category{
			"akku":  {Label: "Akku", Base: energy.CategoryStorage},
			"sauna": {Label: "Sauna", Base: energy.CategoryConsumer},
		}
	})
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{
		"role:custom:akku":  census("2026-10-10T00:00:00Z", 1),
		"role:custom:weg":   census("2026-10-10T00:00:00Z", 1),
		"role:custom:sauna": census("2026-10-10T00:00:00Z", 1),
	})
	if len(h.sent) != 1 || h.sent[0].body["series"] != "role:custom:sauna" {
		t.Fatalf("Nachfragen = %+v, erwartet nur role:custom:sauna", h.sent)
	}
}

func TestOfferWithoutCategoriesRejectsCustomSeries(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.OnOffer("p-ha", map[string]historyexchange.Census{"role:custom:sauna": census("2026-10-10T00:00:00Z", 1)})
	if len(h.sent) != 0 {
		t.Fatalf("Nachfragen = %+v, erwartet keine ohne Kategorien", h.sent)
	}
}

func TestDeliveryForStorageCategoryIsDropped(t *testing.T) {
	h := newSyncHarness(t, "2026-10-11T12:30:00Z")
	h.sync.SetCategories(func() map[string]energy.Category {
		return map[string]energy.Category{"akku": {Label: "Akku", Base: energy.CategoryStorage}}
	})
	// Eine offene Nachfrage fuer den Speicher (z. B. Kategorie vor der Antwort
	// umgestellt) darf nichts in den Store schreiben.
	h.sync.pending = &pendingRequest{id: "stats-x", peer: "p-ha", series: "role:custom:akku", days: 1, oldest: ms("2026-10-10T00:00:00Z"), sent: h.now}
	h.sync.OnDeliver("p-ha", "stats-x", []historyexchange.Row{hourRow("role:custom:akku", "2026-10-10T05:00:00Z", 1500)}, true)
	if source, _ := h.store.SourceAt("custom:akku", hourAt("2026-10-10T05:00:00Z")); source == SourceHA {
		t.Fatalf("Speicherkategorie wurde aus Home Assistant geschrieben")
	}
}
