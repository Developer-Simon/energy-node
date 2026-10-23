package historyexchange

import (
	"testing"
	"time"
)

var samplerBase = time.Date(2026, 10, 23, 12, 0, 5, 0, time.UTC)

func samplerRowsAt(t *testing.T, buffer *Buffer, series string) []Row {
	t.Helper()
	return buffer.Rows(series, [][2]int64{{0, 1 << 62}}, 0)
}

func TestSamplerWritesTheClosedMinuteAsOneRow(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	sampler := NewSampler()
	sampler.Attach(buffer)

	sampler.Observe(samplerBase, map[string]float64{"role:grid": -400})
	sampler.Observe(samplerBase.Add(20*time.Second), map[string]float64{"role:grid": -200})
	sampler.Observe(samplerBase.Add(40*time.Second), map[string]float64{"role:grid": -300})
	rows := sampler.Observe(samplerBase.Add(60*time.Second), map[string]float64{"role:grid": -100})

	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one closed-minute row", rows)
	}
	want := Row{Series: "role:grid", TS: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).UnixMilli(), Min: -400, Max: -200, Avg: -300, N: 3, U: "W"}
	if rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows[0], want)
	}
	if buffer.Len() != 1 {
		t.Fatalf("Len = %d, want 1", buffer.Len())
	}
}

func TestSamplerKeepsTheOpenMinuteBack(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	sampler := NewSampler()
	sampler.Attach(buffer)

	for _, offset := range []time.Duration{0, 20 * time.Second, 40 * time.Second} {
		if rows := sampler.Observe(samplerBase.Add(offset), map[string]float64{"role:grid": -300}); len(rows) != 0 {
			t.Fatalf("rows = %+v, want none while the minute is open", rows)
		}
	}
	if buffer.Len() != 0 {
		t.Fatalf("Len = %d, want 0", buffer.Len())
	}
}

func TestSamplerSkipsSeriesWithoutValuesInAMinute(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	sampler := NewSampler()
	sampler.Attach(buffer)

	sampler.Observe(samplerBase, map[string]float64{"role:grid": -100})
	sampler.Observe(samplerBase.Add(60*time.Second), map[string]float64{"role:grid": -100, "automation_scenario_a_output": 615})
	sampler.Observe(samplerBase.Add(120*time.Second), map[string]float64{"role:grid": -100})

	output := samplerRowsAt(t, buffer, "automation_scenario_a_output")
	if len(output) != 1 {
		t.Fatalf("scenario rows = %+v, want exactly one", output)
	}
	if output[0].TS != time.Date(2026, 10, 23, 12, 1, 0, 0, time.UTC).UnixMilli() || output[0].N != 1 || output[0].Avg != 615 {
		t.Fatalf("scenario row = %+v, want minute 12:01 with one sample of 615", output[0])
	}
}

func TestSamplerNeverOverwritesABrowserRow(t *testing.T) {
	buffer := NewBuffer(time.Hour, 100)
	browserTS := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).UnixMilli()
	buffer.Append([]Row{{Series: "role:grid", TS: browserTS, Min: 1, Max: 1, Avg: 1, N: 60, U: "W"}}, browserTS+1000)

	sampler := NewSampler()
	sampler.Attach(buffer)
	sampler.Observe(samplerBase, map[string]float64{"role:grid": -400})
	sampler.Observe(samplerBase.Add(60*time.Second), map[string]float64{"role:grid": -100})

	rows := samplerRowsAt(t, buffer, "role:grid")
	if len(rows) != 1 || rows[0].Avg != 1 {
		t.Fatalf("rows = %+v, want the browser row with avg 1 kept", rows)
	}
}

func TestSamplerWithoutBufferOnlyReturnsRows(t *testing.T) {
	sampler := NewSampler()
	sampler.Observe(samplerBase, map[string]float64{"role:grid": -400})
	rows := sampler.Observe(samplerBase.Add(60*time.Second), map[string]float64{"role:grid": -100})
	if len(rows) != 1 || rows[0].Avg != -400 {
		t.Fatalf("rows = %+v, want one row with avg -400", rows)
	}
}

func TestSamplerTreatsABackwardsClockJumpAsANewMinute(t *testing.T) {
	sampler := NewSampler()
	sampler.Observe(samplerBase.Add(60*time.Second), map[string]float64{"role:grid": -100})
	rows := sampler.Observe(samplerBase, map[string]float64{"role:grid": -200})
	if len(rows) != 1 || rows[0].Avg != -100 {
		t.Fatalf("rows = %+v, want the minute before the jump written", rows)
	}
}
