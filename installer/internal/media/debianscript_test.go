package media

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDisk is one block device in the fake sysfs tree of the early-script test.
type fakeDisk struct {
	name      string  // kernel name, e.g. sda or mmcblk1
	usb       bool    // device path runs through a usb bus
	removable string  // content of the removable file
	mmcType   string  // content of device/type, empty writes no file
	gb        float64 // size in decimal GB
}

func internalDisk(name string, gb float64) fakeDisk {
	return fakeDisk{name: name, removable: "0", gb: gb}
}

func usbDisk(name string, gb float64) fakeDisk {
	return fakeDisk{name: name, usb: true, removable: "0", gb: gb}
}

func removableDisk(name string, gb float64) fakeDisk {
	return fakeDisk{name: name, removable: "1", gb: gb}
}

// sdCardDisk is an mmcblk device whose card reports type SD.
func sdCardDisk(name string, gb float64) fakeDisk {
	return fakeDisk{name: name, removable: "0", mmcType: "SD", gb: gb}
}

// emmcDisk is an mmcblk device whose card reports type MMC (soldered eMMC).
func emmcDisk(name string, gb float64) fakeDisk {
	return fakeDisk{name: name, removable: "0", mmcType: "MMC", gb: gb}
}

// earlyCase is one row of the disk selection table.
type earlyCase struct {
	name      string
	disks     []fakeDisk
	stick     string   // partition the install stick is mounted from, e.g. sda1; empty for none
	efi       bool     // /sys/firmware/efi exists
	confirm   string   // EN_CONFIRM in the env file, empty means no
	answerYes bool     // what the confirm dialog answers
	chosen    string   // expected partman-auto/disk value, e.g. /dev/sda
	class     string   // expected size class: small, medium or large
	abort     string   // expected abort template, e.g. no-disk
	substs    []string // expected db_subst lines for an abort
	powerOff  bool     // expected poweroff without an abort (confirm answered no)
}

// fakeConfmodule records every debconf call the script makes to REC_FILE.
const fakeConfmodule = `
db_x_loadtemplatefile() { printf 'loadtemplate|%s|%s\n' "$1" "$2" >>"$REC_FILE"; }
db_subst() { printf 'subst|%s|%s|%s\n' "$1" "$2" "$3" >>"$REC_FILE"; }
db_fset() { printf 'fset|%s|%s|%s\n' "$1" "$2" "$3" >>"$REC_FILE"; }
db_input() { printf 'input|%s|%s\n' "$1" "$2" >>"$REC_FILE"; }
db_go() { printf 'go\n' >>"$REC_FILE"; return 0; }
db_get() { printf 'get|%s\n' "$1" >>"$REC_FILE"; RET=$FAKE_CONFIRM; }
`

func TestEarlyScriptPicksOnlyAnInternalDisk(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on this host")
	}
	script, err := filepath.Abs(filepath.Join("debiantpl", "energy-node.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const sd = "/dev/sda"
	cases := []earlyCase{
		{
			name:   "USB stick not chosen",
			disks:  []fakeDisk{usbDisk("sdb", 16), internalDisk("sda", 20)},
			chosen: sd, class: "medium",
		},
		{
			name:   "SD card not chosen",
			disks:  []fakeDisk{sdCardDisk("mmcblk0", 16), internalDisk("sda", 20)},
			chosen: sd, class: "medium",
		},
		{
			name:   "eMMC chosen as internal",
			disks:  []fakeDisk{emmcDisk("mmcblk1", 20)},
			chosen: "/dev/mmcblk1", class: "medium",
		},
		{
			name:   "removable disk not chosen",
			disks:  []fakeDisk{removableDisk("sdc", 16), internalDisk("sda", 20)},
			chosen: sd, class: "medium",
		},
		{
			name:   "boot medium from proc mounts not chosen",
			disks:  []fakeDisk{internalDisk("sda", 20), internalDisk("sdb", 20)},
			stick:  "sda1",
			chosen: "/dev/sdb", class: "medium",
		},
		{
			name:   "one internal disk chosen",
			disks:  []fakeDisk{internalDisk("sda", 20)},
			chosen: sd, class: "medium",
		},
		{
			name:   "two internal disks abort many-disks",
			disks:  []fakeDisk{internalDisk("sda", 20), internalDisk("sdb", 20)},
			abort:  "many-disks",
			substs: []string{"subst|energy-node/many-disks|DISKS|/dev/sda, /dev/sdb"},
		},
		{
			name:  "no internal disk aborts no-disk",
			disks: []fakeDisk{usbDisk("sdb", 16), removableDisk("sdc", 16)},
			abort: "no-disk",
		},
		{
			name:  "2 GB aborts too-small",
			disks: []fakeDisk{internalDisk("sda", 2)},
			abort: "too-small",
			substs: []string{
				"subst|energy-node/too-small|DISK|/dev/sda",
				"subst|energy-node/too-small|SIZE|2.0",
			},
		},
		{
			name:   "7.9 GB is small",
			disks:  []fakeDisk{internalDisk("sda", 7.9)},
			chosen: sd, class: "small",
		},
		{
			name:   "8 GB is medium",
			disks:  []fakeDisk{internalDisk("sda", 8)},
			chosen: sd, class: "medium",
		},
		{
			name:   "34 GB is medium",
			disks:  []fakeDisk{internalDisk("sda", 34)},
			chosen: sd, class: "medium",
		},
		{
			name:   "35 GB is large",
			disks:  []fakeDisk{internalDisk("sda", 35)},
			chosen: sd, class: "large",
		},
		{
			name:   "EFI system uses the efi method",
			disks:  []fakeDisk{internalDisk("sda", 20)},
			efi:    true,
			chosen: sd, class: "medium",
		},
		{
			name:   "BIOS system uses biosgrub",
			disks:  []fakeDisk{internalDisk("sda", 20)},
			chosen: sd, class: "medium",
		},
		{
			name:      "confirm yes proceeds",
			disks:     []fakeDisk{internalDisk("sda", 20)},
			confirm:   "yes",
			answerYes: true,
			chosen:    sd, class: "medium",
		},
		{
			name:     "confirm no powers off",
			disks:    []fakeDisk{internalDisk("sda", 20)},
			confirm:  "yes",
			powerOff: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runEarlyCase(t, script, c)
		})
	}
}

