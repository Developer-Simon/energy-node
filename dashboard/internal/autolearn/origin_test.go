package autolearn

import (
	"fmt"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

var originT0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

func ruleDocState(id string, count int, firedAt time.Time) string {
	return fmt.Sprintf(`{"rules":{%q:{"fire_count":%d,"fired_at":%d}}}`, id, count, firedAt.Unix())
}

func targetsFor(id string, targets ...Target) func(string) []Target {
	return func(ruleID string) []Target {
		if ruleID == id {
			return targets
		}
		return nil
	}
}

func TestOriginHand(t *testing.T) {
	rs := NewReactions()
	tr := NewTracker(rs)
	tr.Command(originT0, "dev", "light.a", true)
	tr.StateChange(originT0.Add(2*time.Second), "dev", "light.a", true)

	events, actions := tr.Drain(originT0.Add(time.Minute))
	if len(events) != 1 || events[0].Origin != OriginHand {
		t.Fatalf("events = %+v, want one hand event", events)
	}
	if len(actions) != 0 {
		t.Fatalf("actions = %+v, want none", actions)
	}
	if got := rs.Typical("light.a"); got != 2*time.Second {
		t.Fatalf("Typical after hand = %v, want 2s", got)
	}
}

func TestOriginRule(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Device: "dev", Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 3, originT0), tg)
	tr.RuleState(originT0, ruleDocState("r1", 4, originT0), tg)
	tr.StateChange(originT0.Add(3*time.Second), "dev", "light.a", true)

	events, _ := tr.Drain(originT0.Add(time.Minute))
	if len(events) != 1 || events[0].Origin != OriginRule || events[0].RuleID != "r1" {
		t.Fatalf("events = %+v, want one rule event r1", events)
	}
}

func TestOriginRuleStateArrivesLate(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Device: "dev", Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 3, originT0.Add(-time.Hour)), tg)
	tr.StateChange(originT0.Add(3*time.Second), "dev", "light.a", true)
	tr.RuleState(originT0.Add(5*time.Second), ruleDocState("r1", 4, originT0), tg)

	events, _ := tr.Drain(originT0.Add(30 * time.Second))
	if len(events) != 1 || events[0].Origin != OriginRule {
		t.Fatalf("events = %+v, want one rule event", events)
	}
}

func TestOriginExternal(t *testing.T) {
	tr := NewTracker(NewReactions())
	tr.StateChange(originT0, "dev", "light.a", true)

	events, _ := tr.Drain(originT0.Add(time.Minute))
	if len(events) != 1 || events[0].Origin != OriginExternal {
		t.Fatalf("events = %+v, want one external event", events)
	}
}

func TestOriginRuleCounterReset(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 9, originT0), tg)
	tr.RuleState(originT0.Add(time.Second), ruleDocState("r1", 0, originT0), tg)
	if len(tr.ruleActs) != 0 {
		t.Fatalf("ruleActs after counter reset = %d, want 0", len(tr.ruleActs))
	}
}

func TestOriginTrackerReadsBalance(t *testing.T) {
	tr := NewTracker(NewReactions())
	tr.ObserveMinute(learnrec.Frame{Balance: map[string]float64{
		"load_total": 800, "grid_export": 100, "battery_charge": 50,
	}})
	tr.StateChange(originT0, "dev", "light.a", true)

	events, _ := tr.Drain(originT0.Add(time.Minute))
	if len(events) != 1 || events[0].House != 800 || events[0].Surplus != 150 {
		t.Fatalf("events = %+v, want house 800 and surplus 150", events)
	}
}

func TestRuleActionNoEffect(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 0, originT0), tg)
	tr.RuleState(originT0, ruleDocState("r1", 1, originT0), tg)

	if _, actions := tr.Drain(originT0.Add(9 * time.Second)); len(actions) != 0 {
		t.Fatalf("actions before 2P95 = %+v, want none", actions)
	}
	_, actions := tr.Drain(originT0.Add(10 * time.Second))
	if len(actions) != 1 || actions[0].Effective || actions[0].AlreadyThere {
		t.Fatalf("actions = %+v, want one ineffective action", actions)
	}
}

func TestRuleActionAlreadyThere(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Entity: "light.a", On: boolPtr(true)})
	tr.StateChange(originT0.Add(-5*time.Second), "dev", "light.a", true)
	tr.RuleState(originT0, ruleDocState("r1", 0, originT0), tg)
	tr.RuleState(originT0, ruleDocState("r1", 1, originT0), tg)

	_, actions := tr.Drain(originT0.Add(10 * time.Second))
	if len(actions) != 1 || !actions[0].AlreadyThere || actions[0].Effective {
		t.Fatalf("actions = %+v, want one already-there action", actions)
	}
}

