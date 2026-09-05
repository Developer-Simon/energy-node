package historyexchange

import (
	"encoding/json"
	"testing"
	"time"
)

func receive(t *testing.T, out <-chan Message) Message {
	t.Helper()
	select {
	case msg := <-out:
		return msg
	case <-time.After(time.Second):
		t.Fatal("keine Nachricht erhalten")
		return Message{}
	}
}

func TestJoinVergibtEindeutigeIDs(t *testing.T) {
	hub := NewHub(4)
	first, _, leaveFirst := hub.Join()
	second, _, leaveSecond := hub.Join()
	defer leaveFirst()
	defer leaveSecond()
	if first == second {
		t.Fatalf("IDs sind gleich: %q", first)
	}
	if !hub.Has(first) || !hub.Has(second) {
		t.Fatal("beide Peers muessen bekannt sein")
	}
	if got := hub.Peers(); len(got) != 2 {
		t.Fatalf("Peers = %v, want 2", got)
	}
}

func TestBroadcastErreichtAlleAusserDemAbsender(t *testing.T) {
	hub := NewHub(4)
	sender, senderOut, leaveSender := hub.Join()
	_, otherOut, leaveOther := hub.Join()
	defer leaveSender()
	defer leaveOther()

	hub.Broadcast(sender, Message{Event: "offer", Data: json.RawMessage(`{"x":1}`)})

	got := receive(t, otherOut)
	if got.Event != "offer" {
		t.Fatalf("Event = %q, want offer", got.Event)
	}
	select {
	case msg := <-senderOut:
		t.Fatalf("Absender hat sein eigenes Angebot erhalten: %+v", msg)
	default:
	}
}

func TestSendToErreichtGenauEinenPeer(t *testing.T) {
	hub := NewHub(4)
	target, targetOut, leaveTarget := hub.Join()
	_, otherOut, leaveOther := hub.Join()
	defer leaveTarget()
	defer leaveOther()

	if !hub.SendTo(target, Message{Event: "request", Data: json.RawMessage(`{}`)}) {
		t.Fatal("SendTo = false, want true")
	}
	if got := receive(t, targetOut); got.Event != "request" {
		t.Fatalf("Event = %q, want request", got.Event)
	}
	select {
	case msg := <-otherOut:
		t.Fatalf("fremder Peer hat die Nachfrage erhalten: %+v", msg)
	default:
	}
}

func TestSendToAnUnbekanntenPeerMeldetFehlschlag(t *testing.T) {
	hub := NewHub(4)
	if hub.SendTo("gibt-es-nicht", Message{Event: "request"}) {
		t.Fatal("SendTo = true, want false")
	}
}

func TestLeaveEntferntDenPeerUndSchliesstDenKanal(t *testing.T) {
	hub := NewHub(4)
	id, out, leave := hub.Join()
	leave()
	if hub.Has(id) {
		t.Fatal("Peer ist nach leave() noch bekannt")
	}
	select {
	case _, open := <-out:
		if open {
			t.Fatal("Kanal liefert nach leave() noch Nachrichten")
		}
	case <-time.After(time.Second):
		t.Fatal("Kanal wurde nicht geschlossen")
	}
}

func TestLeaveIstMehrfachAufrufbar(t *testing.T) {
	hub := NewHub(4)
	_, _, leave := hub.Join()
	leave()
	leave()
}

// Ein Peer, dessen Kanal vollgelaufen ist, wird getrennt statt gepuffert -
// sonst waechst der Speicherbedarf des Hubs unbeschraenkt.
func TestVollerKanalTrenntDenPeer(t *testing.T) {
	hub := NewHub(1)
	slow, _, leaveSlow := hub.Join()
	defer leaveSlow()
	fast, _, leaveFast := hub.Join()
	defer leaveFast()

	hub.Broadcast(fast, Message{Event: "offer", Data: json.RawMessage(`{"seq":1}`)})
	hub.Broadcast(fast, Message{Event: "offer", Data: json.RawMessage(`{"seq":2}`)})

	if hub.Has(slow) {
		t.Fatal("langsamer Peer ist noch verbunden")
	}
}
