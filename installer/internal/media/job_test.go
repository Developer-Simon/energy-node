package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newTestWriter returns a writer whose OS list comes from testdata and whose
// drives and imager are fixed. The OS list URL is unreachable, so the stored copy is used.
func newTestWriter(t *testing.T, drives []Drive) *Writer {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", "os_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, osListFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return &Writer{
		Cache: &Cache{Dir: dir, OSListURL: "http://127.0.0.1:1/os_list.json"},
		Imager: func(context.Context) (Imager, bool) {
			return Imager{Kind: ImagerNative, Path: "/usr/bin/rpi-imager", Version: "2.0.1"}, true
		},
		Drives:     func(context.Context) ([]Drive, error) { return drives, nil },
		KnownHosts: filepath.Join(dir, "known_hosts"),
		KeyPath:    filepath.Join(dir, "installer_key"),
		WorkDir:    filepath.Join(dir, "jobs"),
	}
}

func jobTestSettings() Settings {
	return Settings{Hostname: "energy-node", User: "pi", AuthorizedKey: "ssh-ed25519 AAAA test"}
}

func TestStartRefusesADriveThatIsNoLongerListed(t *testing.T) {
	w := newTestWriter(t, []Drive{{Path: "/dev/sdb", Model: "Stick", Size: 32e9, Removable: true}})
	err := w.Start(context.Background(), WriteRequest{
		Device:   "pi4",
		Drive:    "/dev/sdc",
		Confirm:  "yes",
		Settings: jobTestSettings(),
	})
	if err != ErrDriveGone {
		t.Fatalf("Start error = %v, want ErrDriveGone", err)
	}
	if v := w.View(); v.State != JobIdle {
		t.Fatalf("state after refused start = %q, want %q", v.State, JobIdle)
	}
}

func TestAnInternalDriveNeedsItsModelName(t *testing.T) {
	internal := Drive{Path: "/dev/nvme0n1", Model: "Samsung SSD 870", Size: 500e9, Removable: false}
	w := newTestWriter(t, []Drive{internal})
	req := WriteRequest{Device: "pi4", Drive: internal.Path, Confirm: "yes", Settings: jobTestSettings()}

	if err := w.Start(context.Background(), req); err != ErrConfirm {
		t.Fatalf("Start with confirm yes = %v, want ErrConfirm", err)
	}

	req.Confirm = internal.Model
	if err := w.Start(context.Background(), req); err != nil {
		t.Fatalf("Start with model name = %v, want nil", err)
	}
	t.Cleanup(w.Cancel)
}

func TestASecondStartIsBusy(t *testing.T) {
	w := newTestWriter(t, []Drive{{Path: "/dev/sdb", Model: "Stick", Size: 32e9, Removable: true}})
	req := WriteRequest{Device: "pi4", Drive: "/dev/sdb", Confirm: "yes", Settings: jobTestSettings()}

	if err := w.Start(context.Background(), req); err != nil {
		t.Fatalf("first Start = %v, want nil", err)
	}
	t.Cleanup(w.Cancel)
	if err := w.Start(context.Background(), req); err != ErrBusy {
		t.Fatalf("second Start = %v, want ErrBusy", err)
	}
}
