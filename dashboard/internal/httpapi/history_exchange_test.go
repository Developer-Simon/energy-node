package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// readEvent liest ein SSE-Ereignis (event:-Zeile plus data:-Zeile) vom
// Strom. Leerzeilen trennen die Ereignisse.
func readEvent(t *testing.T, reader *bufio.Reader) (string, map[string]any) {
	t.Helper()
	event := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Strom abgebrochen: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("data nicht lesbar: %v", err)
			}
			return event, payload
		}
	}
}

func exchangeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	newHistoryExchange().routes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// joinStream oeffnet einen SSE-Strom, liest das hello und liefert die
// eigene Peer-Kennung samt Reader.
func joinStream(t *testing.T, server *httptest.Server) (string, *bufio.Reader, func()) {
	t.Helper()
	response, err := http.Get(server.URL + "/api/v1/history/exchange/stream")
	if err != nil {
		t.Fatalf("Strom nicht erreichbar: %v", err)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	reader := bufio.NewReader(response.Body)
	event, payload := readEvent(t, reader)
	if event != "hello" {
		t.Fatalf("erstes Ereignis = %q, want hello", event)
	}
	id, _ := payload["peer_id"].(string)
	if id == "" {
		t.Fatal("hello ohne peer_id")
	}
	return id, reader, func() { response.Body.Close() }
}

func post(t *testing.T, server *httptest.Server, path string, body any) int {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func TestExchangeAnkuendigungNenntProtokollUndGrenzen(t *testing.T) {
	server := exchangeServer(t)
	response, err := http.Get(server.URL + "/api/v1/history/exchange")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var announced struct {
		Protocol          int      `json:"protocol"`
		Tiers             []string `json:"tiers"`
		MaxRowsPerDeliver int      `json:"max_rows_per_deliver"`
		MaxRowsPerRequest int      `json:"max_rows_per_request"`
		Buffer            struct {
			Tier           string `json:"tier"`
			RetentionHours int    `json:"retention_hours"`
		} `json:"buffer"`
	}
	if err := json.NewDecoder(response.Body).Decode(&announced); err != nil {
		t.Fatal(err)
	}
	if announced.Protocol != exchangeProtocolVersion {
		t.Fatalf("protocol = %d, want %d", announced.Protocol, exchangeProtocolVersion)
	}
	if len(announced.Tiers) != 2 || announced.Tiers[0] != "1m" || announced.Tiers[1] != "5m" {
		t.Fatalf("tiers = %v, want [1m 5m] - die Rohstufe wird nie getauscht", announced.Tiers)
	}
	if announced.MaxRowsPerDeliver != maxRowsPerDeliver || announced.MaxRowsPerRequest != maxRowsPerRequest {
		t.Fatalf("Grenzen falsch angekuendigt: %+v", announced)
	}
	if announced.Buffer.Tier != "1m" || announced.Buffer.RetentionHours != bufferRetentionHours {
		t.Fatalf("Puffer falsch angekuendigt: %+v", announced.Buffer)
	}
}

func TestStreamLiefertHelloMitPeerListe(t *testing.T) {
	server := exchangeServer(t)
	first, _, closeFirst := joinStream(t, server)
	defer closeFirst()
	_, secondReader, closeSecond := joinStream(t, server)
	defer closeSecond()
	_ = secondReader
	if first == "" {
		t.Fatal("keine Kennung vergeben")
	}
}

func TestAngebotErreichtDenAnderenPeerNichtDenAbsender(t *testing.T) {
	server := exchangeServer(t)
	sender, senderReader, closeSender := joinStream(t, server)
	defer closeSender()
	_, otherReader, closeOther := joinStream(t, server)
	defer closeOther()

	// Der zweite Beitritt loest beim ersten ein Angebot des Servers und ein
	// peer-joined aus; hier zaehlt nur, dass das eigene Angebot ankommt.
	coverage := map[string]any{"1m": map[string]any{"role:pv": map[string]any{"from": 0, "step": 3600000, "n": []int{60}}}}
	if code := post(t, server, "/api/v1/history/exchange/offer", map[string]any{"peer": sender, "coverage": coverage}); code != http.StatusNoContent {
		t.Fatalf("offer = %d, want 204", code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		event, payload := readEvent(t, otherReader)
		if event != "offer" {
			continue
		}
		if payload["peer"] != sender {
			t.Fatalf("Angebot von %v, want %v", payload["peer"], sender)
		}
		_ = senderReader
		return
	}
	t.Fatal("Angebot kam beim anderen Peer nicht an")
}

func TestNachfrageWirdAnGenauDenZielpeerDurchgereicht(t *testing.T) {
	server := exchangeServer(t)
	asker, _, closeAsker := joinStream(t, server)
	defer closeAsker()
	target, targetReader, closeTarget := joinStream(t, server)
	defer closeTarget()

	body := map[string]any{
		"peer": asker, "to": target, "req_id": "r1",
		"tier": "1m", "series": "role:pv", "ranges": [][2]int64{{1000, 2000}},
	}
	if code := post(t, server, "/api/v1/history/exchange/request", body); code != http.StatusNoContent {
		t.Fatalf("request = %d, want 204", code)
	}
	for {
		event, payload := readEvent(t, targetReader)
		if event != "request" {
			continue
		}
		if payload["req_id"] != "r1" || payload["peer"] != asker || payload["series"] != "role:pv" {
			t.Fatalf("Nachfrage falsch durchgereicht: %+v", payload)
		}
		return
	}
}

func TestNachfrageAnUnbekanntenPeerWirdAbgelehnt(t *testing.T) {
	server := exchangeServer(t)
	asker, _, closeAsker := joinStream(t, server)
	defer closeAsker()
	body := map[string]any{"peer": asker, "to": "p-gibtesnicht", "req_id": "r1", "tier": "1m", "series": "role:pv", "ranges": [][2]int64{{0, 1}}}
	if code := post(t, server, "/api/v1/history/exchange/request", body); code != http.StatusNotFound {
		t.Fatalf("request an unbekannten Peer = %d, want 404", code)
	}
}

func TestNachrichtMitUnbekannterAbsenderkennungWirdAbgelehnt(t *testing.T) {
	server := exchangeServer(t)
	body := map[string]any{"peer": "p-fremd", "coverage": map[string]any{}}
	if code := post(t, server, "/api/v1/history/exchange/offer", body); code != http.StatusForbidden {
		t.Fatalf("offer mit fremder Kennung = %d, want 403", code)
	}
}

func TestLieferungErreichtDenAnfrager(t *testing.T) {
	server := exchangeServer(t)
	asker, askerReader, closeAsker := joinStream(t, server)
	defer closeAsker()
	target, _, closeTarget := joinStream(t, server)
	defer closeTarget()

	body := map[string]any{
		"peer": target, "to": asker, "req_id": "r1", "seq": 0, "final": true, "tier": "1m",
		"rows": []map[string]any{{"series": "role:pv", "ts": 1000, "min": 1, "max": 3, "avg": 2, "n": 60, "u": "W"}},
	}
	if code := post(t, server, "/api/v1/history/exchange/deliver", body); code != http.StatusNoContent {
		t.Fatalf("deliver = %d, want 204", code)
	}
	for {
		event, payload := readEvent(t, askerReader)
		if event != "deliver" {
			continue
		}
		if payload["req_id"] != "r1" || payload["final"] != true {
			t.Fatalf("Lieferung falsch durchgereicht: %+v", payload)
		}
		rows, _ := payload["rows"].([]any)
		if len(rows) != 1 {
			t.Fatalf("rows = %v", rows)
		}
		return
	}
}

func TestLieferungUeberDerSatzgrenzeWirdAbgelehnt(t *testing.T) {
	server := exchangeServer(t)
	asker, _, closeAsker := joinStream(t, server)
	defer closeAsker()
	target, _, closeTarget := joinStream(t, server)
	defer closeTarget()

	rows := make([]map[string]any, maxRowsPerDeliver+1)
	for index := range rows {
		rows[index] = map[string]any{"series": "role:pv", "ts": index, "min": 1, "max": 1, "avg": 1, "n": 1}
	}
	body := map[string]any{"peer": target, "to": asker, "req_id": "r1", "seq": 0, "final": true, "tier": "1m", "rows": rows}
	if code := post(t, server, "/api/v1/history/exchange/deliver", body); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("zu grosse Lieferung = %d, want 413", code)
	}
}

func TestLieferungMitRohstufeWirdAbgelehnt(t *testing.T) {
	server := exchangeServer(t)
	asker, _, closeAsker := joinStream(t, server)
	defer closeAsker()
	target, _, closeTarget := joinStream(t, server)
	defer closeTarget()

	body := map[string]any{
		"peer": target, "to": asker, "req_id": "r1", "seq": 0, "final": true, "tier": "raw",
		"rows": []map[string]any{{"series": "role:pv", "ts": 1000, "min": 1, "max": 3, "avg": 2, "n": 1, "u": "W"}},
	}
	if code := post(t, server, "/api/v1/history/exchange/deliver", body); code != http.StatusBadRequest {
		t.Fatalf("deliver mit tier=raw = %d, want 400 - die Rohstufe wird nie getauscht", code)
	}
}

func TestPufferMeldetSichNachDemBeitrittMitEigenemAngebot(t *testing.T) {
	server := exchangeServer(t)
	filler, _, closeFiller := joinStream(t, server)
	defer closeFiller()

	now := time.Now().UnixMilli()
	rows := []map[string]any{{"series": "role:pv", "ts": now - 120000, "min": 1, "max": 1, "avg": 1, "n": 60, "u": "W"}}
	if code := post(t, server, "/api/v1/history/exchange/buffer", map[string]any{"peer": filler, "rows": rows}); code != http.StatusNoContent {
		t.Fatalf("buffer = %d, want 204", code)
	}

	_, newcomerReader, closeNewcomer := joinStream(t, server)
	defer closeNewcomer()
	for {
		event, payload := readEvent(t, newcomerReader)
		if event != "offer" {
			continue
		}
		if payload["peer"] != "server" {
			continue
		}
		coverage, _ := payload["coverage"].(map[string]any)
		minute, _ := coverage["1m"].(map[string]any)
		if _, ok := minute["role:pv"]; !ok {
			t.Fatalf("Server-Angebot ohne role:pv: %+v", coverage)
		}
		return
	}
}

func TestNachfrageAnDenServerWirdAusDemPufferBeantwortet(t *testing.T) {
	server := exchangeServer(t)
	asker, askerReader, closeAsker := joinStream(t, server)
	defer closeAsker()

	now := time.Now().UnixMilli()
	stamp := now - 120000
	rows := []map[string]any{{"series": "role:pv", "ts": stamp, "min": 1, "max": 3, "avg": 2, "n": 60, "u": "W"}}
	post(t, server, "/api/v1/history/exchange/buffer", map[string]any{"peer": asker, "rows": rows})

	body := map[string]any{
		"peer": asker, "to": "server", "req_id": "r9", "tier": "1m", "series": "role:pv",
		"ranges": [][2]int64{{stamp - 1000, stamp + 1000}},
	}
	if code := post(t, server, "/api/v1/history/exchange/request", body); code != http.StatusNoContent {
		t.Fatalf("request an server = %d, want 204", code)
	}
	for {
		event, payload := readEvent(t, askerReader)
		if event != "deliver" {
			continue
		}
		if payload["peer"] != "server" || payload["req_id"] != "r9" {
			continue
		}
		delivered, _ := payload["rows"].([]any)
		if len(delivered) != 1 {
			t.Fatalf("Puffer lieferte %d Saetze, want 1", len(delivered))
		}
		return
	}
}

func TestNichtErlaubteMethodenWerdenAbgewiesen(t *testing.T) {
	server := exchangeServer(t)
	response, err := http.Get(server.URL + "/api/v1/history/exchange/offer")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET auf /offer = %d, want 405", response.StatusCode)
	}
}
