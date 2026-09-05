// dashboard/internal/httpapi/history_exchange.go
// Die am Server angekuendigte Austausch-Schnittstelle der Verlaufs-
// Historie. Der Server ist Vermittler, kein Speicher: Angebote werden
// verteilt, Nachfragen und Lieferungen an genau einen Peer durchgereicht.
// Auf Platte landet nichts.
//
// Der Ringpuffer tritt als gewoehnlicher Peer mit der Kennung "server" auf
// und meldet sich nach jedem Beitritt mit einem eigenen Angebot - fuer den
// Browser ist er dadurch von einem zweiten Tab nicht zu unterscheiden, und
// ein einziger Codepfad deckt beide Faelle ab.
package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/historyexchange"
)

const (
	exchangeProtocolVersion = 1
	maxRowsPerDeliver       = 500
	maxRowsPerRequest       = 20000
	maxExchangeBodyBytes    = 1 << 20
	requestTimeoutSeconds   = 30
	bufferRetentionHours    = 24
	// 24 h x 60 Minutenwerte x 20 Serien. Bei rund 80 Byte je Satz sind das
	// gut zwei Megabyte - auf dem Pi vertretbar, und die Obergrenze
	// verhindert, dass eine falsch konfigurierte Serienmenge den Speicher
	// auffrisst.
	bufferMaxRows = 24 * 60 * 20
	// Ohne regelmaessiges Lebenszeichen schliessen Zwischenstellen einen
	// stillen SSE-Strom nach wenigen Minuten.
	exchangePingInterval = 25 * time.Second
	// Der Puffer ist die einzige Stufe, die der Client nachliefert.
	bufferTier = "1m"
)

var exchangeTiers = []string{"1m", "5m"}

// rasterFor spiegelt RASTER aus history-coverage.js. Beide Seiten muessen
// dieselben Zahlen fuehren, sonst passen die Raster nicht aufeinander.
func rasterFor(tier string) (stepMs int64, windowMs int64, ok bool) {
	switch tier {
	case "1m":
		return 3600000, 7 * 24 * 3600000, true
	case "5m":
		return 21600000, 30 * 24 * 3600000, true
	default:
		return 0, 0, false
	}
}

type historyExchange struct {
	hub    *historyexchange.Hub
	buffer *historyexchange.Buffer
	now    func() time.Time
}

func newHistoryExchange() *historyExchange {
	return &historyExchange{
		// Vier Nachrichten Vorlauf reichen: ein Client, der nicht mitkommt,
		// soll getrennt werden und neu beginnen, nicht gepuffert werden.
		hub:    historyexchange.NewHub(8),
		buffer: historyexchange.NewBuffer(bufferRetentionHours*time.Hour, bufferMaxRows),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (x *historyExchange) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/history/exchange", x.handleAnnounce)
	mux.HandleFunc("/api/v1/history/exchange/stream", x.handleStream)
	mux.HandleFunc("/api/v1/history/exchange/offer", x.handleOffer)
	mux.HandleFunc("/api/v1/history/exchange/request", x.handleRequest)
	mux.HandleFunc("/api/v1/history/exchange/deliver", x.handleDeliver)
	mux.HandleFunc("/api/v1/history/exchange/buffer", x.handleBuffer)
}

type exchangeBufferInfo struct {
	Tier           string `json:"tier"`
	RetentionHours int    `json:"retention_hours"`
	Rows           int    `json:"rows"`
}

type exchangeAnnouncement struct {
	Protocol              int                `json:"protocol"`
	Tiers                 []string           `json:"tiers"`
	MaxRowsPerDeliver     int                `json:"max_rows_per_deliver"`
	MaxRowsPerRequest     int                `json:"max_rows_per_request"`
	MaxBodyBytes          int                `json:"max_body_bytes"`
	RequestTimeoutSeconds int                `json:"request_timeout_seconds"`
	Peers                 int                `json:"peers"`
	Buffer                exchangeBufferInfo `json:"buffer"`
}

// Announcement ist die selbstbeschreibende Adresse der Schnittstelle: ein
// Fremdsystem liest hier Protokollversion und Grenzen und weiss danach, ob
// es sich anschliessen kann.
func (x *historyExchange) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, exchangeAnnouncement{
		Protocol:              exchangeProtocolVersion,
		Tiers:                 exchangeTiers,
		MaxRowsPerDeliver:     maxRowsPerDeliver,
		MaxRowsPerRequest:     maxRowsPerRequest,
		MaxBodyBytes:          maxExchangeBodyBytes,
		RequestTimeoutSeconds: requestTimeoutSeconds,
		Peers:                 len(x.hub.Peers()),
		Buffer: exchangeBufferInfo{
			Tier:           bufferTier,
			RetentionHours: x.buffer.RetentionHours(),
			Rows:           x.buffer.Len(),
		},
	})
}

