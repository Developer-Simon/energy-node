// Package companionpair erzeugt Kopplungscodes für die Home-Assistant-Companion-Integration
// und berechnet die Nachweise, mit denen beide Seiten den Code prüfen, ohne ihn zu senden.
package companionpair

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Alphabet enthält keine leicht verwechselbaren Zeichen (kein I, O, 0, 1).
const Alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// CodeLength ist die Anzahl der Zeichen eines Kopplungscodes ohne Bindestriche.
const CodeLength = 12

const proofPrefix = "energy-node-pair-v1"

// NewCode liefert einen zufälligen Kopplungscode aus Alphabet mit CodeLength Zeichen.
func NewCode() (string, error) {
	max := big.NewInt(int64(len(Alphabet)))
	buf := make([]byte, CodeLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("companionpair: Zufallszeichen: %w", err)
		}
		buf[i] = Alphabet[n.Int64()]
	}
	return string(buf), nil
}

// Format gruppiert einen Code in Vierer-Blöcke mit Bindestrich: "K7PM-3QXD-8HWT".
func Format(code string) string {
	var b strings.Builder
	for i := 0; i < len(code); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(code[i])
	}
	return b.String()
}

// Normalize macht eine Eingabe vergleichbar: Großbuchstaben, ohne '-' und ' '.
func Normalize(input string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return toUpper(r)
	}, input)
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// ClientProof ist der Nachweis der Companion-Seite: HMAC-SHA256 über Fingerprint und Nonce.
func ClientProof(code, fingerprint, nonce string) string {
	msg := proofPrefix + "|client|" + fingerprint + "|" + nonce
	return mac(Normalize(code), msg)
}

// ServerProof ist der Nachweis der Dashboard-Seite, der zusätzlich das Token bindet.
func ServerProof(code, fingerprint, nonce, token string) string {
	msg := proofPrefix + "|server|" + fingerprint + "|" + nonce + "|" + token
	return mac(Normalize(code), msg)
}

// Fingerprint ist der SHA-256 eines DER-kodierten Zertifikats als Kleinbuchstaben-Hex.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

func mac(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}
