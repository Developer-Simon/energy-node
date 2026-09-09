// Package nodeagent publiziert den Raspberry-Pi-Knoten selbst als HA-Gerät
// "energy_node" (Systemdiagnose ueber MQTT Discovery), spiegelt den globalen
// simulation_active-Broadcast und leitet die Live-Erreichbarkeit der
// konfigurierten Bridges ab. Es ersetzt den frueheren, inzwischen
// geloeschten Python-Node-Dienst (Spec V2). Die Metrik-Leser hier sind
// bewusst injizierbar (readFile/run/now), damit metrics_test.go ohne echtes
// /proc, /sys oder CLIs laeuft - dasselbe Muster wie internal/storagehealth.
package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

const aptUpdatesTTL = 24 * time.Hour

type reader struct {
	readFile    func(string) ([]byte, error)
	run         func(ctx context.Context, name string, args ...string) ([]byte, error)
	statfs      func(string) (blocks, bfree, frsize uint64, err error)
	loadavg     func() (float64, error)
	numCPU      func() int
	dialLocalIP func() string
	now         func() time.Time

	aptTS    time.Time
	aptValue *int
}

func newReader() *reader {
	return &reader{
		readFile: os.ReadFile,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		statfs: func(path string) (uint64, uint64, uint64, error) {
			var s syscall.Statfs_t
			if err := syscall.Statfs(path, &s); err != nil {
				return 0, 0, 0, err
			}
			return uint64(s.Blocks), uint64(s.Bfree), uint64(s.Frsize), nil
		},
		loadavg: func() (float64, error) {
			data, err := os.ReadFile("/proc/loadavg")
			if err != nil {
				return 0, err
			}
			fields := strings.Fields(string(data))
			if len(fields) == 0 {
				return 0, fmt.Errorf("loadavg empty")
			}
			return strconv.ParseFloat(fields[0], 64)
		},
		numCPU: func() int {
			n := runtimeNumCPU()
			if n < 1 {
				return 1
			}
			return n
		},
		dialLocalIP: dialLocalIP,
		now:         time.Now,
	}
}

// runtimeNumCPU ist ausgelagert, damit metrics_test.go es nicht faken muss.
func runtimeNumCPU() int { return numCPUImpl() }

func ptrF(v float64) *float64 { return &v }
func ptrS(v string) *string   { return &v }
func ptrI(v int) *int         { return &v }

func (r *reader) cpuTempC() *float64 {
	data, err := r.readFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return nil
	}
	milli, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}
	return ptrF(float64(milli) / 1000.0)
}

func (r *reader) cpuLoadPct() *float64 {
	load1, err := r.loadavg()
	if err != nil {
		return nil
	}
	pct := load1 / float64(r.numCPU()) * 100
	if pct > 100 {
		pct = 100
	}
	return ptrF(round1(pct))
}

func parseMemUsedPct(content string) *float64 {
	var total, available uint64
	for _, line := range strings.Split(content, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = kb
		case "MemAvailable":
			available = kb
		}
	}
	if total == 0 {
		return nil
	}
	return ptrF(round1(float64(total-available) / float64(total) * 100))
}

func (r *reader) ramUsedPct() *float64 {
	data, err := r.readFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	return parseMemUsedPct(string(data))
}

func (r *reader) diskUsedPct() *float64 {
	blocks, bfree, frsize, err := r.statfs("/")
	if err != nil || blocks == 0 {
		return nil
	}
	total := blocks * frsize
	free := bfree * frsize
	return ptrF(round1(float64(total-free) / float64(total) * 100))
}

func parseWirelessDBm(content, iface string) *float64 {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return nil
	}
	for _, line := range lines[2:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if strings.TrimSuffix(fields[0], ":") != iface {
			continue
		}
		// Spalte 3 (0-basiert) ist der Signalpegel, wie in read_wifi_signal_dbm.
		v, err := strconv.ParseFloat(strings.TrimSuffix(fields[3], "."), 64)
		if err != nil {
			return nil
		}
		return ptrF(v)
	}
	return nil
}

func (r *reader) wifiSignalDBm() *float64 {
	data, err := r.readFile("/proc/net/wireless")
	if err != nil {
		return nil
	}
	return parseWirelessDBm(string(data), "wlan0")
}

