package mqttclient

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Credentials holds the broker password for the dashboard's own MQTT
// connection. It is kept out of settings.MQTTConfig (mqtt.json) so it never
// ends up in a settings revision copy - same reasoning as
// tinytuya.CredentialStore.
type Credentials struct {
	Password string `json:"password"`
}

type CredentialStore struct {
	mu   sync.Mutex
	path string
}

func NewCredentialStore(dataDir string) *CredentialStore {
	return &CredentialStore{path: filepath.Join(filepath.Clean(dataDir), "mqtt_credentials.json")}
}

// NewBridgeCredentialStore holds the Mosquitto bridge's remote_password
// (the Hauptsystem broker's credentials, not the dashboard's own broker
// connection), kept in its own file for the same reason mqtt_credentials.json
// is separate from mqtt.json: it must never end up in a bridge.json
// revision copy.
func NewBridgeCredentialStore(dataDir string) *CredentialStore {
	return &CredentialStore{path: filepath.Join(filepath.Clean(dataDir), "mqtt_bridge_credentials.json")}
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
		return Credentials{}, err
	}
	return credentials, nil
}

func (s *CredentialStore) Save(credentials Credentials) error {
	if credentials.Password == "" {
		return errors.New("mqttclient: password must not be empty")
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
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".mqtt-credentials-*")
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

func (s *CredentialStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
