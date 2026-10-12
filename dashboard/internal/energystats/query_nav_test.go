package energystats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

func TestNavigationStringsPerPeriod(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, loc) // Mittwoch
	cases := []struct{ period, at, wantAt, wantPrev, wantNext string }{
		{"day", "", "2026-10-14", "2026-10-13", ""},
		{"day", "2026-10-13", "2026-10-13", "2026-10-12", "2026-10-14"},
		{"week", "", "2026-10-12", "2026-10-05", ""},
		{"week", "2026-10-08", "2026-10-05", "2026-09-28", "2026-10-12"},
		{"month", "", "2026-10", "2026-09", ""},
		{"month", "2026-01", "2026-01", "2025-12", "2026-02"},
		{"year", "", "2026", "2025", ""},
		{"year", "2025", "2025", "2024", "2026"},
		{"year", "2000", "2000", "", "2001"},
	}
	for _, c := range cases {
		res, err := Query(NewStore(t.TempDir()), c.period, c.at, loc, now)
		if err != nil {
			t.Fatalf("%s %s: %v", c.period, c.at, err)
		}
		if res.At != c.wantAt || res.Prev != c.wantPrev || res.Next != c.wantNext {
			t.Errorf("%s %q: at=%q prev=%q next=%q, erwartet %q %q %q", c.period, c.at, res.At, res.Prev, res.Next, c.wantAt, c.wantPrev, c.wantNext)
		}
	}
}

func TestBucketsCarryThePiLocalTime(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, loc)
	// 25.10.2026: Ende der Sommerzeit, 25 Stunden, 02:00 kommt zweimal.
	res, err := Query(NewStore(t.TempDir()), "day", "2026-10-25", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Buckets) != 25 {
		t.Fatalf("%d Buckets", len(res.Buckets))
	}
	want := []string{"2026-10-25T00:00", "2026-10-25T01:00", "2026-10-25T02:00", "2026-10-25T02:00", "2026-10-25T03:00"}
	for i, w := range want {
		if res.Buckets[i].Local != w {
			t.Errorf("Bucket %d: %q, erwartet %q", i, res.Buckets[i].Local, w)
		}
	}
	month, _ := Query(NewStore(t.TempDir()), "month", "2026-10", loc, now)
	if month.Buckets[0].Local != "2026-10-01T00:00" || month.Buckets[30].Local != "2026-10-31T00:00" {
		t.Fatalf("Monat: %q .. %q", month.Buckets[0].Local, month.Buckets[30].Local)
	}
}

func TestServiceAddsCategoriesAndCorruptMonths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-09.json"), []byte("{kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(dir)
	store.logf = func(string, ...any) {}
	svc := &Service{
		Store: store, Location: time.UTC,
		Now: func() time.Time { return hourAt("2026-10-11T12:00:00Z") },
		Categories: func() map[string]energy.Category {
			return map[string]energy.Category{"sauna": {Label: "Sauna", Base: energy.CategoryConsumer, Color: "cat_2"}}
		},
	}
	resp, err := svc.Stats("year", "2026")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Categories["sauna"].Label != "Sauna" {
		t.Fatalf("Categories = %+v", resp.Categories)
	}
	if len(resp.Status.CorruptMonths) != 1 || resp.Status.CorruptMonths[0] != "2026-09" {
		t.Fatalf("CorruptMonths = %v", resp.Status.CorruptMonths)
	}
	data, _ := json.Marshal(resp)
	var decoded map[string]any
	json.Unmarshal(data, &decoded)
	for _, key := range []string{"at", "prev", "next", "categories"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("Feld %q fehlt in %s", key, data)
		}
	}
}
