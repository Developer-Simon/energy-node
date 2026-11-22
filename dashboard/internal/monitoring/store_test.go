package monitoring

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreFirstSeenAckAndGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.json")
	s := OpenStore(path, t.Logf)
	t0 := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	n := []Notice{{ID: "energy:unassigned"}}
	s.Apply(n, t0)
	if n[0].FirstSeen != t0.Unix() || n[0].Ack != nil {
		t.Fatalf("first apply = %+v", n[0])
	}
	if err := s.SetAck("energy:unassigned", true, "admin", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// 59 Minuten weg, dann wieder da: Quittung und Erstsehen bleiben.
	s.Apply(nil, t0.Add(59*time.Minute))
	n = []Notice{{ID: "energy:unassigned"}}
	s.Apply(n, t0.Add(60*time.Minute))
	if n[0].FirstSeen != t0.Unix() || n[0].Ack == nil || n[0].Ack.User != "admin" {
		t.Fatalf("after short absence = %+v", n[0])
	}
	// Über eine Stunde weg: vergessen, kommt neu.
	s.Apply(nil, t0.Add(3*time.Hour))
	n = []Notice{{ID: "energy:unassigned"}}
	s.Apply(n, t0.Add(3*time.Hour+time.Minute))
	if n[0].Ack != nil || n[0].FirstSeen != t0.Add(3*time.Hour+time.Minute).Unix() {
		t.Fatalf("after long absence = %+v", n[0])
	}
}

func TestStoreSinceWinsWhenEarlier(t *testing.T) {
	s := OpenStore("", t.Logf)
	since := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	n := []Notice{{ID: "chain:a:drift", Since: since}}
	s.Apply(n, time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))
	if n[0].FirstSeen != since.Unix() {
		t.Fatalf("first_seen = %d, want %d", n[0].FirstSeen, since.Unix())
	}
}

func TestStoreUnknownAckAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.json")
	s := OpenStore(path, t.Logf)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	if err := s.SetAck("nope", true, "admin", now); !errors.Is(err, ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty store wrote a file: %v", err)
	}
	s.Apply([]Notice{{ID: "x"}}, now)
	_ = s.SetAck("x", true, "admin", now)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	n := []Notice{{ID: "x"}}
	OpenStore(path, t.Logf).Apply(n, now.Add(time.Minute))
	if n[0].Ack == nil || n[0].FirstSeen != now.Unix() {
		t.Fatalf("reloaded = %+v", n[0])
	}
}
