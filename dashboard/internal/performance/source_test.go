package performance

import (
	"testing"
)

func TestProcSourceReadsTotalsAndUnits(t *testing.T) {
	src := NewProcSource("testdata/full/proc", "testdata/full/cgroup")
	raw, err := src.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if raw.CPUTotal != 9315 || raw.CPUBusy != 5670 {
		t.Errorf("cpu jiffies = busy %d total %d, want busy 5670 total 9315", raw.CPUBusy, raw.CPUTotal)
	}
	if raw.Cores != 4 {
		t.Errorf("Cores = %d, want 4", raw.Cores)
	}
	if raw.MemTotalKB != 1014560 || raw.MemAvailKB != 612340 {
		t.Errorf("mem = total %d avail %d", raw.MemTotalKB, raw.MemAvailKB)
	}
	if raw.SwapIn != 1200 || raw.SwapOut != 340 {
		t.Errorf("swap = in %d out %d, want 1200 340", raw.SwapIn, raw.SwapOut)
	}
	u, ok := raw.Units["shelly-rpc.service"]
	if !ok {
		t.Fatal("unit shelly-rpc.service missing")
	}
	if u.CPUUsec != 1234567 {
		t.Errorf("CPUUsec = %d, want 1234567", u.CPUUsec)
	}
	if u.MemBytes != 23068672 || !u.HasMem {
		t.Errorf("mem = %d HasMem %v, want 23068672 true", u.MemBytes, u.HasMem)
	}
	self, ok := raw.Units[DashboardUnit]
	if !ok {
		t.Fatalf("unit %s missing", DashboardUnit)
	}
	// utime 530 + stime 120 jiffies at USER_HZ 100
	if self.CPUUsec != 6500000 {
		t.Errorf("self CPUUsec = %d, want 6500000", self.CPUUsec)
	}
}

func TestProcSourceWithoutMemoryController(t *testing.T) {
	src := NewProcSource("testdata/no-memory/proc", "testdata/no-memory/cgroup")
	raw, err := src.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	u, ok := raw.Units["shelly-rpc.service"]
	if !ok {
		t.Fatal("unit shelly-rpc.service missing")
	}
	if u.HasMem {
		t.Error("HasMem = true, want false without memory.current")
	}
	if u.CPUUsec != 1234567 {
		t.Errorf("CPUUsec = %d, want 1234567", u.CPUUsec)
	}
}

func TestProcSourceSkipsMissingUnit(t *testing.T) {
	src := NewProcSource("testdata/missing-unit/proc", "testdata/missing-unit/cgroup")
	raw, err := src.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := raw.Units["shelly-rpc.service"]; ok {
		t.Error("missing unit present in Units")
	}
	if _, ok := raw.Units[DashboardUnit]; !ok {
		t.Errorf("unit %s missing", DashboardUnit)
	}
}

func TestProcSourceFailsWithoutProcStat(t *testing.T) {
	src := NewProcSource("testdata/does-not-exist", "testdata/full/cgroup")
	if _, err := src.Read(nil); err == nil {
		t.Error("Read without /proc/stat: want error")
	}
}

func TestFakeSourceIsDeterministicAndGrows(t *testing.T) {
	a := NewFakeSource(7)
	b := NewFakeSource(7)
	first, err := a.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	second, err := b.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if first.CPUTotal != second.CPUTotal || first.Units["shelly-rpc.service"].CPUUsec != second.Units["shelly-rpc.service"].CPUUsec {
		t.Error("same seed gives different samples")
	}
	next, err := a.Read([]string{"shelly-rpc.service"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if next.CPUTotal <= first.CPUTotal {
		t.Errorf("CPUTotal did not grow: %d then %d", first.CPUTotal, next.CPUTotal)
	}
	if next.Cores != 4 {
		t.Errorf("Cores = %d, want 4", next.Cores)
	}
	if _, ok := next.Units[DashboardUnit]; !ok {
		t.Errorf("unit %s missing", DashboardUnit)
	}
}
