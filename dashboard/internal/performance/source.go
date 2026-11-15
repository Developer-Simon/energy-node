package performance

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DashboardUnit is the unit name under which the dashboard's own load is reported.
// It comes from /proc/self/stat, so it works without rights on the own cgroup.
const DashboardUnit = "energy-node-dashboard.service"

// userHZ is the kernel's USER_HZ, the unit of the utime and stime fields in /proc/<pid>/stat.
const userHZ = 100

// Raw is one cumulative sample of the device and its units.
type Raw struct {
	At                     time.Time
	CPUBusy, CPUTotal      uint64 // Jiffies aus der Zeile "cpu" von /proc/stat
	Cores                  int
	MemTotalKB, MemAvailKB uint64
	SwapIn, SwapOut        uint64 // pswpin, pswpout aus /proc/vmstat
	Units                  map[string]UnitRaw
}

// UnitRaw is the cumulative sample of one systemd unit.
type UnitRaw struct {
	CPUUsec  uint64 // usage_usec aus cpu.stat
	MemBytes int64
	HasMem   bool // false ohne memory.current
}

// Source reads a Raw sample for the device and the given units.
type Source interface {
	Read(units []string) (Raw, error)
}

// NewProcSource reads the counters from procRoot (normally "/proc") and
// cgroupRoot (normally "/sys/fs/cgroup/system.slice").
func NewProcSource(procRoot, cgroupRoot string) Source {
	return &procSource{procRoot: procRoot, cgroupRoot: cgroupRoot}
}

type procSource struct {
	procRoot   string
	cgroupRoot string
}

func (s *procSource) Read(units []string) (Raw, error) {
	raw := Raw{At: time.Now(), Units: map[string]UnitRaw{}}
	if err := s.readStat(&raw); err != nil {
		return Raw{}, err
	}
	if err := s.readMeminfo(&raw); err != nil {
		return Raw{}, err
	}
	if err := s.readVmstat(&raw); err != nil {
		return Raw{}, err
	}
	if u, ok := s.readSelf(); ok {
		raw.Units[DashboardUnit] = u
	}
	for _, name := range units {
		if u, ok := s.readUnit(name); ok {
			raw.Units[name] = u
		}
	}
	return raw, nil
}

func (s *procSource) readStat(raw *Raw) error {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "stat"))
	if err != nil {
		return fmt.Errorf("read proc stat: %w", err)
	}
	gotTotal := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] == "cpu" {
			busy, total, err := parseCPUFields(fields[1:])
			if err != nil {
				return fmt.Errorf("proc stat cpu line: %w", err)
			}
			raw.CPUBusy, raw.CPUTotal = busy, total
			gotTotal = true
			continue
		}
		if _, err := strconv.Atoi(fields[0][3:]); err == nil {
			raw.Cores++
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("proc stat: %w", err)
	}
	if !gotTotal {
		return errors.New("proc stat: no cpu line")
	}
	return nil
}

// parseCPUFields sums user..steal (guest is already part of user) and
// treats idle and iowait as not busy.
func parseCPUFields(fields []string) (busy, total uint64, err error) {
	const counted = 8
	if len(fields) < counted {
		return 0, 0, fmt.Errorf("want %d fields, got %d", counted, len(fields))
	}
	values := make([]uint64, counted)
	for i := 0; i < counted; i++ {
		values[i], err = strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		total += values[i]
	}
	idle, iowait := values[3], values[4]
	return total - idle - iowait, total, nil
}

func (s *procSource) readMeminfo(raw *Raw) error {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "meminfo"))
	if err != nil {
		return fmt.Errorf("read proc meminfo: %w", err)
	}
	var foundTotal, foundAvail bool
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := parseKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			raw.MemTotalKB, foundTotal = value, true
		case "MemAvailable":
			raw.MemAvailKB, foundAvail = value, true
		}
	}
	if !foundTotal || !foundAvail {
		return errors.New("proc meminfo: MemTotal or MemAvailable missing")
	}
	return nil
}

func (s *procSource) readVmstat(raw *Raw) error {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "vmstat"))
	if err != nil {
		return fmt.Errorf("read proc vmstat: %w", err)
	}
	var foundIn, foundOut bool
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := parseKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "pswpin":
			raw.SwapIn, foundIn = value, true
		case "pswpout":
			raw.SwapOut, foundOut = value, true
		}
	}
	if !foundIn || !foundOut {
		return errors.New("proc vmstat: pswpin or pswpout missing")
	}
	return nil
}

// readSelf reads the dashboard's own CPU time. A missing or unreadable file
// only drops this unit.
func (s *procSource) readSelf() (UnitRaw, bool) {
	data, err := os.ReadFile(filepath.Join(s.procRoot, "self", "stat"))
	if err != nil {
		return UnitRaw{}, false
	}
	// The comm field may contain spaces, so split after the last ')'.
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 {
		return UnitRaw{}, false
	}
	// fields[0] is state (field 3), so utime (field 14) is fields[11] and stime (field 15) is fields[12].
	fields := strings.Fields(string(data[idx+1:]))
	if len(fields) < 13 {
		return UnitRaw{}, false
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return UnitRaw{}, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return UnitRaw{}, false
	}
	return UnitRaw{CPUUsec: (utime + stime) * (1_000_000 / userHZ)}, true
}

// readUnit reads one unit from the cgroup tree. A missing or unreadable cpu.stat
// drops this unit only. A missing memory.current keeps the unit without memory.
func (s *procSource) readUnit(name string) (UnitRaw, bool) {
	dir := filepath.Join(s.cgroupRoot, name)
	cpuData, err := os.ReadFile(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return UnitRaw{}, false
	}
	usec, ok := parseCPUStatUsage(cpuData)
	if !ok {
		return UnitRaw{}, false
	}
	u := UnitRaw{CPUUsec: usec}
	if memData, err := os.ReadFile(filepath.Join(dir, "memory.current")); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(memData)), 10, 64); err == nil {
			u.MemBytes, u.HasMem = v, true
		}
	}
	return u, true
}

func parseCPUStatUsage(data []byte) (uint64, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := parseKeyValue(line)
		if ok && key == "usage_usec" {
			return value, true
		}
	}
	return 0, false
}

// parseKeyValue parses lines of the form "key value" as used by vmstat, meminfo
// (with a trailing colon and unit) and cpu.stat.
func parseKeyValue(line string) (string, uint64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}
	key := strings.TrimSuffix(fields[0], ":")
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return key, value, true
}
