// Package companiontls verwaltet das langlebige TLS-Zertifikat, über das sich der
// Dashboard-Port für die Home-Assistant-Companion-Integration ausweist.
package companiontls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/companionpair"
)

const (
	CertFile = "companion-tls.pem"
	KeyFile  = "companion-tls.key"
)

const commonName = "energy-node-companion"

// LoadOrCreate liest Zertifikat und Schlüssel aus dir oder erzeugt beide:
// ECDSA P-256, CommonName "energy-node-companion", NotBefore jetzt minus 1 h,
// NotAfter plus 20 Jahre, KeyUsage DigitalSignature, ExtKeyUsage ServerAuth,
// Seriennummer 128 Bit Zufall. Schlüsseldatei 0600, Zertifikat 0644.
// fingerprint ist companionpair.Fingerprint des Blatt-DER.
func LoadOrCreate(dir string) (cert tls.Certificate, fingerprint string, err error) {
	cert, err = load(dir)
	if err != nil {
		cert, err = create(dir)
		if err != nil {
			return tls.Certificate{}, "", err
		}
	}
	return cert, companionpair.Fingerprint(cert.Certificate[0]), nil
}

// load liefert das gespeicherte Schlüsselpaar, wenn beide Dateien gültig sind.
func load(dir string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile))
	if err != nil {
		return tls.Certificate{}, err
	}
	if _, err := x509.ParseCertificate(cert.Certificate[0]); err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

// create erzeugt ein neues Schlüsselpaar und ersetzt damit beide Dateien.
func create(dir string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("companiontls: Schlüssel erzeugen: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("companiontls: Seriennummer: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("companiontls: Zertifikat erzeugen: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("companiontls: Schlüssel kodieren: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(dir, 0700); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, KeyFile), keyPEM, 0600); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, CertFile), certPEM, 0644); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// writeFileAtomic schreibt data mit mode über eine temporäre Datei und ein Rename,
// damit ein Absturz nie eine halb geschriebene Datei hinterlässt.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
