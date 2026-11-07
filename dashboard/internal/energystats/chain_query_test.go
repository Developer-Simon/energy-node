package energystats

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
)

// chainTestService wires a Service with fresh stores and a fixed clock in UTC.
func chainTestService(t *testing.T, now time.Time, chains ...ChainInfo) *Service {
	t.Helper()
	return &Service{
		Store:      NewStore(t.TempDir()),
		ChainStore: NewChainStore(t.TempDir(), nil),
		Chains:     func() []ChainInfo { return chains },
		Location:   time.UTC,
		Now:        func() time.Time { return now },
	}
}

func addChainHour(t *testing.T, svc *Service, at string, pairs map[ChainPair]PairEnergy, standby map[string]StandbyEnergy) {
	t.Helper()
	if err := svc.ChainStore.Add(ChainHour{Hour: hourAt(at), Pairs: pairs, Standby: standby}); err != nil {
		t.Fatal(err)
	}
}

// pe is a full hour of a pair, source and target in kWh.
func pe(source, target float64) PairEnergy {
	return PairEnergy{SourceKWh: source, TargetKWh: target, Seconds: 3600}
}

// sb is a full hour of standby in kWh.
func sb(kwh float64) StandbyEnergy { return StandbyEnergy{KWh: kwh, Seconds: 3600} }

func chainPeriodOf(t *testing.T, res ChainStatsResponse, id string) ChainPeriod {
	t.Helper()
	for _, c := range res.Chains {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("chain %q missing from response", id)
	return ChainPeriod{}
}

func efficiencyOf(t *testing.T, res EfficiencyResponse, id string) ChainEfficiency {
	t.Helper()
	for _, c := range res.Chains {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("chain %q missing from response", id)
	return ChainEfficiency{}
}

// monthTotals returns the total of the chain for November 2026.
func monthTotals(t *testing.T, svc *Service, id string) ChainTotals {
	t.Helper()
	res, err := svc.ChainStats("month", "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	return chainPeriodOf(t, res, id).Total
}

func wantFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if !approx(got, want) {
		t.Errorf("%s = %v, erwartet %v", name, got, want)
	}
}

func wantPointer(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, erwartet %v", name, want)
	}
	wantFloat(t, name, *got, want)
}

func wantNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, erwartet nil", name, *got)
	}
}

var chainNow = hourAt("2026-11-06T12:00:00Z")

func TestChainTotalsLossIsSegmentsBothWaysPlusStandby(t *testing.T) {
	ab := energy.Chain{Label: "ab", Points: []string{"a", "b"}, Counting: "a"}
	svc := chainTestService(t, chainNow, ChainInfo{ID: "ab", Chain: ab})
	addChainHour(t, svc, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.9)}, map[string]StandbyEnergy{"a": sb(0.05)})
	addChainHour(t, svc, "2026-11-02T11:00:00Z", map[ChainPair]PairEnergy{{"b", "a"}: pe(0.5, 0.4)}, nil)

	got := monthTotals(t, svc, "ab")
	wantFloat(t, "LossKWh", got.LossKWh, 0.25)
	wantFloat(t, "StandbyKWh", got.StandbyKWh, 0.05)
	if !got.HasData {
		t.Error("HasData = false, erwartet true")
	}
}

func TestChainEfficiencyIsProductOfSegments(t *testing.T) {
	abc := energy.Chain{Label: "abc", Points: []string{"a", "b", "c"}, Counting: "a"}
	svc := chainTestService(t, chainNow, ChainInfo{ID: "abc", Chain: abc})
	addChainHour(t, svc, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{
		{"a", "b"}: pe(1.0, 0.9),
		{"b", "c"}: pe(0.9, 0.81),
	}, nil)
	// The reverse direction of the first segment counts into that segment:
	// (0.9 + 0.4) / (1.0 + 0.5) * (0.81 / 0.9) = 0.78.
	addChainHour(t, svc, "2026-11-02T11:00:00Z", map[ChainPair]PairEnergy{{"b", "a"}: pe(0.5, 0.4)}, nil)

	got := monthTotals(t, svc, "abc")
	wantPointer(t, "Efficiency", got.Efficiency, 0.78)
	// Source of the first segment in both directions: 1.0 + 0.5.
	wantFloat(t, "SourceKWh", got.SourceKWh, 1.5)
}

