// Package runtimecache will persist a reduced stale-/health-purpose cache to
// <dashboard-data-dir>/runtime.json: last value, last-seen timestamp and
// availability per device/topic. Writes are triggered by a real value change
// or a periodic sweep (see settings.json), never on every single MQTT
// update, and use a temp-file-plus-rename write so a power loss cannot leave
// a half-written JSON file. Corrupt or incomplete cache files are ignored at
// startup rather than blocking the service.
package runtimecache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type Entry struct {
	DeviceID        string    `json:"device_id"`
	EntityID        string    `json:"entity_id"`
	Topic           string    `json:"topic"`
	Value           string    `json:"value"`
	LastSeen        time.Time `json:"last_seen"`
	LastTopicAt     time.Time `json:"last_topic_at,omitempty"`
	Available       bool      `json:"available"`
	HasAvailability bool      `json:"has_availability"`
	Payload         string    `json:"payload,omitempty"`
}

type Status struct {
	Loaded      bool      `json:"loaded"`
	LoadError   string    `json:"load_error,omitempty"`
	WriteError  string    `json:"write_error,omitempty"`
	LastWriteAt time.Time `json:"last_write_at,omitempty"`
	EntryCount  int       `json:"entry_count"`
}

type StatusProvider interface {
	Status() Status
}

func (s Status) Degraded() bool {
	return s.LoadError != "" || s.WriteError != ""
}

type file struct {
	Entries []Entry `json:"entries"`
}

type Store struct {
	mu          sync.Mutex
	path        string
	entries     map[string]Entry
	loaded      bool
	loadErr     error
	writeErr    error
	lastWriteAt time.Time
}

func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(filepath.Clean(dir), "runtime.json"), entries: make(map[string]Entry)}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		s.loadErr = nil
		return nil
	}
	if err != nil {
		s.loaded = true
		s.loadErr = err
		return err
	}
	var saved file
	if err := json.Unmarshal(data, &saved); err != nil {
		s.loaded = true
		s.loadErr = err
		return nil
	}
	s.entries = make(map[string]Entry, len(saved.Entries))
	for _, entry := range saved.Entries {
		if entry.DeviceID != "" && entry.EntityID != "" && !entry.LastSeen.IsZero() {
			if entry.LastTopicAt.IsZero() {
				entry.LastTopicAt = entry.LastSeen
			}
			s.entries[key(entry.DeviceID, entry.EntityID)] = entry
		}
	}
	s.loaded = true
	s.loadErr = nil
	return nil
}

func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Status{Loaded: s.loaded, LastWriteAt: s.lastWriteAt, EntryCount: len(s.entries)}
	if s.loadErr != nil {
		status.LoadError = s.loadErr.Error()
	}
	if s.writeErr != nil {
		status.WriteError = s.writeErr.Error()
	}
	return status
}

// Observe persists only changed values. It also returns the current cache
// snapshot so callers can expose cache health without reading the file.
func (s *Store) Observe(devices []registry.DeviceView, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.observeLocked(devices)
	if changed {
		return s.writeLocked()
	}
	return nil
}

// ObserveMemory updates the in-memory fallback state without touching the
// filesystem. The periodic Sweep is the only MQTT-independent persistence
// point, which keeps high-frequency state updates off the SSD.
func (s *Store) ObserveMemory(devices []registry.DeviceView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observeLocked(devices)
}

// ObserveChanges is ObserveMemory's counterpart for the MQTT client's
// per-message state observer: it updates only the entities a single
// incoming message actually changed instead of walking every device in the
// registry, so a busy broker doesn't force a full snapshot per message. The
// periodic Sweep remains the only path that writes to disk.
func (s *Store) ObserveChanges(changes []registry.StateChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, change := range changes {
		if !change.HasValue {
			continue
		}
		entry := Entry{
			DeviceID:        change.DeviceID,
			EntityID:        change.UniqueID,
			Topic:           change.StateTopic,
			Value:           change.Value,
			LastSeen:        change.LastSeen,
			LastTopicAt:     change.LastTopicAt,
			Available:       change.Available,
			HasAvailability: change.HasAvailability,
			Payload:         change.Payload,
		}
		s.entries[key(entry.DeviceID, entry.EntityID)] = entry
	}
}

func (s *Store) observeLocked(devices []registry.DeviceView) bool {
	changed := false
	for _, device := range devices {
		for _, entity := range device.Entities {
			if !entity.HasValue {
				continue
			}
			payload := ""
			if entity.LastMessage != nil {
				payload = entity.LastMessage.Payload
			}
			entry := Entry{DeviceID: device.ID, EntityID: entity.UniqueID, Topic: entity.StateTopic, Value: entity.Value, LastSeen: entity.LastSeen, LastTopicAt: entity.LastTopicAt, Available: entity.Available, HasAvailability: entity.HasAvailability, Payload: payload}
			k := key(entry.DeviceID, entry.EntityID)
			old, ok := s.entries[k]
			s.entries[k] = entry
			if !ok || old.Value != entry.Value || old.Available != entry.Available || old.HasAvailability != entry.HasAvailability {
				changed = true
			}
		}
	}
	return changed
}

func (s *Store) Sweep(devices []registry.DeviceView, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, device := range devices {
		for _, entity := range device.Entities {
			if entity.HasValue {
				payload := ""
				if entity.LastMessage != nil {
					payload = entity.LastMessage.Payload
				}
				s.entries[key(device.ID, entity.UniqueID)] = Entry{DeviceID: device.ID, EntityID: entity.UniqueID, Topic: entity.StateTopic, Value: entity.Value, LastSeen: entity.LastSeen, LastTopicAt: entity.LastTopicAt, Available: entity.Available, HasAvailability: entity.HasAvailability, Payload: payload}
			}
		}
	}
	return s.writeLocked()
}

func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		result = append(result, entry)
	}
	return result
}

// Restore hydrates a discovered entity with its last known cached state.
func (s *Store) Restore(reg *registry.Registry, deviceID, entityID string) bool {
	s.mu.Lock()
	entry, ok := s.entries[key(deviceID, entityID)]
	s.mu.Unlock()
	if !ok {
		return false
	}
	lastTopicAt := entry.LastTopicAt
	if lastTopicAt.IsZero() {
		lastTopicAt = entry.LastSeen
	}
	return reg.RestoreStateAt(deviceID, entityID, entry.Value, entry.Available, entry.HasAvailability, entry.LastSeen, lastTopicAt, entry.Payload)
}

func (s *Store) writeLocked() error {
	data, err := json.MarshalIndent(file{Entries: s.entriesAsSliceLocked()}, "", "  ")
	if err != nil {
		s.writeErr = err
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		s.writeErr = err
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".runtime-*")
	if err != nil {
		s.writeErr = err
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		s.writeErr = err
		return err
	}
	s.writeErr = nil
	s.lastWriteAt = time.Now().UTC()
	return nil
}

func (s *Store) entriesAsSliceLocked() []Entry {
	result := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		result = append(result, entry)
	}
	return result
}

func key(deviceID, entityID string) string { return deviceID + "\x00" + entityID }
