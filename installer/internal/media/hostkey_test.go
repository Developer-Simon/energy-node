package media

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
	"golang.org/x/crypto/ssh"
)

func TestNewHostKeyRoundTrips(t *testing.T) {
	hk, err := NewHostKey()
	if err != nil {
		t.Fatalf("NewHostKey: %v", err)
	}
	if !strings.HasPrefix(string(hk.PrivatePEM), "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("PrivatePEM is not OpenSSH format: %q", hk.PrivatePEM[:min(40, len(hk.PrivatePEM))])
	}
	parsed, err := ssh.ParsePrivateKey(hk.PrivatePEM)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if !bytes.Equal(parsed.PublicKey().Marshal(), hk.Public.Marshal()) {
		t.Fatal("parsed public key differs from Public")
	}
	if !strings.HasPrefix(hk.PublicLine, "ssh-ed25519 ") {
		t.Fatalf("PublicLine = %q", hk.PublicLine)
	}
}

func TestHashPasswordIsSha512Crypt(t *testing.T) {
	hash, err := HashPassword("geheim")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$6$") {
		t.Fatalf("hash = %q, want $6$ prefix", hash)
	}
	if err := crypt.SHA512.New().Verify(hash, []byte("geheim")); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestValidHostnameAndUser(t *testing.T) {
	long := strings.Repeat("a", 64)
	for _, name := range []string{"energy-node", "node"} {
		if !ValidHostname(name) {
			t.Errorf("ValidHostname(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"-x", "Energy", "a_b", long} {
		if ValidHostname(name) {
			t.Errorf("ValidHostname(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"energy-node", "node"} {
		if !ValidUser(name) {
			t.Errorf("ValidUser(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"root", "1x", "Energy"} {
		if ValidUser(name) {
			t.Errorf("ValidUser(%q) = true, want false", name)
		}
	}
}
