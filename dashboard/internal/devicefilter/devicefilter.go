package devicefilter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

type DiscoveryRecord struct {
	Topic       string    `json:"topic"`
	UniqueID    string    `json:"unique_id,omitempty"`
	Component   string    `json:"component,omitempty"`
	ObjectID    string    `json:"object_id,omitempty"`
	EntityName  string    `json:"entity_name,omitempty"`
	StateTopics []string  `json:"state_topics,omitempty"`
	Present     bool      `json:"present"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

type DeviceRecord struct {
	DeviceID     string            `json:"device_id"`
	Name         string            `json:"name,omitempty"`
	Manufacturer string            `json:"manufacturer,omitempty"`
	Model        string            `json:"model,omitempty"`
	Firmware     string            `json:"firmware,omitempty"`
	ViaDevice    string            `json:"via_device,omitempty"`
	IgnoredAt    time.Time         `json:"ignored_at"`
	Discoveries  []DiscoveryRecord `json:"discoveries,omitempty"`
}

type Summary struct {
	DeviceID       string    `json:"device_id"`
	Name           string    `json:"name,omitempty"`
	Model          string    `json:"model,omitempty"`
	IgnoredAt      time.Time `json:"ignored_at"`
	DiscoveryCount int       `json:"discovery_count"`
	Stale          bool      `json:"stale"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	records map[string]DeviceRecord
	epoch   map[string]map[string]bool
}

func NewStore(dataDir string) *Store {
	path := dataDir
	if !strings.HasSuffix(path, ".json") {
		path = filepath.Join(path, "ignored_devices.json")
	}
	return &Store{path: filepath.Clean(path), records: make(map[string]DeviceRecord), epoch: make(map[string]map[string]bool)}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.records = make(map[string]DeviceRecord)
		s.epoch = make(map[string]map[string]bool)
		return nil
	}
	if err != nil {
		return err
	}
	var values []DeviceRecord
	if err := json.Unmarshal(data, &values); err != nil {
		s.records = make(map[string]DeviceRecord)
		s.epoch = make(map[string]map[string]bool)
		return err
	}
	s.records = make(map[string]DeviceRecord, len(values))
	s.epoch = make(map[string]map[string]bool)
	for _, value := range values {
		if value.DeviceID == "" {
			continue
		}
		normalizeRecord(&value)
		s.records[value.DeviceID] = value
	}
	return nil
}

func (s *Store) IsIgnored(deviceID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[deviceID]
	return ok
}

func (s *Store) Get(deviceID string) (DeviceRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[deviceID]
	if !ok {
		return DeviceRecord{}, false
	}
	return cloneRecord(record), true
}

func (s *Store) List() []DeviceRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DeviceRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, cloneRecord(record))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeviceID < result[j].DeviceID })
	return result
}

func (s *Store) Summaries() []Summary {
	records := s.List()
	result := make([]Summary, 0, len(records))
	for _, record := range records {
		result = append(result, Summary{DeviceID: record.DeviceID, Name: record.Name, Model: record.Model, IgnoredAt: record.IgnoredAt, DiscoveryCount: len(record.Discoveries), Stale: missingRecord(record)})
	}
	return result
}

func (s *Store) Ignore(view registry.DeviceView) error {
	if view.ID == "" {
		return errors.New("devicefilter: device ID is empty")
	}
	now := time.Now().UTC()
	record := DeviceRecord{DeviceID: view.ID, Name: view.Name, Manufacturer: view.Manufacturer, Model: view.Model, Firmware: view.Firmware, ViaDevice: view.ViaDevice, IgnoredAt: now, Discoveries: make([]DiscoveryRecord, 0, len(view.Entities))}
	for _, entity := range view.Entities {
		if entity.DiscoveryTopic == "" {
			continue
		}
		record.Discoveries = append(record.Discoveries, DiscoveryRecord{Topic: entity.DiscoveryTopic, UniqueID: entity.UniqueID, Component: entity.Component, ObjectID: entity.ObjectID, EntityName: entity.Name, StateTopics: entityTopics(entity), Present: true, LastSeen: now})
	}
	normalizeRecord(&record)
	return s.update(record)
}

func (s *Store) Unignore(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[deviceID]; !ok {
		return os.ErrNotExist
	}
	delete(s.records, deviceID)
	return s.saveLocked()
}

func (s *Store) MarkPresent(discovery registry.Discovery) error {
	if discovery.Device.ID == "" || discovery.DiscoveryTopic == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[discovery.Device.ID]
	if !ok {
		return nil
	}
	seen := time.Now().UTC()
	if pending := s.epoch[discovery.Device.ID]; pending != nil {
		pending[discovery.DiscoveryTopic] = true
	}
	index := -1
	for i := range record.Discoveries {
		if record.Discoveries[i].Topic == discovery.DiscoveryTopic {
			index = i
			break
		}
	}
	if index < 0 {
		record.Discoveries = append(record.Discoveries, discoveryRecord(discovery, true, seen))
	} else {
		record.Discoveries[index] = discoveryRecord(discovery, true, seen)
	}
	normalizeRecord(&record)
	s.records[record.DeviceID] = record
	return s.saveLocked()
}

