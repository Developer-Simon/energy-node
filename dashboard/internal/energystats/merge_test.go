package energystats

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeMonthFile schreibt einen Monat mit den gegebenen Stunden als Datei.
// own und ha sind Stunde -> kWh je Rolle "pv".
func writeMonthFile(t *testing.T, dir, name string, own, ha map[string]float64) {
	t.Helper()
	start, err := time.Parse("2006-01", strings.TrimSuffix(name, ".json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewMonth(start)
	for h, v := range own {
		if !m.AddOwn(hourAt(h), map[string]float64{"pv": v}, 3600) {
			t.Fatalf("AddOwn %s", h)
		}
	}
	for h, v := range ha {
		if !m.SetHA(hourAt(h), "pv", v) {
			t.Fatalf("SetHA %s", h)
		}
	}
	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMonthFile(t *testing.T, dir, name string) *Month {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := decodeMonth(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMergeDirHAWinsAndFillsGaps(t *testing.T) {
	dst := t.TempDir()
	src := t.TempDir()
	writeMonthFile(t, dst, "2026-10.json",
		map[string]float64{"2026-10-05T10:00:00Z": 1.0}, nil)
	writeMonthFile(t, src, "2026-10.json",
		map[string]float64{"2026-10-05T11:00:00Z": 3.0},
		map[string]float64{"2026-10-05T10:00:00Z": 2.0})
	writeMonthFile(t, src, "2026-09.json",
		map[string]float64{"2026-09-30T23:00:00Z": 4.0}, nil)

	if err := MergeDir(dst, src); err != nil {
		t.Fatal(err)
	}

	m := readMonthFile(t, dst, "2026-10.json")
	pv := m.Roles["pv"]
	i10, _ := m.Index(hourAt("2026-10-05T10:00:00Z"))
	if pv.KWh[i10] != 2.0 || pv.Source[i10] != SourceHA {
		t.Fatalf("eigene Stunde durch HA nicht ersetzt: %v %q", pv.KWh[i10], pv.Source[i10])
	}
	i11, _ := m.Index(hourAt("2026-10-05T11:00:00Z"))
	if pv.KWh[i11] != 3.0 || pv.Source[i11] != SourceOwn {
		t.Fatalf("leere Stunde nicht aus dem Archiv gefuellt: %v %q", pv.KWh[i11], pv.Source[i11])
	}

	sept := readMonthFile(t, dst, "2026-09.json")
	i9, _ := sept.Index(hourAt("2026-09-30T23:00:00Z"))
	if sept.Roles["pv"].KWh[i9] != 4.0 {
		t.Fatalf("fehlender Monat nicht kopiert: %v", sept.Roles["pv"].KWh[i9])
	}
}

func TestMergeDirKeepsExistingOwnHours(t *testing.T) {
	dst := t.TempDir()
	src := t.TempDir()
	writeMonthFile(t, dst, "2026-10.json",
		map[string]float64{"2026-10-05T10:00:00Z": 1.0}, nil)
	writeMonthFile(t, src, "2026-10.json",
		map[string]float64{"2026-10-05T10:00:00Z": 9.0}, nil)

	if err := MergeDir(dst, src); err != nil {
		t.Fatal(err)
	}

	m := readMonthFile(t, dst, "2026-10.json")
	i, _ := m.Index(hourAt("2026-10-05T10:00:00Z"))
	pv := m.Roles["pv"]
	if pv.KWh[i] != 1.0 || pv.Source[i] != SourceOwn {
		t.Fatalf("eigene Stunde der Ziel-Seite verloren: %v %q", pv.KWh[i], pv.Source[i])
	}
}

func TestMergeDirSkipsCorruptMonth(t *testing.T) {
	dst := t.TempDir()
	src := t.TempDir()
	writeMonthFile(t, src, "2026-10.json",
		map[string]float64{"2026-10-05T10:00:00Z": 5.0}, nil)
	if err := os.WriteFile(filepath.Join(src, "2026-09.json"), []byte("{kaputt"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := MergeDir(dst, src)
	var skipped *MergeSkipped
	if !errors.As(err, &skipped) {
		t.Fatalf("MergeSkipped erwartet, bekommen %v", err)
	}
	if !reflect.DeepEqual(skipped.Files, []string{"2026-09.json"}) {
		t.Fatalf("falsche Dateiliste: %v", skipped.Files)
	}
	if skipped.Error() == "" {
		t.Fatal("leere Fehlermeldung")
	}

	m := readMonthFile(t, dst, "2026-10.json")
	i, _ := m.Index(hourAt("2026-10-05T10:00:00Z"))
	if got := m.Roles["pv"].KWh[i]; math.IsNaN(got) || got != 5.0 {
		t.Fatalf("gueltiger Monat nicht gemerged: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "2026-09.json")); !os.IsNotExist(err) {
		t.Fatalf("kaputter Monat wurde kopiert: %v", err)
	}
}
