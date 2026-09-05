// Package storagehealth reads health information reported by the root storage medium.
package storagehealth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Provider interface {
	Check(context.Context) (Report, error)
}

type Flusher interface {
	Flush() error
}

type Report struct {
	Available  bool      `json:"available"`
	Device     string    `json:"device,omitempty"`
	Medium     string    `json:"medium,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Confidence string    `json:"confidence,omitempty"`
	Source     string    `json:"source,omitempty"`
	ReadAt     time.Time `json:"read_at,omitempty"`
	LifeTimeA  *LifeTime `json:"life_time_a,omitempty"`
	LifeTimeB  *LifeTime `json:"life_time_b,omitempty"`
	PreEOL     *PreEOL   `json:"pre_eol,omitempty"`
	Estimate   *Estimate `json:"estimate,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

type Estimate struct {
	HostWritesBytes        uint64  `json:"host_writes_bytes"`
	HostWritesPerDayBytes  float64 `json:"host_writes_per_day_bytes"`
	ObservationDays        float64 `json:"observation_days"`
	SystemUptimeSeconds    float64 `json:"system_uptime_seconds"`
	AssumedEnduranceTBWMin float64 `json:"assumed_endurance_tbw_min"`
	AssumedEnduranceTBWMax float64 `json:"assumed_endurance_tbw_max"`
	RemainingDaysMin       float64 `json:"remaining_days_min,omitempty"`
	RemainingDaysMax       float64 `json:"remaining_days_max,omitempty"`
	RemainingLabel         string  `json:"remaining_label"`
	ConsumedPercentMin     float64 `json:"consumed_percent_min,omitempty"`
	ConsumedPercentMax     float64 `json:"consumed_percent_max,omitempty"`
	ConsumedLabel          string  `json:"consumed_label,omitempty"`
	Method                 string  `json:"method"`
}

type LifeTime struct {
	Code  int    `json:"code"`
	Label string `json:"label"`
}

type PreEOL struct {
	Code  int    `json:"code"`
	Label string `json:"label"`
}

type OSProvider struct {
	readFile  func(string) ([]byte, error)
	run       func(context.Context, string, ...string) ([]byte, error)
	now       func() time.Time
	statePath string
	mu        sync.Mutex
	state     estimateState
	loaded    bool
	dirty     bool

	// Check liest /proc und sysfs und ruft im Zweifel "mmc extcsd read" mit
	// 2 s Timeout - am Pi gemessene 489 ms, alle 30 s, pro offenem Tab. Die
	// Daten dahinter aendern sich in Tagen. Bewusst kein Hintergrund-Poll und
	// keine Goroutine: der Cache ist traege, fuellt sich beim ersten Zugriff
	// und ist nach einem Neustart des Dienstes leer - genau richtig.
	ttl           time.Duration
	lastReport    Report
	lastCheckedAt time.Time
}

// defaultCheckTTL ist die Vorgabe; Tests injizieren ttl direkt.
const defaultCheckTTL = time.Minute

func New(dataDirs ...string) Provider {
	provider := &OSProvider{
		readFile: os.ReadFile,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
		now: time.Now,
		ttl: defaultCheckTTL,
	}
	if len(dataDirs) > 0 && strings.TrimSpace(dataDirs[0]) != "" {
		provider.statePath = filepath.Join(filepath.Clean(dataDirs[0]), "storage-health.json")
	}
	return provider
}

type estimateState struct {
	Device string `json:"device"`
	// DeviceFingerprint identifies the physical medium (from its CID register)
	// so a genuine card swap can be told apart from a reboot: both make the
	// kernel's sector-write counter drop back to near zero, but only a swap
	// should discard the accumulated history below.
	DeviceFingerprint         string            `json:"device_fingerprint,omitempty"`
	FirstObservedAt           time.Time         `json:"first_observed_at"`
	LastObservedAt            time.Time         `json:"last_observed_at"`
	LastSectors               uint64            `json:"last_sectors"`
	AccumulatedHostWriteBytes float64           `json:"accumulated_host_write_bytes"`
	AccumulatedUptimeSeconds  float64           `json:"accumulated_uptime_seconds"`
	LastUptimeSeconds         float64           `json:"last_uptime_seconds"`
	LastPersistedAt           time.Time         `json:"last_persisted_at"`
	Calibration               *calibrationState `json:"calibration,omitempty"`
}

// calibrationState tracks a running average of the storage medium's implied
// total write endurance, derived whenever vendor-measured wear data (life_time)
// is available alongside our own host-write tracking for the same device. It
// lets the write-rate-based estimate (used only when no vendor data exists)
// converge toward reality instead of relying solely on the conservative
// hardcoded 1-3 TBW assumption.
type calibrationState struct {
	SampleCount       int       `json:"sample_count"`
	EnduranceMinBytes float64   `json:"endurance_min_bytes"`
	EnduranceMaxBytes float64   `json:"endurance_max_bytes"`
	LastLifeTimeCode  int       `json:"last_life_time_code,omitempty"`
	LastCalibratedAt  time.Time `json:"last_calibrated_at,omitempty"`
}

// Check liefert den Bericht aus dem TTL-Cache, solange der Eintrag jung genug
// ist, und laesst /proc, sysfs und mmc-utils dann unberuehrt.
func (p *OSProvider) Check(ctx context.Context) (Report, error) {
	if cached, ok := p.cachedReport(); ok {
		return cached, nil
	}
	report, err := p.check(ctx)
	if err != nil {
		return report, err
	}
	p.storeReport(report)
	return report, nil
}

func (p *OSProvider) cachedReport() (Report, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ttl <= 0 || p.lastCheckedAt.IsZero() {
		return Report{}, false
	}
	if p.now().UTC().Sub(p.lastCheckedAt) >= p.ttl {
		return Report{}, false
	}
	return cloneReport(p.lastReport), true
}

func (p *OSProvider) storeReport(report Report) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lastReport = cloneReport(report)
	p.lastCheckedAt = p.now().UTC()
}

