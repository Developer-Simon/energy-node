package mqttclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// discoveryPayload mirrors the subset of the Home Assistant MQTT Discovery
// schema that the existing bridges (energy_node_common/discovery.py)
// actually publish - see dashboard/mqtt-topics-und-discovery-format.md
// ("Verifizierte Objekt-IDs und Payload-Muster").
type discoveryPayload struct {
	Name                string          `json:"name"`
	UniqueID            string          `json:"unique_id"`
	Icon                string          `json:"icon"`
	Device              deviceBlock     `json:"device"`
	StateTopic          string          `json:"state_topic"`
	AvailabilityTopic   string          `json:"availability_topic"`
	Availability        json.RawMessage `json:"availability"`
	AvailabilityMode    string          `json:"availability_mode"`
	CommandTopic        string          `json:"command_topic"`
	PayloadOn           string          `json:"payload_on"`
	PayloadOff          string          `json:"payload_off"`
	PayloadAvailable    string          `json:"payload_available"`
	PayloadNotAvailable string          `json:"payload_not_available"`
	ValueTemplate       string          `json:"value_template"`
	UnitOfMeasurement   string          `json:"unit_of_measurement"`
	DeviceClass         string          `json:"device_class"`
	MinValue            *float64        `json:"min"`
	MaxValue            *float64        `json:"max"`
	Step                *float64        `json:"step"`
	EnabledByDefault    *bool           `json:"enabled_by_default"`
	EntityCategory      string          `json:"entity_category"`
}

// deviceBlock mirrors the discovery "device" object. Home Assistant accepts
// "identifiers" either as a single string or as a list of strings, and
// energy_node_common/discovery.py's shared helper is not necessarily used
// identically by every bridge (battery_soc_mqtt.py in particular re-uses/
// overrides the Trucki-Stick's device block, see
// dashboard/mqtt-topics-und-discovery-format.md). identifiersOrString
// accepts both encodings so a bridge using the "bare string" form doesn't
// fail json.Unmarshal and silently drop every one of its entities.
type deviceBlock struct {
	Identifiers  identifiersOrString `json:"identifiers"`
	Name         string              `json:"name"`
	Manufacturer string              `json:"manufacturer"`
	Model        string              `json:"model"`
	Firmware     string              `json:"sw_version"`
	ViaDevice    string              `json:"via_device"`
}

// identifiersOrString unmarshals a JSON string or a JSON array of strings
// into a []string.
type identifiersOrString []string

func (ids *identifiersOrString) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if single == "" {
			*ids = nil
		} else {
			*ids = identifiersOrString{single}
		}
		return nil
	}

	var multi []string
	if err := json.Unmarshal(data, &multi); err != nil {
		return err
	}
	*ids = identifiersOrString(multi)
	return nil
}

// parseDiscoveryTopic splits a
// <prefix>/{component}/{device_id}/{object_id}/config topic into its three
// variable segments. Only the 3-level form is accepted (see Kernprinzip:
// Scope-Reduktion - the optional 4-level node_id form is deferred to Phase
// 5, and this function is the single place that would need to change if
// it's ever added). prefix is normally "homeassistant", but is configurable
// via Config.DiscoveryPrefix.
func parseDiscoveryTopic(prefix, topic string) (component, deviceID, objectID string, ok bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != prefix || parts[4] != "config" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

// parseDiscoveryMessage turns one discovery MQTT message into a
// registry.Discovery. isRemoval is true for an empty payload, which is HA's
// convention for un-registering a previously announced entity.
func parseDiscoveryMessage(prefix, topic string, payload []byte) (disc registry.Discovery, isRemoval bool, err error) {
	component, topicDeviceID, objectID, ok := parseDiscoveryTopic(prefix, topic)
	if !ok {
		return registry.Discovery{}, false, errNotADiscoveryTopic
	}

	if len(strings.TrimSpace(string(payload))) == 0 {
		return registry.Discovery{}, true, nil
	}

	var p discoveryPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return registry.Discovery{}, false, err
	}
	availability, err := parseAvailability(p.Availability, p.AvailabilityTopic, p.PayloadAvailable, p.PayloadNotAvailable)
	if err != nil {
		return registry.Discovery{}, false, err
	}

	deviceID := topicDeviceID
	if len(p.Device.Identifiers) > 0 && p.Device.Identifiers[0] != "" {
		deviceID = p.Device.Identifiers[0]
	}

	uniqueID := p.UniqueID
	if uniqueID == "" {
		// Same fallback scheme the bridges use for the discovery topic
		// itself ({device_id}_{object_id}), for the unlikely case unique_id
		// is missing from the payload.
		uniqueID = topicDeviceID + "_" + objectID
	}

	disc = registry.Discovery{
		Device: registry.DeviceInfo{
			ID:           deviceID,
			Name:         p.Device.Name,
			Manufacturer: p.Device.Manufacturer,
			Model:        p.Device.Model,
			Firmware:     p.Device.Firmware,
			ViaDevice:    p.Device.ViaDevice,
		},
		Entity: registry.EntityInfo{
			UniqueID:            uniqueID,
			Component:           component,
			ObjectID:            objectID,
			Name:                p.Name,
			Icon:                p.Icon,
			StateTopic:          p.StateTopic,
			AvailabilityTopic:   p.AvailabilityTopic,
			CommandTopic:        p.CommandTopic,
			PayloadOn:           p.PayloadOn,
			PayloadOff:          p.PayloadOff,
			PayloadAvailable:    p.PayloadAvailable,
			PayloadNotAvailable: p.PayloadNotAvailable,
			Availability:        availability,
			AvailabilityMode:    p.AvailabilityMode,
			ValueTemplate:       p.ValueTemplate,
			UnitOfMeasurement:   p.UnitOfMeasurement,
			DeviceClass:         p.DeviceClass,
			MinValue:            p.MinValue,
			MaxValue:            p.MaxValue,
			Step:                p.Step,
			DefaultHidden:       p.EnabledByDefault != nil && !*p.EnabledByDefault,
			EntityCategory:      p.EntityCategory,
		},
		RawJSON: string(payload),
	}
	return disc, false, nil
}

func parseAvailability(raw json.RawMessage, legacyTopic, legacyAvailable, legacyUnavailable string) ([]registry.AvailabilityInfo, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		if legacyTopic == "" {
			return nil, nil
		}
		return []registry.AvailabilityInfo{{Topic: legacyTopic, PayloadAvailable: legacyAvailable, PayloadNotAvailable: legacyUnavailable}}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var values []registry.AvailabilityInfo
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("availability must be an object or array: %w", err)
		}
		return validateAvailability(values)
	}

	var value registry.AvailabilityInfo
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("availability must be an object or array: %w", err)
	}
	values := []registry.AvailabilityInfo{value}
	return validateAvailability(values)
}

func validateAvailability(values []registry.AvailabilityInfo) ([]registry.AvailabilityInfo, error) {
	for _, value := range values {
		if strings.TrimSpace(value.Topic) == "" {
			return nil, fmt.Errorf("availability topic is empty")
		}
	}
	return values, nil
}

// removalUniqueID derives the unique_id an empty discovery payload refers
// to. HA's removal convention publishes the empty payload to the exact same
// discovery topic the entity was originally announced on, so the
// {device_id}_{object_id} fallback scheme is what we can reconstruct without
// the (now absent) payload's unique_id field.
func removalUniqueID(prefix, topic string) (uniqueID string, ok bool) {
	_, deviceID, objectID, ok := parseDiscoveryTopic(prefix, topic)
	if !ok {
		return "", false
	}
	return deviceID + "_" + objectID, true
}
