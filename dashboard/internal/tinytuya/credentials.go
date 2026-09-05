package tinytuya

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Credentials contains the Cloud values needed for TinyTuya device discovery.
// AccessSecret is deliberately never returned by an HTTP handler.
type Credentials struct {
	Region       string `json:"region"`
	AccessID     string `json:"access_id"`
	AccessSecret string `json:"access_secret"`
}

type CredentialStore struct {
	mu   sync.Mutex
	path string
}

func NewCredentialStore(dataDir string) *CredentialStore {
	return &CredentialStore{path: filepath.Join(filepath.Clean(dataDir), "tinytuya_credentials.json")}
}

func (s *CredentialStore) Load() (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return Credentials{}, err
	}
	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("TinyTuya-Zugangsdaten enthalten ungültiges JSON: %w", err)
	}
	if err := validateCredentials(credentials); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func (s *CredentialStore) Save(credentials Credentials) error {
	if err := validateCredentials(credentials); err != nil {
		return err
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".tinytuya-credentials-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, s.path)
}

func validateCredentials(credentials Credentials) error {
	if strings.TrimSpace(credentials.Region) == "" ||
		strings.TrimSpace(credentials.AccessID) == "" ||
		strings.TrimSpace(credentials.AccessSecret) == "" {
		return errors.New("Region, Access ID und Access Secret sind erforderlich")
	}
	return nil
}
