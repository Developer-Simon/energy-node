package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storageusage"
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

// Trägt das Laufwerk die Daten, liegen laufende Daten und Backups nicht mehr auf
// der Systemplatte. Fällt es aus, kommen sie zurück.
func TestDemoStorageUsageDropsDataMovedToTheDrive(t *testing.T) {
	dir := t.TempDir()
	if err := storage.SaveConfig(dir, storage.Config{UUID: "u", UseData: true, UseBackups: true}); err != nil {
		t.Fatal(err)
	}
	mode := storage.ModeNone
	meter := demoStorageUsage(func() storage.Status { return storage.Status{Mode: mode} }, dir)
	wait := func(key string) storageusage.Parts {
		t.Helper()
		for i := 0; i < 200; i++ {
			if p, ok := meter.Get(key); ok {
				return p
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("no measurement")
		return storageusage.Parts{}
	}
	if p := wait("none"); p.Runtime == 0 || p.Backups == 0 || p.Install == 0 {
		t.Fatalf("without a drive all parts count, got %+v", p)
	}
	mode = storage.ModeDrive
	if p := wait("drive"); p.Runtime != 0 || p.Backups != 0 || p.Install == 0 {
		t.Fatalf("with the drive active only the installation stays, got %+v", p)
	}
}
