package companiontls

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadOrCreateWritesAndReusesTheCertificate(t *testing.T) {
	dir := t.TempDir()

	_, first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate (erster Aufruf): %v", err)
	}
	_, second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate (zweiter Aufruf): %v", err)
	}
	if first == "" {
		t.Fatal("fingerprint ist leer")
	}
	if first != second {
		t.Fatalf("fingerprint ändert sich zwischen Aufrufen: %q != %q", first, second)
	}

	certPath := filepath.Join(dir, CertFile)
	keyPath := filepath.Join(dir, KeyFile)
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("Zertifikatsdatei fehlt: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("Schlüsseldatei fehlt: %v", err)
	}

	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := keyInfo.Mode().Perm(); mode != 0600 {
		t.Fatalf("Schlüssel-Modus = %o, want 0600", mode)
	}
	certInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := certInfo.Mode().Perm(); mode != 0644 {
		t.Fatalf("Zertifikat-Modus = %o, want 0644", mode)
	}
}

func TestCertificateIsLongLived(t *testing.T) {
	cert, _, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("kein Zertifikat in tls.Certificate")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}
	if min := time.Now().AddDate(19, 0, 0); !leaf.NotAfter.After(min) {
		t.Fatalf("NotAfter = %v, want nach %v", leaf.NotAfter, min)
	}
	if leaf.Subject.CommonName != "energy-node-companion" {
		t.Fatalf("CommonName = %q, want energy-node-companion", leaf.Subject.CommonName)
	}
}

func TestBrokenFilesAreNotReplaced(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LoadOrCreate(dir); err != nil {
		t.Fatalf("LoadOrCreate (Ausgangszustand): %v", err)
	}
	certPath := filepath.Join(dir, CertFile)
	keyPath := filepath.Join(dir, KeyFile)
	if err := os.WriteFile(certPath, []byte("Müll"), 0644); err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("LoadOrCreate mit kaputter Zertifikatsdatei: kein Fehler")
	}
	if certAfter, err := os.ReadFile(certPath); err != nil || string(certAfter) != "Müll" {
		t.Fatalf("kaputte Zertifikatsdatei wurde verändert: %q, %v", certAfter, err)
	}
	if keyAfter, err := os.ReadFile(keyPath); err != nil || string(keyAfter) != string(keyBefore) {
		t.Fatalf("Schlüsseldatei wurde verändert: %v", err)
	}
}

func TestMissingFileIsCreatedAgain(t *testing.T) {
	dir := t.TempDir()
	_, before, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate (Ausgangszustand): %v", err)
	}
	if err := os.Remove(filepath.Join(dir, KeyFile)); err != nil {
		t.Fatal(err)
	}
	_, after, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate ohne Schlüsseldatei: %v", err)
	}
	if after == "" || after == before {
		t.Fatalf("fehlende Schlüsseldatei: fingerprint %q, vorher %q", after, before)
	}
}
