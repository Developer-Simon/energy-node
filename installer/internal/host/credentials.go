package host

import (
	"github.com/Developer-Simon/energy-node-installer/internal/credstore"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

const (
	// CodeCredentialsMissing: die Oberflaeche wollte das gemerkte Passwort,
	// fuer diesen Host und Benutzer ist aber keines gemerkt.
	CodeCredentialsMissing = "CREDENTIALS_MISSING"
	// CodeCredentialsStoreFailed: Anmeldung gelungen, Merken oder Loeschen
	// im Schluesselbund nicht.
	CodeCredentialsStoreFailed = "CREDENTIALS_STORE_FAILED"
)

// savedCredentials haelt den gemerkten Zugang im Speicher. Describe laeuft bei
// jedem API-Aufruf und soll den Schluesselbund nicht jedes Mal fragen.
// Zugriff nur unter Host.mu.
type savedCredentials struct {
	store     credstore.Store
	available bool
	saved     *credstore.Credentials
}

// loadCredentials fragt den Schluesselbund einmal. Ein Fehler schaltet die
// Funktion fuer diesen Lauf ab, der Installer startet trotzdem.
func loadCredentials(store credstore.Store) *savedCredentials {
	if store == nil {
		return &savedCredentials{}
	}
	c, found, err := store.Load()
	if err != nil {
		return &savedCredentials{}
	}
	creds := savedCredentials{store: store, available: true}
	if found {
		creds.saved = &c
	}
	return &creds
}

func (s *savedCredentials) view() *hostapi.SavedCredentials {
	if !s.available {
		return nil
	}
	if s.saved == nil {
		return &hostapi.SavedCredentials{}
	}
	return &hostapi.SavedCredentials{
		Host: s.saved.Host, User: s.saved.User, Kind: hostapi.AuthKind(s.saved.Kind),
		KeyPath: s.saved.KeyPath, HasSecret: s.saved.Secret != "",
	}
}

// secret liefert das gemerkte Passwort nur fuer genau den gemerkten Host und
// Benutzer. Ein anderer Node bekommt es nie.
func (s *savedCredentials) secret(host, user string) (string, bool) {
	if s.saved == nil || s.saved.Secret == "" || s.saved.Host != host || s.saved.User != user {
		return "", false
	}
	return s.saved.Secret, true
}

// remember merkt oder loescht nach einer gelungenen Anmeldung. Rueckgabe ist
// ein Fehlercode fuer die Oberflaeche, leer bei Erfolg oder ohne
// Schluesselbund.
func (s *savedCredentials) remember(req hostapi.ConnectRequest, secret string) string {
	if !s.available {
		return ""
	}
	if !req.Remember {
		if s.saved == nil {
			return ""
		}
		if err := s.store.Delete(); err != nil {
			return CodeCredentialsStoreFailed
		}
		s.saved = nil
		return ""
	}
	c := credstore.Credentials{Host: req.Host, User: req.User, Kind: string(req.Kind)}
	if req.Kind == hostapi.AuthKey {
		c.KeyPath = req.KeyPath
	} else {
		c.Secret = secret
	}
	if err := s.store.Save(c); err != nil {
		return CodeCredentialsStoreFailed
	}
	s.saved = &c
	return ""
}
