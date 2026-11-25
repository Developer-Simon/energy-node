package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// windowsDiskScript liefert Platten, Partitionen und Volumes als JSON.
// Die Ausgabe wird in ParseWindowsDisks ausgewertet.
const windowsDiskScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$d = @(Get-Disk | Select-Object Number, FriendlyName, Size, @{n='BusType';e={[string]$_.BusType}}, IsBoot, IsSystem)
$p = @(Get-Partition | Select-Object DiskNumber, PartitionNumber, @{n='DriveLetter';e={[string]$_.DriveLetter}}, Size, @{n='AccessPaths';e={@($_.AccessPaths)}})
$v = @(Get-Volume | Select-Object Path, FileSystemLabel, FileSystem, Size, SizeRemaining)
@{ disks = $d; partitions = $p; volumes = $v; systemDrive = $env:SystemDrive } | ConvertTo-Json -Depth 4 -Compress`

// windowsBusNames ist die Microsoft-Tabelle der BusType-Werte.
var windowsBusNames = []string{
	"Unknown", "SCSI", "ATAPI", "ATA", "1394", "SSA", "Fibre Channel", "USB",
	"RAID", "iSCSI", "SAS", "SATA", "SD", "MMC", "Virtual", "FileBackedVirtual",
	"Storage Spaces", "NVMe", "SCM", "UFS",
}

type winDisk struct {
	Number       int             `json:"Number"`
	FriendlyName string          `json:"FriendlyName"`
	Size         int64           `json:"Size"`
	BusType      json.RawMessage `json:"BusType"`
	IsBoot       *bool           `json:"IsBoot"`
	IsSystem     *bool           `json:"IsSystem"`
}

type winPartition struct {
	DiskNumber      int             `json:"DiskNumber"`
	PartitionNumber int             `json:"PartitionNumber"`
	DriveLetter     string          `json:"DriveLetter"`
	Size            int64           `json:"Size"`
	AccessPaths     json.RawMessage `json:"AccessPaths"`
}

type winVolume struct {
	Path            string `json:"Path"`
	FileSystemLabel string `json:"FileSystemLabel"`
	FileSystem      string `json:"FileSystem"`
	Size            int64  `json:"Size"`
	SizeRemaining   int64  `json:"SizeRemaining"`
}

type winDisksDoc struct {
	Disks       json.RawMessage `json:"disks"`
	Partitions  json.RawMessage `json:"partitions"`
	Volumes     json.RawMessage `json:"volumes"`
	SystemDrive string          `json:"systemDrive"`
}

// ParseWindowsDisks liefert die Wechselmedien und internen Platten ohne das
// Systemlaufwerk. Fail closed: fehlt systemDrive, ist das ein Fehler. Platten
// mit fehlenden Boot- oder Systemflags werden ausgeblendet.
func ParseWindowsDisks(data []byte) ([]Drive, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("media: windows disk list: empty output")
	}
	var doc winDisksDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("media: windows disk list: %w", err)
	}
	sys := strings.TrimSpace(doc.SystemDrive)
	if sys == "" {
		return nil, errors.New("media: windows disk list: systemDrive fehlt")
	}
	sysLetter := strings.ToUpper(sys[:1])

	disks, err := decodeWinList[winDisk](doc.Disks)
	if err != nil {
		return nil, fmt.Errorf("media: windows disk list: disks: %w", err)
	}
	parts, err := decodeWinList[winPartition](doc.Partitions)
	if err != nil {
		return nil, fmt.Errorf("media: windows disk list: partitions: %w", err)
	}
	vols, err := decodeWinList[winVolume](doc.Volumes)
	if err != nil {
		return nil, fmt.Errorf("media: windows disk list: volumes: %w", err)
	}

	volByPath := make(map[string]winVolume, len(vols))
	for _, v := range vols {
		volByPath[v.Path] = v
	}
	partsByDisk := make(map[int][]winPartition)
	for _, p := range parts {
		partsByDisk[p.DiskNumber] = append(partsByDisk[p.DiskNumber], p)
	}

	sort.SliceStable(disks, func(i, j int) bool { return disks[i].Number < disks[j].Number })
	drives := make([]Drive, 0, len(disks))
	for _, d := range disks {
		if d.Size <= 0 || d.IsBoot == nil || d.IsSystem == nil || *d.IsBoot || *d.IsSystem {
			continue
		}
		dParts := partsByDisk[d.Number]
		if holdsSystemLetter(dParts, sysLetter) {
			continue
		}
		drives = append(drives, d.drive(dParts, volByPath))
	}
	return drives, nil
}

// holdsSystemLetter reports whether one partition has the system drive letter.
func holdsSystemLetter(parts []winPartition, sysLetter string) bool {
	for _, p := range parts {
		if letter := driveLetter(p.DriveLetter); letter != "" && strings.EqualFold(letter, sysLetter) {
			return true
		}
	}
	return false
}

// driveLetter liefert den Laufwerksbuchstaben, NUL und Leerzeichen gelten als leer.
func driveLetter(raw string) string {
	s := strings.Trim(raw, "\x00 \t\r\n")
	if s == "" {
		return ""
	}
	return s[:1]
}

func (d winDisk) drive(parts []winPartition, vols map[string]winVolume) Drive {
	model := strings.TrimSpace(d.FriendlyName)
	if model == "" {
		model = fmt.Sprintf("PhysicalDrive%d", d.Number)
	}
	bus := windowsBusName(d.BusType)
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].PartitionNumber < parts[j].PartitionNumber })
	partitions := make([]Partition, 0, len(parts))
	for _, p := range parts {
		partitions = append(partitions, p.partition(vols))
	}
	return Drive{
		Path:       fmt.Sprintf(`\\.\PhysicalDrive%d`, d.Number),
		Model:      model,
		Size:       d.Size,
		Transport:  windowsTransport(bus),
		Removable:  isWindowsRemovableBus(bus),
		Partitions: partitions,
	}
}

func (p winPartition) partition(vols map[string]winVolume) Partition {
	out := Partition{Name: fmt.Sprintf("Partition %d", p.PartitionNumber)}
	if letter := driveLetter(p.DriveLetter); letter != "" {
		out.Name = letter + ":"
	}
	paths, err := decodeWinList[string](p.AccessPaths)
	if err != nil {
		return out
	}
	for _, path := range paths {
		if v, ok := vols[path]; ok {
			out.Label = v.FileSystemLabel
			out.FSType = v.FileSystem
			out.Used = v.Size - v.SizeRemaining
			break
		}
	}
	return out
}

// windowsBusName liefert den Namen eines BusType, auch als Zahl oder Zahlentext.
func windowsBusName(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if n, err := strconv.Atoi(s); err == nil {
			return windowsBusByNumber(n)
		}
		return s
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return windowsBusByNumber(n)
	}
	return ""
}

func windowsBusByNumber(n int) string {
	if n < 0 || n >= len(windowsBusNames) {
		return ""
	}
	return windowsBusNames[n]
}

func windowsTransport(bus string) string {
	switch strings.ToUpper(bus) {
	case "USB":
		return "usb"
	case "SD", "MMC":
		return "mmc"
	case "SATA", "ATA":
		return "sata"
	case "NVME":
		return "nvme"
	}
	return strings.ToLower(bus)
}

func isWindowsRemovableBus(bus string) bool {
	switch strings.ToUpper(bus) {
	case "USB", "SD", "MMC":
		return true
	}
	return false
}

// decodeWinList liest eine JSON-Liste, die als Array, Einzelobjekt oder null kommen darf.
func decodeWinList[T any](raw json.RawMessage) ([]T, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '[' {
		var out []T
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	var one T
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}
