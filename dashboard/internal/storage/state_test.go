package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVolumeOKNeedsMatchingMarker(t *testing.T) {
	const nodeID = "0123456789abcdef0123456789abcdef"

	empty := t.TempDir()
	if VolumeOK(empty, nodeID) {
		t.Fatal("VolumeOK on an empty folder = true, want false")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, markerName), []byte("ffffffffffffffffffffffffffffffff\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if VolumeOK(other, nodeID) {
		t.Fatal("VolumeOK with a foreign marker = true, want false")
	}

	own := t.TempDir()
	if err := os.WriteFile(filepath.Join(own, markerName), []byte(nodeID+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !VolumeOK(own, nodeID) {
		t.Fatal("VolumeOK with the matching marker = false, want true")
	}
	if VolumeOK(own, "") {
		t.Fatal("VolumeOK with an empty node id = true, want false")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig on empty dir: %v", err)
	}
	if !reflect.DeepEqual(loaded, Config{}) {
		t.Fatalf("LoadConfig on empty dir = %+v, want zero value", loaded)
	}

	want := Config{
		UUID:       "3f2a9c1e-0000-4000-8000-000000000001",
		Name:       "Datenlaufwerk",
		Fallback:   FallbackRAM,
		UseData:    true,
		UseBackups: false,
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "storage.json")); err != nil {
		t.Fatalf("storage.json not written: %v", err)
	}

	got, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}
