package companion

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// startStream öffnet einen Strom gegen hub und liefert den Leser. Die
// Aufräumfunktionen laufen in umgekehrter Reihenfolge: erst der Hub, dann der
// Körper, dann der Server.
func startStream(t *testing.T, hub *Hub) *bufio.Reader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeStream(w, r, nil)
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache control = %q", cc)
	}
	t.Cleanup(hub.Close)
	return bufio.NewReader(resp.Body)
}

// readEvent liest ein SSE-Ereignis und liefert Name und Datenzeile.
func readEvent(t *testing.T, br *bufio.Reader) (string, string) {
	t.Helper()
	var event, data string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "":
			if event != "" {
				return event, data
			}
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

// waitConnected wartet, bis der Hub den Strom registriert hat.
func waitConnected(t *testing.T, hub *Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !hub.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("stream not registered")
		}
		time.Sleep(time.Millisecond)
	}
}

// verifyAsync startet VerifyHA in einer Goroutine und liefert die Ergebnisse.
type verifyOutcome struct {
	identity auth.HAIdentity
	err      error
}

func verifyAsync(ctx context.Context, hub *Hub, username, password string) <-chan verifyOutcome {
	out := make(chan verifyOutcome, 1)
	go func() {
		identity, err := hub.VerifyHA(ctx, username, password)
		out <- verifyOutcome{identity, err}
	}()
	return out
}

func TestVerifyWithoutStreamIsUnavailable(t *testing.T) {
	hub := NewHub()
	start := time.Now()
	_, err := hub.VerifyHA(context.Background(), "anna", "secret")
	if !errors.Is(err, auth.ErrHomeAssistantUnavailable) {
		t.Fatalf("err = %v, want ErrHomeAssistantUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("returned after %v, want < 100 ms", elapsed)
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	hub := NewHub()
	br := startStream(t, hub)
	waitConnected(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome := verifyAsync(ctx, hub, "anna", "secret")

	event, data := readEvent(t, br)
	if event != "verify" {
		t.Fatalf("event = %q, want verify", event)
	}
	var req verifyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if req.Username != "anna" || req.Password != "secret" {
		t.Fatalf("verify data = %+v", req)
	}
	if req.ID == "" {
		t.Fatal("verify data has no id")
	}

	if !hub.Resolve(req.ID, VerifyResult{
		OK:       true,
		HAUserID: "ha-42",
		Username: "anna",
		Name:     "Anna",
		IsAdmin:  true,
	}) {
		t.Fatal("Resolve returned false for a pending id")
	}

	got := <-outcome
	if got.err != nil {
		t.Fatalf("VerifyHA error: %v", got.err)
	}
	want := auth.HAIdentity{UserID: "ha-42", Username: "anna", Name: "Anna", Admin: true}
	if got.identity != want {
		t.Fatalf("identity = %+v, want %+v", got.identity, want)
	}
}

func TestVerifyRejected(t *testing.T) {
	hub := NewHub()
	br := startStream(t, hub)
	waitConnected(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome := verifyAsync(ctx, hub, "anna", "wrong")

	_, data := readEvent(t, br)
	var req verifyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	hub.Resolve(req.ID, VerifyResult{OK: false})

	got := <-outcome
	if !errors.Is(got.err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", got.err)
	}
}

func TestVerifyTimesOutAndForgetsTheRequest(t *testing.T) {
	hub := NewHub()
	br := startStream(t, hub)
	waitConnected(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := hub.VerifyHA(ctx, "anna", "secret")
	if !errors.Is(err, auth.ErrHomeAssistantUnavailable) {
		t.Fatalf("err = %v, want ErrHomeAssistantUnavailable", err)
	}

	_, data := readEvent(t, br)
	var req verifyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	if hub.Resolve(req.ID, VerifyResult{OK: true, HAUserID: "ha-42"}) {
		t.Fatal("Resolve accepted an id after the request timed out")
	}
}

func TestNewStreamReplacesOldOne(t *testing.T) {
	hub := NewHub()
	first := startStream(t, hub)
	waitConnected(t, hub)

	// Ein zweiter Strom gegen denselben Hub. Der erste Leser muss EOF sehen.
	second := startStream(t, hub)
	waitConnected(t, hub)

	if _, err := first.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("first stream read err = %v, want EOF", err)
	}
	if !hub.Connected() {
		t.Fatal("Connected = false after replacing the stream")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome := verifyAsync(ctx, hub, "anna", "secret")
	event, data := readEvent(t, second)
	if event != "verify" {
		t.Fatalf("second stream event = %q, want verify", event)
	}
	var req verifyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("decode verify data: %v", err)
	}
	hub.Resolve(req.ID, VerifyResult{OK: true, HAUserID: "ha-42"})
	if got := <-outcome; got.err != nil {
		t.Fatalf("VerifyHA error: %v", got.err)
	}
}

func TestCloseEndsStreamAndPendingVerify(t *testing.T) {
	hub := NewHub()
	br := startStream(t, hub)
	waitConnected(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome := verifyAsync(ctx, hub, "anna", "secret")
	readEvent(t, br)

	hub.Close()

	got := <-outcome
	if !errors.Is(got.err, auth.ErrHomeAssistantUnavailable) {
		t.Fatalf("err = %v, want ErrHomeAssistantUnavailable", got.err)
	}
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream read err = %v, want EOF", err)
	}
	if hub.Connected() {
		t.Fatal("Connected = true after Close")
	}
}

func TestResolveUnknownID(t *testing.T) {
	hub := NewHub()
	if hub.Resolve("does-not-exist", VerifyResult{OK: true, HAUserID: "ha-42"}) {
		t.Fatal("Resolve returned true for an unknown id")
	}
}
