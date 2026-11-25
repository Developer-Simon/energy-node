package media

import (
	"os"
	"testing"
)

func loadWindowsDisks(t *testing.T, name string) []Drive {
	t.Helper()
	data, err := os.ReadFile("testdata/windows/" + name)
	if err != nil {
		t.Fatal(err)
	}
	drives, err := ParseWindowsDisks(data)
	if err != nil {
		t.Fatalf("ParseWindowsDisks(%s): %v", name, err)
	}
	return drives
}

func mustParseWindowsDisks(t *testing.T, data string) []Drive {
	t.Helper()
	drives, err := ParseWindowsDisks([]byte(data))
	if err != nil {
		t.Fatalf("ParseWindowsDisks: %v", err)
	}
	return drives
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWindowsDisksHideTheSystemDisk(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [{"Number": 0, "FriendlyName": "CT2000P3SSD8", "Size": 2000398934016, "BusType": "NVMe", "IsSystem": true}],
	  "partitions": [],
	  "volumes": [],
	  "systemDrive": "C:"
	}`)
	if len(drives) != 0 {
		t.Fatalf("got %d drives, want none: %v", len(drives), drivePaths(drives))
	}
}

func TestWindowsDisksHideTheSystemDriveLetterDisk(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [
	    {"Number": 0, "FriendlyName": "SYS", "Size": 500000000000, "BusType": "NVMe", "IsBoot": false, "IsSystem": false},
	    {"Number": 1, "FriendlyName": "Stick", "Size": 8000000000, "BusType": "USB", "IsBoot": false, "IsSystem": false}
	  ],
	  "partitions": [
	    {"DiskNumber": 0, "PartitionNumber": 1, "DriveLetter": "C", "Size": 400000000000, "AccessPaths": []},
	    {"DiskNumber": 1, "PartitionNumber": 1, "DriveLetter": "", "Size": 7000000000, "AccessPaths": []}
	  ],
	  "volumes": [],
	  "systemDrive": "c:"
	}`)
	if got, want := drivePaths(drives), []string{`\\.\PhysicalDrive1`}; !equalStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestWindowsDisksUsbStickIsRemovable(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [{"Number": 2, "FriendlyName": "  Kingston DataTraveler 3.0  ", "Size": 15931539456, "BusType": "USB", "IsBoot": false, "IsSystem": false}],
	  "partitions": [{"DiskNumber": 2, "PartitionNumber": 1, "DriveLetter": "E", "Size": 15931000000, "AccessPaths": ["E:\\", "\\\\?\\Volume{abc}\\"]}],
	  "volumes": [{"Path": "\\\\?\\Volume{abc}\\", "FileSystemLabel": "WERKSTATT", "FileSystem": "FAT32", "Size": 1000, "SizeRemaining": 400}],
	  "systemDrive": "C:"
	}`)
	if len(drives) != 1 {
		t.Fatalf("got %d drives, want 1", len(drives))
	}
	d := drives[0]
	if d.Path != `\\.\PhysicalDrive2` {
		t.Errorf("Path = %q", d.Path)
	}
	if d.Model != "Kingston DataTraveler 3.0" {
		t.Errorf("Model = %q", d.Model)
	}
	if d.Size != 15931539456 {
		t.Errorf("Size = %d", d.Size)
	}
	if d.Transport != "usb" || !d.Removable {
		t.Errorf("Transport = %q, Removable = %v", d.Transport, d.Removable)
	}
	if len(d.Partitions) != 1 {
		t.Fatalf("got %d partitions, want 1", len(d.Partitions))
	}
	want := Partition{Name: "E:", Label: "WERKSTATT", FSType: "FAT32", Used: 600}
	if d.Partitions[0] != want {
		t.Errorf("partition = %+v, want %+v", d.Partitions[0], want)
	}
}

func TestWindowsDisksSdReaderIsRemovable(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [{"Number": 4, "FriendlyName": "SD Reader", "Size": 64000000000, "BusType": "SD", "IsBoot": false, "IsSystem": false}],
	  "partitions": [],
	  "volumes": [],
	  "systemDrive": "C:"
	}`)
	if len(drives) != 1 {
		t.Fatalf("got %d drives, want 1", len(drives))
	}
	if drives[0].Transport != "mmc" || !drives[0].Removable {
		t.Errorf("Transport = %q, Removable = %v", drives[0].Transport, drives[0].Removable)
	}
}

