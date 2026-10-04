// Package credstore merkt sich den letzten Zugang zum Node im Schluesselbund
// des Betriebssystems: Windows Credential Manager, macOS Keychain oder
// Secret Service unter Linux. Gespeichert wird ein einziger Eintrag als JSON.
package credstore

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Credentials ist der gemerkte Zugang. Secret ist leer bei einer
// Schluesseldatei.
type Credentials struct {
	Host    string `json:"host"`
	User    string `json:"user"`
	Kind    string `json:"kind"`
	KeyPath string `json:"key_path,omitempty"`
	Secret  string `json:"secret,omitempty"`
}

// Store ist die Ablage. Load meldet found=false, wenn nichts gemerkt ist; ein
// Fehler heisst: die Ablage ist nicht nutzbar.
type Store interface {
	Load() (Credentials, bool, error)
	Save(Credentials) error
	Delete() error
}

// Keyring legt den Eintrag im Schluesselbund des Betriebssystems ab.
type Keyring struct {
	Service string
	Account string
}

func (k Keyring) Load() (Credentials, bool, error) {
	raw, err := keyring.Get(k.Service, k.Account)
	if errors.Is(err, keyring.ErrNotFound) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, fmt.Errorf("credstore: lesen: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		// Ein unlesbarer Eintrag zaehlt als leer; das naechste Save ersetzt ihn.
		return Credentials{}, false, nil
	}
	return c, true, nil
}

func (k Keyring) Save(c Credentials) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("credstore: kodieren: %w", err)
	}
	if err := keyring.Set(k.Service, k.Account, string(raw)); err != nil {
		return fmt.Errorf("credstore: schreiben: %w", err)
	}
	return nil
}

func (k Keyring) Delete() error {
	if err := keyring.Delete(k.Service, k.Account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credstore: loeschen: %w", err)
	}
	return nil
}
