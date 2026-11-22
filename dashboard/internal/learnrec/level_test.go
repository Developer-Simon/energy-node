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
		{"eco 1 core", LevelEco, 1, Limits{Workers: 1, Budget: 2 * time.Second, LightEvery: 15 * time.Minute, MaxDevices: 64, RAMMinuteDays: 3, PVCellAzDeg: 5, PVCellElDeg: 3, GeoStepDeg: 5, GeoSurfaces: 2, SimilarDays: 7, SimilarPoolDays: 56, SimilarLastYear: false}},
		{"eco 4 cores", LevelEco, 4, Limits{Workers: 1, Budget: 2 * time.Second, LightEvery: 15 * time.Minute, MaxDevices: 64, RAMMinuteDays: 3, PVCellAzDeg: 5, PVCellElDeg: 3, GeoStepDeg: 5, GeoSurfaces: 2, SimilarDays: 7, SimilarPoolDays: 56, SimilarLastYear: false}},
		{"balanced 1 core", LevelBalanced, 1, Limits{Workers: 1, Budget: 10 * time.Second, LightEvery: 5 * time.Minute, MaxDevices: 128, RAMMinuteDays: 7, PVCellAzDeg: 5, PVCellElDeg: 3, GeoStepDeg: 2, GeoSurfaces: 2, SimilarDays: 7, SimilarPoolDays: 56, SimilarLastYear: true}},
		{"balanced 4 cores", LevelBalanced, 4, Limits{Workers: 3, Budget: 10 * time.Second, LightEvery: 5 * time.Minute, MaxDevices: 128, RAMMinuteDays: 7, PVCellAzDeg: 5, PVCellElDeg: 3, GeoStepDeg: 2, GeoSurfaces: 2, SimilarDays: 7, SimilarPoolDays: 56, SimilarLastYear: true}},
		{"full 1 core", LevelFull, 1, Limits{Workers: 1, Budget: 60 * time.Second, LightEvery: 1 * time.Minute, MaxDevices: 256, RAMMinuteDays: 14, PVCellAzDeg: 2, PVCellElDeg: 1, GeoStepDeg: 1, GeoSurfaces: 3, SimilarDays: 15, SimilarPoolDays: 0, SimilarLastYear: true}},
		{"full 4 cores", LevelFull, 4, Limits{Workers: 3, Budget: 60 * time.Second, LightEvery: 1 * time.Minute, MaxDevices: 256, RAMMinuteDays: 14, PVCellAzDeg: 2, PVCellElDeg: 1, GeoStepDeg: 1, GeoSurfaces: 3, SimilarDays: 15, SimilarPoolDays: 0, SimilarLastYear: true}},
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
	if got := AutoLevel(1, ""); got != LevelEco {
		t.Errorf("AutoLevel(1) = %s, want eco", got)
	}
	if got := AutoLevel(0, ""); got != LevelEco {
		t.Errorf("AutoLevel(0) = %s, want eco", got)
	}
	if got := AutoLevel(4, ""); got != LevelBalanced {
		t.Errorf("AutoLevel(4) = %s, want balanced", got)
	}
}

func TestAutoLevelRemembersDowngrade(t *testing.T) {
	tests := []struct {
		name       string
		cores      int
		remembered Level
		want       Level
	}{
		{"downgrade from balanced", 4, LevelEco, LevelEco},
		{"no downgrade stored", 4, "", LevelBalanced},
		{"stored level above the natural one is ignored", 4, LevelFull, LevelBalanced},
		{"downgrade on one core stays eco", 1, LevelEco, LevelEco},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AutoLevel(tt.cores, tt.remembered); got != tt.want {
				t.Errorf("AutoLevel(%d, %q) = %s, want %s", tt.cores, tt.remembered, got, tt.want)
			}
		})
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
		if got := ResolveLevel(tt.setting, tt.cores, ""); got != tt.want {
			t.Errorf("ResolveLevel(%q, %d) = %s, want %s", tt.setting, tt.cores, got, tt.want)
		}
	}
}

