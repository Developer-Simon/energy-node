package autolearn

import (
	"context"
	"math"
	"math/rand"
	"testing"
	"time"
)

// couplingHand is a hand switching event of a device at the given time.
func couplingHand(device string, on bool, at time.Time) EventRec {
	return EventRec{At: at.Unix(), Device: device, Entity: "switch." + device, On: on, Origin: string(OriginHand)}
}

// couplingDays builds one day file per day from start, with the events that f
// returns for the day index i.
func couplingDays(start time.Time, n int, f func(i int, day time.Time) []EventRec) []DayFile {
	out := make([]DayFile, 0, n)
	for i := 0; i < n; i++ {
		day := time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, start.Location())
		out = append(out, DayFile{V: dayFileVersion, Day: day.Format(dayLayout),
			Devices: map[string]*DeviceDay{}, Events: f(i, day), Actions: []ActionRec{}})
	}
	return out
}

// couplingInput builds the input of the coupling detector over the days, with
// the given rule document and reaction times.
func couplingInput(t *testing.T, days []DayFile, doc Doc, reactions *Reactions) *Input {
	t.Helper()
	loc := berlinLoc(t)
	last, err := time.ParseInLocation(dayLayout, days[len(days)-1].Day, loc)
	if err != nil {
		t.Fatal(err)
	}
	devices := map[string]DeviceInfo{}
	for _, d := range []string{"a", "b"} {
		devices[d] = DeviceInfo{ID: d, Switchable: true, Entities: []string{"switch." + d}}
	}
	return &Input{Now: last.Add(24 * time.Hour), Loc: loc, Days: days, Doc: doc,
		Devices: devices, Reactions: reactions, Base: Base{DayType: map[string]int{}, Away: map[string]bool{}}}
}

// couplingFor keeps the candidates with the given ID.
func couplingFor(cands []Candidate, id string) []Candidate {
	var out []Candidate
	for _, c := range cands {
		if c.Finding.ID == id {
			out = append(out, c)
		}
	}
	return out
}

func TestCouplingFollowsWithinTwoMinutesIsSuggested(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	// A has a human clock jitter of about 19:00 plus or minus 8 minutes, seeded.
	// Without it A is a pure clock habit, and the day-shift null cannot tell
	// it from coincidence.
	rng := rand.New(rand.NewSource(7))
	days := couplingDays(start, 14, func(i int, day time.Time) []EventRec {
		jitter := time.Duration(math.Round((rng.Float64()*16-8)*60)) * time.Second
		aAt := time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 0, 0, loc).Add(jitter)
		evs := []EventRec{couplingHand("a", true, aAt)}
		if i != 13 {
			// B follows A after 40 seconds on 13 of 14 days.
			evs = append(evs, couplingHand("b", true, aAt.Add(40*time.Second)))
		}
		return evs
	})
	cands := CouplingDetector.Run(context.Background(), couplingInput(t, days, Doc{}, NewReactions()), "")
	got := couplingFor(cands, "coupling:a:on:b:on")
	if len(got) != 1 {
		t.Fatalf("coupling:a:on:b:on candidates = %d, want 1 (all: %d)", len(got), len(cands))
	}
	c := got[0]
	if c.Finding.Confidence != "high" {
		t.Errorf("confidence = %s, want high (lower %.2f)", c.Finding.Confidence, c.Lower)
	}
	if c.Finding.Summary.Code != "coupling.follow" || c.Finding.Summary.Params["a"] != "a" || c.Finding.Summary.Params["b"] != "b" {
		t.Errorf("summary = %+v, want coupling.follow for a and b", c.Finding.Summary)
	}
	if c.Finding.Summary.Params["hits"] != 13 || c.Finding.Summary.Params["cases"] != 14 {
		t.Errorf("hits and cases = %v and %v, want 13 and 14", c.Finding.Summary.Params["hits"], c.Finding.Summary.Params["cases"])
	}
	if c.Finding.Summary.Params["lag_s"] != int64(40) {
		t.Errorf("lag_s = %v, want 40", c.Finding.Summary.Params["lag_s"])
	}
	if w := couplingFact(c, "window_s"); w != int64(120) {
		t.Errorf("window = %v s, want 120", w)
	}
	if len(c.Finding.Apply) != 1 || c.Finding.Apply[0].Template != "follow_switch" {
		t.Fatalf("apply = %+v, want one follow_switch fix", c.Finding.Apply)
	}
	p := c.Finding.Apply[0].Prefill
	if p["source"] != "a" || p["source_on"] != true || p["target"] != "b" || p["target_on"] != true {
		t.Errorf("prefill = %+v, want source a on, target b on", p)
	}
	if p["source_entity"] != "switch.a" || p["target_entity"] != "switch.b" {
		t.Errorf("prefill entities = %v and %v, want switch.a and switch.b", p["source_entity"], p["target_entity"])
	}
	if c.Finding.Benefit == nil || c.Finding.Benefit.Unit != "actions_week" || c.Finding.Benefit.Value <= 0 {
		t.Errorf("benefit = %+v, want actions_week above zero", c.Finding.Benefit)
	}
}

