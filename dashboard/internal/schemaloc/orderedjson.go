package schemaloc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// object is a decoded JSON object that remembers its key order. The schema
// form renders fields in document order, so a round trip through a Go map
// (which json.Marshal sorts) would reorder the form.
type object struct {
	keys   []string
	values map[string]any
}

// decode parses raw into *object, []any, string, json.Number, bool or nil.
// json.Number keeps number literals byte-identical on the way back out.
func decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("schemaloc: trailing data after the schema")
	}
	return value, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		obj := &object{values: map[string]any{}}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key := keyToken.(string)
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			if _, seen := obj.values[key]; !seen {
				obj.keys = append(obj.keys, key)
			}
			obj.values[key] = value
		}
		_, err := dec.Token() // closing brace
		return obj, err
	case '[':
		list := []any{}
		for dec.More() {
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		_, err := dec.Token() // closing bracket
		return list, err
	}
	return nil, fmt.Errorf("schemaloc: unexpected delimiter %v", delim)
}

func encode(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case *object:
		buf.WriteByte('{')
		for i, key := range v.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encode(buf, key); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := encode(buf, v.values[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encode(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
	}
	return nil
}
