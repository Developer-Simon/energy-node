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

func parseInline(t *testing.T, doc, rootSource string) []Drive {
	t.Helper()
	drives, err := Parse([]byte(doc), rootSource)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	return drives
}

func TestParseGroupsPartitionsUnderDisk(t *testing.T) {
	doc := `{"blockdevices":[{"name":"sda","kname":"sda","path":"/dev/sda","size":32000000000,
		"model":"SanDisk Ultra","tran":"usb","type":"disk","mountpoint":null,"children":[
		{"name":"sda1","kname":"sda1","path":"/dev/sda1","size":1000000000,"fstype":"iso9660",
			"label":"INSTALL","model":null,"tran":null,"type":"part","mountpoint":null,"pkname":"sda"},
		{"name":"sda2","kname":"sda2","path":"/dev/sda2","size":500000000,"fstype":"vfat",
			"label":"EFI","model":null,"tran":null,"type":"part","mountpoint":null,"pkname":"sda"}]}]}`
	drives := parseInline(t, doc, "")
	if got, want := knames(drives), []string{"sda", "sda1", "sda2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drives = %v, want %v", got, want)
	}
	whole := drives[0]
	if !whole.Whole || !whole.NeedsFormat || whole.Contents != ContentsWholeDisk {
		t.Fatalf("whole = %+v, want whole, needs format, contents %q", whole, ContentsWholeDisk)
	}
	if whole.Size != 32000000000 || whole.DiskSize != whole.Size {
		t.Fatalf("whole size = %d, disk size = %d, want 32000000000 for both", whole.Size, whole.DiskSize)
	}
	if whole.Disk != "sda" || whole.UUID != "" || whole.FSType != "" || whole.Used != 0 {
		t.Fatalf("whole = %+v, want disk sda and empty UUID, FSType and Used", whole)
	}
	for i, label := range []string{"INSTALL", "EFI"} {
		part := drives[i+1]
		if part.Disk != "sda" || part.DiskSize != 32000000000 || part.Label != label {
			t.Fatalf("partition %s: Disk = %q, DiskSize = %d, Label = %q", part.Kname, part.Disk, part.DiskSize, part.Label)
		}
		if part.Name != "SanDisk Ultra" || part.Whole {
			t.Fatalf("partition %s: Name = %q, Whole = %v, want SanDisk Ultra and not whole", part.Kname, part.Name, part.Whole)
		}
	}
}

func TestParseSinglePartitionHasNoWholeEntry(t *testing.T) {
	doc := `{"blockdevices":[{"name":"sdb","kname":"sdb","path":"/dev/sdb","size":8000000000,
		"model":"Stick","tran":"usb","type":"disk","mountpoint":null,"children":[
		{"name":"sdb1","kname":"sdb1","path":"/dev/sdb1","size":7000000000,"fstype":"ext4",
			"model":null,"tran":null,"type":"part","mountpoint":null,"pkname":"sdb"}]}]}`
	drives := parseInline(t, doc, "")
	if got, want := knames(drives), []string{"sdb1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drives = %v, want %v", got, want)
	}
	if drives[0].Whole || drives[0].Disk != "sdb" {
		t.Fatalf("partition = %+v, want not whole, disk sdb", drives[0])
	}
}

func TestParseBlankDiskKeepsOwnGroup(t *testing.T) {
	doc := `{"blockdevices":[{"name":"sdc","kname":"sdc","path":"/dev/sdc","size":4000000000,
		"model":"Blank","tran":"usb","type":"disk","mountpoint":null}]}`
	drives := parseInline(t, doc, "")
	if got, want := knames(drives), []string{"sdc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drives = %v, want %v", got, want)
	}
	if drives[0].Disk != "sdc" || drives[0].Whole {
		t.Fatalf("blank disk = %+v, want disk sdc and not whole", drives[0])
	}
}

func TestParseHidesWholeGroupWhenPartitionMounted(t *testing.T) {
	doc := `{"blockdevices":[{"name":"sda","kname":"sda","path":"/dev/sda","size":32000000000,
		"model":"SanDisk Ultra","tran":"usb","type":"disk","mountpoint":null,"children":[
		{"name":"sda1","kname":"sda1","path":"/dev/sda1","size":1000000000,"fstype":"ext4",
			"model":null,"tran":null,"type":"part","mountpoint":"/media/usb","pkname":"sda"},
		{"name":"sda2","kname":"sda2","path":"/dev/sda2","size":500000000,"fstype":"vfat",
			"model":null,"tran":null,"type":"part","mountpoint":null,"pkname":"sda"}]}]}`
	drives := parseInline(t, doc, "")
	if len(drives) != 0 {
		t.Fatalf("drives = %v, want none (disk with a mounted partition hidden entirely)", knames(drives))
	}
}
