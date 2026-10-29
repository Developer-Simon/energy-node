package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

// ActivityEntry is one line of the device activity: a recent command or one
// state of a recent MQTT message. Key stays stable across refreshes so the
// client can keep rows in place.
type ActivityEntry struct {
	Key   string    `json:"key"`
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	Label string    `json:"label"`
	Value string    `json:"value,omitempty"`
	OK    bool      `json:"ok,omitempty"`
}

const (
	activityCommandLimit = 3
	activityMessageLimit = 3
)

// buildActivity is the Go port of activityEntries, recentMessages and
// messageStates in device-modal.js: up to three commands first, then the
// states of the three most recent messages. Messages sharing a topic and
// timestamp count once. A JSON object payload splits into one entry per
// leaf key (dot paths for nested objects, arrays stay one JSON value), a
// scalar payload becomes one entry labelled with the entity name, and an
// empty payload becomes one entry with key "empty".
func buildActivity(dev registry.DeviceView, commands []CommandAction) []ActivityEntry {
	entries := []ActivityEntry{}
	for i, action := range commands {
		if i == activityCommandLimit {
			break
		}
		entries = append(entries, ActivityEntry{
			Key:   "cmd-" + action.At.Format(time.RFC3339Nano) + "-" + action.EntityID,
			At:    action.At,
			Kind:  "command",
			Label: action.EntityID,
			OK:    action.Result == "published",
		})
	}

	messages := recentActivityMessages(dev)
	for i, message := range messages {
		if i == activityMessageLimit {
			break
		}
		at := message.Message.At.Format(time.RFC3339Nano)
		for _, state := range messageActivityStates(dev, message) {
			entries = append(entries, ActivityEntry{
				Key:   "msg-" + message.UniqueID + "-" + at + "-" + state.key,
				At:    message.Message.At,
				Kind:  "message",
				Label: state.label,
				Value: state.value,
			})
		}
	}
	return entries
}

type activityMessage struct {
	UniqueID string
	Message  registry.MQTTMessage
}

type activityState struct {
	key   string
	label string
	value string
}

// recentActivityMessages returns the last message of every entity, newest
// first. An entity sharing a topic and timestamp with an earlier one (for
// example the balance topic of energy_node) is dropped.
func recentActivityMessages(dev registry.DeviceView) []activityMessage {
	seen := map[string]bool{}
	var out []activityMessage
	for _, entity := range dev.Entities {
		if entity.LastMessage == nil {
			continue
		}
		message := activityMessage{UniqueID: entity.UniqueID, Message: *entity.LastMessage}
		id := message.UniqueID
		if message.Message.Topic != "" {
			id = message.Message.Topic + "|" + message.Message.At.UTC().Format(time.RFC3339Nano)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, message)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Message.At.After(out[j].Message.At)
	})
	return out
}

// messageActivityStates is messageStates of device-modal.js.
func messageActivityStates(dev registry.DeviceView, message activityMessage) []activityState {
	raw := message.Message.Payload
	if strings.TrimSpace(raw) == "" {
		return []activityState{{key: "empty", label: activityLabel(dev, message.UniqueID), value: ""}}
	}

	if parsed, ok := parseJSONOrdered(raw); ok {
		if object, isObject := parsed.(*jsObject); isObject {
			var states []activityState
			walkJSONObject(object, "", &states)
			if len(states) > 0 {
				return states
			}
		}
	}

	return []activityState{{key: "value", label: activityLabel(dev, message.UniqueID), value: raw}}
}

// activityLabel names a message by its entity. The topic is never a label:
// the endpoint must not expose topics to users without view_device_analysis.
func activityLabel(dev registry.DeviceView, uniqueID string) string {
	for _, entity := range dev.Entities {
		if entity.UniqueID == uniqueID {
			return entityDisplayName(dev.Name, entity)
		}
	}
	return "MQTT"
}

// walkJSONObject expands nested objects into dot paths. An empty nested
// object is a state of its own, rendered as "{}".
func walkJSONObject(object *jsObject, prefix string, states *[]activityState) {
	for _, key := range object.orderedKeys() {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		value := object.values[key]
		if child, ok := value.(*jsObject); ok && len(child.keys) > 0 {
			walkJSONObject(child, path, states)
			continue
		}
		*states = append(*states, activityState{key: path, label: path, value: formatJSValue(value)})
	}
}

// entityDisplayName is entityDisplayName of device-modal.js: the entity name
// without the device prefix, falling back to object_id or unique_id when the
// entity has no name.
func entityDisplayName(deviceName string, entity registry.EntityView) string {
	fallback := entity.ObjectID
	if fallback == "" {
		fallback = entity.UniqueID
	}
	name := strings.TrimSpace(entity.Name)
	if name == "" {
		return fallback
	}
	return webui.StripDeviceName(deviceName, name)
}

// formatJSValue is formatStateValue of device-modal.js.
func formatJSValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "–"
	case *jsObject, []any:
		return jsStringify(v)
	case json.Number:
		return jsNumberString(v)
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	}
	return fmt.Sprint(value)
}

// jsObject is a decoded JSON object that keeps the key order of the source,
// since JavaScript objects iterate in insertion order.
type jsObject struct {
	keys   []string
	values map[string]any
}

// orderedKeys follows the JavaScript property order: array-index keys ascending
// first, then the other keys in insertion order.
func (o *jsObject) orderedKeys() []string {
	var indexes, named []string
	for _, key := range o.keys {
		if isJSArrayIndex(key) {
			indexes = append(indexes, key)
		} else {
			named = append(named, key)
		}
	}
	sort.Slice(indexes, func(i, j int) bool {
		a, _ := strconv.ParseUint(indexes[i], 10, 32)
		b, _ := strconv.ParseUint(indexes[j], 10, 32)
		return a < b
	})
	return append(indexes, named...)
}

func isJSArrayIndex(key string) bool {
	n, err := strconv.ParseUint(key, 10, 32)
	if err != nil || n == math.MaxUint32 {
		return false
	}
	return strconv.FormatUint(n, 10) == key
}

// parseJSONOrdered decodes raw like JSON.parse. Numbers stay json.Number so
// they can be formatted like JavaScript numbers.
func parseJSONOrdered(raw string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	value, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return value, true
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, nil
	}
	switch delim {
	case '{':
		object := &jsObject{values: map[string]any{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			value, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			if _, seen := object.values[key]; !seen {
				object.keys = append(object.keys, key)
			}
			object.values[key] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		items := []any{}
		for dec.More() {
			value, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return items, nil
	}
	return nil, errors.New("unexpected delimiter")
}

// jsStringify is JSON.stringify for values produced by parseJSONOrdered.
func jsStringify(value any) string {
	var b strings.Builder
	writeJSValue(&b, value)
	return b.String()
}

func writeJSValue(b *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case json.Number:
		b.WriteString(jsNumberString(v))
	case string:
		writeJSString(b, v)
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSValue(b, item)
		}
		b.WriteByte(']')
	case *jsObject:
		b.WriteByte('{')
		for i, key := range v.orderedKeys() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSString(b, key)
			b.WriteByte(':')
			writeJSValue(b, v.values[key])
		}
		b.WriteByte('}')
	}
}

func writeJSString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// jsNumberString formats a JSON number like String(Number(text)) in JavaScript.
func jsNumberString(n json.Number) string {
	f, _ := strconv.ParseFloat(string(n), 64)
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	sign := exponent[:1]
	digits := strings.TrimLeft(exponent[1:], "0")
	return mantissa + "e" + sign + digits
}
