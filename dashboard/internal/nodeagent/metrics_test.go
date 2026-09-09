package nodeagent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func fakeReader(files map[string]string, cmds map[string]string) *reader {
	return &reader{
		readFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := name + " " + strings.Join(args, " ")
			if v, ok := cmds[key]; ok {
				return []byte(v), nil
			}
			return nil, fmt.Errorf("no fake for %q", key)
		},
		statfs:      func(string) (uint64, uint64, uint64, error) { return 100, 25, 4096, nil },
		loadavg:     func() (float64, error) { return 0.5, nil },
		numCPU:      func() int { return 4 },
		dialLocalIP: func() string { return "192.168.1.50" },
		now:         func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) },
	}
}

func TestParseThrottled(t *testing.T) {
	cases := []struct {
		raw               string
		wantNow, wantOccd bool
	}{
		{"throttled=0x0", false, false},
		{"throttled=0x1", true, false},
		{"throttled=0x50000", false, true},
		{"throttled=0x50005", true, true},
		{"garbage", false, false},
	}
	for _, c := range cases {
		now, occd := parseThrottled(c.raw)
		if now != c.wantNow || occd != c.wantOccd {
			t.Errorf("parseThrottled(%q) = %v,%v want %v,%v", c.raw, now, occd, c.wantNow, c.wantOccd)
		}
	}
}

func TestParseMemUsedPct(t *testing.T) {
	content := "MemTotal:       1000000 kB\nMemFree:         100000 kB\nMemAvailable:    250000 kB\n"
	got := parseMemUsedPct(content)
	if got == nil || *got != 75.0 {
		t.Fatalf("parseMemUsedPct = %v, want 75.0", got)
	}
	if parseMemUsedPct("nonsense") != nil {
		t.Fatal("parseMemUsedPct(nonsense) != nil")
	}
}

func TestParseWirelessDBm(t *testing.T) {
	content := "Inter-| sta-|   Quality        |   Discarded packets\n" +
		" face | link | level | noise | nwid crypt frag retry misc\n" +
		" wlan0: 0000   54.  -61.  -256        0      0      0      0      0\n"
	got := parseWirelessDBm(content, "wlan0")
	if got == nil || *got != -61.0 {
		t.Fatalf("parseWirelessDBm = %v, want -61", got)
	}
	if parseWirelessDBm(content, "eth0") != nil {
		t.Fatal("parseWirelessDBm(eth0) != nil")
	}
}

func TestParseAptUpgradable(t *testing.T) {
	out := "Listing...\nbash/stable 5.2-1 armhf [upgradable from: 5.1-6]\nvim/stable 2:9.0 armhf [upgradable from: 2:8.2]\n"
	if got := parseAptUpgradable(out); got != 2 {
		t.Fatalf("parseAptUpgradable = %d, want 2", got)
	}
}

func TestFastStateFromFakes(t *testing.T) {
	r := fakeReader(
		map[string]string{
			"/sys/class/thermal/thermal_zone0/temp": "48200\n",
			"/proc/meminfo":                         "MemTotal: 1000000 kB\nMemAvailable: 400000 kB\n",
			"/proc/uptime":                          "3600.0 3000.0\n",
			"/proc/net/wireless":                    "h\nh\n wlan0: 0 54. -55. -256 0 0 0 0 0\n",
		},
		map[string]string{"vcgencmd get_throttled": "throttled=0x50005"},
	)
	fs := r.FastState(context.Background())
	if fs.CPUTempC == nil || *fs.CPUTempC != 48.2 {
		t.Errorf("CPUTempC = %v, want 48.2", fs.CPUTempC)
	}
	if fs.RAMUsedPct == nil || *fs.RAMUsedPct != 60.0 {
		t.Errorf("RAMUsedPct = %v, want 60.0", fs.RAMUsedPct)
	}
	if fs.DiskUsedPct == nil || *fs.DiskUsedPct != 75.0 {
		t.Errorf("DiskUsedPct = %v, want 75.0", fs.DiskUsedPct)
	}
	if !fs.UndervoltageNow || !fs.UndervoltageOccurred || !fs.ThrottledNow {
		t.Errorf("throttled flags = %+v, want all true", fs)
	}
	if fs.LastBoot == nil || *fs.LastBoot != "2026-09-09T11:00:00+0000" {
		t.Errorf("LastBoot = %v, want 2026-09-09T11:00:00+0000", fs.LastBoot)
	}
}

func TestAptUpdatesTTLCache(t *testing.T) {
	calls := 0
	r := newReader()
	r.now = func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) }
	r.run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		calls++
		return []byte("Listing...\na/x 1 armhf [upgradable from: 0]\n"), nil
	}
	if v := r.aptUpdatesPending(context.Background()); v == nil || *v != 1 {
		t.Fatalf("first call = %v", v)
	}
	_ = r.aptUpdatesPending(context.Background())
	if calls != 1 {
		t.Fatalf("apt run called %d times within TTL, want 1", calls)
	}
}
