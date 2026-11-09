package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

// cryptHeaderLen ist Magie, Salz und Iterationen vor dem ersten Block.
const cryptHeaderLen = 6 + 16 + 4

// encryptBytes verschlüsselt plain mit password in einen Puffer.
func encryptBytes(t *testing.T, plain []byte, password string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewEncryptWriter(&buf, password)
	if err != nil {
		t.Fatalf("NewEncryptWriter: %v", err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

// decryptBytes liest den ganzen Strom mit password.
func decryptBytes(stream []byte, password string) ([]byte, error) {
	r, err := NewDecryptReader(bytes.NewReader(stream), password)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// splitFrames trennt den Strom in Kopf und Blöcke. Jeder Block behält sein Längenfeld.
func splitFrames(t *testing.T, stream []byte) ([]byte, [][]byte) {
	t.Helper()
	if len(stream) < cryptHeaderLen {
		t.Fatalf("Strom ist kürzer als der Kopf: %d Byte", len(stream))
	}
	head := stream[:cryptHeaderLen]
	rest := stream[cryptHeaderLen:]
	var frames [][]byte
	for len(rest) > 0 {
		n := int(binary.BigEndian.Uint32(rest[:4]))
		frames = append(frames, rest[:4+n])
		rest = rest[4+n:]
	}
	return head, frames
}

// joinFrames setzt Kopf und Blöcke wieder zusammen.
func joinFrames(head []byte, frames [][]byte) []byte {
	out := append([]byte(nil), head...)
	for _, f := range frames {
		out = append(out, f...)
	}
	return out
}

// bigPayload liefert 3,5 MiB Zufall, also vier Blöcke.
func bigPayload(t *testing.T) []byte {
	t.Helper()
	plain := make([]byte, 3<<20+512<<10)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestEncryptRoundTripOverSeveralBlocks(t *testing.T) {
	plain := bigPayload(t)
	stream := encryptBytes(t, plain, "geheim")
	if !IsEncrypted(stream[:6]) {
		t.Fatal("verschlüsselter Strom beginnt nicht mit der Magie")
	}
	if _, frames := splitFrames(t, stream); len(frames) < 4 {
		t.Fatalf("Blöcke = %d, want mindestens 4", len(frames))
	}
	got, err := decryptBytes(stream, "geheim")
	if err != nil {
		t.Fatalf("decryptBytes: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Klartext weicht ab: %d Byte, want %d", len(got), len(plain))
	}
}

func TestEncryptEmptyPayloadRoundTrip(t *testing.T) {
	stream := encryptBytes(t, nil, "geheim")
	got, err := decryptBytes(stream, "geheim")
	if err != nil {
		t.Fatalf("decryptBytes: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Klartext = %d Byte, want 0", len(got))
	}
}

func TestNewEncryptWriterRejectsEmptyPassword(t *testing.T) {
	var buf bytes.Buffer
	if _, err := NewEncryptWriter(&buf, ""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("NewEncryptWriter(leer) = %v, want ErrPasswordRequired", err)
	}
}

func TestWrongPasswordFailsOnFirstBlock(t *testing.T) {
	stream := encryptBytes(t, bigPayload(t), "geheim")
	_, err := decryptBytes(stream, "falsch")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestTruncatedAfterFullBlockFails(t *testing.T) {
	stream := encryptBytes(t, bigPayload(t), "geheim")
	head, frames := splitFrames(t, stream)
	cut := joinFrames(head, frames[:len(frames)-1])
	_, err := decryptBytes(cut, "geheim")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, ein fehlender letzter Block darf kein stilles EOF sein", err)
	}
}

func TestReorderedBlocksFail(t *testing.T) {
	stream := encryptBytes(t, bigPayload(t), "geheim")
	head, frames := splitFrames(t, stream)
	frames[1], frames[2] = frames[2], frames[1]
	_, err := decryptBytes(joinFrames(head, frames), "geheim")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestFlippedByteFails(t *testing.T) {
	stream := encryptBytes(t, bigPayload(t), "geheim")
	head, frames := splitFrames(t, stream)
	frames[1][4+10] ^= 0x01
	_, err := decryptBytes(joinFrames(head, frames), "geheim")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestEncryptedArchiveRoundTrip(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	src := testSources(t)
	var buf bytes.Buffer
	opts := CreateOptions{
		Areas:       allAreas(),
		Trigger:     "manual",
		NodeVersion: "0.11.7",
		Password:    "geheim",
	}
	created, err := Create(context.Background(), src, opts, &buf)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !IsEncrypted(buf.Bytes()) {
		t.Fatal("Archiv mit Passwort ist nicht verschlüsselt")
	}

	staging := filepath.Join(t.TempDir(), "staging")
	got, err := ExtractToStaging(context.Background(), &buf, "geheim", staging)
	if err != nil {
		t.Fatalf("ExtractToStaging: %v", err)
	}
	if len(got.Files) != len(created.Files) {
		t.Errorf("Dateien = %d, want %d", len(got.Files), len(created.Files))
	}
}

func TestEncryptedArchiveMissingPassword(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	var buf bytes.Buffer
	opts := CreateOptions{Areas: allAreas(), NodeVersion: "0.11.7", Password: "geheim"}
	if _, err := Create(context.Background(), testSources(t), opts, &buf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	staging := filepath.Join(t.TempDir(), "staging")
	_, err := ExtractToStaging(context.Background(), &buf, "", staging)
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("err = %v, want ErrPasswordRequired", err)
	}
}

func TestEncryptedArchiveWrongPassword(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	var buf bytes.Buffer
	opts := CreateOptions{Areas: allAreas(), NodeVersion: "0.11.7", Password: "geheim"}
	if _, err := Create(context.Background(), testSources(t), opts, &buf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	staging := filepath.Join(t.TempDir(), "staging")
	_, err := ExtractToStaging(context.Background(), &buf, "falsch", staging)
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestPasswordIgnoredForPlainArchive(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	var buf bytes.Buffer
	opts := CreateOptions{Areas: allAreas(), NodeVersion: "0.11.7"}
	if _, err := Create(context.Background(), testSources(t), opts, &buf); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if IsEncrypted(buf.Bytes()) {
		t.Fatal("Archiv ohne Passwort ist verschlüsselt")
	}
	staging := filepath.Join(t.TempDir(), "staging")
	if _, err := ExtractToStaging(context.Background(), &buf, "egal", staging); err != nil {
		t.Fatalf("ExtractToStaging mit Passwort für ungeschütztes Archiv: %v", err)
	}
}
