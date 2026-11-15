package performance

import (
	"math/rand"
	"sync"
	"time"
)

// fakeCores is the core count reported by the fake source.
const fakeCores = 4

// fakeSource returns plausible, cumulative counters that grow on every Read.
// It is meant for demo and smoke runs without a Raspberry Pi.
type fakeSource struct {
	mu          sync.Mutex
	rng         *rand.Rand
	cpuBusy     uint64
	cpuTotal    uint64
	swapIn      uint64
	swapOut     uint64
	unitCPUUsec map[string]uint64
}

// NewFakeSource returns a Source with erfundene, plausible Werte. The same seed
// gives the same sequence of samples.
func NewFakeSource(seed int64) Source {
	return &fakeSource{
		rng:         rand.New(rand.NewSource(seed)),
		unitCPUUsec: map[string]uint64{},
	}
}

func (f *fakeSource) Read(units []string) (Raw, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// One interval of roughly one second on four cores: 400 jiffies total.
	total := uint64(400 + f.rng.Intn(100))
	busy := uint64(120 + f.rng.Intn(180))
	f.cpuTotal += total
	f.cpuBusy += busy
	f.swapIn += uint64(f.rng.Intn(3))
	f.swapOut += uint64(f.rng.Intn(2))

	raw := Raw{
		At:         time.Now(),
		CPUBusy:    f.cpuBusy,
		CPUTotal:   f.cpuTotal,
		Cores:      fakeCores,
		MemTotalKB: 1014560,
		MemAvailKB: uint64(560000 + f.rng.Intn(120000)),
		SwapIn:     f.swapIn,
		SwapOut:    f.swapOut,
		Units:      map[string]UnitRaw{},
	}

	names := append([]string{DashboardUnit}, units...)
	for _, name := range names {
		if _, done := raw.Units[name]; done {
			continue
		}
		f.unitCPUUsec[name] += uint64(10000 + f.rng.Intn(60000))
		raw.Units[name] = UnitRaw{
			CPUUsec:  f.unitCPUUsec[name],
			MemBytes: int64(16<<20 + f.rng.Intn(48<<20)),
			HasMem:   true,
		}
	}
	return raw, nil
}