func (x *historyExchange) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, out, leave := x.hub.Join()
	defer leave()

	write := func(event string, data any) bool {
		payload, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	peers := append([]string{historyexchange.ServerPeerID}, x.hub.Peers()...)
	if !write("hello", map[string]any{"protocol": exchangeProtocolVersion, "peer_id": id, "peers": peers}) {
		return
	}
	// Der Puffer meldet sich unmittelbar nach dem Beitritt mit seinem
	// eigenen Angebot - so, als waere er ein weiterer Client, der zufaellig
	// kurz darauf sein Angebot macht.
	if offer := x.bufferOffer(); offer != nil {
		if !write("offer", offer) {
			return
		}
	}
	x.hub.Broadcast(id, historyexchange.Message{Event: "peer-joined", Data: json.RawMessage(fmt.Sprintf(`{"peer":%q}`, id))})
	defer x.hub.Broadcast(id, historyexchange.Message{Event: "peer-left", Data: json.RawMessage(fmt.Sprintf(`{"peer":%q}`, id))})

	ticker := time.NewTicker(exchangePingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, open := <-out:
			if !open {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Event, msg.Data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if !write("ping", map[string]any{}) {
				return
			}
		}
	}
}

// bufferOffer baut das Deckungsraster des Ringpuffers. nil, wenn er leer
// ist - ein leeres Angebot zu senden waere nur Rauschen.
func (x *historyExchange) bufferOffer() map[string]any {
	stepMs, windowMs, _ := rasterFor(bufferTier)
	now := x.now().UnixMilli()
	from := (now - windowMs) / stepMs * stepMs
	census := x.buffer.Census(from, now, stepMs)
	if len(census) == 0 {
		return nil
	}
	return map[string]any{
		"peer":     historyexchange.ServerPeerID,
		"coverage": map[string]any{bufferTier: census},
	}
}