func (r *reader) lastBootISO() *string {
	data, err := r.readFile("/proc/uptime")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return nil
	}
	up, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil
	}
	boot := r.now().Add(-time.Duration(up) * time.Second)
	return ptrS(boot.Format("2006-01-02T15:04:05-0700"))
}

func parseThrottled(raw string) (now bool, occurred bool) {
	_, hex, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok {
		return false, false
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(hex), "0x"), 16, 64)
	if err != nil {
		return false, false
	}
	nowBit := value&0x1 != 0 || value&0x4 != 0
	occurredBit := value&0x10000 != 0 || value&0x40000 != 0
	return nowBit, occurredBit
}

func (r *reader) throttled(ctx context.Context) (now, occurred bool) {
	out, err := r.run(ctx, "vcgencmd", "get_throttled")
	if err != nil {
		return false, false
	}
	return parseThrottled(string(out))
}

func dialLocalIP() string {
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func parseAptUpgradable(stdout string) int {
	n := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "/") {
			n++
		}
	}
	return n
}

func (r *reader) aptUpdatesPending(ctx context.Context) *int {
	nowT := r.now()
	if r.aptValue != nil && nowT.Sub(r.aptTS) < aptUpdatesTTL {
		return r.aptValue
	}
	out, err := r.run(ctx, "apt", "list", "--upgradable")
	if err != nil {
		return r.aptValue // fehlgeschlagener Aufruf wird nicht gecacht
	}
	v := parseAptUpgradable(string(out))
	r.aptTS = nowT
	r.aptValue = &v
	return &v
}

func (r *reader) FastState(ctx context.Context) FastState {
	nowBit, occurredBit := r.throttled(ctx)
	return FastState{
		CPUTempC:             r.cpuTempC(),
		CPULoadPct:           r.cpuLoadPct(),
		RAMUsedPct:           r.ramUsedPct(),
		DiskUsedPct:          r.diskUsedPct(),
		WiFiSignalDBm:        r.wifiSignalDBm(),
		LastBoot:             r.lastBootISO(),
		UndervoltageNow:      nowBit,
		UndervoltageOccurred: occurredBit,
		ThrottledNow:         nowBit,
	}
}

func (r *reader) SlowDiagnostics(ctx context.Context, tailscaleBin string) SlowDiagnostics {
	var ip *string
	if v := r.dialLocalIP(); v != "" {
		ip = &v
	}
	return SlowDiagnostics{
		IPAddress:          ip,
		MosquittoActive:    systemactions.ServiceIsActive(ctx, systemactions.ExecOutputRunner{}, "mosquitto") == "active",
		TailscaleConnected: r.tailscaleConnected(ctx, tailscaleBin),
		AptUpdatesPending:  r.aptUpdatesPending(ctx),
	}
}

func (r *reader) tailscaleConnected(ctx context.Context, bin string) bool {
	if bin == "" {
		bin = "tailscale"
	}
	out, err := r.run(ctx, bin, "status", "--json")
	if err != nil {
		return false
	}
	var parsed struct {
		Self struct {
			Online bool `json:"Online"`
		} `json:"Self"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		return false
	}
	return parsed.Self.Online
}

type FastState struct {
	CPUTempC             *float64 `json:"cpu_temp_c"`
	CPULoadPct           *float64 `json:"cpu_load_pct"`
	RAMUsedPct           *float64 `json:"ram_used_pct"`
	DiskUsedPct          *float64 `json:"disk_used_pct"`
	WiFiSignalDBm        *float64 `json:"wifi_signal_dbm"`
	LastBoot             *string  `json:"last_boot"`
	UndervoltageNow      bool     `json:"undervoltage_now"`
	UndervoltageOccurred bool     `json:"undervoltage_occurred"`
	ThrottledNow         bool     `json:"throttled_now"`
}

type SlowDiagnostics struct {
	IPAddress          *string `json:"ip_address"`
	MosquittoActive    bool    `json:"mosquitto_active"`
	TailscaleConnected bool    `json:"tailscale_connected"`
	AptUpdatesPending  *int    `json:"apt_updates_pending"`
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