func TestResolveLevelExplicitIgnoresRemembered(t *testing.T) {
	if got := ResolveLevel("full", 4, LevelEco); got != LevelFull {
		t.Errorf("ResolveLevel(full, remembered eco) = %s, want full", got)
	}
	if got := ResolveLevel("auto", 4, LevelEco); got != LevelEco {
		t.Errorf("ResolveLevel(auto, remembered eco) = %s, want eco", got)
	}
}

func TestIsAutoSetting(t *testing.T) {
	for _, s := range []string{"auto", "", " auto ", "bogus"} {
		if !IsAutoSetting(s) {
			t.Errorf("IsAutoSetting(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"eco", "balanced", "full"} {
		if IsAutoSetting(s) {
			t.Errorf("IsAutoSetting(%q) = true, want false", s)
		}
	}
}

func TestTargetShare(t *testing.T) {
	if got := TargetShare(LevelEco); got != 0.05 {
		t.Errorf("TargetShare(eco) = %v, want 0.05", got)
	}
	if got := TargetShare(LevelBalanced); got != 0.10 {
		t.Errorf("TargetShare(balanced) = %v, want 0.10", got)
	}
	if got := TargetShare(LevelFull); got != 0.10 {
		t.Errorf("TargetShare(full) = %v, want 0.10", got)
	}
}

// shareWithDays legt je Tag vor suggestNow einen Anteil an, days Tage zurueck.
func shareWithDays(days int, share float64) State {
	st := State{DailyShare: map[string]float64{}}
	for i := 1; i <= days; i++ {
		st.DailyShare[suggestNow.AddDate(0, 0, -i).Format(dayLayout)] = share
	}
	return st
}

var suggestNow = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

func TestSuggestUp(t *testing.T) {
	withDay := func(st State, i int, share float64) State {
		st.DailyShare[suggestNow.AddDate(0, 0, -i).Format(dayLayout)] = share
		return st
	}
	tests := []struct {
		name    string
		st      State
		current Level
		want    Level
		ok      bool
	}{
		{"seven days at 0.02 on eco", shareWithDays(7, 0.02), LevelEco, LevelBalanced, true},
		{"six days only", shareWithDays(6, 0.02), LevelEco, "", false},
		{"one day at 0.03 in between", withDay(shareWithDays(7, 0.02), 4, 0.03), LevelEco, "", false},
		{"full never suggests", shareWithDays(7, 0.001), LevelFull, "", false},
		{"balanced needs 0.05 or less", shareWithDays(7, 0.04), LevelBalanced, LevelFull, true},
		{"balanced above half of target", shareWithDays(7, 0.06), LevelBalanced, "", false},
		{"today does not count", withDay(shareWithDays(7, 0.02), 0, 0.5), LevelEco, LevelBalanced, true},
		{"missing day among seven", func() State {
			st := shareWithDays(7, 0.02)
			delete(st.DailyShare, suggestNow.AddDate(0, 0, -2).Format(dayLayout))
			return st
		}(), LevelEco, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SuggestUp(tt.st, tt.current, suggestNow)
			if !tt.ok {
				tt.want = tt.current
			}
			if ok != tt.ok || got != tt.want {
				t.Errorf("SuggestUp = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRecordShareKeepsFourteenDays(t *testing.T) {
	st := State{}
	for i := 20; i >= 0; i-- {
		st = RecordShare(st, suggestNow.AddDate(0, 0, -i), 0.01*float64(i))
	}
	if len(st.DailyShare) != shareDays {
		t.Fatalf("kept %d days, want %d", len(st.DailyShare), shareDays)
	}
	if _, ok := st.DailyShare[suggestNow.AddDate(0, 0, -20).Format(dayLayout)]; ok {
		t.Errorf("oldest day was not pruned")
	}
	if got := st.DailyShare[suggestNow.Format(dayLayout)]; got != 0 {
		t.Errorf("today = %v, want 0", got)
	}
}
