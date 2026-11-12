package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func parseFixture(t *testing.T, name, rootSource string) []Drive {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	drives, err := Parse(data, rootSource)
	if err != nil {
		t.Fatalf("Parse(%s) error: %v", name, err)
	}
	return drives
}

func knames(drives []Drive) []string {
	names := []string{}
	for _, d := range drives {
		names = append(names, d.Kname)
	}
	return names
}

func TestParseHidesSystemDrive(t *testing.T) {
	cases := []struct {
		fixture    string
		rootSource string
		want       []string
	}{
		{"lsblk-sd-system.json", "/dev/mmcblk0p2", []string{"sda1"}},
		{"lsblk-nvme-system.json", "/dev/nvme0n1p2", []string{"sdb1"}},
	}
	for _, tc := range cases {
		drives := parseFixture(t, tc.fixture, tc.rootSource)
		if got := knames(drives); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: drives = %v, want %v", tc.fixture, got, tc.want)
		}
		for _, d := range drives {
			if strings.HasPrefix(d.Kname, "mmcblk") || strings.HasPrefix(d.Kname, "nvme") {
				t.Fatalf("%s: system drive %s offered", tc.fixture, d.Kname)
			}
		}
	}
}

func TestParseHidesMountedAndLoop(t *testing.T) {
	drives := parseFixture(t, "lsblk-mounted-loop.json", "")
	if got, want := knames(drives), []string{"sdd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drives = %v, want %v (loop, ram, zram, mounted and partly mounted disk hidden)", got, want)
	}
}

func TestParseEmptyExt4IsReady(t *testing.T) {
	drives := parseFixture(t, "lsblk-ext4-empty.json", "")
	if len(drives) != 1 {
		t.Fatalf("drives = %d, want 1", len(drives))
	}
	d := drives[0]
	if !d.Ready || d.NeedsFormat {
		t.Fatalf("Ready = %v, NeedsFormat = %v, want ready ext4", d.Ready, d.NeedsFormat)
	}
	if d.FSType != "ext4" || d.Contents != "" {
		t.Fatalf("FSType = %q, Contents = %q, want ext4 and no contents key", d.FSType, d.Contents)
	}
	if d.Used != 1245184 || d.Transport != "usb" || d.Name != "External SSD" {
		t.Fatalf("Used = %d, Transport = %q, Name = %q", d.Used, d.Transport, d.Name)
	}
}

func TestParseExfatNeedsFormat(t *testing.T) {
	drives := parseFixture(t, "lsblk-exfat.json", "")
	if len(drives) != 1 {
		t.Fatalf("drives = %d, want 1", len(drives))
	}
	d := drives[0]
	if d.Ready || !d.NeedsFormat {
		t.Fatalf("Ready = %v, NeedsFormat = %v, want needs format", d.Ready, d.NeedsFormat)
	}
	if d.Contents != ContentsForeign {
		t.Fatalf("Contents = %q, want %q", d.Contents, ContentsForeign)
	}
	if d.FSType != "exfat" || d.UUID != "1234-ABCD" {
		t.Fatalf("FSType = %q, UUID = %q", d.FSType, d.UUID)
	}
}

func TestParseHandlesMissingFields(t *testing.T) {
	drives := parseFixture(t, "lsblk-missing-fields.json", "")
	if got, want := knames(drives), []string{"sdb", "sdc1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drives = %v, want %v", got, want)
	}
	blank := drives[0]
	if blank.Name != "sdb" || blank.Size != 0 || blank.Contents != ContentsNone || !blank.NeedsFormat {
		t.Fatalf("blank disk = %+v, want name sdb, size 0, contents %q, needs format", blank, ContentsNone)
	}
	part := drives[1]
	if part.Name != "sdc1" || part.Size != 2000000000 || part.Transport != "usb" || !part.Ready {
		t.Fatalf("partition = %+v, want name fallback sdc1, size from string, usb, ready", part)
	}
	if part.UUID != "" || part.Used != 0 {
		t.Fatalf("null fields = UUID %q, Used %d, want empty and zero", part.UUID, part.Used)
	}
}