// cloneReport kopiert die drei Zeigerfelder mit. Die Werte dahinter werden bei
// jedem Check frisch alloziert und danach nie mehr veraendert - eine flache
// Kopie waere also *heute* sicher. Der Aufwand hier ist trotzdem gerechtfertigt:
// die Zusicherung ist eine Eigenschaft des Codes, nicht des Typs, und ein
// Aufrufer, der am zurueckgegebenen Report herumschreibt, wuerde sonst den
// Cache aller folgenden Abfragen vergiften.
func cloneReport(report Report) Report {
	clone := report
	if report.LifeTimeA != nil {
		value := *report.LifeTimeA
		clone.LifeTimeA = &value
	}
	if report.LifeTimeB != nil {
		value := *report.LifeTimeB
		clone.LifeTimeB = &value
	}
	if report.PreEOL != nil {
		value := *report.PreEOL
		clone.PreEOL = &value
	}
	if report.Estimate != nil {
		value := *report.Estimate
		clone.Estimate = &value
	}
	return clone
}

func (p *OSProvider) check(ctx context.Context) (Report, error) {
	device, err := p.rootMMCDevice()
	if err != nil {
		return unavailableReport("Das Root-Dateisystem liegt nicht auf einem auslesbaren MMC-Medium."), nil
	}

	now := p.now().UTC()
	report := Report{Device: device, Medium: p.medium(device), ReadAt: now}
	sysfsValues := map[string]string{}
	for name, path := range map[string]string{
		"life_a":  filepath.Join("/sys/class/block", filepath.Base(device), "device", "life_time"),
		"pre_eol": filepath.Join("/sys/class/block", filepath.Base(device), "device", "pre_eol_info"),
	} {
		if data, readErr := p.readFile(path); readErr == nil {
			sysfsValues[name] = strings.TrimSpace(string(data))
		}
	}
	if first, second, ok := parseLifeTimes(sysfsValues["life_a"]); ok {
		report.LifeTimeA = &first
		if second.Code > 0 {
			report.LifeTimeB = &second
		}
	}
	if value, ok := parsePreEOL(sysfsValues["pre_eol"]); ok {
		report.PreEOL = &value
	}
	if report.LifeTimeA != nil || report.PreEOL != nil {
		report.Available = true
		report.Mode = "measured"
		report.Source = "Linux-Sysfs"
	} else if report.Medium != "SD-Karte" {
		output, runErr := p.runWithTimeout(ctx, "mmc", "extcsd", "read", device)
		if runErr == nil {
			parsed, parseErr := ParseEXTCSD(string(output))
			if parseErr == nil {
				report.LifeTimeA = parsed.LifeTimeA
				report.LifeTimeB = parsed.LifeTimeB
				report.PreEOL = parsed.PreEOL
				if report.LifeTimeA != nil || report.LifeTimeB != nil || report.PreEOL != nil {
					report.Available = true
					report.Mode = "measured"
					report.Source = "mmc-utils"
				}
			}
		}
	}

	// Runtime/write-rate tracking always runs, regardless of medium or whether
	// vendor-measured data was found above, so that the calibration model keeps
	// accumulating samples. The resulting estimate is only surfaced in the report
	// (and thus shown in the UI) when no vendor-measured data won this check.
	estimate, estimateErr := p.estimate(ctx, device, now, report.LifeTimeA)
	if estimateErr == nil && report.Mode != "measured" {
		report.Available = true
		report.Mode = "estimated"
		report.Confidence = "low"
		report.Source = "Linux-Blockstatistik"
		report.Estimate = estimate
		if estimate != nil {
			return report, nil
		}
	}

	if !report.Available {
		report.Reason = "Das Medium stellt keine verwertbaren Gesundheitsdaten bereit."
	}
	return report, nil
}

