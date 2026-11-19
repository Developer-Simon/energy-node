package autolearn

import (
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energystats"
)

func TestSeasonMaxMonthAndDays(t *testing.T) {
	store := energystats.NewStore(t.TempDir())
	records := []energystats.HourRecord{
		{Hour: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC), KWh: map[string]float64{"grid_export": 3.5}, Covered: 3600},
		{Hour: time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC), KWh: map[string]float64{"grid_export": 1.5}, Covered: 3600},
	}
	for _, rec := range records {
		if err := store.AddOwn(rec); err != nil {
			t.Fatal(err)
		}
	}

	season := EnergyStatsSeason(store)
	from := testNow.AddDate(-1, 0, 0)
	maxW, months, days, ok := season.MaxW("grid_export", from, testNow)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if maxW != 3500 {
		t.Errorf("maxW = %v, want 3500", maxW)
	}
	if len(months) != 1 || months[0] != 6 {
		t.Errorf("months = %v, want [6]", months)
	}
	if days != 2 {
		t.Errorf("coveredDays = %d, want 2", days)
	}

	if _, _, _, ok := season.MaxW("pv", from, testNow); ok {
		t.Error("unknown field: ok = true, want false")
	}
}