func TestChainEfficiencyEmptyWhenSegmentHasNoData(t *testing.T) {
	abc := energy.Chain{Label: "abc", Points: []string{"a", "b", "c"}, Counting: "a"}
	svc := chainTestService(t, chainNow, ChainInfo{ID: "abc", Chain: abc})
	addChainHour(t, svc, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.9)}, nil)

	got := monthTotals(t, svc, "abc")
	if got.Efficiency != nil {
		t.Errorf("Efficiency = %v, erwartet nil ohne Daten von b->c", *got.Efficiency)
	}
	// Without an efficiency the source does not count for a combined one.
	wantFloat(t, "SourceKWh ohne Efficiency", got.SourceKWh, 0)
	wantFloat(t, "LossKWh", got.LossKWh, 0.1)

	tiny := chainTestService(t, chainNow, ChainInfo{ID: "ab", Chain: energy.Chain{Label: "ab", Points: []string{"a", "b"}, Counting: "a"}})
	addChainHour(t, tiny, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(0.005, 0.004)}, nil)
	if got := monthTotals(t, tiny, "ab"); got.Efficiency != nil {
		t.Errorf("Efficiency unter 0,01 kWh = %v, erwartet nil", *got.Efficiency)
	}
}

func TestChainTotalsIgnoresNonAdjacentPairs(t *testing.T) {
	abc := energy.Chain{Label: "abc", Points: []string{"a", "b", "c"}, Counting: "a"}
	svc := chainTestService(t, chainNow, ChainInfo{ID: "abc", Chain: abc})
	addChainHour(t, svc, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{{"a", "c"}: pe(1.0, 0.5)}, nil)

	got := monthTotals(t, svc, "abc")
	wantFloat(t, "LossKWh", got.LossKWh, 0)
	if got.HasData {
		t.Error("HasData = true, erwartet false: a->c liegt nicht benachbart")
	}
	if got.Efficiency != nil {
		t.Errorf("Efficiency = %v, erwartet nil", *got.Efficiency)
	}
}

func TestChainTotalsSurviveReorder(t *testing.T) {
	ab := energy.Chain{Label: "ab", Points: []string{"a", "b"}, Counting: "a"}
	ba := energy.Chain{Label: "ba", Points: []string{"b", "a"}, Counting: "b"}
	svc := chainTestService(t, chainNow, ChainInfo{ID: "ab", Chain: ab}, ChainInfo{ID: "ba", Chain: ba})
	addChainHour(t, svc, "2026-11-02T10:00:00Z", map[ChainPair]PairEnergy{
		{"a", "b"}: pe(1.0, 0.9),
		{"b", "a"}: pe(0.5, 0.4),
	}, nil)

	forward, reordered := monthTotals(t, svc, "ab"), monthTotals(t, svc, "ba")
	wantFloat(t, "LossKWh [b,a]", reordered.LossKWh, forward.LossKWh)
	wantFloat(t, "LossKWh [a,b]", forward.LossKWh, 0.2)
	wantPointer(t, "Efficiency [b,a]", reordered.Efficiency, *forward.Efficiency)
}

func TestRoundTrip(t *testing.T) {
	plugAC := []string{"plug", "ac"}
	// One hour in the window: charge through plug->ac is 10 kWh at plug and
	// 9.5 kWh at ac, discharge ac->plug is 8.4 kWh at ac and 8 kWh at plug.
	standard := map[ChainPair]PairEnergy{
		{"plug", "ac"}: pe(10, 9.5),
		{"ac", "plug"}: pe(8.4, 8),
	}
	roundTrip := func(t *testing.T, info ChainInfo, pairs map[ChainPair]PairEnergy) *float64 {
		t.Helper()
		svc := chainTestService(t, chainNow, info)
		addChainHour(t, svc, "2026-11-05T10:00:00Z", pairs, nil)
		res, err := svc.Efficiency()
		if err != nil {
			t.Fatal(err)
		}
		return efficiencyOf(t, res, info.ID).RoundTrip30D
	}
	storage := func(counting string, direction string) ChainInfo {
		return ChainInfo{ID: "bat", Storage: true, Chain: energy.Chain{Label: "Speicher", Points: plugAC, Counting: counting, Direction: direction}}
	}

	t.Run("counting point at the start", func(t *testing.T) {
		wantPointer(t, "RoundTrip30D", roundTrip(t, storage("plug", "auto"), standard), 0.8)
	})
	t.Run("counting point at the end", func(t *testing.T) {
		wantPointer(t, "RoundTrip30D", roundTrip(t, storage("ac", "auto"), standard), 8.4/9.5)
	})
	t.Run("reversed swaps charge and discharge", func(t *testing.T) {
		wantPointer(t, "RoundTrip30D", roundTrip(t, storage("plug", "reversed"), standard), 10.0/8.0)
	})
	t.Run("not a storage role", func(t *testing.T) {
		info := storage("plug", "auto")
		info.Storage = false
		wantNil(t, "RoundTrip30D", roundTrip(t, info, standard))
	})
	t.Run("fixed direction", func(t *testing.T) {
		wantNil(t, "RoundTrip30D", roundTrip(t, storage("plug", "fixed_down"), standard))
	})
	t.Run("charge below 0.1 kWh", func(t *testing.T) {
		small := map[ChainPair]PairEnergy{
			{"plug", "ac"}: pe(0.05, 0.04),
			{"ac", "plug"}: pe(0.04, 0.03),
		}
		wantNil(t, "RoundTrip30D", roundTrip(t, storage("plug", "auto"), small))
	})
}

