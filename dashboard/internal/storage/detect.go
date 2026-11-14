// Package storage finds the drives that can hold the node's runtime data and
// backups. It only lists candidates; mounting and formatting happen elsewhere.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// lsblkColumns are the columns Parse expects. They are fixed, so the JSON
// layout the tests use stays stable.
const lsblkColumns = "NAME,KNAME,PATH,UUID,FSTYPE,SIZE,FSUSED,MODEL,TRAN,TYPE,MOUNTPOINT,PKNAME,LABEL"

// Catalog keys for Drive.Contents when a drive needs formatting.
const (
	// ContentsNone marks a drive without any filesystem.
	ContentsNone = "storage.contents.none"
	// ContentsForeign marks a drive with a filesystem other than ext4.
	ContentsForeign = "storage.contents.foreign"
	// ContentsWholeDisk marks the entry for a whole disk that has partitions.
	ContentsWholeDisk = "storage.contents.disk"
)

// Drive is a candidate for the second drive. Ready drives can be used as they
// are, NeedsFormat drives have to be formatted first, and Contents says why.
// Disk is the top-level disk a device belongs to, DiskSize its size. Whole
// marks the entry that stands for the entire disk, which is offered before
// the partitions of a disk with at least two partitions.
type Drive struct {
	Kname       string `json:"kname"`
	Name        string `json:"name"`
	UUID        string `json:"uuid"`
	FSType      string `json:"fstype"`
	Transport   string `json:"transport"`
	Size        int64  `json:"size"`
	Used        int64  `json:"used"`
	Ready       bool   `json:"ready"`
	NeedsFormat bool   `json:"needs_format"`
	Contents    string `json:"contents"`
	Disk        string `json:"disk"`
	DiskSize    int64  `json:"disk_size"`
	Label       string `json:"label"`
	Whole       bool   `json:"whole"`
}

// Lister lists the candidate drives.
type Lister interface {
	Drives(ctx context.Context) ([]Drive, error)
}

// LsblkLister reads the drives from lsblk and the root source from findmnt.
// Both run without root.
type LsblkLister struct {
	Runner systemactions.OutputRunner
}

// Drives runs lsblk and parses its output. A failing findmnt is not an error:
// without it, Parse still hides the drive that has "/" mounted.
func (l LsblkLister) Drives(ctx context.Context) ([]Drive, error) {
	runner := l.Runner
	if runner == nil {
		runner = systemactions.ExecOutputRunner{}
	}
	out, err := runner.Output(ctx, "lsblk", "-J", "-b", "-o", lsblkColumns)
	if err != nil {
		return nil, fmt.Errorf("lsblk: %w", err)
	}
	rootSource, _ := runner.Output(ctx, "findmnt", "-n", "-o", "SOURCE", "/")
	return Parse([]byte(out), strings.TrimSpace(rootSource))
}

// byteCount accepts the numbers lsblk prints with -b, quoted or not, and null.
type byteCount int64

func (n *byteCount) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("size %q: %w", s, err)
	}
	*n = byteCount(v)
	return nil
}

// lsblkNode is one entry of `lsblk -J`. Nested children are read as well as
// the flat PKNAME link.
type lsblkNode struct {
	Name       string      `json:"name"`
	KName      string      `json:"kname"`
	Path       string      `json:"path"`
	UUID       string      `json:"uuid"`
	FSType     string      `json:"fstype"`
	Size       byteCount   `json:"size"`
	FSUsed     byteCount   `json:"fsused"`
	Model      string      `json:"model"`
	Tran       string      `json:"tran"`
	Type       string      `json:"type"`
	Mountpoint string      `json:"mountpoint"`
	PKName     string      `json:"pkname"`
	Label      string      `json:"label"`
	Children   []lsblkNode `json:"children"`
}

// device is one flattened lsblk entry with its parent link.
type device struct {
	lsblkNode
	parent   string
	children int
}

func flatten(nodes []lsblkNode, parent string, out *[]device) {
	for _, n := range nodes {
		kname := n.KName
		if kname == "" {
			kname = n.Name
		}
		p := n.PKName
		if p == "" {
			p = parent
		}
		*out = append(*out, device{lsblkNode: n, parent: p})
		flatten(n.Children, kname, out)
	}
}