func (s *Store) BeginPresenceEpoch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch = make(map[string]map[string]bool, len(s.records))
	for deviceID, record := range s.records {
		pending := make(map[string]bool, len(record.Discoveries))
		for _, discovery := range record.Discoveries {
			pending[discovery.Topic] = false
		}
		s.epoch[deviceID] = pending
	}
	return nil
}

func (s *Store) CompletePresenceEpoch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for deviceID, pending := range s.epoch {
		record, ok := s.records[deviceID]
		if !ok {
			continue
		}
		for index := range record.Discoveries {
			present, seen := pending[record.Discoveries[index].Topic]
			if !seen {
				continue
			}
			if record.Discoveries[index].Present != present {
				record.Discoveries[index].Present = present
				changed = true
			}
		}
		s.records[deviceID] = record
	}
	s.epoch = make(map[string]map[string]bool)
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) MarkMissing(topic string) error {
	if topic == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for deviceID, record := range s.records {
		recordChanged := false
		for index := range record.Discoveries {
			if record.Discoveries[index].Topic == topic && record.Discoveries[index].Present {
				record.Discoveries[index].Present = false
				recordChanged = true
			}
		}
		if recordChanged {
			s.records[deviceID] = record
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

func (s *Store) Missing(deviceID string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[deviceID]
	if !ok {
		return nil, false
	}
	missing := make([]string, 0, len(record.Discoveries))
	for _, discovery := range record.Discoveries {
		if !discovery.Present {
			missing = append(missing, discovery.Topic)
		}
	}
	sort.Strings(missing)
	return missing, missingRecord(record)
}

func (s *Store) DiscoveryTopics(deviceID string) []string {
	record, ok := s.Get(deviceID)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(record.Discoveries))
	for _, discovery := range record.Discoveries {
		result = append(result, discovery.Topic)
	}
	sort.Strings(result)
	return result
}

func (s *Store) update(record DeviceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.records[record.DeviceID]
	s.records[record.DeviceID] = cloneRecord(record)
	if err := s.saveLocked(); err != nil {
		if existed {
			s.records[record.DeviceID] = previous
		} else {
			delete(s.records, record.DeviceID)
		}
		return err
	}
	return nil
}

func (s *Store) Restore(record DeviceRecord) error {
	if record.DeviceID == "" {
		return errors.New("devicefilter: device ID is empty")
	}
	return s.update(record)
}

func (s *Store) saveLocked() error {
	values := make([]DeviceRecord, 0, len(s.records))
	for _, record := range s.records {
		normalizeRecord(&record)
		values = append(values, record)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].DeviceID < values[j].DeviceID })
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".ignored-devices-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
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
	return os.Rename(temporaryName, s.path)
}

func discoveryRecord(discovery registry.Discovery, present bool, seen time.Time) DiscoveryRecord {
	return DiscoveryRecord{Topic: discovery.DiscoveryTopic, UniqueID: discovery.Entity.UniqueID, Component: discovery.Entity.Component, ObjectID: discovery.Entity.ObjectID, EntityName: discovery.Entity.Name, StateTopics: entityTopicsFromDiscovery(discovery), Present: present, LastSeen: seen}
}

func entityTopics(entity registry.EntityView) []string {
	result := []string{entity.StateTopic, entity.AvailabilityTopic}
	for _, availability := range entity.Availability {
		result = append(result, availability.Topic)
	}
	return uniqueSorted(result)
}

func entityTopicsFromDiscovery(discovery registry.Discovery) []string {
	result := []string{discovery.Entity.StateTopic, discovery.Entity.AvailabilityTopic}
	for _, availability := range discovery.Entity.Availability {
		result = append(result, availability.Topic)
	}
	return uniqueSorted(result)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func normalizeRecord(record *DeviceRecord) {
	sort.Slice(record.Discoveries, func(i, j int) bool { return record.Discoveries[i].Topic < record.Discoveries[j].Topic })
	for index := range record.Discoveries {
		record.Discoveries[index].StateTopics = uniqueSorted(record.Discoveries[index].StateTopics)
	}
}

func missingRecord(record DeviceRecord) bool {
	if len(record.Discoveries) == 0 {
		return false
	}
	for _, discovery := range record.Discoveries {
		if discovery.Present {
			return false
		}
	}
	return true
}

func cloneRecord(record DeviceRecord) DeviceRecord {
	clone := record
	clone.Discoveries = append([]DiscoveryRecord(nil), record.Discoveries...)
	for index := range clone.Discoveries {
		clone.Discoveries[index].StateTopics = append([]string(nil), clone.Discoveries[index].StateTopics...)
	}
	return clone
}