func TestWindowsDisksSataIsInternal(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [{"Number": 1, "FriendlyName": "WD", "Size": 3000592982016, "BusType": "SATA", "IsBoot": false, "IsSystem": false}],
	  "partitions": [],
	  "volumes": [],
	  "systemDrive": "C:"
	}`)
	if len(drives) != 1 {
		t.Fatalf("got %d drives, want 1", len(drives))
	}
	if drives[0].Transport != "sata" || drives[0].Removable {
		t.Errorf("Transport = %q, Removable = %v", drives[0].Transport, drives[0].Removable)
	}
}

func TestWindowsDisksAcceptsSingleObjectsAndNumericBusType(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": {"Number": 2, "FriendlyName": "", "Size": 8000000000, "BusType": 7, "IsBoot": false, "IsSystem": false},
	  "partitions": {"DiskNumber": 2, "PartitionNumber": 1, "DriveLetter": "", "Size": 7900000000, "AccessPaths": null},
	  "volumes": null,
	  "systemDrive": "C:"
	}`)
	if len(drives) != 1 {
		t.Fatalf("got %d drives, want 1", len(drives))
	}
	d := drives[0]
	if d.Model != `PhysicalDrive2` {
		t.Errorf("Model = %q, want the fallback name", d.Model)
	}
	if d.Transport != "usb" || !d.Removable {
		t.Errorf("Transport = %q, Removable = %v", d.Transport, d.Removable)
	}
	if len(d.Partitions) != 1 || d.Partitions[0].Name != "Partition 1" {
		t.Errorf("partitions = %+v", d.Partitions)
	}
}

func TestWindowsDisksRecordedOutput(t *testing.T) {
	drives := loadWindowsDisks(t, "disks-usb.json")
	if got, want := drivePaths(drives), []string{
		`\\.\PhysicalDrive0`, `\\.\PhysicalDrive1`, `\\.\PhysicalDrive2`,
		`\\.\PhysicalDrive4`, `\\.\PhysicalDrive5`,
	}; !equalStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	var removable []string
	for _, d := range drives {
		if d.Removable {
			removable = append(removable, d.Path)
		}
	}
	if want := []string{`\\.\PhysicalDrive4`, `\\.\PhysicalDrive5`}; !equalStrings(removable, want) {
		t.Fatalf("removable = %v, want %v", removable, want)
	}
	reader := drives[3]
	if reader.Removable != true || reader.Transport != "usb" {
		t.Errorf("reader: Transport = %q, Removable = %v", reader.Transport, reader.Removable)
	}
	if len(reader.Partitions) != 2 {
		t.Fatalf("reader partitions = %+v", reader.Partitions)
	}
	boot := reader.Partitions[0]
	if boot.Name != "E:" || boot.Label != "bootfs" || boot.FSType != "FAT32" || boot.Used != 78434816 {
		t.Errorf("boot partition = %+v", boot)
	}
	stick := drives[4]
	if stick.Model != "TDK LoR Trans-It Drive" || stick.Size != 1992294400 {
		t.Errorf("stick = %q, %d", stick.Model, stick.Size)
	}
}

func TestWindowsDisksHideTheEmptyReader(t *testing.T) {
	drives := loadWindowsDisks(t, "disks-empty-reader.json")
	if got, want := drivePaths(drives), []string{
		`\\.\PhysicalDrive0`, `\\.\PhysicalDrive1`, `\\.\PhysicalDrive2`, `\\.\PhysicalDrive5`,
	}; !equalStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestWindowsDisksWithoutRemovableMedia(t *testing.T) {
	drives := loadWindowsDisks(t, "disks-none.json")
	if got, want := drivePaths(drives), []string{
		`\\.\PhysicalDrive0`, `\\.\PhysicalDrive1`, `\\.\PhysicalDrive2`,
	}; !equalStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if got := Visible(drives, false); len(got) != 0 {
		t.Fatalf("removable drives = %v, want none", drivePaths(got))
	}
}

func TestWindowsDisksWithoutSystemDriveIsAnError(t *testing.T) {
	for name, data := range map[string]string{
		"empty":   `{"disks": [{"Number": 1, "Size": 8000000000, "BusType": "USB", "IsBoot": false, "IsSystem": false}], "partitions": [], "volumes": [], "systemDrive": ""}`,
		"missing": `{"disks": [{"Number": 1, "Size": 8000000000, "BusType": "USB", "IsBoot": false, "IsSystem": false}], "partitions": [], "volumes": []}`,
	} {
		drives, err := ParseWindowsDisks([]byte(data))
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(drives) != 0 {
			t.Errorf("%s: got drives %v", name, drivePaths(drives))
		}
	}
}

func TestWindowsDisksHideDisksWithUnknownBootFlags(t *testing.T) {
	drives := mustParseWindowsDisks(t, `{
	  "disks": [
	    {"Number": 1, "FriendlyName": "NullBoot", "Size": 8000000000, "BusType": "USB", "IsBoot": null, "IsSystem": false},
	    {"Number": 2, "FriendlyName": "NoSystem", "Size": 8000000000, "BusType": "USB", "IsBoot": false},
	    {"Number": 3, "FriendlyName": "Known", "Size": 8000000000, "BusType": "USB", "IsBoot": false, "IsSystem": false}
	  ],
	  "partitions": [],
	  "volumes": [],
	  "systemDrive": "C:"
	}`)
	if got, want := drivePaths(drives), []string{`\\.\PhysicalDrive3`}; !equalStrings(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestWindowsDisksInvalidInputIsAnError(t *testing.T) {
	for name, data := range map[string]string{
		"invalid json": `{"disks": [`,
		"empty input":  ``,
	} {
		if _, err := ParseWindowsDisks([]byte(data)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