func TestCouplingIndependentEveningIsNull(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := synthDays(start, 14, synthOpts{
		Seed:    23,
		Devices: []string{"a", "b"},
		Habits: []synthHabit{
			// Both devices switch on in the evening, each on its own draw.
			{Device: "a", On: true, Minute: 1140, Spread: 30, Hit: 1},
			{Device: "b", On: true, Minute: 1140, Spread: 30, Hit: 1},
		},
	})
	cands := CouplingDetector.Run(context.Background(), couplingInput(t, days, Doc{}, NewReactions()), "")
	if got := couplingFor(cands, "coupling:a:on:b:on"); len(got) != 0 {
		t.Fatalf("coupling candidates for independent evenings = %d, want 0", len(got))
	}
	if got := couplingFor(cands, "coupling:b:on:a:on"); len(got) != 0 {
		t.Fatalf("reverse coupling candidates for independent evenings = %d, want 0", len(got))
	}
}

func TestCouplingExistingRuleIsNull(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := couplingDays(start, 14, func(i int, day time.Time) []EventRec {
		return []EventRec{
			couplingHand("a", true, time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 0, 0, loc)),
			couplingHand("b", true, time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 40, 0, loc)),
		}
	})
	// The rule switches b on when a switches on.
	doc := Doc{Rules: []Rule{{ID: "r1", Name: "A to B", Enabled: true,
		Conditions: []Condition{{Type: "entity_value", EntityID: "switch.a"}},
		Actions:    []Action{{Type: "publish", EntityID: "switch.b"}}}}}
	cands := CouplingDetector.Run(context.Background(), couplingInput(t, days, doc, NewReactions()), "")
	if got := couplingFor(cands, "coupling:a:on:b:on"); len(got) != 0 {
		t.Fatalf("coupling candidates with an existing rule A to B = %d, want 0", len(got))
	}
}

func TestCouplingLagBelowReactionIsNull(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	days := couplingDays(start, 14, func(i int, day time.Time) []EventRec {
		return []EventRec{
			couplingHand("a", true, time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 0, 0, loc)),
			couplingHand("b", true, time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 1, 0, loc)),
		}
	})
	// The typical reaction time of b is the default 5 s, so a 1 s lag is one
	// switch that both devices got from the same button or scene.
	cands := CouplingDetector.Run(context.Background(), couplingInput(t, days, Doc{}, NewReactions()), "")
	if got := couplingFor(cands, "coupling:a:on:b:on"); len(got) != 0 {
		t.Fatalf("coupling candidates with a 1 s lag = %d, want 0", len(got))
	}
}

// couplingFact returns the value of the fact with the code, or -1.
func couplingFact(c Candidate, code string) any {
	for _, f := range c.Finding.Facts {
		if f.Code == code {
			return f.Value
		}
	}
	return -1
}

func TestCouplingSkipsUnreachableAndAway(t *testing.T) {
	loc := berlinLoc(t)
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, loc)
	// Days 0 to 4: B is out of reach, so it cannot follow A. Days 5 to 9: the
	// days are away. Days 10 to 19: B follows A within two minutes every day.
	days := couplingDays(start, 20, func(i int, day time.Time) []EventRec {
		aAt := time.Date(day.Year(), day.Month(), day.Day(), 19, 0, 0, 0, loc)
		evs := []EventRec{couplingHand("a", true, aAt)}
		if i >= 10 {
			evs = append(evs, couplingHand("b", true, aAt.Add(40*time.Second)))
		}
		return evs
	})
	for i := range days {
		for _, id := range []string{"a", "b"} {
			dd := &DeviceDay{}
			dd.Seen[76], dd.Reach[76] = 4, 4
			if id == "b" && i < 5 {
				dd.Reach[76] = 0
			}
			days[i].Devices[id] = dd
		}
	}
	in := couplingInput(t, days, Doc{}, NewReactions())
	for i := 5; i < 10; i++ {
		in.Base.Away[days[i].Day] = true
	}
	cands := CouplingDetector.Run(context.Background(), in, "")
	got := couplingFor(cands, "coupling:a:on:b:on")
	if len(got) != 1 {
		t.Fatalf("coupling:a:on:b:on candidates = %d, want 1 (all: %d)", len(got), len(cands))
	}
	if got[0].Finding.Summary.Params["hits"] != 10 || got[0].Finding.Summary.Params["cases"] != 10 {
		t.Errorf("hits and cases = %v and %v, want 10 and 10", got[0].Finding.Summary.Params["hits"], got[0].Finding.Summary.Params["cases"])
	}
}