func (p *OSProvider) medium(device string) string {
	data, err := p.readFile(filepath.Join("/sys/class/block", filepath.Base(device), "device", "type"))
	if err == nil && strings.EqualFold(strings.TrimSpace(string(data)), "MMC") {
		return "eMMC"
	}
	return "SD-Karte"
}

func (p *OSProvider) rootMMCDevice() (string, error) {
	data, err := p.readFile("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(line, " - ", 2)
		if len(fields) != 2 {
			continue
		}
		mountFields := strings.Fields(fields[0])
		postFields := strings.Fields(fields[1])
		if len(mountFields) < 5 || len(postFields) < 2 || unescape(mountFields[4]) != "/" {
			continue
		}
		source := unescape(postFields[1])
		if strings.HasPrefix(source, "/dev/mmcblk") {
			return mmcParentDevice(source), nil
		}
	}
	return "", errors.New("root device is not MMC")
}

var mmcPartitionPattern = regexp.MustCompile(`^(.*)p[0-9]+$`)

func mmcParentDevice(device string) string {
	base := filepath.Base(device)
	if match := mmcPartitionPattern.FindStringSubmatch(base); len(match) == 2 {
		return filepath.Join(filepath.Dir(device), match[1])
	}
	return device
}

func (p *OSProvider) runWithTimeout(parent context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	return p.run(ctx, name, args...)
}

