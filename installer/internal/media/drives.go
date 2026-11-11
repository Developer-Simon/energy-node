package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrUnsupported reports that drive listing is not available on this system.
var ErrUnsupported = errors.New("media: not supported on this system")

// Partition is one partition or child block device of a drive.
type Partition struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	FSType string `json:"fstype"`
	Used   int64  `json:"used"` // 0 = unbekannt
}

// Drive is a whole disk that the media mode may write an image to.
type Drive struct {
	Path       string      `json:"path"`  // "/dev/sdb"
	Model      string      `json:"model"` // getrimmt, leer -> Name des Geraets
	Size       int64       `json:"size"`
	Transport  string      `json:"transport"` // "usb", "mmc", "sata", "nvme", ""
	Removable  bool        `json:"removable"`
	Partitions []Partition `json:"partitions"`
}

// SystemMounts are the mount points that mark a disk as the system disk.
var SystemMounts = []string{"/", "/boot", "/boot/efi", "/home", "/usr", "/var"}

// lsblkNode is one entry of the lsblk JSON tree. Fields that changed between
// lsblk versions stay raw and are read with flexBool, flexInt and mounts.
type lsblkNode struct {
	Name        string          `json:"name"`
	Path        string          `json:"path"`
	Size        json.RawMessage `json:"size"`
	Model       string          `json:"model"`
	Tran        string          `json:"tran"`
	RM          json.RawMessage `json:"rm"`
	Type        string          `json:"type"`
	FSType      string          `json:"fstype"`
	Label       string          `json:"label"`
	FSUsed      json.RawMessage `json:"fsused"`
	Mountpoints json.RawMessage `json:"mountpoints"`
	Mountpoint  json.RawMessage `json:"mountpoint"`
	Children    []lsblkNode     `json:"children"`
}

// ParseLsblk liefert alle Laufwerke ausser den Systemlaufwerken.
// system = Laufwerk, unter dem (auch verschachtelt ueber LVM/LUKS/RAID)
// ein Mountpunkt aus SystemMounts oder "[SWAP]" haengt.
func ParseLsblk(data []byte) ([]Drive, error) {
	var doc struct {
		BlockDevices []lsblkNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("media: lsblk output: %w", err)
	}
	drives := make([]Drive, 0, len(doc.BlockDevices))
	for _, n := range doc.BlockDevices {
		if !isDrive(n) || n.holdsSystem() {
			continue
		}
		drives = append(drives, n.drive())
	}
	return drives, nil
}

// Visible filtert: ohne internal nur Removable.
func Visible(drives []Drive, internal bool) []Drive {
	if internal {
		return drives
	}
	out := make([]Drive, 0, len(drives))
	for _, d := range drives {
		if d.Removable {
			out = append(out, d)
		}
	}
	return out
}

func isDrive(n lsblkNode) bool {
	if n.Type != "disk" || flexInt(n.Size) <= 0 {
		return false
	}
	return !strings.HasPrefix(n.Name, "loop") && !strings.HasPrefix(n.Name, "zram")
}

// holdsSystem reports whether n or anything below it is a system mount.
func (n lsblkNode) holdsSystem() bool {
	for _, m := range n.mounts() {
		if (m == "[SWAP]" && n.FSType == "swap") || isSystemMount(m) {
			return true
		}
	}
	for _, c := range n.Children {
		if c.holdsSystem() {
			return true
		}
	}
	return false
}

func isSystemMount(m string) bool {
	for _, s := range SystemMounts {
		if m == s {
			return true
		}
	}
	return false
}

func (n lsblkNode) drive() Drive {
	model := strings.TrimSpace(n.Model)
	if model == "" {
		model = n.Name
	}
	path := n.Path
	if path == "" {
		path = "/dev/" + n.Name
	}
	parts := make([]Partition, 0, len(n.Children))
	for _, c := range n.Children {
		parts = append(parts, Partition{
			Name:   c.Name,
			Label:  c.Label,
			FSType: c.FSType,
			Used:   flexInt(c.FSUsed),
		})
	}
	return Drive{
		Path:       path,
		Model:      model,
		Size:       flexInt(n.Size),
		Transport:  n.Tran,
		Removable:  flexBool(n.RM) || n.Tran == "usb" || n.Tran == "mmc",
		Partitions: parts,
	}
}

// mounts returns the non-empty mount points of n. lsblk >= 2.37 reports a
// list under "mountpoints", older versions a single "mountpoint".
func (n lsblkNode) mounts() []string {
	var out []string
	if len(n.Mountpoints) > 0 && string(n.Mountpoints) != "null" {
		var list []*string
		if json.Unmarshal(n.Mountpoints, &list) == nil {
			for _, p := range list {
				if p != nil && *p != "" {
					out = append(out, *p)
				}
			}
		}
		return out
	}
	var one *string
	if json.Unmarshal(n.Mountpoint, &one) == nil && one != nil && *one != "" {
		out = append(out, *one)
	}
	return out
}

// flexBool reads a JSON bool, or the strings "0"/"1" and "true"/"false".
func flexBool(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "1" || s == "true"
	}
	return false
}

// flexInt reads a JSON number or a numeric string. Missing or null is 0.
func flexInt(raw json.RawMessage) int64 {
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
	return 0
}
