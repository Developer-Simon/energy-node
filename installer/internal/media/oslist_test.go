package media

import (
	"os"
	"testing"
)

func loadTestList(t *testing.T) *OSList {
	t.Helper()
	data, err := os.ReadFile("testdata/os_list.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	list, err := ParseOSList(data)
	if err != nil {
		t.Fatalf("parse testdata: %v", err)
	}
	return list
}

func TestFindWalksIntoSubitems(t *testing.T) {
	list := loadTestList(t)
	img, ok := list.Find("Raspberry Pi OS (Legacy, 32-bit) Lite", "pi1-32bit")
	if !ok {
		t.Fatal("expected to find the legacy image for pi1-32bit")
	}
	if img.InitFormat != "systemd" {
		t.Errorf("InitFormat = %q, want %q", img.InitFormat, "systemd")
	}
	if img.DownloadSize != 520503180 {
		t.Errorf("DownloadSize = %d, want 520503180", img.DownloadSize)
	}
}

func TestFindNeedsTheDeviceTag(t *testing.T) {
	list := loadTestList(t)
	if _, ok := list.Find("Raspberry Pi OS Lite (64-bit)", "pi2-32bit"); ok {
		t.Error("expected no match for a tag the image does not list")
	}
}

func TestEveryDeviceFindsItsImage(t *testing.T) {
	list := loadTestList(t)
	for _, d := range Devices() {
		if _, ok := list.Find(d.ImageName, d.Tag); !ok {
			t.Errorf("device %s: no image for %q with tag %q", d.ID, d.ImageName, d.Tag)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseOSList([]byte("<html>")); err == nil {
		t.Error("expected an error for non-JSON input")
	}
}
