package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindReferencesUsesExactRecursiveStringMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`{"devices":[{"id":"node-1","topic":"state/node-1"},{"id":"node-10"}],"note":"prefix-node-1"}`), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.schema.json"), []byte(`{"type":"object"}`), 0640); err != nil {
		t.Fatal(err)
	}
	references, err := NewManager(dir).FindReferences(ReferenceQuery{DeviceID: "node-1", StateTopics: []string{"state/node-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 2 {
		t.Fatalf("references = %#v", references)
	}
	if references[0].Path != "$.devices[0].id" || references[1].Path != "$.devices[0].topic" {
		t.Fatalf("reference paths = %#v", references)
	}
	if references[0].URL != "/?panel=config&config=devices" {
		t.Fatalf("configuration URL = %q", references[0].URL)
	}
}
