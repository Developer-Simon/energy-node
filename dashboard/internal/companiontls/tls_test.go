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

func TestBrokenFilesAreReplaced(t *testing.T) {
	dir := t.TempDir()

	_, before, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate (Ausgangszustand): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, CertFile), []byte("Müll"), 0644); err != nil {
		t.Fatal(err)
	}

	_, after, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate mit kaputter Datei: %v", err)
	}
	if after == "" {
		t.Fatal("fingerprint ist leer")
	}
	if after == before {
		t.Fatal("kaputte Zertifikatsdatei wurde nicht ersetzt")
	}
}