func (p *OSProvider) estimate(ctx context.Context, device string, now time.Time, measuredLifeTime *LifeTime) (*Estimate, error) {
	sectors, err := p.sectorsWritten(device)
	if err != nil {
		return nil, err
	}
	uptime, err := p.systemUptime()
	if err != nil {
		return nil, err
	}
	fingerprint := p.deviceFingerprint(device)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.loadStateLocked(); err != nil {
		return nil, err
	}

	// A genuine card swap is only recognized when both the previous and the
	// current CID are known and differ. A dropping sector count alone is NOT
	// treated as a swap below, because the kernel resets that counter on
	// every reboot too - conflating the two would silently wipe the
	// accumulated history each time the host restarts.
	deviceChanged := p.state.Device != device || p.state.FirstObservedAt.IsZero() ||
		(fingerprint != "" && p.state.DeviceFingerprint != "" && fingerprint != p.state.DeviceFingerprint)
	if deviceChanged {
		p.state = estimateState{Device: device, DeviceFingerprint: fingerprint, FirstObservedAt: now, LastObservedAt: now, LastSectors: sectors, AccumulatedUptimeSeconds: uptime, LastUptimeSeconds: uptime}
		p.dirty = true
		_ = p.persistStateLocked(now)
		return nil, nil
	}
	if p.state.DeviceFingerprint == "" && fingerprint != "" {
		p.state.DeviceFingerprint = fingerprint
	}

	if sectors >= p.state.LastSectors {
		p.state.AccumulatedHostWriteBytes += float64(sectors-p.state.LastSectors) * 512
	} else {
		// The kernel's per-device write counter restarts near zero on every
		// reboot; a swap was already ruled out above, so treat the sectors
		// written since this restart as newly accumulated rather than
		// resetting the whole observation.
		p.state.AccumulatedHostWriteBytes += float64(sectors) * 512
	}
	p.state.LastSectors = sectors

	if p.state.LastUptimeSeconds == 0 || uptime < p.state.LastUptimeSeconds {
		p.state.AccumulatedUptimeSeconds += uptime
	} else {
		p.state.AccumulatedUptimeSeconds += uptime - p.state.LastUptimeSeconds
	}
	p.state.LastUptimeSeconds = uptime
	p.state.LastObservedAt = now
	p.dirty = true

	if p.state.AccumulatedHostWriteBytes > 0 && measuredLifeTime != nil {
		p.calibrateLocked(measuredLifeTime.Code, uint64(p.state.AccumulatedHostWriteBytes), now)
	}

	if p.state.LastPersistedAt.IsZero() || now.Sub(p.state.LastPersistedAt) >= 6*time.Hour {
		_ = p.persistStateLocked(now)
	}
	operatingDays := p.state.AccumulatedUptimeSeconds / (24 * 60 * 60)
	if operatingDays < 1 {
		return nil, nil
	}

	writesPerDay := p.state.AccumulatedHostWriteBytes / operatingDays
	if writesPerDay <= 0 {
		return nil, nil
	}
	enduranceMinBytes, enduranceMaxBytes := p.enduranceBoundsLocked()
	remainingMin := enduranceMinBytes - p.state.AccumulatedHostWriteBytes
	remainingMax := enduranceMaxBytes - p.state.AccumulatedHostWriteBytes
	if remainingMin < 0 {
		remainingMin = 0
	}
	if remainingMax < 0 {
		remainingMax = 0
	}
	consumedMin, consumedMax, consumedLabel := consumedPercent(p.state.AccumulatedHostWriteBytes, enduranceMinBytes, enduranceMaxBytes)
	method := "Host-Schreiblast seit erster Messung; konservative Annahme 1-3 TBW"
	if p.state.Calibration != nil && p.state.Calibration.SampleCount > 0 {
		method = fmt.Sprintf("Host-Schreiblast seit erster Messung; aus %d Herstellermessung(en) kalibrierte Ausdauerannahme", p.state.Calibration.SampleCount)
	}
	return &Estimate{
		HostWritesBytes:        uint64(p.state.AccumulatedHostWriteBytes),
		HostWritesPerDayBytes:  writesPerDay,
		ObservationDays:        operatingDays,
		SystemUptimeSeconds:    uptime,
		AssumedEnduranceTBWMin: enduranceMinBytes / 1_000_000_000_000.0,
		AssumedEnduranceTBWMax: enduranceMaxBytes / 1_000_000_000_000.0,
		RemainingDaysMin:       remainingMin / writesPerDay,
		RemainingDaysMax:       remainingMax / writesPerDay,
		RemainingLabel:         remainingLabel(remainingMin/writesPerDay, remainingMax/writesPerDay),
		ConsumedPercentMin:     consumedMin,
		ConsumedPercentMax:     consumedMax,
		ConsumedLabel:          consumedLabel,
		Method:                 method,
	}, nil
}

