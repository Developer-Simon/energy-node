package media

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeResolver answers LookupHost from a fixed set of names and counts calls.
type fakeResolver struct {
	known map[string]bool
	calls int
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	f.calls++
	if f.known[host] {
		return []string{"192.0.2.10"}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestCheckNameSuggestsTheNextFreeName(t *testing.T) {
	r := &fakeResolver{known: map[string]bool{
		"energy-node.local":   true,
		"energy-node-2.local": true,
	}}
	got := CheckName(context.Background(), r, "energy-node")
	want := NameCheck{Valid: true, Taken: true, Suggestion: "energy-node-3"}
	if got != want {
		t.Fatalf("CheckName = %+v, want %+v", got, want)
	}
}

func TestCheckNameRejectsInvalidNames(t *testing.T) {
	r := &fakeResolver{known: map[string]bool{}}
	got := CheckName(context.Background(), r, "Energy Node")
	if got.Valid {
		t.Fatalf("Valid = true for %q, want false", "Energy Node")
	}
	if r.calls != 0 {
		t.Fatalf("resolver was called %d times for an invalid name", r.calls)
	}
}

func testSigner(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	return signer, sshPub
}

// startSSHServer runs a minimal SSH server that offers hostKeys and refuses
// every login, so Probe sees the key and the handshake ends there.
func startSSHServer(t *testing.T, hostKeys ...ssh.Signer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, errors.New("login refused")
		},
	}
	for _, k := range hostKeys {
		cfg.AddHostKey(k)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _, _, _ = ssh.NewServerConn(c, cfg)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestProbeComparesTheHostKey(t *testing.T) {
	hostKey, keyA := testSigner(t)
	_, keyB := testSigner(t)
	addr := startSSHServer(t, hostKey)

	if got := Probe(context.Background(), addr, keyA); got != (Reachability{Reachable: true, KeyMatches: true}) {
		t.Fatalf("Probe(want=A) = %+v, want {true true}", got)
	}
	if got := Probe(context.Background(), addr, keyB); got != (Reachability{Reachable: true, KeyMatches: false}) {
		t.Fatalf("Probe(want=B) = %+v, want {true false}", got)
	}
}

// A Pi's sshd usually offers several host keys. The installer pins only the
// ed25519 one, so Probe must ask for that key type and not take whatever the
// client prefers (Go prefers ECDSA and RSA over ed25519).
func TestProbeMatchesThePinnedKeyAmongSeveralHostKeys(t *testing.T) {
	edSigner, edPub := testSigner(t)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa key: %v", err)
	}
	ecSigner, err := ssh.NewSignerFromKey(ecKey)
	if err != nil {
		t.Fatalf("ecdsa signer: %v", err)
	}
	addr := startSSHServer(t, ecSigner, edSigner)

	if got := Probe(context.Background(), addr, edPub); got != (Reachability{Reachable: true, KeyMatches: true}) {
		t.Fatalf("Probe(pinned ed25519) = %+v, want {true true}", got)
	}
}

func TestProbeReportsAClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	_, key := testSigner(t)
	if got := Probe(context.Background(), addr, key); got != (Reachability{}) {
		t.Fatalf("Probe on closed port = %+v, want zero value", got)
	}
}
