package learnrec

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const testMinuteDay = "2026-11-21"

var testGap = float32(math.NaN())

// appendMinuteBlock appends one block to path the way the recorder does.
func appendMinuteBlock(t *testing.T, path string, b MinuteBlock, known map[string]int) int64 {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n, err := AppendBlock(f, testMinuteDay, b, known)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func readMinuteFile(t *testing.T, path string) ([]string, map[string]*[1440]float32) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, values, err := ReadDay(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	return keys, values
}

func requireMinuteValue(t *testing.T, values map[string]*[1440]float32, key string, minute int, want float32) {
	t.Helper()
	arr, ok := values[key]
	if !ok {
		t.Fatalf("key %q missing", key)
	}
	got := arr[minute]
	if math.IsNaN(float64(want)) {
		if !math.IsNaN(float64(got)) {
			t.Fatalf("%s at minute %d: got %v, want gap", key, minute, got)
		}
		return
	}
	if got != want {
		t.Fatalf("%s at minute %d: got %v, want %v", key, minute, got, want)
	}
}

func TestBlockRoundTripWithNewKeysMidDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	known := map[string]int{}
	appendMinuteBlock(t, path, MinuteBlock{First: 0, Keys: []string{"e:a"}, Values: [][]float32{{1, 2, 3}}}, known)
	appendMinuteBlock(t, path, MinuteBlock{First: 3, Keys: []string{"e:a", "t:outdoor"}, Values: [][]float32{{4, 5}, {-2, -3}}}, known)
	// b:grid starts at minute 720, so its minutes before that stay gaps.
	appendMinuteBlock(t, path, MinuteBlock{First: 720, Keys: []string{"b:grid"}, Values: [][]float32{{7}}}, known)

	keys, values := readMinuteFile(t, path)
	if want := []string{"e:a", "t:outdoor", "b:grid"}; fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("keys: got %v, want %v", keys, want)
	}
	if known["t:outdoor"] != 1 || known["b:grid"] != 2 {
		t.Fatalf("known: got %v", known)
	}
	requireMinuteValue(t, values, "e:a", 0, 1)
	requireMinuteValue(t, values, "e:a", 4, 5)
	requireMinuteValue(t, values, "e:a", 5, testGap)
	requireMinuteValue(t, values, "e:a", 719, testGap)
	requireMinuteValue(t, values, "t:outdoor", 2, testGap)
	requireMinuteValue(t, values, "t:outdoor", 3, -2)
	requireMinuteValue(t, values, "t:outdoor", 4, -3)
	requireMinuteValue(t, values, "b:grid", 719, testGap)
	requireMinuteValue(t, values, "b:grid", 720, 7)
	requireMinuteValue(t, values, "b:grid", 721, testGap)
}

func TestBlockTruncatedTailIsDroppedAndAppendResumes(t *testing.T) {
	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	known := map[string]int{}
	appendMinuteBlock(t, path, MinuteBlock{First: 0, Keys: []string{"e:a"}, Values: [][]float32{{1, 2}}}, known)
	appendMinuteBlock(t, path, MinuteBlock{First: 3, Keys: []string{"t:outdoor"}, Values: [][]float32{{-2}}}, known)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sizeAfterTwo := fi.Size()
	// The last block is a value block only, so cutting into it leaves the
	// key table of the first two blocks intact.
	appendMinuteBlock(t, path, MinuteBlock{First: 10, Keys: []string{"e:a", "t:outdoor"}, Values: [][]float32{{9}, {8}}}, known)

	fi, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, fi.Size()-7); err != nil {
		t.Fatal(err)
	}

	keys, values := readMinuteFile(t, path)
	if want := []string{"e:a", "t:outdoor"}; fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("keys after cut: got %v, want %v", keys, want)
	}
	requireMinuteValue(t, values, "e:a", 1, 2)
	requireMinuteValue(t, values, "t:outdoor", 3, -2)
	requireMinuteValue(t, values, "e:a", 10, testGap)

	known, size, err := OpenAppend(path)
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if size != sizeAfterTwo {
		t.Fatalf("size after OpenAppend: got %d, want %d", size, sizeAfterTwo)
	}
	if fi, _ := os.Stat(path); fi.Size() != sizeAfterTwo {
		t.Fatalf("file size on disk: got %d, want %d", fi.Size(), sizeAfterTwo)
	}
	if len(known) != 2 || known["e:a"] != 0 || known["t:outdoor"] != 1 {
		t.Fatalf("known: got %v", known)
	}

	appendMinuteBlock(t, path, MinuteBlock{First: 20, Keys: []string{"e:a"}, Values: [][]float32{{5}}}, known)
	keys, values = readMinuteFile(t, path)
	if want := []string{"e:a", "t:outdoor"}; fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("keys after resume: got %v, want %v", keys, want)
	}
	requireMinuteValue(t, values, "e:a", 1, 2)
	requireMinuteValue(t, values, "e:a", 20, 5)
}

func TestBlockFlippedCRCBitDropsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	known := map[string]int{}
	appendMinuteBlock(t, path, MinuteBlock{First: 0, Keys: []string{"e:a"}, Values: [][]float32{{1, 2}}}, known)
	appendMinuteBlock(t, path, MinuteBlock{First: 2, Keys: []string{"e:a"}, Values: [][]float32{{3}}}, known)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0x01 // last byte is the CRC of the last block
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, values := readMinuteFile(t, path)
	requireMinuteValue(t, values, "e:a", 1, 2)
	requireMinuteValue(t, values, "e:a", 2, testGap)
}

func TestBlockFullDayWith64KeysStaysUnder400KiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	known := map[string]int{}
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = fmt.Sprintf("e:dev%02d", i)
	}
	for block := 0; block < 288; block++ {
		values := make([][]float32, len(keys))
		for k := range keys {
			values[k] = []float32{float32(k), float32(k) + 0.5, 1, 2, 3}
		}
		appendMinuteBlock(t, path, MinuteBlock{First: block * 5, Keys: keys, Values: values}, known)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() >= 400<<10 {
		t.Fatalf("file size %d bytes, want under 400 KiB", fi.Size())
	}
	_, values := readMinuteFile(t, path)
	requireMinuteValue(t, values, "e:dev63", 1439, 3)
	requireMinuteValue(t, values, "e:dev05", 5, 5)
}

func TestBlockRejectsInvalidInput(t *testing.T) {
	tests := map[string]MinuteBlock{
		"no keys":         {First: 0},
		"row count":       {First: 0, Keys: []string{"e:a"}, Values: nil},
		"row length":      {First: 0, Keys: []string{"e:a", "e:b"}, Values: [][]float32{{1}, {1, 2}}},
		"outside the day": {First: 1439, Keys: []string{"e:a"}, Values: [][]float32{{1, 2}}},
		"duplicate key":   {First: 0, Keys: []string{"e:a", "e:a"}, Values: [][]float32{{1}, {2}}},
		"empty key":       {First: 0, Keys: []string{""}, Values: [][]float32{{1}}},
	}
	for name, b := range tests {
		var buf bytes.Buffer
		if _, err := AppendBlock(&buf, testMinuteDay, b, map[string]int{}); err == nil {
			t.Errorf("%s: want error", name)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: wrote %d bytes before failing", name, buf.Len())
		}
	}
}

func TestReadDayRejectsBadHeader(t *testing.T) {
	if _, _, err := ReadDay(bytes.NewReader(nil)); err == nil {
		t.Error("empty input: want error")
	}
	if _, _, err := ReadDay(bytes.NewReader([]byte("XXXX\x00\x00"))); err == nil {
		t.Error("wrong magic: want error")
	}

	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	foreign := []byte("XXXX\x00\x00 not a minute file")
	if err := os.WriteFile(path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenAppend(path); err == nil {
		t.Fatal("OpenAppend on a foreign file: want error")
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, foreign) {
		t.Fatalf("foreign file was changed: %q", data)
	}
}

func TestOpenAppendRecoversFromPartialHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), testMinuteDay+".enm")
	if err := os.WriteFile(path, []byte("ENM"), 0o644); err != nil {
		t.Fatal(err)
	}
	known, size, err := OpenAppend(path)
	if err != nil {
		t.Fatalf("OpenAppend: %v", err)
	}
	if size != 0 || len(known) != 0 {
		t.Fatalf("got size %d, known %v, want empty", size, known)
	}
	if fi, _ := os.Stat(path); fi.Size() != 0 {
		t.Fatalf("file size %d, want 0", fi.Size())
	}
	appendMinuteBlock(t, path, MinuteBlock{First: 4, Keys: []string{"e:a"}, Values: [][]float32{{1}}}, known)
	_, values := readMinuteFile(t, path)
	requireMinuteValue(t, values, "e:a", 4, 1)
}