// decodeExchange liest den Rumpf einer Austausch-Nachricht und prueft, dass
// die Absenderkennung zu einem verbundenen Peer gehoert. Ohne diese Pruefung
// koennte ein beliebiger Aufruf fremde Angebote in die Runde streuen.
func (x *historyExchange) decodeExchange(w http.ResponseWriter, r *http.Request, target any) (string, bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return "", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxExchangeBodyBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Die Nachricht ist zu groß")
		return "", false
	}
	if err := json.Unmarshal(body, target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return "", false
	}
	var envelope struct {
		Peer string `json:"peer"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Peer == "" {
		writeError(w, http.StatusBadRequest, "peer_missing", "Die Nachricht nennt keinen Absender")
		return "", false
	}
	if !x.hub.Has(envelope.Peer) {
		writeError(w, http.StatusForbidden, "peer_unknown", "Der Absender ist nicht verbunden")
		return "", false
	}
	return envelope.Peer, true
}

func relay(w http.ResponseWriter, sent bool) {
	if !sent {
		writeError(w, http.StatusNotFound, "peer_gone", "Der angesprochene Peer ist nicht mehr verbunden")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (x *historyExchange) handleOffer(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Peer     string                                       `json:"peer"`
		Coverage map[string]map[string]historyexchange.Census `json:"coverage"`
	}
	from, ok := x.decodeExchange(w, r, &payload)
	if !ok {
		return
	}
	for tier := range payload.Coverage {
		if _, _, known := rasterFor(tier); !known {
			writeError(w, http.StatusBadRequest, "tier_unknown", "Nur die Stufen 1m und 5m werden getauscht")
			return
		}
	}
	data, err := json.Marshal(map[string]any{"peer": from, "coverage": payload.Coverage})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode_failed", err.Error())
		return
	}
	x.hub.Broadcast(from, historyexchange.Message{Event: "offer", Data: data})
	w.WriteHeader(http.StatusNoContent)
}

func (x *historyExchange) handleRequest(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Peer   string     `json:"peer"`
		To     string     `json:"to"`
		ReqID  string     `json:"req_id"`
		Tier   string     `json:"tier"`
		Series string     `json:"series"`
		Ranges [][2]int64 `json:"ranges"`
	}
	from, ok := x.decodeExchange(w, r, &payload)
	if !ok {
		return
	}
	if _, _, known := rasterFor(payload.Tier); !known {
		writeError(w, http.StatusBadRequest, "tier_unknown", "Nur die Stufen 1m und 5m werden getauscht")
		return
	}
	if payload.ReqID == "" || payload.Series == "" || len(payload.Ranges) == 0 {
		writeError(w, http.StatusBadRequest, "request_incomplete", "Die Nachfrage ist unvollständig")
		return
	}
	if payload.To == historyexchange.ServerPeerID {
		x.answerFromBuffer(w, from, payload.ReqID, payload.Tier, payload.Series, payload.Ranges)
		return
	}
	data, err := json.Marshal(map[string]any{
		"peer": from, "req_id": payload.ReqID, "tier": payload.Tier,
		"series": payload.Series, "ranges": payload.Ranges,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode_failed", err.Error())
		return
	}
	relay(w, x.hub.SendTo(payload.To, historyexchange.Message{Event: "request", Data: data}))
}

// answerFromBuffer beantwortet eine Nachfrage an den Pseudo-Peer "server"
// direkt aus dem Ring, statt sie durchzureichen.
func (x *historyExchange) answerFromBuffer(w http.ResponseWriter, to, reqID, tier, series string, ranges [][2]int64) {
	if tier != bufferTier {
		// Der Puffer fuehrt nur Minutenwerte; eine leere Schlusslieferung
		// ist die ehrliche Antwort und beendet die Nachfrage sauber.
		x.sendBufferChunk(to, reqID, tier, nil, 0, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rows := x.buffer.Rows(series, ranges, maxRowsPerRequest)
	seq := 0
	for start := 0; start < len(rows); start += maxRowsPerDeliver {
		end := start + maxRowsPerDeliver
		if end > len(rows) {
			end = len(rows)
		}
		x.sendBufferChunk(to, reqID, tier, rows[start:end], seq, end == len(rows))
		seq++
	}
	if len(rows) == 0 {
		x.sendBufferChunk(to, reqID, tier, nil, 0, true)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (x *historyExchange) sendBufferChunk(to, reqID, tier string, rows []historyexchange.Row, seq int, final bool) {
	if rows == nil {
		rows = []historyexchange.Row{}
	}
	data, err := json.Marshal(map[string]any{
		"peer": historyexchange.ServerPeerID, "req_id": reqID, "seq": seq,
		"final": final, "tier": tier, "rows": rows,
	})
	if err != nil {
		return
	}
	x.hub.SendTo(to, historyexchange.Message{Event: "deliver", Data: data})
}

func (x *historyExchange) handleDeliver(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Peer  string                `json:"peer"`
		To    string                `json:"to"`
		ReqID string                `json:"req_id"`
		Seq   int                   `json:"seq"`
		Final bool                  `json:"final"`
		Tier  string                `json:"tier"`
		Rows  []historyexchange.Row `json:"rows"`
	}
	from, ok := x.decodeExchange(w, r, &payload)
	if !ok {
		return
	}
	if len(payload.Rows) > maxRowsPerDeliver {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_rows",
			fmt.Sprintf("Höchstens %d Sätze je Lieferung", maxRowsPerDeliver))
		return
	}
	if _, _, known := rasterFor(payload.Tier); !known {
		writeError(w, http.StatusBadRequest, "tier_unknown", "Nur die Stufen 1m und 5m werden getauscht")
		return
	}
	data, err := json.Marshal(map[string]any{
		"peer": from, "req_id": payload.ReqID, "seq": payload.Seq,
		"final": payload.Final, "tier": payload.Tier, "rows": payload.Rows,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encode_failed", err.Error())
		return
	}
	relay(w, x.hub.SendTo(payload.To, historyexchange.Message{Event: "deliver", Data: data}))
}

func (x *historyExchange) handleBuffer(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Peer string                `json:"peer"`
		Rows []historyexchange.Row `json:"rows"`
	}
	if _, ok := x.decodeExchange(w, r, &payload); !ok {
		return
	}
	if len(payload.Rows) > maxRowsPerDeliver {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_rows",
			fmt.Sprintf("Höchstens %d Sätze je Nachschub", maxRowsPerDeliver))
		return
	}
	x.buffer.Append(payload.Rows, x.now().UnixMilli())
	w.WriteHeader(http.StatusNoContent)
}
