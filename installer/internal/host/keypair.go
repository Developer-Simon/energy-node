package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// KeyComment steht am Ende der authorized_keys-Zeile, damit der Betreiber
// spaeter erkennt, woher der Schluessel kam.
const KeyComment = "energy-node-installer"

// KeyFileName ist der Dateiname des privaten Schluessels.
const KeyFileName = "id_ed25519"

// GenerateEd25519 legt ein Schluesselpaar in dir an und liefert den Pfad des
// privaten Schluessels sowie die authorized_keys-Zeile. Ein bereits
// vorhandener Schluessel wird nicht ersetzt - der Betreiber hat ihn
// moeglicherweise schon auf mehreren Nodes hinterlegt.
func GenerateEd25519(dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("host: Schluesselverzeichnis: %w", err)
	}
	privatePath := filepath.Join(dir, KeyFileName)

	if data, err := os.ReadFile(privatePath); err == nil {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return "", "", fmt.Errorf("host: vorhandener Schluessel %s ist unlesbar: %w", privatePath, err)
		}
		return privatePath, publicLine(signer.PublicKey()), nil
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("host: Schluessel erzeugen: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(private, KeyComment)
	if err != nil {
		return "", "", fmt.Errorf("host: Schluessel kodieren: %w", err)
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", "", fmt.Errorf("host: Schluessel schreiben: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", "", fmt.Errorf("host: oeffentlichen Schluessel bilden: %w", err)
	}
	return privatePath, publicLine(sshPublic), nil
}

func publicLine(key ssh.PublicKey) string {
	return fmt.Sprintf("%s %s", trimNewline(string(ssh.MarshalAuthorizedKey(key))), KeyComment)
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