// Parse turns `lsblk -J -b -o NAME,KNAME,PATH,UUID,FSTYPE,SIZE,FSUSED,MODEL,TRAN,TYPE,MOUNTPOINT,PKNAME`
// output into the drives that can be offered. rootSource is the device that
// holds "/" (for example /dev/mmcblk0p2), or empty when it is unknown.
//
// Hidden: the whole disk that holds "/", every disk with a mounted partition
// (together with all its partitions), loop, ram and zram devices, and all MMC
// devices (eMMC and SD cards look the same without the removable flag).
// A disk with at least two partitions is offered as a whole (Whole, formatting
// the entire disk) directly before its partitions. A disk with one partition is
// offered only through that partition.
func Parse(lsblkJSON []byte, rootSource string) ([]Drive, error) {
	var doc struct {
		BlockDevices []lsblkNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(lsblkJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse lsblk output: %w", err)
	}

	var devs []device
	flatten(doc.BlockDevices, "", &devs)

	index := make(map[string]int, len(devs))
	for i, d := range devs {
		index[d.KName] = i
	}
	for _, d := range devs {
		if i, ok := index[d.parent]; ok && d.parent != "" {
			devs[i].children++
		}
	}

	top := make([]int, len(devs))
	for i := range devs {
		top[i] = topOf(devs, index, i)
	}

	systemOrMounted := make(map[int]bool)
	for i, d := range devs {
		isRoot := d.Mountpoint == "/" ||
			(rootSource != "" && (d.Path == rootSource || "/dev/"+d.KName == rootSource))
		if isRoot || d.Mountpoint != "" {
			systemOrMounted[top[i]] = true
		}
	}

	drives := []Drive{}
	for i, d := range devs {
		if systemOrMounted[top[i]] || excluded(d) {
			continue
		}
		if top[i] == i && d.children >= 2 {
			drives = append(drives, wholeDisk(devs, index, i))
			continue
		}
		if d.children > 0 {
			continue
		}
		drives = append(drives, toDrive(devs, index, i))
	}
	return drives, nil
}

// topOf follows the parent links up to the top-level disk of devs[i].
func topOf(devs []device, index map[string]int, i int) int {
	for steps := 0; steps <= len(devs); steps++ {
		j, ok := index[devs[i].parent]
		if devs[i].parent == "" || !ok {
			return i
		}
		i = j
	}
	return i
}

// excluded reports devices that are never offered, whatever their state.
func excluded(d device) bool {
	switch d.Type {
	case "loop", "ram", "rom":
		return true
	}
	for _, prefix := range []string{"loop", "ram", "zram", "mmcblk"} {
		if strings.HasPrefix(d.KName, prefix) {
			return true
		}
	}
	return false
}

// toDrive builds the Drive for the leaf devs[i]. Model and transport are
// inherited from the parent chain, because lsblk only prints them on disks.
func toDrive(devs []device, index map[string]int, i int) Drive {
	d := devs[i]
	disk := devs[topOf(devs, index, i)]
	transport := inherited(devs, index, i, func(n lsblkNode) string { return n.Tran })

	drive := Drive{
		Kname:     d.KName,
		Name:      displayName(devs, index, i),
		UUID:      d.UUID,
		FSType:    d.FSType,
		Transport: transport,
		Size:      int64(d.Size),
		Used:      int64(d.FSUsed),
		Disk:      disk.KName,
		DiskSize:  int64(disk.Size),
		Label:     d.Label,
	}
	switch {
	case d.FSType == "ext4":
		drive.Ready = true
	case d.FSType == "":
		drive.NeedsFormat = true
		drive.Contents = ContentsNone
	default:
		drive.NeedsFormat = true
		drive.Contents = ContentsForeign
	}
	return drive
}

// wholeDisk builds the entry that stands for the entire disk devs[i], which
// has partitions. Formatting it replaces the whole partition table.
func wholeDisk(devs []device, index map[string]int, i int) Drive {
	d := devs[i]
	return Drive{
		Kname:       d.KName,
		Name:        displayName(devs, index, i),
		Transport:   inherited(devs, index, i, func(n lsblkNode) string { return n.Tran }),
		Size:        int64(d.Size),
		Disk:        d.KName,
		DiskSize:    int64(d.Size),
		NeedsFormat: true,
		Contents:    ContentsWholeDisk,
		Whole:       true,
	}
}

// displayName is the model of devs[i] or its nearest parent, or the kname.
func displayName(devs []device, index map[string]int, i int) string {
	name := inherited(devs, index, i, func(n lsblkNode) string { return strings.TrimSpace(n.Model) })
	if name == "" {
		name = devs[i].KName
	}
	return name
}

// inherited returns the first non-empty value of get along devs[i] and its parents.
func inherited(devs []device, index map[string]int, i int, get func(lsblkNode) string) string {
	for steps := 0; steps <= len(devs); steps++ {
		if v := get(devs[i].lsblkNode); v != "" {
			return v
		}
		j, ok := index[devs[i].parent]
		if devs[i].parent == "" || !ok {
			return ""
		}
		i = j
	}
	return ""
}
