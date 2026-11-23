package learnrec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"math"
	"os"
)

// Binary day format of the minute recording.
//
// A day file starts with the header "ENM1", a u16 length and the day.
// Blocks follow. Each block is a u32 body length, the body and the u32
// CRC32 (IEEE) of the body. The first body byte is the block type:
//
//   - 'K' key block: u16 count, then per key u16 length and the key name.
//     New keys get the next indices of the file's key table.
//   - 'V' value block: u16 first minute, u16 minute count, u16 key count,
//     then per key of the table (indices 0 to key count minus 1) the minute
//     count of float32 values, little endian. Keys without a value in the
//     block are written as gaps (NaN).
//
// A block that is incomplete or fails its CRC ends the valid part of the
// file. Reading stops there, and OpenAppend cuts the file at that point.

const (
	minuteMagic   = "ENM1"
	minutesPerDay = 1440
	minuteKeys    = 'K'
	minuteValues  = 'V'
)

var (
	minuteGap = float32(math.NaN())

	errShortHeader = errors.New("minute file: header incomplete")
	errBadHeader   = errors.New("minute file: bad header")
)

// MinuteBlock holds the values of several keys for consecutive minutes.
// First is the minute of the day (0 to 1439), Values[k][i] is the value of
// Keys[k] at minute First+i.
type MinuteBlock struct {
	First  int
	Keys   []string
	Values [][]float32
}

// AppendBlock writes b to w. The first call for a file also writes the
// header. Keys that are not yet in known get a key block first. known maps
// each key to its index in the file and is extended on success.
func AppendBlock(w io.Writer, day string, b MinuteBlock, known map[string]int) (int64, error) {
	if known == nil {
		return 0, errors.New("minute block: known map is nil")
	}
	if len(b.Keys) == 0 {
		return 0, errors.New("minute block: no keys")
	}
	if len(b.Values) != len(b.Keys) {
		return 0, fmt.Errorf("minute block: %d keys but %d value rows", len(b.Keys), len(b.Values))
	}
	count := len(b.Values[0])
	if count == 0 || b.First < 0 || b.First+count > minutesPerDay {
		return 0, fmt.Errorf("minute block: %d minutes from %d outside the day", count, b.First)
	}
	seen := make(map[string]bool, len(b.Keys))
	for k, key := range b.Keys {
		if key == "" || len(key) > math.MaxUint16 {
			return 0, fmt.Errorf("minute block: key %q has an invalid length", key)
		}
		if seen[key] {
			return 0, fmt.Errorf("minute block: key %q twice", key)
		}
		seen[key] = true
		if len(b.Values[k]) != count {
			return 0, fmt.Errorf("minute block: key %q has %d values, want %d", key, len(b.Values[k]), count)
		}
	}

	var newKeys []string
	for _, key := range b.Keys {
		if _, ok := known[key]; !ok {
			newKeys = append(newKeys, key)
		}
	}
	base := len(known)
	if base+len(newKeys) > math.MaxUint16 {
		return 0, errors.New("minute block: too many keys for one day file")
	}

	var out []byte
	if base == 0 {
		if len(day) > math.MaxUint16 {
			return 0, errors.New("minute block: day name too long")
		}
		out = append(out, minuteMagic...)
		out = binary.LittleEndian.AppendUint16(out, uint16(len(day)))
		out = append(out, day...)
	}
	if len(newKeys) > 0 {
		body := []byte{minuteKeys}
		body = binary.LittleEndian.AppendUint16(body, uint16(len(newKeys)))
		for _, key := range newKeys {
			body = binary.LittleEndian.AppendUint16(body, uint16(len(key)))
			body = append(body, key...)
		}
		out = appendFrame(out, body)
	}

	// The value block covers the whole key table in index order.
	order := make([]string, base+len(newKeys))
	for key, idx := range known {
		if idx < 0 || idx >= base {
			return 0, fmt.Errorf("minute block: key %q has index %d of %d", key, idx, base)
		}
		order[idx] = key
	}
	for i, key := range newKeys {
		order[base+i] = key
	}
	rows := make(map[string][]float32, len(b.Keys))
	for k, key := range b.Keys {
		rows[key] = b.Values[k]
	}
	body := []byte{minuteValues}
	body = binary.LittleEndian.AppendUint16(body, uint16(b.First))
	body = binary.LittleEndian.AppendUint16(body, uint16(count))
	body = binary.LittleEndian.AppendUint16(body, uint16(len(order)))
	for _, key := range order {
		row, ok := rows[key]
		for i := 0; i < count; i++ {
			v := minuteGap
			if ok {
				v = row[i]
			}
			body = binary.LittleEndian.AppendUint32(body, math.Float32bits(v))
		}
	}
	out = appendFrame(out, body)

	n, err := w.Write(out)
	if err != nil {
		return int64(n), err
	}
	if n != len(out) {
		return int64(n), io.ErrShortWrite
	}
	for i, key := range newKeys {
		known[key] = base + i
	}
	return int64(len(out)), nil
}

// ReadDay decodes a day file. Gaps are NaN. An incomplete or invalid last
// block is dropped without error. A bad header is an error.
func ReadDay(r io.Reader) (keys []string, values map[string]*[1440]float32, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	p, err := parseMinuteDay(data)
	if err != nil {
		return nil, nil, err
	}
	return p.keys, p.values, nil
}