func TestStandbyMeanPower(t *testing.T) {
	plugAC := energy.Chain{Label: "Speicher", Points: []string{"plug", "ac"}, Counting: "plug"}
	efficiency := func(t *testing.T, svc *Service) *float64 {
		t.Helper()
		res, err := svc.Efficiency()
		if err != nil {
			t.Fatal(err)
		}
		return efficiencyOf(t, res, "bat").StandbyW30D
	}

	svc := chainTestService(t, chainNow, ChainInfo{ID: "bat", Chain: plugAC, Storage: true})
	// Only the first point counts as standby, the other entity is ignored.
	addChainHour(t, svc, "2026-11-05T10:00:00Z", nil, map[string]StandbyEnergy{
		"plug": sb(0.01),
		"ac":   sb(1.0),
	})
	wantPointer(t, "StandbyW30D", efficiency(t, svc), 10)

	empty := chainTestService(t, chainNow, ChainInfo{ID: "bat", Chain: plugAC, Storage: true})
	addChainHour(t, empty, "2026-11-05T10:00:00Z", map[ChainPair]PairEnergy{{"plug", "ac"}: pe(1, 0.9)}, nil)
	wantNil(t, "StandbyW30D ohne Standby", efficiency(t, empty))
}

func TestEfficiencyWindows(t *testing.T) {
	// Berlin is UTC+1 in November 2026: local midnight of 1.11. is 31.10. 23:00 UTC.
	now := hourAt("2026-11-06T11:30:00Z") // 12:30 Berlin, the current hour starts at 11:00 UTC
	loss := energy.Chain{Label: "Verlust", Points: []string{"a", "b"}, Counting: "a"}
	bat := energy.Chain{Label: "Speicher", Points: []string{"plug", "ac"}, Counting: "plug"}
	svc := chainTestService(t, now, ChainInfo{ID: "loss", Chain: loss}, ChainInfo{ID: "bat", Chain: bat, Storage: true})
	svc.Location = berlin(t)

	lossPair := map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.9)}
	for _, at := range []string{
		"2026-10-31T11:00:00Z", // 31.10. 12:00, in the last month, counts for the day only
		"2026-10-31T22:00:00Z", // 31.10. 23:00, same
		"2026-10-31T23:00:00Z", // 1.11. 00:00, first hour of the month
		"2026-11-06T10:00:00Z", // 6.11. 11:00, last full hour
		"2026-11-06T11:00:00Z", // current hour, not counted
	} {
		addChainHour(t, svc, at, lossPair, nil)
	}
	// Round trip: 2026-10-07 10:00 UTC lies before the 30 day window, the
	// hour at 11:00 UTC is its first hour and counts, the current hour does not.
	addChainHour(t, svc, "2026-10-07T10:00:00Z", map[ChainPair]PairEnergy{{"plug", "ac"}: pe(100, 99), {"ac", "plug"}: pe(50, 1)}, nil)
	addChainHour(t, svc, "2026-10-07T11:00:00Z", map[ChainPair]PairEnergy{{"plug", "ac"}: pe(1, 0.9), {"ac", "plug"}: pe(0.1, 0.1)}, nil)
	addChainHour(t, svc, "2026-11-05T10:00:00Z", map[ChainPair]PairEnergy{{"plug", "ac"}: pe(10, 9.5), {"ac", "plug"}: pe(8.4, 8)}, nil)
	addChainHour(t, svc, "2026-11-06T11:00:00Z", map[ChainPair]PairEnergy{{"plug", "ac"}: pe(1000, 999), {"ac", "plug"}: pe(1000, 1000)}, nil)

	res, err := svc.Efficiency()
	if err != nil {
		t.Fatal(err)
	}
	got := efficiencyOf(t, res, "loss")
	if got.Month != "2026-11" {
		t.Errorf("Month = %q, erwartet 2026-11", got.Month)
	}
	wantFloat(t, "LossMonthKWh", got.LossMonthKWh, 0.2)
	if len(got.Days) != 7 {
		t.Fatalf("%d Tage, erwartet 7", len(got.Days))
	}
	if got.Days[0].Local != "2026-10-31T00:00" || got.Days[6].Local != "2026-11-06T00:00" {
		t.Errorf("Tage von %q bis %q, erwartet 31.10. bis 6.11.", got.Days[0].Local, got.Days[6].Local)
	}
	wantFloat(t, "Days[0].LossKWh", got.Days[0].LossKWh, 0.2)
	wantFloat(t, "Days[1].LossKWh", got.Days[1].LossKWh, 0.1)
	wantFloat(t, "Days[6].LossKWh", got.Days[6].LossKWh, 0.1)

	wantPointer(t, "RoundTrip30D", efficiencyOf(t, res, "bat").RoundTrip30D, 8.1/11)
}

