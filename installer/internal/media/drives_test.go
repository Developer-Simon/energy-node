package media

import (
	"os"
	"testing"
)

func loadLsblk(t *testing.T, name string) []Drive {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	drives, err := ParseLsblk(data)
	if err != nil {
		t.Fatalf("ParseLsblk(%s): %v", name, err)
	}
	return drives
}

func drivePaths(drives []Drive) []string {
	out := make([]string, 0, len(drives))
	for _, d := range drives {
		out = append(out, d.Path)
	}
	return out
}

func findDrive(t *testing.T, drives []Drive, path string) Drive {
	t.Helper()
	for _, d := range drives {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("no drive %s in %v", path, drivePaths(drives))
	return Drive{}
}

func TestParseLsblkDropsTheSystemDisk(t *testing.T) {
	got := drivePaths(loadLsblk(t, "lsblk-new.json"))
	want := []string{"/dev/sda", "/dev/sdb"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestParseLsblkMarksRemovable(t *testing.T) {
	drives := loadLsblk(t, "lsblk-new.json")
	sda := findDrive(t, drives, "/dev/sda")
	sdb := findDrive(t, drives, "/dev/sdb")
	if !sdb.Removable {
		t.Error("sdb.Removable = false, want true")
	}
	if sda.Removable {
		t.Error("sda.Removable = true, want false")
	}
	if sdb.Partitions[0].Label != "bootfs" {
		t.Errorf("sdb partition label = %q, want bootfs", sdb.Partitions[0].Label)
	}
	if sda.Model != "Samsung SSD 870" {
		t.Errorf("sda.Model = %q, want trimmed Samsung SSD 870", sda.Model)
	}
}

func TestVisibleHidesInternalByDefault(t *testing.T) {
	drives := loadLsblk(t, "lsblk-new.json")

	hidden := drivePaths(Visible(drives, false))
	if len(hidden) != 1 || hidden[0] != "/dev/sdb" {
		t.Errorf("Visible(false) = %v, want [/dev/sdb]", hidden)
	}
	all := drivePaths(Visible(drives, true))
	if len(all) != 2 || all[0] != "/dev/sda" || all[1] != "/dev/sdb" {
		t.Errorf("Visible(true) = %v, want [/dev/sda /dev/sdb]", all)
	}
}

func TestParseLsblkAcceptsTheOldFormat(t *testing.T) {
	newDrives := loadLsblk(t, "lsblk-new.json")
	oldDrives := loadLsblk(t, "lsblk-old.json")

	gotPaths := drivePaths(oldDrives)
	if len(gotPaths) != 2 || gotPaths[0] != "/dev/sda" || gotPaths[1] != "/dev/sdb" {
		t.Fatalf("old paths = %v, want [/dev/sda /dev/sdb]", gotPaths)
	}
	for _, path := range []string{"/dev/sda", "/dev/sdb"} {
		if o, n := findDrive(t, oldDrives, path).Size, findDrive(t, newDrives, path).Size; o != n {
			t.Errorf("%s size old = %d, new = %d", path, o, n)
		}
	}
	if !findDrive(t, oldDrives, "/dev/sdb").Removable {
		t.Error("old sdb.Removable = false, want true")
	}
}
