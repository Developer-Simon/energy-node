package historyexchange

import (
	"testing"
	"time"
)

func row(series string, ts int64, avg float64) Row {
	return Row{Series: series, TS: ts, Min: avg, Max: avg, Avg: avg, N: 60, U: "W"}
}

func TestAppendEntdoppeltNachSerieUndZeitstempel(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	if got := buffer.Append([]Row{row("role:pv", 1000, 5)}, 2000); got != 1 {
		t.Fatalf("Append = %d, want 1", got)
	}
	if got := buffer.Append([]Row{row("role:pv", 1000, 9)}, 2000); got != 0 {
		t.Fatalf("zweites Append = %d, want 0", got)
	}
	if buffer.Len() != 1 {
		t.Fatalf("Len = %d, want 1", buffer.Len())
	}
	// Der zuerst gesehene Satz bleibt stehen - dieselbe Regel wie im Browser.
	rows := buffer.Rows("role:pv", [][2]int64{{0, 2000}}, 10)
	if len(rows) != 1 || rows[0].Avg != 5 {
		t.Fatalf("Rows = %+v, want avg 5", rows)
	}
}

func TestAppendVerwirftSaetzeAelterAlsDieAufbewahrung(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	now := int64(10 * 3600 * 1000)
	added := buffer.Append([]Row{
		row("role:pv", now-2*3600*1000, 1),
		row("role:pv", now-60*1000, 2),
	}, now)
	if added != 1 {
		t.Fatalf("Append = %d, want 1", added)
	}
	if buffer.Len() != 1 {
		t.Fatalf("Len = %d, want 1", buffer.Len())
	}
}

func TestAppendRaeumtAbgelaufeneSaetzeAuf(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	start := int64(10 * 3600 * 1000)
	buffer.Append([]Row{row("role:pv", start, 1)}, start)
	buffer.Append([]Row{row("role:pv", start+2*3600*1000, 2)}, start+2*3600*1000)
	if buffer.Len() != 1 {
		t.Fatalf("Len = %d, want 1 - der alte Satz muss herausgefallen sein", buffer.Len())
	}
}

func TestAppendHaeltDieSatzobergrenzeEin(t *testing.T) {
	buffer := NewBuffer(24*time.Hour, 3)
	now := int64(24 * 3600 * 1000)
	for index := int64(0); index < 5; index++ {
		buffer.Append([]Row{row("role:pv", now-4*60000+index*60000, float64(index))}, now)
	}
	if buffer.Len() != 3 {
		t.Fatalf("Len = %d, want 3", buffer.Len())
	}
	// Die aeltesten fallen heraus, die juengsten bleiben.
	rows := buffer.Rows("role:pv", [][2]int64{{0, now + 1}}, 10)
	if rows[0].Avg != 2 {
		t.Fatalf("aeltester verbliebener Satz = %+v, want avg 2", rows[0])
	}
}

func TestCensusZaehltJeSerieInRasterbuckets(t *testing.T) {
	buffer := NewBuffer(24*time.Hour, 100)
	now := int64(24 * 3600 * 1000)
	buffer.Append([]Row{
		row("role:pv", now-90*60000, 1),
		row("role:pv", now-80*60000, 2),
		row("role:pv", now-30*60000, 3),
		row("role:grid", now-30*60000, 4),
	}, now)
	census := buffer.Census(now-2*3600*1000, now, 3600*1000)
	if got := census["role:pv"].N; len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("role:pv N = %v, want [2 1]", got)
	}
	if got := census["role:grid"].N; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("role:grid N = %v, want [0 1]", got)
	}
	if census["role:pv"].Step != 3600*1000 {
		t.Fatalf("Step = %d", census["role:pv"].Step)
	}
}

func TestRowsLiefertNurDieAngefragtenBereicheSortiert(t *testing.T) {
	buffer := NewBuffer(24*time.Hour, 100)
	buffer.Append([]Row{
		row("role:pv", 5000, 1),
		row("role:pv", 1000, 2),
		row("role:pv", 3000, 3),
		row("role:grid", 3000, 4),
	}, 6000)
	rows := buffer.Rows("role:pv", [][2]int64{{1000, 3001}}, 10)
	if len(rows) != 2 {
		t.Fatalf("Rows = %+v, want 2", rows)
	}
	if rows[0].TS != 1000 || rows[1].TS != 3000 {
		t.Fatalf("Reihenfolge = %+v", rows)
	}
}

func TestRowsDeckeltDieMenge(t *testing.T) {
	buffer := NewBuffer(24*time.Hour, 100)
	for index := int64(0); index < 10; index++ {
		buffer.Append([]Row{row("role:pv", index*1000, float64(index))}, 10000)
	}
	if got := buffer.Rows("role:pv", [][2]int64{{0, 100000}}, 4); len(got) != 4 {
		t.Fatalf("Rows = %d, want 4", len(got))
	}
}