func TestChainStatsBucketsMatchRoleStats(t *testing.T) {
	now := hourAt("2026-11-10T12:00:00Z")
	ab := energy.Chain{Label: "ab", Points: []string{"a", "b"}, Counting: "a"}
	svc := chainTestService(t, now, ChainInfo{ID: "ab", Chain: ab})
	addChainHour(t, svc, "2026-11-04T10:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.9)}, nil)
	addChainHour(t, svc, "2026-11-04T23:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.9)}, nil)
	// Sunday 1.11. belongs to the week before.
	addChainHour(t, svc, "2026-11-01T10:00:00Z", map[ChainPair]PairEnergy{{"a", "b"}: pe(1.0, 0.5)}, nil)

	res, err := svc.ChainStats("week", "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	query, err := Query(svc.Store, "week", "2026-11-02", time.UTC, now)
	if err != nil {
		t.Fatal(err)
	}
	period := chainPeriodOf(t, res, "ab")
	if len(period.Buckets) != len(query.Buckets) || len(period.Buckets) != 7 {
		t.Fatalf("%d Buckets, Query liefert %d, erwartet 7", len(period.Buckets), len(query.Buckets))
	}
	for i, bucket := range period.Buckets {
		want := 0.0
		if i == 2 { // Wednesday, both hours of the day count
			want = 0.2
		}
		wantFloat(t, "Bucket loss", bucket.LossKWh, want)
	}
	wantFloat(t, "Total.LossKWh", period.Total.LossKWh, 0.2)
	if !query.Buckets[2].Start.Equal(hourAt("2026-11-04T00:00:00Z")) {
		t.Errorf("Query-Bucket 2 beginnt %v, erwartet Mittwoch 00:00 UTC", query.Buckets[2].Start)
	}

	if _, err := svc.ChainStats("fortnight", ""); !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("ungueltiger Zeitraum: err = %v, erwartet ErrInvalidPeriod", err)
	}
}

func TestStatsHasChains(t *testing.T) {
	now := hourAt("2026-11-06T12:00:00Z")
	without := chainTestService(t, now)
	without.Chains = nil
	with := chainTestService(t, now, ChainInfo{ID: "ab", Chain: energy.Chain{Label: "ab", Points: []string{"a", "b"}}})

	statsJSON := func(svc *Service) map[string]any {
		t.Helper()
		resp, err := svc.Stats("day", "2026-11-06")
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	plain := statsJSON(without)
	if _, ok := plain["has_chains"]; ok {
		t.Errorf("has_chains ohne Ketten im JSON: %v", plain["has_chains"])
	}
	withChains := statsJSON(with)
	if withChains["has_chains"] != true {
		t.Errorf("has_chains = %v, erwartet true", withChains["has_chains"])
	}
	if len(withChains["buckets"].([]any)) != len(plain["buckets"].([]any)) {
		t.Errorf("Buckets veraendert durch has_chains")
	}
}