// OpenAppend reads the keys of the day file at path and cuts the file to the
// end of its last valid block, so that new blocks can be appended. A missing
// file counts as empty. A header cut short by an interrupted first write
// empties the file, and so does a file without keys.
func OpenAppend(path string) (known map[string]int, size int64, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]int{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	// Ein Stromausfall kann den Kopf auf Nullen stehen lassen. Solche Dateien
	// sind leer. Fremde Dateien mit anderem Kopf bleiben unangetastet.
	if head := data[:min(len(data), 6)]; len(head) > 0 && allZero(head) {
		log.Printf("learnrec: %s: header zeroed, resetting the file", path)
		if err := os.Truncate(path, 0); err != nil {
			return nil, 0, err
		}
		return map[string]int{}, 0, nil
	}
	p, err := parseMinuteDay(data)
	switch {
	case errors.Is(err, errShortHeader):
		p = minuteParse{}
	case err != nil:
		return nil, 0, err
	}
	if len(p.keys) == 0 {
		p.end = 0
	}
	if p.end < len(data) {
		if err := os.Truncate(path, int64(p.end)); err != nil {
			return nil, 0, err
		}
	}
	known = make(map[string]int, len(p.keys))
	for i, key := range p.keys {
		known[key] = i
	}
	return known, int64(p.end), nil
}

// minuteParse is the valid part of a day file.
type minuteParse struct {
	keys   []string
	values map[string]*[1440]float32
	end    int // offset after the last valid block
}

func parseMinuteDay(data []byte) (minuteParse, error) {
	headerEnd, err := checkMinuteHeader(data)
	if err != nil {
		return minuteParse{}, err
	}
	p := minuteParse{values: map[string]*[1440]float32{}, end: headerEnd}
	for pos := headerEnd; pos < len(data); {
		body, next, ok := nextMinuteFrame(data, pos)
		if !ok || !p.apply(body) {
			break
		}
		pos = next
		p.end = pos
	}
	return p, nil
}

func checkMinuteHeader(data []byte) (int, error) {
	n := len(data)
	if n > len(minuteMagic) {
		n = len(minuteMagic)
	}
	if string(data[:n]) != minuteMagic[:n] {
		return 0, errBadHeader
	}
	if len(data) < 6 {
		return 0, errShortHeader
	}
	end := 6 + int(binary.LittleEndian.Uint16(data[4:6]))
	if len(data) < end {
		return 0, errShortHeader
	}
	return end, nil
}

// nextMinuteFrame returns the body of the frame at pos and the offset after
// it. It reports false for an incomplete frame or a CRC mismatch.
func nextMinuteFrame(data []byte, pos int) ([]byte, int, bool) {
	if len(data)-pos < 8 {
		return nil, 0, false
	}
	n := uint64(binary.LittleEndian.Uint32(data[pos:]))
	if uint64(len(data)-pos-8) < n {
		return nil, 0, false
	}
	bodyEnd := pos + 4 + int(n)
	body := data[pos+4 : bodyEnd]
	if crc32.ChecksumIEEE(body) != binary.LittleEndian.Uint32(data[bodyEnd:]) {
		return nil, 0, false
	}
	return body, bodyEnd + 4, true
}

// apply adds one block to p. It validates the whole block before it changes
// anything and reports false for an invalid block.
func (p *minuteParse) apply(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	switch body[0] {
	case minuteKeys:
		return p.applyKeys(body[1:])
	case minuteValues:
		return p.applyValues(body[1:])
	}
	return false
}

func (p *minuteParse) applyKeys(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}
	n := int(binary.LittleEndian.Uint16(payload))
	rest := payload[2:]
	names := make([]string, 0, n)
	fresh := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		if len(rest) < 2 {
			return false
		}
		l := int(binary.LittleEndian.Uint16(rest))
		rest = rest[2:]
		if l == 0 || len(rest) < l {
			return false
		}
		name := string(rest[:l])
		rest = rest[l:]
		if _, exists := p.values[name]; exists || fresh[name] {
			return false
		}
		fresh[name] = true
		names = append(names, name)
	}
	if len(rest) != 0 {
		return false
	}
	for _, name := range names {
		arr := new([minutesPerDay]float32)
		for i := range arr {
			arr[i] = minuteGap
		}
		p.keys = append(p.keys, name)
		p.values[name] = arr
	}
	return true
}

func (p *minuteParse) applyValues(payload []byte) bool {
	if len(payload) < 6 {
		return false
	}
	first := int(binary.LittleEndian.Uint16(payload))
	count := int(binary.LittleEndian.Uint16(payload[2:]))
	nkeys := int(binary.LittleEndian.Uint16(payload[4:]))
	rest := payload[6:]
	if count == 0 || first+count > minutesPerDay || nkeys > len(p.keys) {
		return false
	}
	if len(rest) != nkeys*count*4 {
		return false
	}
	for k := 0; k < nkeys; k++ {
		arr := p.values[p.keys[k]]
		for i := 0; i < count; i++ {
			arr[first+i] = math.Float32frombits(binary.LittleEndian.Uint32(rest))
			rest = rest[4:]
		}
	}
	return true
}

// appendFrame appends a block frame: u32 body length, body, u32 CRC32.
func appendFrame(dst, body []byte) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(body)))
	dst = append(dst, body...)
	return binary.LittleEndian.AppendUint32(dst, crc32.ChecksumIEEE(body))
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
