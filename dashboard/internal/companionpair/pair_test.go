package companionpair

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestProofsMatchSharedVector(t *testing.T) {
	fp, nonce, token := strings.Repeat("ab", 32), strings.Repeat("00", 32), strings.Repeat("11", 32)
	// Werte aus Global Constraints, der Python-Test prüft dieselben.
	if got := ClientProof("k7pm-3qxd-8hwt", fp, nonce); got != "b5e9f8c6a6d988192bd0a1260722cc046b84b54246050f6fb9a585d2e03512c6" {
		t.Fatal(got)
	}
	if got := ServerProof("K7PM3QXD8HWT", fp, nonce, token); got != "a37bc927a473de888af78e8d6a87ac5760347bf6a813933fd52921489ccd6505" {
		t.Fatal(got)
	}
}

func TestNewCodeUsesAlphabetAndLength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != CodeLength {
			t.Fatalf("Länge %d, erwartet %d: %q", len(code), CodeLength, code)
		}
		for _, c := range code {
			if !strings.ContainsRune(Alphabet, c) {
				t.Fatalf("Zeichen %q nicht im Alphabet: %q", c, code)
			}
		}
		seen[code] = true
	}
	if len(seen) < 2 {
		t.Fatal("alle Codes gleich")
	}
}

func TestFormatAndNormalizeRoundTrip(t *testing.T) {
	const code = "K7PM3QXD8HWT"
	if got := Format(code); got != "K7PM-3QXD-8HWT" {
		t.Fatalf("Format = %q", got)
	}
	if got := Normalize(Format(code)); got != code {
		t.Fatalf("Normalize(Format(c)) = %q", got)
	}
	if got := Normalize(" k7pm 3qxd-8hwt "); got != "K7PM3QXD8HWT" {
		t.Fatalf("Normalize = %q", got)
	}
}

func TestFingerprintIsLowerHexSHA256(t *testing.T) {
	const want = "5e9c223a5fabd41bacf734ed83596b0f25c8386a3abf88862bd279640f85a391"
	got := Fingerprint([]byte("energy-node"))
	if got != want {
		t.Fatalf("Fingerprint = %q", got)
	}
	sum := sha256.Sum256([]byte("energy-node"))
	if got != hex.EncodeToString(sum[:]) {
		t.Fatal("Fingerprint ist nicht SHA-256 hex")
	}
}
