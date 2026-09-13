package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func fakePublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	return sshPub
}

func fakeAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
}

func TestCallbackTrustsUnknownHostOnlyOnConsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := NewHostKeyStore(path)
	key := fakePublicKey(t)

	prompted := 0
	callback, err := store.Callback(func(hostname, fingerprint string) (bool, error) {
		prompted++
		if hostname != "node.example:22" {
			t.Errorf("unexpected hostname in prompt: %q", hostname)
		}
		if fingerprint == "" {
			t.Errorf("fingerprint must not be empty")
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}

	if err := callback("node.example:22", fakeAddr(), key); err != nil {
		t.Fatalf("first contact should be accepted after consent: %v", err)
	}
	if prompted != 1 {
		t.Fatalf("expected exactly one prompt, got %d", prompted)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(raw), "node.example") {
		t.Fatalf("known_hosts does not contain the pinned host: %q", raw)
	}
}

func TestCallbackSkipsPromptOnceKnown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := NewHostKeyStore(path)
	key := fakePublicKey(t)

	prompted := 0
	always := func(string, string) (bool, error) { prompted++; return true, nil }

	callback, _ := store.Callback(always)
	if err := callback("node.example:22", fakeAddr(), key); err != nil {
		t.Fatalf("first contact: %v", err)
	}

	// A second HostKeyStore instance reading the same file must recognise the
	// pinned key without prompting -- this is what makes the pin durable
	// across separate runs of the application, not just within one process.
	second, _ := NewHostKeyStore(path).Callback(always)
	if err := second("node.example:22", fakeAddr(), key); err != nil {
		t.Fatalf("known host should be accepted silently: %v", err)
	}
	if prompted != 1 {
		t.Fatalf("expected the prompt to fire only once, got %d", prompted)
	}
}

func TestCallbackRejectsUnknownHostOnDecline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := NewHostKeyStore(path)
	key := fakePublicKey(t)

	callback, _ := store.Callback(func(string, string) (bool, error) { return false, nil })
	if err := callback("node.example:22", fakeAddr(), key); err == nil {
		t.Fatalf("expected an error when the operator declines")
	}

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "node.example") {
		t.Fatalf("declined host must not be pinned: %q", raw)
	}
}

func TestCallbackRejectsChangedKeyWithoutPrompting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := NewHostKeyStore(path)
	original := fakePublicKey(t)
	swapped := fakePublicKey(t)

	callback, _ := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err := callback("node.example:22", fakeAddr(), original); err != nil {
		t.Fatalf("first contact: %v", err)
	}

	prompted := 0
	second, _ := NewHostKeyStore(path).Callback(func(string, string) (bool, error) {
		prompted++
		return true, nil
	})
	if err := second("node.example:22", fakeAddr(), swapped); err == nil {
		t.Fatalf("a changed host key must be rejected")
	}
	if prompted != 0 {
		t.Fatalf("a changed host key must never reach the prompt, got %d calls", prompted)
	}
}
