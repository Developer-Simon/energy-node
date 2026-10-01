package bundle_test

import (
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

func TestDiffManifestFindsChangedNewAndRemovedFiles(t *testing.T) {
	old := &bundle.Manifest{Files: map[string]string{
		"dashboard/energy-node-dashboard": "aaa",
		"wheels/old-only.whl":             "bbb",
		"manifest.json":                   "ccc",
	}}
	newM := &bundle.Manifest{Files: map[string]string{
		"dashboard/energy-node-dashboard": "aaa2", // changed
		"manifest.json":                   "ccc",  // unchanged
		"wheels/new-only.whl":             "ddd",  // new
	}}

	changed, removed := bundle.DiffManifest(old, newM)
	if !reflect.DeepEqual(changed, []string{"dashboard/energy-node-dashboard", "wheels/new-only.whl"}) {
		t.Fatalf("changed = %v", changed)
	}
	if !reflect.DeepEqual(removed, []string{"wheels/old-only.whl"}) {
		t.Fatalf("removed = %v", removed)
	}
}

func TestDiffManifestOfIdenticalManifestsIsEmpty(t *testing.T) {
	m := &bundle.Manifest{Files: map[string]string{"a": "1", "b": "2"}}
	changed, removed := bundle.DiffManifest(m, m)
	if len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed = %v, removed = %v, want both empty", changed, removed)
	}
}

func TestDiffManifestAgainstAnEmptyOldTreatsEveryFileAsChanged(t *testing.T) {
	old := &bundle.Manifest{Files: map[string]string{}}
	newM := &bundle.Manifest{Files: map[string]string{"a": "1", "b": "2"}}
	changed, removed := bundle.DiffManifest(old, newM)
	if !reflect.DeepEqual(changed, []string{"a", "b"}) {
		t.Fatalf("changed = %v", changed)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}
}