func runEarlyCase(t *testing.T, script string, c earlyCase) {
	t.Helper()
	dir := t.TempDir()
	if strings.Contains(dir, "usb") {
		t.Fatalf("temp dir %q contains usb, the sysfs path check would match every disk; rename the case", dir)
	}
	sys := filepath.Join(dir, "sys")
	bin := filepath.Join(dir, "bin")
	cdrom := filepath.Join(dir, "cdrom")
	rec := filepath.Join(dir, "debconf.rec")
	classFile := filepath.Join(dir, "class")
	mounts := filepath.Join(dir, "mounts")
	confmodule := filepath.Join(dir, "confmodule")
	logFile := filepath.Join(dir, "install.log")
	for _, p := range []string{bin, cdrom} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	buildFakeSysfs(t, sys, c.disks, c.stick, c.efi)

	mountLine := ""
	if c.stick != "" {
		mountLine = fmt.Sprintf("/dev/%s %s iso9660 ro 0 0\n", c.stick, cdrom)
	}
	writeTestFile(t, mounts, mountLine, 0o644)
	confirm := c.confirm
	if confirm == "" {
		confirm = "no"
	}
	writeTestFile(t, filepath.Join(cdrom, "energy-node.env"), "EN_CONFIRM="+confirm+"\n", 0o644)
	writeTestFile(t, confmodule, fakeConfmodule, 0o644)

	var devs []string
	for _, d := range c.disks {
		devs = append(devs, "/dev/"+d.name)
	}
	writeTestFile(t, filepath.Join(bin, "poweroff"),
		"#!/bin/sh\nprintf 'poweroff\\n' >>\"$REC_FILE\"\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "listdev"),
		"#!/bin/sh\nprintf '%s\\n' "+strings.Join(devs, " ")+"\n", 0o755)
	writeTestFile(t, filepath.Join(bin, "debconf"),
		"#!/bin/sh\nprintf 'debconf-set|%s|%s\\n' \"$1\" \"$2\" >>\"$REC_FILE\"\n", 0o755)

	cmd := exec.Command("sh", script, "early")
	cmd.Env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"REC_FILE=" + rec,
		"EN_CDROM=" + cdrom,
		"EN_SYSFS=" + sys,
		"EN_PROC_MOUNTS=" + mounts,
		"EN_LIST_DEVICES=" + filepath.Join(bin, "listdev"),
		"EN_DEBCONF=" + filepath.Join(bin, "debconf"),
		"EN_CONFMODULE=" + confmodule,
		"EN_LOG=" + logFile,
		"EN_CLASS_FILE=" + classFile,
		fmt.Sprintf("FAKE_CONFIRM=%t", c.answerYes),
	}
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		exit = ee.ExitCode()
	}
	lines := readTestLines(t, rec)
	t.Logf("script output:\n%s", out)

	if c.abort != "" {
		if exit != 1 {
			t.Errorf("exit = %d, want 1 after abort", exit)
		}
		wantTestLine(t, lines, "input|critical|energy-node/"+c.abort)
		wantTestLine(t, lines, "poweroff")
		for _, s := range c.substs {
			wantTestLine(t, lines, s)
		}
		noTestLinePrefix(t, lines, "debconf-set|")
		return
	}
	if c.powerOff {
		if exit != 1 {
			t.Errorf("exit = %d, want 1 after declining the confirm dialog", exit)
		}
		wantTestLine(t, lines, "input|critical|energy-node/confirm")
		wantTestLine(t, lines, "poweroff")
		noTestLinePrefix(t, lines, "debconf-set|")
		return
	}

	if exit != 0 {
		t.Fatalf("exit = %d, want 0\n%s", exit, out)
	}
	noTestLinePrefix(t, lines, "poweroff")
	wantTestLine(t, lines, "debconf-set|partman-auto/disk|"+c.chosen)
	if got := countTestPrefix(lines, "debconf-set|partman-auto/disk|"); got != 1 {
		t.Errorf("partman-auto/disk set %d times, want once", got)
	}
	wantTestLine(t, lines, "debconf-set|grub-installer/bootdev|"+c.chosen)

	recipe := firstTestWithPrefix(lines, "debconf-set|partman-auto/expert_recipe|")
	wantMethod := "method{ biosgrub }"
	if c.efi {
		wantMethod = "method{ efi }"
	}
	if !strings.Contains(recipe, wantMethod) {
		t.Errorf("expert recipe %q does not contain %q", recipe, wantMethod)
	}

	classOut, err := os.ReadFile(classFile)
	if err != nil {
		t.Fatalf("class file: %v", err)
	}
	if got := strings.TrimSpace(string(classOut)); got != c.class {
		t.Errorf("class = %q, want %q", got, c.class)
	}
	hasPkgsel := countTestPrefix(lines, "debconf-set|pkgsel/include|") > 0
	if hasPkgsel != (c.class == "small") {
		t.Errorf("pkgsel/include set = %t for class %s", hasPkgsel, c.class)
	}

	if c.confirm == "yes" {
		wantTestLine(t, lines, "input|critical|energy-node/confirm")
		wantTestLine(t, lines, "subst|energy-node/confirm|DISK|"+c.chosen)
	}
}

