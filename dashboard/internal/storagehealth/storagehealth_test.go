package storagehealth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// impliedEnduranceBounds mirrors calibrateLocked's math at runtime float64
// precision, so expected values in calibration tests don't drift from the
// implementation due to Go's arbitrary-precision constant-expression folding.
func impliedEnduranceBounds(t *testing.T, hostWritesBytes uint64, lifeTimeCode int) (min, max float64) {
	t.Helper()
	const epsilon = 0.01
	lowFraction := float64(lifeTimeCode-1) / 10
	highFraction := float64(lifeTimeCode) / 10
	if lowFraction < epsilon {
		lowFraction = epsilon
	}
	bytesWritten := float64(hostWritesBytes)
	return bytesWritten / highFraction, bytesWritten / lowFraction
}

func TestParseEXTCSD(t *testing.T) {
	parsed, err := ParseEXTCSD(`
DEVICE_LIFE_TIME_EST_TYP_A [268]: 0x04
DEVICE_LIFE_TIME_EST_TYP_B [269]: 0x05
PRE_EOL_INFO [267]: 0x01
`)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.LifeTimeA == nil || parsed.LifeTimeA.Label != "30-40 % des Ausdauerbudgets verbraucht" {
		t.Fatalf("unexpected lifetime A: %#v", parsed.LifeTimeA)
	}
	if parsed.LifeTimeB == nil || parsed.LifeTimeB.Code != 5 {
		t.Fatalf("unexpected lifetime B: %#v", parsed.LifeTimeB)
	}
	if parsed.PreEOL == nil || parsed.PreEOL.Label != "normal" {
		t.Fatalf("unexpected pre-EOL: %#v", parsed.PreEOL)
	}
}

func TestParseEXTCSDFailsWithoutSupportedFields(t *testing.T) {
	if _, err := ParseEXTCSD("CARD_TYPE [196]: 0x57"); err == nil {
		t.Fatal("expected missing health fields to fail")
	}
}

func TestOSProviderReadsSysfsAndResolvesMMCParent(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03 0x04\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x02\n",
	}
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils should not be needed")
		},
		now: func() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) },
	}

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Available || report.Device != "/dev/mmcblk0" || report.Source != "Linux-Sysfs" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.LifeTimeA == nil || report.LifeTimeB == nil || report.PreEOL == nil || !strings.Contains(report.LifeTimeA.Label, "20-30") || !strings.Contains(report.LifeTimeB.Label, "30-40") {
		t.Fatalf("unexpected health values: %#v", report)
	}
}

func TestOSProviderReportsUnsupportedMediumWithoutError(t *testing.T) {
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			if path == "/proc/self/mountinfo" {
				return []byte("36 25 8:2 / / rw,relatime - ext4 /dev/sda2 rw\n"), nil
			}
			return nil, errors.New("not found")
		},
		run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("not available") },
		now: time.Now,
	}

	report, err := provider.Check(context.Background())
	if err != nil || report.Available || report.Reason == "" {
		t.Fatalf("unexpected unsupported report: %#v, err %v", report, err)
	}
}

func TestOSProviderEstimatesSDLifeFromWriteRate(t *testing.T) {
	current := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/stat":         "1 2 3 4 5 6 7 8 9 10 11\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
		"/sys/class/block/mmcblk0/device/type":         "SD\n",
		"/sys/class/block/mmcblk0/device/life_time":    "invalid\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "invalid\n",
	}
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := current[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils unavailable")
		},
		now: func() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) },
	}
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
	provider.loaded = true

	// Seven sectors per day at 512 bytes per sector yields a deterministic estimate.
	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Available || report.Mode != "estimated" || report.Estimate == nil {
		t.Fatalf("unexpected SD estimate report: %#v", report)
	}
	if report.Estimate.HostWritesBytes != 7*512 || report.Estimate.ObservationDays != 1 {
		t.Fatalf("unexpected estimate values: %#v", report.Estimate)
	}
	if report.Estimate.AssumedEnduranceTBWMin != 1 || report.Estimate.AssumedEnduranceTBWMax != 3 {
		t.Fatalf("unexpected endurance assumptions: %#v", report.Estimate)
	}
}

func TestOSProviderFlushPersistsStateAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	current := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/type":         "SD\n",
		"/sys/class/block/mmcblk0/device/life_time":    "invalid\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "invalid\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	currentTime := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	readFile := func(path string) ([]byte, error) {
		value, ok := current[path]
		if !ok {
			return nil, errors.New("not found")
		}
		return []byte(value), nil
	}
	newProvider := func() *OSProvider {
		provider := New(dataDir).(*OSProvider)
		provider.readFile = readFile
		provider.run = func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils unavailable")
		}
		provider.now = func() time.Time { return currentTime }
		return provider
	}

	provider := newProvider()
	if _, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	current["/sys/class/block/mmcblk0/stat"] = "1 2 3 4 5 6 14 8 9 10 11\n"
	current["/proc/uptime"] = "86410.00 100.00\n"
	currentTime = currentTime.Add(10 * time.Minute)
	if report, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	} else if report.Estimate == nil {
		t.Fatalf("expected estimate before restart: %#v", report)
	}
	if err := provider.Flush(); err != nil {
		t.Fatal(err)
	}
	persistedAt := provider.state.LastPersistedAt
	currentTime = currentTime.Add(time.Hour)
	if err := provider.Flush(); err != nil {
		t.Fatal(err)
	}
	if !provider.state.LastPersistedAt.Equal(persistedAt) {
		t.Fatal("flush persisted unchanged state a second time")
	}

	// Simulate an actual host reboot: the kernel resets both the per-device
	// sector-write counter and /proc/uptime back down near zero, even though
	// this is still the same physical card.
	current["/sys/class/block/mmcblk0/stat"] = "1 2 3 4 5 6 2 8 9 10 11\n"
	current["/proc/uptime"] = "10.00 1.00\n"
	restartedProvider := newProvider()
	report, err := restartedProvider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Before the reboot: (14-7) = 7 sectors' worth of deltas were accumulated.
	// After the simulated reboot: 2 more sectors written since the counter restarted.
	wantAccumulatedBytes := (14-7)*512.0 + 2*512.0
	if report.Estimate == nil || float64(report.Estimate.HostWritesBytes) != wantAccumulatedBytes {
		t.Fatalf("reboot must not lose previously accumulated host writes: %#v, want %v bytes", report.Estimate, wantAccumulatedBytes)
	}
	if restartedProvider.state.AccumulatedHostWriteBytes != wantAccumulatedBytes || restartedProvider.state.AccumulatedUptimeSeconds != 86420 {
		t.Fatalf("unexpected restored state: %#v", restartedProvider.state)
	}
}

func TestOSProviderFlushKeepsDirtyStateAfterPersistFailure(t *testing.T) {
	blockedPath := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blockedPath, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	previousPersistedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		now:       func() time.Time { return previousPersistedAt.Add(time.Hour) },
		statePath: filepath.Join(blockedPath, "storage-health.json"),
		state: estimateState{
			Device:                    "/dev/mmcblk0",
			LastPersistedAt:           previousPersistedAt,
			LastObservedAt:            previousPersistedAt,
			FirstObservedAt:           previousPersistedAt,
			AccumulatedHostWriteBytes: 3584,
			LastSectors:               14,
			LastUptimeSeconds:         86410,
		},
		dirty: true,
	}

	if err := provider.Flush(); err == nil {
		t.Fatal("expected persist failure")
	}
	if !provider.dirty {
		t.Fatal("persist failure cleared dirty state")
	}
	if !provider.state.LastPersistedAt.Equal(previousPersistedAt) {
		t.Fatal("persist failure changed last persisted timestamp")
	}
}

func TestOSProviderTracksWriteRateWhileMeasured(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x01\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils should not be needed")
		},
		now: func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) },
	}
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	provider.loaded = true

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "measured" || report.Estimate != nil {
		t.Fatalf("expected measured mode without a surfaced estimate: %#v", report)
	}
	if provider.state.Calibration == nil || provider.state.Calibration.SampleCount != 1 {
		t.Fatalf("expected background tracking to record a calibration sample: %#v", provider.state.Calibration)
	}
	// life_time code 3 -> consumed fraction band [0.2, 0.3); hostWrites = 7 sectors * 512 bytes.
	wantMin, wantMax := impliedEnduranceBounds(t, 7*512, 3)
	if provider.state.Calibration.EnduranceMinBytes != wantMin || provider.state.Calibration.EnduranceMaxBytes != wantMax {
		t.Fatalf("unexpected calibration bounds: %#v", provider.state.Calibration)
	}
	if provider.state.Calibration.LastLifeTimeCode != 3 {
		t.Fatalf("unexpected calibration source code: %#v", provider.state.Calibration)
	}
}

