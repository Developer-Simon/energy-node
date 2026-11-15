package performance

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

// FileState is the scenario that the fake console writes to the state file
// for demo and smoke runs. Field names follow the JSON contract of the console.
type FileState struct {
	Preset     string              `json:"preset"`
	MeasuredS  float64             `json:"measured_s"`
	Cores      int                 `json:"cores"`
	CPUPct     float64             `json:"cpu_pct"`
	TempC      *float64            `json:"temp_c"`
	Throttled  bool                `json:"throttled"`
	RAMFreePct float64             `json:"ram_free_pct"`
	SwapPagesS float64             `json:"swap_pages_s"`
	ClientsMax int                 `json:"clients_max"`
	External   *FileExternal       `json:"external"`
	Units      map[string]FileUnit `json:"units"`
}

// FileExternal is the foreign device that the counter measures.
type FileExternal struct {
	Name   string  `json:"name"`
	EveryS float64 `json:"every_s"`
}

// FileUnit is the load of one unit. Units with Present false are left out of
// the snapshot. RoundS and MsgPerMin only apply to services, not to the
// .service units.
type FileUnit struct {
	Present   bool     `json:"present"`
	CPUPct    float64  `json:"cpu_pct"`
	RAMMB     float64  `json:"ram_mb"`
	RoundS    *float64 `json:"round_s"`
	MsgPerMin float64  `json:"msg_per_min"`
}

// FileProvider serves the Snapshot from the state file. The file is read on
// every call, a missing or unreadable file gives the preset good.
type FileProvider struct {
	path string
}

// NewFileProvider returns a provider for the state file at path.
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Snapshot returns the scenario of the state file as a Snapshot. now is not
// used, the file is the whole window.
func (p *FileProvider) Snapshot(now time.Time) Snapshot {
	return p.state().snapshot()
}

// state reads the state file. Any read or parse error gives the default state.
func (p *FileProvider) state() FileState {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return DefaultFileState()
	}
	var s FileState
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultFileState()
	}
	return s
}

// snapshot converts the scenario into the Snapshot the page reads.
func (s FileState) snapshot() Snapshot {
	clientsMax := s.ClientsMax
	if clientsMax < 1 {
		clientsMax = 1
	}
	snap := Snapshot{
		MeasuredFor:   time.Duration(s.MeasuredS * float64(time.Second)),
		CPUP95:        s.CPUPct,
		Cores:         s.Cores,
		TempC:         s.TempC,
		Throttled:     s.Throttled,
		RAMFreePct:    s.RAMFreePct,
		RAMFreeKnown:  true,
		SwapPagesPerS: s.SwapPagesS,
		ClientsMax:    clientsMax,
		ClientsSeen:   clientsMax > 1,
	}
	if s.External != nil {
		snap.External = &SourceRate{Name: s.External.Name, EveryS: s.External.EveryS}
	}
	for id, u := range s.Units {
		if !u.Present {
			continue
		}
		fixed := isFixedUnit(id)
		st := UnitStats{
			ID:       id,
			CPUP95:   u.CPUPct,
			RAMBytes: int64(u.RAMMB * (1 << 20)),
			HasRAM:   true,
			Fixed:    fixed,
		}
		if !fixed {
			st.RoundS = u.RoundS
			st.MsgPerMin = u.MsgPerMin
		}
		snap.Units = append(snap.Units, st)
	}
	sort.Slice(snap.Units, func(i, j int) bool { return snap.Units[i].ID < snap.Units[j].ID })
	return snap
}

// isFixedUnit reports whether a unit key is a .service unit (Fixed) rather than a service ID.
func isFixedUnit(id string) bool {
	const suffix = ".service"
	return len(id) > len(suffix) && id[len(id)-len(suffix):] == suffix
}

// DefaultFileState is the preset good, used when the state file is missing or unreadable.
func DefaultFileState() FileState {
	return Presets()["good"]
}

// Presets returns the scenarios of the console, keyed by preset name. Each call
// returns fresh copies, so the caller may change them.
func Presets() map[string]FileState {
	good := func() FileState {
		temp := 54.0
		return FileState{
			Preset:     "good",
			MeasuredS:  25200,
			Cores:      4,
			CPUPct:     32.0,
			TempC:      &temp,
			Throttled:  false,
			RAMFreePct: 61.0,
			SwapPagesS: 0.0,
			ClientsMax: 3,
			External:   &FileExternal{Name: "Wärmepumpe", EveryS: 15},
			Units: map[string]FileUnit{
				"shelly":                        serviceUnit(5.0, 22, 0.4, 126),
				"trucki":                        serviceUnit(3.0, 18, 1.2, 40),
				"battery_soc":                   serviceUnit(2.0, 16, 0.1, 60),
				"apsystems":                     serviceUnit(1.0, 20, 3.1, 8),
				"tuya":                          serviceUnit(1.5, 24, 2.4, 12),
				"automation.service":            fixedUnit(4.0, 31),
				"mosquitto.service":             fixedUnit(3.0, 12),
				"tailscaled.service":            fixedUnit(0.5, 40),
				"caddy.service":                 fixedUnit(0.5, 36),
				"energy-node-dashboard.service": fixedUnit(8.0, 70),
			},
		}
	}
	presets := map[string]FileState{}

	measuring := good()
	measuring.Preset = "measuring"
	measuring.MeasuredS = 1800
	presets["measuring"] = measuring

	presets["good"] = good()

	tight := good()
	tight.Preset = "tight"
	tight.CPUPct = 64
	presets["tight"] = tight

	over := good()
	over.Preset = "over"
	over.CPUPct = 78
	over.Units["shelly"] = serviceUnit(14.0, 22, 0.4, 126)
	over.Units["trucki"] = serviceUnit(9.0, 18, 1.2, 40)
	presets["over"] = over

	throttled := good()
	throttled.Preset = "throttled"
	throttled.Throttled = true
	temp := 82.0
	throttled.TempC = &temp
	throttled.CPUPct = 40
	presets["throttled"] = throttled

	memory := good()
	memory.Preset = "memory"
	memory.RAMFreePct = 9
	memory.SwapPagesS = 40
	presets["memory"] = memory

	lonely := good()
	lonely.Preset = "lonely"
	lonely.External = nil
	lonely.ClientsMax = 1
	lonely.Units = map[string]FileUnit{
		"energy-node-dashboard.service": fixedUnit(8.0, 70),
		"mosquitto.service":             fixedUnit(3.0, 12),
		"automation.service":            fixedUnit(4.0, 31),
	}
	presets["lonely"] = lonely

	browsers := good()
	browsers.Preset = "many-browsers"
	browsers.ClientsMax = 7
	presets["many-browsers"] = browsers

	return presets
}

// serviceUnit builds a present service unit with its round duration and message rate.
func serviceUnit(cpu, ramMB, roundS, msgPerMin float64) FileUnit {
	round := roundS
	return FileUnit{Present: true, CPUPct: cpu, RAMMB: ramMB, RoundS: &round, MsgPerMin: msgPerMin}
}

// fixedUnit builds a present .service unit.
func fixedUnit(cpu, ramMB float64) FileUnit {
	return FileUnit{Present: true, CPUPct: cpu, RAMMB: ramMB}
}