func TestDrainHoldsUntilGrace(t *testing.T) {
	tr := NewTracker(NewReactions())
	tr.StateChange(originT0, "dev", "light.a", true)

	if events, _ := tr.Drain(originT0.Add(10 * time.Second)); len(events) != 0 {
		t.Fatalf("events before grace = %+v, want none", events)
	}
	if events, _ := tr.Drain(originT0.Add(20 * time.Second)); len(events) != 1 {
		t.Fatalf("events at grace = %+v, want one", events)
	}
}

func TestRuleTargets(t *testing.T) {
	doc := Doc{Rules: []Rule{
		{ID: "direct", Actions: []Action{{Type: "publish", EntityID: "light.a", Payload: "ON"}}},
		{ID: "topic", Actions: []Action{{Type: "publish", Topic: "shelly/relay/0/set", Payload: "OFF"}}},
		{ID: "toggle", Actions: []Action{{Type: "publish", Topic: "shelly/relay/0/set", Payload: "toggle"}}},
	}}
	table := map[string]CommandTarget{
		"shellies/light/set": {Target: Target{Device: "light-dev", Entity: "light.a"}, PayloadOn: "ON", PayloadOff: "OFF"},
		"shelly/relay/0/set": {Target: Target{Device: "shelly", Entity: "switch.relay"}, PayloadOn: "ON", PayloadOff: "OFF"},
	}
	targets := RuleTargets(doc, table)

	if got := targets("missing"); got != nil {
		t.Fatalf("unknown rule targets = %+v, want nil", got)
	}
	direct := targets("direct")
	if len(direct) != 1 || direct[0].Device != "light-dev" || direct[0].Entity != "light.a" || direct[0].On == nil || !*direct[0].On {
		t.Fatalf("direct targets = %+v, want light-dev/light.a on", direct)
	}
	topic := targets("topic")
	if len(topic) != 1 || topic[0].Device != "shelly" || topic[0].Entity != "switch.relay" || topic[0].On == nil || *topic[0].On {
		t.Fatalf("topic targets = %+v, want shelly/switch.relay off", topic)
	}
	toggle := targets("toggle")
	if len(toggle) != 1 || toggle[0].On != nil {
		t.Fatalf("toggle targets = %+v, want On nil", toggle)
	}
}

func TestRuleActionWaitsForAttribution(t *testing.T) {
	rs := NewReactions()
	rs.Observe("light.a", time.Second) // R = P95 = 1 s
	tr := NewTracker(rs)
	tg := targetsFor("r1", Target{Device: "dev", Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 0, originT0), tg)
	tr.RuleState(originT0, ruleDocState("r1", 1, originT0), tg)
	tr.StateChange(originT0.Add(1500*time.Millisecond), "dev", "light.a", true)

	// Grace of the switch ends at 1.5 s + 3 R + 5 s = 9.5 s.
	events, actions := tr.Drain(originT0.Add(2 * time.Second))
	if len(events) != 0 || len(actions) != 0 {
		t.Fatalf("at 2s events = %+v, actions = %+v, want none", events, actions)
	}
	events, actions = tr.Drain(originT0.Add(10 * time.Second))
	if len(events) != 1 || events[0].Origin != OriginRule || events[0].RuleID != "r1" {
		t.Fatalf("at 10s events = %+v, want one rule event", events)
	}
	if len(actions) != 1 || !actions[0].Effective || actions[0].Reaction != 1500*time.Millisecond {
		t.Fatalf("at 10s actions = %+v, want effective with reaction 1.5s", actions)
	}
}

func TestOriginCommandPairsLatest(t *testing.T) {
	rs := NewReactions()
	tr := NewTracker(rs)
	tr.Command(originT0, "dev", "light.a", true)
	tr.Command(originT0.Add(8*time.Second), "dev", "light.a", true)
	tr.StateChange(originT0.Add(9*time.Second), "dev", "light.a", true)

	events, _ := tr.Drain(originT0.Add(time.Minute))
	if len(events) != 1 || events[0].Origin != OriginHand {
		t.Fatalf("events = %+v, want one hand event", events)
	}
	if got := rs.Typical("light.a"); got != time.Second {
		t.Fatalf("Typical = %v, want 1s from the latest command", got)
	}
}

func TestRuleActionEffectiveClaimReaction(t *testing.T) {
	tr := NewTracker(NewReactions())
	tg := targetsFor("r1", Target{Device: "dev", Entity: "light.a", On: boolPtr(true)})
	tr.RuleState(originT0, ruleDocState("r1", 0, originT0), tg)
	tr.RuleState(originT0, ruleDocState("r1", 1, originT0), tg)
	tr.StateChange(originT0.Add(2*time.Second), "dev", "light.a", true)

	events, actions := tr.Drain(originT0.Add(40 * time.Second))
	if len(events) != 1 || events[0].Origin != OriginRule {
		t.Fatalf("events = %+v, want one rule event", events)
	}
	if len(actions) != 1 || !actions[0].Effective || actions[0].AlreadyThere || actions[0].Reaction != 2*time.Second {
		t.Fatalf("actions = %+v, want effective with reaction 2s", actions)
	}
}