func TestOSProviderCalibrationRunningAverageAcrossSamples(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x01\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	currentTime := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils should not be needed")
		},
		now: func() time.Time { return currentTime },
	}
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: currentTime.Add(-24 * time.Hour)}
	provider.loaded = true

	if _, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantMin, wantMax := impliedEnduranceBounds(t, 7*512, 3)
	if provider.state.Calibration.SampleCount != 1 || provider.state.Calibration.EnduranceMinBytes != wantMin || provider.state.Calibration.EnduranceMaxBytes != wantMax {
		t.Fatalf("unexpected calibration after first sample: %#v", provider.state.Calibration)
	}

	files["/sys/class/block/mmcblk0/stat"] = "1 2 3 4 5 6 14 8 9 10 11\n"
	files["/sys/class/block/mmcblk0/device/life_time"] = "0x05\n"
	files["/proc/uptime"] = "86500.00 100.00\n"
	currentTime = currentTime.Add(time.Hour)

	if _, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondMin, secondMax := impliedEnduranceBounds(t, 14*512, 5)
	wantAvgMin := (wantMin + secondMin) / 2
	wantAvgMax := (wantMax + secondMax) / 2
	if provider.state.Calibration.SampleCount != 2 {
		t.Fatalf("expected a second calibration sample, got %#v", provider.state.Calibration)
	}
	if provider.state.Calibration.EnduranceMinBytes != wantAvgMin || provider.state.Calibration.EnduranceMaxBytes != wantAvgMax {
		t.Fatalf("expected calibration to refine as a running average: got %#v, want min %v max %v", provider.state.Calibration, wantAvgMin, wantAvgMax)
	}
}

func TestOSProviderCalibrationIgnoresBudgetExceededCode(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x0b\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x01\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils should not be needed")
		},
		now: func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) },
	}
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	provider.loaded = true

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.LifeTimeA == nil || report.LifeTimeA.Code != 11 {
		t.Fatalf("expected life_time code 11 to be parsed: %#v", report.LifeTimeA)
	}
	if provider.state.Calibration != nil {
		t.Fatalf("expected budget-exceeded code to be ignored for calibration: %#v", provider.state.Calibration)
	}
}

func TestOSProviderCalibratedEnduranceAppliesToDisplayedEstimate(t *testing.T) {
	current := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
		"/sys/class/block/mmcblk0/device/type":         "SD\n",
		"/sys/class/block/mmcblk0/device/life_time":    "invalid\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "invalid\n",
	}
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := current[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils unavailable")
		},
		now: func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) },
	}
	provider.state = estimateState{
		Device:          "/dev/mmcblk0",
		FirstObservedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Calibration:     &calibrationState{SampleCount: 1, EnduranceMinBytes: 500_000_000_000.0, EnduranceMaxBytes: 1_500_000_000_000.0},
	}
	provider.loaded = true

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Available || report.Mode != "estimated" || report.Estimate == nil {
		t.Fatalf("unexpected estimated report: %#v", report)
	}
	if report.Estimate.AssumedEnduranceTBWMin != 0.5 || report.Estimate.AssumedEnduranceTBWMax != 1.5 {
		t.Fatalf("expected calibrated endurance bounds, got %#v", report.Estimate)
	}
	wantMin, wantMax, wantLabel := consumedPercent(7*512.0, 500_000_000_000.0, 1_500_000_000_000.0)
	if report.Estimate.ConsumedPercentMin != wantMin || report.Estimate.ConsumedPercentMax != wantMax || report.Estimate.ConsumedLabel != wantLabel {
		t.Fatalf("unexpected consumed-percent fields: %#v", report.Estimate)
	}
}

func TestConsumedPercentFormatting(t *testing.T) {
	cases := []struct {
		name              string
		hostWritesBytes   float64
		enduranceMinBytes float64
		enduranceMaxBytes float64
		wantMin, wantMax  float64
		wantLabel         string
	}{
		{name: "zero bytes written", hostWritesBytes: 0, enduranceMinBytes: 1_000_000_000_000, enduranceMaxBytes: 3_000_000_000_000, wantMin: 0, wantMax: 0, wantLabel: "ca. 0-0 % des Ausdauerbudgets verbraucht"},
		{name: "clamped above endurance", hostWritesBytes: 5_000_000_000_000, enduranceMinBytes: 1_000_000_000_000, enduranceMaxBytes: 3_000_000_000_000, wantMin: 100, wantMax: 100, wantLabel: "ca. 100-100 % des Ausdauerbudgets verbraucht"},
		{name: "normal range", hostWritesBytes: 500_000_000_000, enduranceMinBytes: 1_000_000_000_000, enduranceMaxBytes: 2_000_000_000_000, wantMin: 25, wantMax: 50, wantLabel: "ca. 25-50 % des Ausdauerbudgets verbraucht"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			min, max, label := consumedPercent(testCase.hostWritesBytes, testCase.enduranceMinBytes, testCase.enduranceMaxBytes)
			if min != testCase.wantMin || max != testCase.wantMax || label != testCase.wantLabel {
				t.Fatalf("consumedPercent(%v, %v, %v) = (%v, %v, %q), want (%v, %v, %q)", testCase.hostWritesBytes, testCase.enduranceMinBytes, testCase.enduranceMaxBytes, min, max, label, testCase.wantMin, testCase.wantMax, testCase.wantLabel)
			}
		})
	}
}

