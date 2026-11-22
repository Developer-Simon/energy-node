package learnrec

import "testing"

func TestLimitsMinuteBudgetPerLevel(t *testing.T) {
	tests := []struct {
		level Level
		want  int64
	}{
		{LevelEco, 4 << 20},
		{LevelBalanced, 16 << 20},
		{LevelFull, 64 << 20},
	}
	for _, tt := range tests {
		if got := LimitsFor(tt.level, 4).RAMMinuteBytes; got != tt.want {
			t.Errorf("LimitsFor(%s).RAMMinuteBytes = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestResolveMinutes(t *testing.T) {
	tests := []struct {
		name     string
		settings MinuteSettings
		drive    bool
		want     MinutePlan
	}{
		{"auto mode, auto location, no drive", MinuteSettings{Mode: "auto", Location: "auto", Days: 60}, false,
			MinutePlan{On: true, Location: LocRAM, AutoLocation: true, Days: 60}},
		{"auto mode, auto location, drive", MinuteSettings{Mode: "auto", Location: "auto", Days: 60}, true,
			MinutePlan{On: true, Location: LocDrive, AutoLocation: true, Days: 60}},
		{"explicit ram beats drive", MinuteSettings{Mode: "auto", Location: "ram", Days: 60}, true,
			MinutePlan{On: true, Location: LocRAM, AutoLocation: false, Days: 60}},
		{"explicit system", MinuteSettings{Mode: "always", Location: "system", Days: 30}, false,
			MinutePlan{On: true, Location: LocSystem, AutoLocation: false, Days: 30}},
		{"mode off", MinuteSettings{Mode: "off", Location: "drive", Days: 60}, true,
			MinutePlan{On: false, Location: LocDrive, AutoLocation: false, Days: 60}},
		{"unknown location counts as auto", MinuteSettings{Mode: "always", Location: "turbo", Days: 60}, false,
			MinutePlan{On: true, Location: LocRAM, AutoLocation: true, Days: 60}},
		{"unknown mode counts as auto", MinuteSettings{Mode: "", Location: "", Days: 60}, false,
			MinutePlan{On: true, Location: LocRAM, AutoLocation: true, Days: 60}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveMinutes(tt.settings, tt.drive); got != tt.want {
				t.Errorf("ResolveMinutes(%+v, %v) = %+v, want %+v", tt.settings, tt.drive, got, tt.want)
			}
		})
	}
}

func TestRAMDaysCapsByBudget(t *testing.T) {
	const dayRAM64 = 368 << 10
	const dayRAM512 = 2944 << 10
	eco := LimitsFor(LevelEco, 4)
	if got := RAMDays(eco, dayRAM64); got != 3 {
		t.Errorf("RAMDays(eco, 64 entities) = %d, want 3", got)
	}
	if got := RAMDays(eco, dayRAM512); got != 1 {
		t.Errorf("RAMDays(eco, 512 entities) = %d, want 1", got)
	}
}
