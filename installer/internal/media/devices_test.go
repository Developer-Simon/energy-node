package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevicesAreInDisplayOrder(t *testing.T) {
	want := []string{"pi5", "pi4", "pi3", "pi2", "pi1"}
	got := Devices()
	if len(got) != len(want) {
		t.Fatalf("got %d devices, want %d", len(got), len(want))
	}
	for i, d := range got {
		if d.ID != want[i] {
			t.Errorf("position %d: got %q, want %q", i, d.ID, want[i])
		}
	}
}

func TestDeviceByID(t *testing.T) {
	d, ok := DeviceByID("pi4")
	if !ok || d.Tag != "pi4-64bit" {
		t.Fatalf("DeviceByID(pi4) = %+v, %v", d, ok)
	}
	if _, ok := DeviceByID("nope"); ok {
		t.Fatal("DeviceByID(nope) found a device")
	}
}

// docTableLabels maps the hardware label in the docs table to the device ID.
var docTableLabels = map[string]string{
	"Pi 5, 500":           "pi5",
	"Pi 4, 400":           "pi4",
	"Pi 3, 3+":            "pi3",
	"Pi Zero 2 W":         "pi3",
	"Pi 2":                "pi2",
	"Pi Zero, Zero W":     "pi1",
	"Pi 1 (A, A+, B, B+)": "pi1",
}

const docTableHeader = "| Hardware | Image | Installation package | Tested |"

func readDocTable(t *testing.T) []string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "docs", "installer-operating-system.md"),
		filepath.Join("..", "..", "..", "docs", "installer.md"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != docTableHeader {
				continue
			}
			var rows []string
			for _, row := range lines[i+2:] {
				if !strings.HasPrefix(row, "|") {
					break
				}
				rows = append(rows, row)
			}
			return rows
		}
		t.Fatalf("%s has no hardware table with header %q", path, docTableHeader)
	}
	t.Fatal("no installer docs found")
	return nil
}

func TestDevicesMatchTheHardwareTableInTheDocs(t *testing.T) {
	seen := map[string]bool{}
	tested := map[string]bool{}
	for _, row := range readDocTable(t) {
		cells := strings.Split(strings.Trim(row, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) < 4 || !strings.HasPrefix(cells[0], "Pi ") {
			continue
		}
		label := cells[0]
		id, ok := docTableLabels[label]
		if !ok {
			t.Errorf("no device mapping for docs row %q", row)
			continue
		}
		dev, ok := DeviceByID(id)
		if !ok {
			t.Errorf("device %q for row %q is missing", id, label)
			continue
		}
		seen[id] = true
		if dev.ImageName != cells[1] {
			t.Errorf("%s: ImageName %q, docs say %q", id, dev.ImageName, cells[1])
		}
		if arch := strings.Trim(cells[2], "`"); dev.Arch != arch {
			t.Errorf("%s: Arch %q, docs say %q", id, dev.Arch, arch)
		}
		if cells[3] == "✓" {
			tested[id] = true
		}
	}
	for id := range seen {
		dev, _ := DeviceByID(id)
		if dev.Experimental == tested[id] {
			t.Errorf("%s: Experimental %v, but the docs mark tested=%v (a group is tested when any row is)", id, dev.Experimental, tested[id])
		}
	}
	for label, id := range docTableLabels {
		if !seen[id] {
			t.Errorf("docs have no row for %q (%s)", label, id)
		}
	}
}