func TestOSProviderLoadsLegacyStateWithoutCalibrationKey(t *testing.T) {
	dataDir := t.TempDir()
	legacyState := `{
  "device": "/dev/mmcblk0",
  "first_observed_at": "2026-08-01T12:00:00Z",
  "initial_sectors": 0,
  "last_observed_at": "2026-08-03T12:00:00Z",
  "last_sectors": 7,
  "accumulated_uptime_seconds": 86400,
  "last_uptime_seconds": 86400,
  "last_persisted_at": "2026-08-03T12:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(dataDir, "storage-health.json"), []byte(legacyState), 0600); err != nil {
		t.Fatal(err)
	}

	current := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 14 8 9 10 11\n",
		"/proc/uptime":                                 "86500.00 100.00\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x01\n",
	}
	provider := New(dataDir).(*OSProvider)
	provider.readFile = func(path string) ([]byte, error) {
		value, ok := current[path]
		if !ok {
			return nil, errors.New("not found")
		}
		return []byte(value), nil
	}
	provider.run = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("mmc-utils should not be needed")
	}
	provider.now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) }

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "measured" {
		t.Fatalf("expected legacy state to load and measured mode to win: %#v", report)
	}
	if provider.state.Calibration == nil || provider.state.Calibration.SampleCount != 1 {
		t.Fatalf("expected a fresh calibration sample built on top of the legacy state: %#v", provider.state.Calibration)
	}
	if provider.state.AccumulatedHostWriteBytes != (14-7)*512.0 || provider.state.AccumulatedUptimeSeconds != 86500 {
		t.Fatalf("legacy fields were not carried over correctly: %#v", provider.state)
	}
}

func TestOSProviderRebootDoesNotResetAccumulatedWrites(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/type":         "SD\n",
		"/sys/class/block/mmcblk0/device/life_time":    "invalid\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "invalid\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	currentTime := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils unavailable")
		},
		now: func() time.Time { return currentTime },
	}
	firstObservedAt := currentTime.Add(-24 * time.Hour)
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: firstObservedAt}
	provider.loaded = true

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Estimate == nil || report.Estimate.HostWritesBytes != 7*512 {
		t.Fatalf("unexpected estimate before reboot: %#v", report.Estimate)
	}

	// Simulate a reboot: the kernel's per-device write counter and /proc/uptime
	// both restart near zero, even though this is still the same physical card
	// (no CID is available here either, mirroring hosts where it can't be read).
	files["/sys/class/block/mmcblk0/stat"] = "1 2 3 4 5 6 2 8 9 10 11\n"
	files["/proc/uptime"] = "5.00 1.00\n"
	currentTime = currentTime.Add(time.Minute)

	report, err = provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !provider.state.FirstObservedAt.Equal(firstObservedAt) {
		t.Fatalf("reboot must not reset the original observation start: got %v, want %v", provider.state.FirstObservedAt, firstObservedAt)
	}
	wantAccumulatedBytes := 7*512.0 + 2*512.0
	if provider.state.AccumulatedHostWriteBytes != wantAccumulatedBytes {
		t.Fatalf("reboot must accumulate rather than reset host writes: got %v, want %v", provider.state.AccumulatedHostWriteBytes, wantAccumulatedBytes)
	}
	if report.Estimate == nil || float64(report.Estimate.HostWritesBytes) != wantAccumulatedBytes {
		t.Fatalf("unexpected estimate after reboot: %#v", report.Estimate)
	}
}

func TestOSProviderDeviceSwapResetsAccumulation(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/type":         "SD\n",
		"/sys/class/block/mmcblk0/device/life_time":    "invalid\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "invalid\n",
		"/sys/class/block/mmcblk0/device/cid":          "old-card-cid\n",
		"/sys/class/block/mmcblk0/stat":                "1 2 3 4 5 6 7 8 9 10 11\n",
		"/proc/uptime":                                 "86400.00 100.00\n",
	}
	currentTime := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("mmc-utils unavailable")
		},
		now: func() time.Time { return currentTime },
	}
	provider.state = estimateState{Device: "/dev/mmcblk0", FirstObservedAt: currentTime.Add(-24 * time.Hour)}
	provider.loaded = true

	if _, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.state.DeviceFingerprint != "old-card-cid" || provider.state.AccumulatedHostWriteBytes != 7*512.0 {
		t.Fatalf("unexpected state before swap: %#v", provider.state)
	}

	// Simulate swapping in a different physical card in the same slot: a new
	// CID and a low sector count that, taken alone, would look just like a reboot.
	files["/sys/class/block/mmcblk0/device/cid"] = "new-card-cid\n"
	files["/sys/class/block/mmcblk0/stat"] = "1 2 3 4 5 6 1 8 9 10 11\n"
	currentTime = currentTime.Add(time.Hour)

	report, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.state.DeviceFingerprint != "new-card-cid" {
		t.Fatalf("expected fingerprint to switch to the new card: %#v", provider.state)
	}
	if !provider.state.FirstObservedAt.Equal(currentTime) {
		t.Fatalf("device swap should restart the observation window: %#v", provider.state)
	}
	if provider.state.AccumulatedHostWriteBytes != 0 {
		t.Fatalf("device swap should reset accumulated writes, got %v", provider.state.AccumulatedHostWriteBytes)
	}
	if report.Estimate != nil {
		t.Fatalf("expected no estimate immediately after a device swap: %#v", report.Estimate)
	}
}

// TestCheckIsServedFromCacheWithinTTL: /api/v1/health wird alle 30 s pro
// offenem Tab abgefragt, Check() braucht am Pi 489 ms (sysfs, /proc und im
// Zweifel "mmc extcsd read" mit 2 s Timeout). Die Daten dahinter aendern sich
// in Tagen. Der Cache ist traege und fuellt sich beim ersten Zugriff; ein
// Neustart des Dienstes leert ihn, was genau richtig ist.
func TestCheckIsServedFromCacheWithinTTL(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03 0x04\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x02\n",
	}
	reads := 0
	current := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			reads++
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("mmc-utils must not run for a cached check")
			return nil, nil
		},
		now: func() time.Time { return current },
		ttl: time.Minute,
	}

	first, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterFirst := reads
	if afterFirst == 0 {
		t.Fatal("the first check did not read anything")
	}

	current = current.Add(59 * time.Second)
	second, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reads != afterFirst {
		t.Fatalf("a check within the TTL read %d more files, want 0", reads-afterFirst)
	}
	if second.Device != first.Device || second.ReadAt != first.ReadAt {
		t.Fatalf("cached report differs: %#v vs %#v", second, first)
	}

	current = current.Add(2 * time.Second) // 61 s nach dem ersten Check
	if _, err := provider.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reads == afterFirst {
		t.Fatal("a check after the TTL did not re-read")
	}
}

// TestCachedReportDoesNotAliasPointers: Report traegt *Estimate, *LifeTime und
// *PreEOL. Die werden bei jedem Check frisch alloziert und danach nie mehr
// veraendert - deshalb ist die flache Kopie im Cache sicher. Weil das eine
// Eigenschaft des Codes ist und nicht des Typs, haelt dieser Test sie am
// Leben: wer die Zusicherung bricht, sieht es hier.
func TestCachedReportDoesNotAliasPointers(t *testing.T) {
	files := map[string]string{
		"/proc/self/mountinfo":                         "36 25 179:2 / / rw,relatime - ext4 /dev/mmcblk0p2 rw\n",
		"/sys/class/block/mmcblk0/device/life_time":    "0x03 0x04\n",
		"/sys/class/block/mmcblk0/device/pre_eol_info": "0x02\n",
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	provider := &OSProvider{
		readFile: func(path string) ([]byte, error) {
			value, ok := files[path]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(value), nil
		},
		run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("not available") },
		now: func() time.Time { return now },
		ttl: time.Minute,
	}

	first, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.LifeTimeA == nil {
		t.Fatalf("expected a life_time value: %#v", first)
	}
	wantCode := first.LifeTimeA.Code
	first.LifeTimeA.Code = 9999

	second, err := provider.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.LifeTimeA == nil || second.LifeTimeA.Code != wantCode {
		t.Fatalf("the cached report was mutated through the first caller's pointer: %#v", second.LifeTimeA)
	}
}
