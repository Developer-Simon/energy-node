package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

type fakeStatsPeer struct {
	mu       sync.Mutex
	send     func(string, []byte) bool
	offers   []string
	coverage []map[string]historyexchange.Census
	delivers []historyexchange.Row
	final    bool
}

func (f *fakeStatsPeer) Attach(send func(string, []byte) bool, _ func(string) bool) { f.send = send }

func (f *fakeStatsPeer) OnOffer(peer string, coverage map[string]historyexchange.Census) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offers = append(f.offers, peer)
	f.coverage = append(f.coverage, coverage)
}

func (f *fakeStatsPeer) OnDeliver(_, _ string, rows []historyexchange.Row, final bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivers = append(f.delivers, rows...)
	f.final = final
}

func statsExchangeServer(t *testing.T) (*httptest.Server, *fakeStatsPeer) {
	t.Helper()
	mux := http.NewServeMux()
	stats := &fakeStatsPeer{}
	exchange := newHistoryExchange(nil)
	exchange.attachStats(stats)
	exchange.routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, stats
}

// readUntil liest Ereignisse, bis event kommt.
func readUntil(t *testing.T, reader *bufio.Reader, event string) map[string]any {
	t.Helper()
	for i := 0; i < 10; i++ {
		got, payload := readEvent(t, reader)
		if got == event {
			return payload
		}
	}
	t.Fatalf("Ereignis %q kam nicht", event)
	return nil
}

var dayCensus = map[string]any{"role:pv": map[string]any{"from": 1791763200000, "step": 86400000, "n": []int{24}}}

func TestCompanionOfferReachesStatsAndBrowsers(t *testing.T) {
	server, stats := statsExchangeServer(t)
	_, browser, closeBrowser := joinStream(t, server)
	defer closeBrowser()
	companion, _, closeCompanion := joinStream(t, server)
	defer closeCompanion()

	status := post(t, server, "/api/v1/history/exchange/offer", map[string]any{
		"peer": companion, "label": "Home Assistant", "kind": "companion",
		"coverage": map[string]any{"1h": dayCensus},
	})
	if status != http.StatusNoContent {
		t.Fatalf("Status %d", status)
	}
	if len(stats.offers) != 1 || stats.offers[0] != companion || stats.coverage[0]["role:pv"].Step != 86400000 {
		t.Fatalf("Statistik bekam %v %v", stats.offers, stats.coverage)
	}
	offer := readUntil(t, browser, "offer")
	if offer["kind"] != "companion" {
		t.Fatalf("Browser-Angebot ohne kind: %v", offer)
	}
	if _, ok := offer["coverage"].(map[string]any)["1h"]; !ok {
		t.Fatalf("Browser-Angebot ohne 1h: %v", offer)
	}
}

func TestOfferWithoutKindDoesNotReachStats(t *testing.T) {
	server, stats := statsExchangeServer(t)
	peer, _, closePeer := joinStream(t, server)
	defer closePeer()
	post(t, server, "/api/v1/history/exchange/offer", map[string]any{"peer": peer, "coverage": map[string]any{"1h": dayCensus}})
	if len(stats.offers) != 0 {
		t.Fatalf("Angebot ohne kind an die Statistik: %v", stats.offers)
	}
}

func TestStatsRequestReachesTheCompanion(t *testing.T) {
	server, stats := statsExchangeServer(t)
	companion, reader, closeCompanion := joinStream(t, server)
	defer closeCompanion()
	payload, _ := json.Marshal(map[string]any{"peer": historyexchange.StatsPeerID, "req_id": "stats-1", "tier": "1h", "series": "role:pv", "ranges": [][2]int64{{0, 3600000}}})
	if !stats.send(companion, payload) {
		t.Fatal("Zustellung gescheitert")
	}
	request := readUntil(t, reader, "request")
	if request["req_id"] != "stats-1" || request["peer"] != historyexchange.StatsPeerID {
		t.Fatalf("Nachfrage = %v", request)
	}
}

func TestDeliveryToStatsIsInterceptedNotRelayed(t *testing.T) {
	server, stats := statsExchangeServer(t)
	companion, _, closeCompanion := joinStream(t, server)
	defer closeCompanion()
	status := post(t, server, "/api/v1/history/exchange/deliver", map[string]any{
		"peer": companion, "to": historyexchange.StatsPeerID, "req_id": "stats-1", "seq": 0, "final": true, "tier": "1h",
		"rows": []map[string]any{{"series": "role:pv", "ts": 1791763200000, "min": 5, "max": 5, "avg": 5, "n": 1, "u": "W"}},
	})
	if status != http.StatusNoContent {
		t.Fatalf("Status %d", status)
	}
	if len(stats.delivers) != 1 || !stats.final || stats.delivers[0].Avg != 5 {
		t.Fatalf("Statistik bekam %+v final=%v", stats.delivers, stats.final)
	}
}

func TestDeliveryToStatsWithoutStatsIsRejected(t *testing.T) {
	server := exchangeServer(t)
	peer, _, closePeer := joinStream(t, server)
	defer closePeer()
	status := post(t, server, "/api/v1/history/exchange/deliver", map[string]any{
		"peer": peer, "to": historyexchange.StatsPeerID, "req_id": "x", "final": true, "tier": "1h", "rows": []any{},
	})
	if status != http.StatusNotFound {
		t.Fatalf("Status %d, erwartet 404", status)
	}
}
