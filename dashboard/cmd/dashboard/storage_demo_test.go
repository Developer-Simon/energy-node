package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
)

func TestStoragePathUsesDemoDirOnlyWhenSet(t *testing.T) {
	if got := storagePath("", "mount", "/mnt/energy-node-data"); got != "/mnt/energy-node-data" {
		t.Fatalf("without demo dir: %q", got)
	}
	if got := storagePath("/demo", "mount", "/mnt/energy-node-data"); got != "/demo/mount" {
		t.Fatalf("with demo dir: %q", got)
	}
}

func TestDemoDriveListerReadsTheFileEachTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drives.json")
	lister := demoDriveLister{Path: path}

	drives, err := lister.Drives(context.Background())
	if err != nil || len(drives) != 0 {
		t.Fatalf("missing file: %v, %v", drives, err)
	}

	body := `[{"kname":"sdb","name":"SanDisk Ultra","fstype":"exfat","size":64424509440,"needs_format":true,"contents":"storage.contents.foreign"}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	drives, err = lister.Drives(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := storage.Drive{Kname: "sdb", Name: "SanDisk Ultra", FSType: "exfat", Size: 64424509440, NeedsFormat: true, Contents: "storage.contents.foreign"}
	if len(drives) != 1 || drives[0] != want {
		t.Fatalf("drives = %+v, want %+v", drives, want)
	}
}