// buildFakeSysfs lays out a sysfs-like tree: block/<name> symlinks to the real
// device directory, which carries removable, size and device/ files, and the
// stick partition under its disk plus a class/block symlink to it.
func buildFakeSysfs(t *testing.T, sys string, disks []fakeDisk, stick string, efi bool) {
	t.Helper()
	for _, d := range disks {
		bus := "devices/pci0000:00/0000:00:17.0/ata1"
		switch {
		case d.usb:
			bus = "devices/pci0000:00/0000:00:14.0/usb1/1-2"
		case strings.HasPrefix(d.name, "mmcblk"):
			bus = "devices/platform/soc/mmc/" + d.name
		}
		real := filepath.Join(sys, bus, "block", d.name)
		if err := os.MkdirAll(filepath.Join(real, "device"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(sys, "block"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(sys, "block", d.name)); err != nil {
			t.Fatal(err)
		}
		sectors := int64(math.Round(d.gb * 1e9 / 512))
		writeTestFile(t, filepath.Join(real, "removable"), d.removable+"\n", 0o644)
		writeTestFile(t, filepath.Join(real, "size"), fmt.Sprintf("%d\n", sectors), 0o644)
		writeTestFile(t, filepath.Join(real, "device", "model"), "Fake Model  \n", 0o644)
		if d.mmcType != "" {
			writeTestFile(t, filepath.Join(real, "device", "type"), d.mmcType+"\n", 0o644)
		}
		if stick != "" && strings.TrimSuffix(stick, "1") == d.name {
			partDir := filepath.Join(real, stick)
			if err := os.MkdirAll(partDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(partDir, "partition"), "1\n", 0o644)
			classDir := filepath.Join(sys, "class", "block")
			if err := os.MkdirAll(classDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(partDir, filepath.Join(classDir, stick)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if efi {
		if err := os.MkdirAll(filepath.Join(sys, "firmware", "efi"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func writeTestFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func readTestLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func wantTestLine(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, l := range lines {
		if l == want {
			return
		}
	}
	t.Errorf("debconf record lacks %q\nrecorded:\n%s", want, strings.Join(lines, "\n"))
}

func noTestLinePrefix(t *testing.T, lines []string, prefix string) {
	t.Helper()
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			t.Errorf("unexpected record %q", l)
		}
	}
}

func countTestPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func firstTestWithPrefix(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}