// deviceFingerprint reads the medium's CID register, a hardware identifier
// unique per physical card, used to distinguish a real card swap from a
// reboot (see estimate()). Returns "" if unavailable (missing driver
// support or permissions), in which case device-change detection falls back
// to the device path alone.
func (p *OSProvider) deviceFingerprint(device string) string {
	data, err := p.readFile(filepath.Join("/sys/class/block", filepath.Base(device), "device", "cid"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// calibrateLocked refines the persisted endurance-budget estimate using a
// vendor-reported life-time code observed alongside our own host-write
// tracking for the same device. Code 11 ("budget exceeded") is a one-sided
// lower bound only and is skipped, since it can't be turned into a min/max
// endurance range. Each sample is folded into a running average rather than
// overwriting the previous calibration, so the assumption keeps refining
// over time instead of jumping around on a single noisy reading.
func (p *OSProvider) calibrateLocked(lifeTimeCode int, hostWritesBytes uint64, now time.Time) {
	if lifeTimeCode < 1 || lifeTimeCode > 10 {
		return
	}
	const epsilon = 0.01
	lowFraction := float64(lifeTimeCode-1) / 10
	highFraction := float64(lifeTimeCode) / 10
	if lowFraction < epsilon {
		lowFraction = epsilon
	}
	bytesWritten := float64(hostWritesBytes)
	impliedMax := bytesWritten / lowFraction
	impliedMin := bytesWritten / highFraction

	if p.state.Calibration == nil {
		p.state.Calibration = &calibrationState{}
	}
	c := p.state.Calibration
	if c.SampleCount == 0 {
		c.EnduranceMinBytes = impliedMin
		c.EnduranceMaxBytes = impliedMax
	} else {
		weight := float64(c.SampleCount)
		c.EnduranceMinBytes = (c.EnduranceMinBytes*weight + impliedMin) / (weight + 1)
		c.EnduranceMaxBytes = (c.EnduranceMaxBytes*weight + impliedMax) / (weight + 1)
	}
	c.SampleCount++
	c.LastLifeTimeCode = lifeTimeCode
	c.LastCalibratedAt = now
	p.dirty = true
}

// enduranceBoundsLocked returns the calibrated endurance-budget range if at
// least one calibration sample has been observed, otherwise it falls back to
// the conservative default assumption used before any real measurement was
// available.
func (p *OSProvider) enduranceBoundsLocked() (min, max float64) {
	const (
		defaultMinBytes = 1_000_000_000_000.0
		defaultMaxBytes = 3_000_000_000_000.0
	)
	if p.state.Calibration == nil || p.state.Calibration.SampleCount == 0 {
		return defaultMinBytes, defaultMaxBytes
	}
	return p.state.Calibration.EnduranceMinBytes, p.state.Calibration.EnduranceMaxBytes
}

// consumedPercent formats the share of the assumed endurance budget that has
// already been written, mirroring the style of the vendor-reported life-time
// labels (see lifeTime()).
func consumedPercent(hostWritesBytes, enduranceMinBytes, enduranceMaxBytes float64) (min, max float64, label string) {
	if enduranceMinBytes <= 0 || enduranceMaxBytes <= 0 {
		return 0, 0, ""
	}
	min = hostWritesBytes / enduranceMaxBytes * 100
	max = hostWritesBytes / enduranceMinBytes * 100
	if min < 0 {
		min = 0
	}
	if max < 0 {
		max = 0
	}
	if min > 100 {
		min = 100
	}
	if max > 100 {
		max = 100
	}
	return min, max, fmt.Sprintf("ca. %.0f-%.0f %% des Ausdauerbudgets verbraucht", min, max)
}

func (p *OSProvider) sectorsWritten(device string) (uint64, error) {
	data, err := p.readFile(filepath.Join("/sys/class/block", filepath.Base(device), "stat"))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 7 {
		return 0, errors.New("block statistics are incomplete")
	}
	return strconv.ParseUint(fields[6], 10, 64)
}

func (p *OSProvider) systemUptime() (float64, error) {
	data, err := p.readFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, errors.New("uptime is unavailable")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func (p *OSProvider) loadStateLocked() error {
	if p.loaded || p.statePath == "" {
		p.loaded = true
		return nil
	}
	data, err := os.ReadFile(p.statePath)
	if errors.Is(err, os.ErrNotExist) {
		p.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &p.state); err != nil {
		return err
	}
	p.loaded = true
	return nil
}

func (p *OSProvider) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.dirty {
		return nil
	}
	return p.persistStateLocked(p.now().UTC())
}

func (p *OSProvider) persistStateLocked(now time.Time) error {
	if p.statePath == "" {
		p.state.LastPersistedAt = now
		p.dirty = false
		return nil
	}
	persistedState := p.state
	persistedState.LastPersistedAt = now
	data, err := json.MarshalIndent(persistedState, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(p.statePath), 0750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(p.statePath), ".storage-health-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, p.statePath); err != nil {
		return err
	}
	p.state.LastPersistedAt = now
	p.dirty = false
	return nil
}

func remainingLabel(minDays, maxDays float64) string {
	if maxDays < 365 {
		return fmt.Sprintf("ca. %.0f bis %.0f Tage", minDays, maxDays)
	}
	return fmt.Sprintf("ca. %.1f bis %.1f Jahre", minDays/365, maxDays/365)
}

type ParsedEXTCSD struct {
	LifeTimeA *LifeTime
	LifeTimeB *LifeTime
	PreEOL    *PreEOL
}

func ParseEXTCSD(output string) (ParsedEXTCSD, error) {
	var parsed ParsedEXTCSD
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		code, ok := extcsdCode(line)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(line, "DEVICE_LIFE_TIME_EST_TYP_A"):
			value, valid := lifeTime(code)
			if !valid {
				return ParsedEXTCSD{}, fmt.Errorf("invalid life time A code %d", code)
			}
			parsed.LifeTimeA = &value
		case strings.Contains(line, "DEVICE_LIFE_TIME_EST_TYP_B"):
			value, valid := lifeTime(code)
			if !valid {
				return ParsedEXTCSD{}, fmt.Errorf("invalid life time B code %d", code)
			}
			parsed.LifeTimeB = &value
		case strings.Contains(line, "PRE_EOL_INFO"):
			value, valid := preEOL(code)
			if !valid {
				return ParsedEXTCSD{}, fmt.Errorf("invalid pre-eol code %d", code)
			}
			parsed.PreEOL = &value
		}
	}
	if err := scanner.Err(); err != nil {
		return ParsedEXTCSD{}, err
	}
	if parsed.LifeTimeA == nil && parsed.LifeTimeB == nil && parsed.PreEOL == nil {
		return ParsedEXTCSD{}, errors.New("no supported health fields found")
	}
	return parsed, nil
}

var extcsdCodePattern = regexp.MustCompile(`:\s*0x([0-9a-fA-F]{1,2})\s*$`)

func extcsdCode(line string) (int, bool) {
	match := extcsdCodePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseUint(match[1], 16, 8)
	return int(value), err == nil
}

func parseLifeTime(raw string) (LifeTime, bool) {
	code, ok := parseHexCode(raw)
	if !ok {
		return LifeTime{}, false
	}
	return lifeTime(code)
}

func parseLifeTimes(raw string) (LifeTime, LifeTime, bool) {
	parts := strings.Fields(raw)
	if len(parts) == 0 || len(parts) > 2 {
		return LifeTime{}, LifeTime{}, false
	}
	first, ok := parseLifeTime(parts[0])
	if !ok {
		return LifeTime{}, LifeTime{}, false
	}
	if len(parts) == 1 {
		return first, LifeTime{}, true
	}
	second, ok := parseLifeTime(parts[1])
	if !ok {
		return LifeTime{}, LifeTime{}, false
	}
	return first, second, true
}

func parsePreEOL(raw string) (PreEOL, bool) {
	code, ok := parseHexCode(raw)
	if !ok {
		return PreEOL{}, false
	}
	return preEOL(code)
}

func parseHexCode(raw string) (int, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "0x"))
	value, err := strconv.ParseUint(raw, 16, 8)
	return int(value), err == nil
}

func lifeTime(code int) (LifeTime, bool) {
	switch {
	case code >= 1 && code <= 10:
		return LifeTime{Code: code, Label: fmt.Sprintf("%d-%d %% des Ausdauerbudgets verbraucht", (code-1)*10, code*10)}, true
	case code == 11:
		return LifeTime{Code: code, Label: "Ausdauerbudget überschritten"}, true
	default:
		return LifeTime{}, false
	}
}

func preEOL(code int) (PreEOL, bool) {
	labels := map[int]string{1: "normal", 2: "Warnung", 3: "kritisch"}
	label, ok := labels[code]
	if !ok {
		return PreEOL{}, false
	}
	return PreEOL{Code: code, Label: label}, true
}

func unavailableReport(reason string) Report {
	return Report{Available: false, Reason: reason}
}

func unescape(value string) string {
	value = strings.ReplaceAll(value, `\040`, " ")
	value = strings.ReplaceAll(value, `\011`, "\t")
	value = strings.ReplaceAll(value, `\012`, "\n")
	return value
}
