package learnrec

import (
	"testing"
	"time"
)

func TestLimitsForLevels(t *testing.T) {
	tests := []struct {
		name  string
		level Level
		cores int
		want  Limits
	}{
		{"eco 1 core", LevelEco, 1, Limits{Workers: 1, Budget: 2 * time.Second, LightEvery: 15 * time.Minute, MaxDevices: 64, RAMMinuteDays: 3}},
		{"eco 4 cores", LevelEco, 4, Limits{Workers: 1, Budget: 2 * time.Second, LightEvery: 15 * time.Minute, MaxDevices: 64, RAMMinuteDays: 3}},
		{"balanced 1 core", LevelBalanced, 1, Limits{Workers: 1, Budget: 10 * time.Second, LightEvery: 5 * time.Minute, MaxDevices: 128, RAMMinuteDays: 7}},
		{"balanced 4 cores", LevelBalanced, 4, Limits{Workers: 3, Budget: 10 * time.Second, LightEvery: 5 * time.Minute, MaxDevices: 128, RAMMinuteDays: 7}},
		{"full 1 core", LevelFull, 1, Limits{Workers: 1, Budget: 60 * time.Second, LightEvery: 1 * time.Minute, MaxDevices: 256, RAMMinuteDays: 14}},
		{"full 4 cores", LevelFull, 4, Limits{Workers: 3, Budget: 60 * time.Second, LightEvery: 1 * time.Minute, MaxDevices: 256, RAMMinuteDays: 14}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LimitsFor(tt.level, tt.cores); got != tt.want {
				t.Errorf("LimitsFor(%s, %d) = %+v, want %+v", tt.level, tt.cores, got, tt.want)
			}
		})
	}
}

func TestAutoLevel(t *testing.T) {
	if got := AutoLevel(1); got != LevelEco {
		t.Errorf("AutoLevel(1) = %s, want eco", got)
	}
	if got := AutoLevel(0); got != LevelEco {
		t.Errorf("AutoLevel(0) = %s, want eco", got)
	}
	if got := AutoLevel(4); got != LevelBalanced {
		t.Errorf("AutoLevel(4) = %s, want balanced", got)
	}
}

func TestResolveLevel(t *testing.T) {
	tests := []struct {
		setting string
		cores   int
		want    Level
	}{
		{"auto", 1, LevelEco},
		{"auto", 4, LevelBalanced},
		{"", 4, LevelBalanced},
		{"full", 1, LevelFull},
		{"eco", 4, LevelEco},
		{"balanced", 1, LevelBalanced},
		{"bogus", 4, LevelBalanced},
	}
	for _, tt := range tests {
		if got := ResolveLevel(tt.setting, tt.cores); got != tt.want {
			t.Errorf("ResolveLevel(%q, %d) = %s, want %s", tt.setting, tt.cores, got, tt.want)
		}
	}
}
