package autolearn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// ErrNotReplayable marks a rule or scenario whose conditions reach outside the
// energy core, so the replay cannot judge it.
var ErrNotReplayable = errors.New("not replayable")

// ReplayMinute is one tick of the replay: the balance and the learned values
// that hold at At. A nil Balance means the balance is stale at that tick.
type ReplayMinute struct {
	At      time.Time
	Balance map[string]float64
	Learned map[string]float64
}

// ReplayTarget is the device the replay switches. OpW is the operating power, PeakW
// the start peak added during the first two minutes after switching on,
// CycleWh the energy of one cycle (0 for a device without cycle) and MinRunS
// the minimum run time before a device without cycle may switch off.
type ReplayTarget struct {
	RuleID, ScenarioID, Device   string
	OpW, PeakW, CycleWh, MinRunS float64
}

// ReplayResult is the outcome of one replay. Fires are the seconds since the
// first tick at which the rule fired or the scenario started. Phases holds the
// result of every tick, On the simulated state of the target at every tick.
type ReplayResult struct {
	Fires    []int64
	On       []bool
	Phases   []string
	PVWh     float64
	GridWh   float64
	ExportWh float64
	Switches int
	Estimate bool
}

// replaySubject is the parsed rule or scenario that a replay runs.
type replaySubject struct {
	rule     *replayRule
	scenario *replayScenario
}

// Replayable reports ErrNotReplayable, wrapped, when a condition of the rule
// reaches outside the energy core. It is nil otherwise.
func Replayable(r Rule) error {
	_, err := ruleFromStruct(r)
	return err
}

// ReplayableScenario checks a scenario given as its raw JSON object, since the
// Scenario type does not keep its conditions. It wraps ErrNotReplayable when
// the scenario is not replayable.
func ReplayableScenario(raw json.RawMessage) error {
	_, err := parseReplayScenario(raw)
	return err
}

// Replay runs a rule or a scenario minute by minute in a closed loop. rule is
// a Rule or the raw JSON object of a rule or a scenario. real holds the real
// switch state of the target per minute, site the location for sun windows
// (the zero value means no location).
func Replay(ctx context.Context, rule any, target ReplayTarget, minutes []ReplayMinute, real []bool, loc *time.Location, site sun.Location) (ReplayResult, error) {
	return replayCore(ctx, rule, target, minutes, real, time.Minute, loc, site)
}

// replayCore runs the replay with a tick of step. Hold, cooldown, repeat and
// minimum run times count real seconds with step.
//
// settling_seconds of the service is deliberately not modelled. It only blocks
// the first moments after a service start, while a replay over up to 56 days
// describes the settled operation.
func replayCore(ctx context.Context, rule any, target ReplayTarget, ticks []ReplayMinute, real []bool, step time.Duration, loc *time.Location, site sun.Location) (ReplayResult, error) {
	subject, err := parseSubject(rule)
	if err != nil {
		return ReplayResult{}, err
	}
	if loc == nil {
		loc = time.UTC
	}
	env := replayEnv{loc: loc, site: site, hasSite: site != (sun.Location{})}
	res := ReplayResult{On: make([]bool, len(ticks)), Phases: make([]string, len(ticks))}
	if len(ticks) == 0 {
		return res, nil
	}
	start := ticks[0].At
	hours := step.Hours()

	var rm *ruleMachine
	var sm *scenarioMachine
	if subject.rule != nil {
		rm = newRuleMachine(*subject.rule)
	} else {
		sm = newScenarioMachine(*subject.scenario)
	}

	on := false
	var onSince time.Time
	cycleUsed := 0.0
	for i, tick := range ticks {
		if err := ctx.Err(); err != nil {
			return ReplayResult{}, err
		}
		now := tick.At
		realOn := i < len(real) && real[i]

		// Closed loop: the balance of this tick carries the difference between
		// the simulated and the real switch state of the target.
		delta := loopDelta(target, on, realOn, onSince, now)
		balance, learned := applyDelta(tick.Balance, tick.Learned, delta)
		ec := evalCtx{now: now, env: env, balance: balance, learned: learned}

		var startEvent, wanted bool
		if rm != nil {
			phase, fired := rm.tick(ec)
			res.Phases[i] = phase
			startEvent, wanted = fired, rm.fired
			if fired {
				res.Fires = append(res.Fires, secondsSince(start, now))
			}
		} else {
			phase, started := sm.tick(ec)
			res.Phases[i] = phase
			startEvent, wanted = started, phase == "active"
			if started {
				res.Fires = append(res.Fires, secondsSince(start, now))
			}
		}

		prevOn := on
		switch {
		case startEvent:
			on, onSince, cycleUsed = true, now, 0
		case on && target.CycleWh > 0 && cycleUsed >= target.CycleWh:
			on = false
		case on && target.CycleWh <= 0 && !wanted && now.Sub(onSince).Seconds() >= target.MinRunS:
			on = false
		}
		if on {
			cycleUsed += target.OpW * hours
		}
		if on != prevOn {
			res.Switches++
		}
		res.On[i] = on

		if balance != nil {
			res.PVWh += balance["pv"] * hours
			res.GridWh += balance["grid_import"] * hours
			res.ExportWh += balance["grid_export"] * hours
		}
	}
	return res, nil
}

// loopDelta is the extra load, in watts, that the simulated switch state adds
// to the real balance of this tick. Positive values are consumption the real
// data does not show, negative values are consumption it still shows.
func loopDelta(t ReplayTarget, simOn, realOn bool, onSince, now time.Time) float64 {
	switch {
	case simOn && !realOn:
		delta := t.OpW
		if now.Sub(onSince) <= 2*time.Minute {
			delta += t.PeakW
		}
		return delta
	case realOn && !simOn:
		return -t.OpW
	}
	return 0
}

// applyDelta returns the balance and the learned values of one tick after the
// load delta. A positive delta takes grid export first, then the PV reserve,
// then grid import. A negative delta lowers grid import first and raises grid
// export with the rest. The inputs are not changed.
func applyDelta(balance, learned map[string]float64, delta float64) (map[string]float64, map[string]float64) {
	if balance == nil || delta == 0 {
		return balance, learned
	}
	b := make(map[string]float64, len(balance)+1)
	for k, v := range balance {
		b[k] = v
	}
	// add changes a key only when it exists or the change is not zero, so a
	// missing key is not created by a zero amount.
	add := func(key string, inc float64) {
		if _, ok := b[key]; ok || inc != 0 {
			b[key] += inc
		}
	}
	add("load_total", delta)
	if delta > 0 {
		take := math.Min(math.Max(b["grid_export"], 0), delta)
		add("grid_export", -take)
		rest := delta - take
		if reserve, ok := learned[reserveKey]; ok && rest > 0 {
			used := math.Min(math.Max(reserve, 0), rest)
			learned = copyLearned(learned)
			learned[reserveKey] = reserve - used
			rest -= used
		}
		add("grid_import", rest)
		return b, learned
	}
	cut := -delta
	take := math.Min(math.Max(b["grid_import"], 0), cut)
	add("grid_import", -take)
	add("grid_export", cut-take)
	return b, learned
}

const reserveKey = "site/pv_reserve_w"

func copyLearned(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func secondsSince(start, now time.Time) int64 {
	return int64(now.Sub(start) / time.Second)
}

// parseSubject turns the rule argument of Replay into a rule or a scenario.
func parseSubject(rule any) (replaySubject, error) {
	switch v := rule.(type) {
	case Rule:
		r, err := ruleFromStruct(v)
		if err != nil {
			return replaySubject{}, err
		}
		return replaySubject{rule: r}, nil
	case json.RawMessage:
		return parseRawSubject(v)
	case []byte:
		return parseRawSubject(v)
	case Scenario:
		return replaySubject{}, fmt.Errorf("%w: a Scenario keeps no conditions, pass its raw JSON", ErrNotReplayable)
	default:
		return replaySubject{}, fmt.Errorf("replay: unsupported rule type %T", rule)
	}
}

func parseRawSubject(raw json.RawMessage) (replaySubject, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return replaySubject{}, fmt.Errorf("replay: %w", err)
	}
	if _, ok := probe["when"]; ok {
		s, err := parseReplayScenario(raw)
		if err != nil {
			return replaySubject{}, err
		}
		return replaySubject{scenario: s}, nil
	}
	r, err := parseReplayRule(raw)
	if err != nil {
		return replaySubject{}, err
	}
	return replaySubject{rule: r}, nil
}

// replayRule is a rule as far as the replay needs it.
type replayRule struct {
	id        string
	enabled   bool
	conds     []replayCond
	cooldownS float64
	repeatS   float64
}

func ruleFromStruct(r Rule) (*replayRule, error) {
	conds := make([]replayCond, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		parsed, err := parseReplayCond(c.Raw)
		if err != nil {
			return nil, err
		}
		conds = append(conds, parsed)
	}
	return &replayRule{id: r.ID, enabled: r.Enabled, conds: conds,
		cooldownS: r.CooldownSeconds, repeatS: r.RepeatSeconds}, nil
}

func parseReplayRule(raw json.RawMessage) (*replayRule, error) {
	var wire struct {
		ID              string            `json:"id"`
		Enabled         bool              `json:"enabled"`
		CooldownSeconds float64           `json:"cooldown_seconds"`
		RepeatSeconds   float64           `json:"repeat_seconds"`
		Conditions      []json.RawMessage `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	conds := make([]replayCond, 0, len(wire.Conditions))
	for _, c := range wire.Conditions {
		parsed, err := parseReplayCond(c)
		if err != nil {
			return nil, err
		}
		conds = append(conds, parsed)
	}
	return &replayRule{id: wire.ID, enabled: wire.Enabled, conds: conds,
		cooldownS: wire.CooldownSeconds, repeatS: wire.RepeatSeconds}, nil
}

// parseReplayScenario reads a scenario. Only the modes none and hold are
// replayable, a regulator is not.
func parseReplayScenario(raw json.RawMessage) (*replayScenario, error) {
	var wire struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		When    *struct {
			Conditions []json.RawMessage `json:"conditions"`
		} `json:"when"`
		Until *struct {
			Conditions []json.RawMessage `json:"conditions"`
		} `json:"until"`
		MinRunS      float64  `json:"min_run_s"`
		MaxDurationS *float64 `json:"max_duration_s"`
		CooldownS    float64  `json:"cooldown_s"`
		While        *struct {
			Mode string `json:"mode"`
		} `json:"while"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	if wire.When == nil {
		return nil, fmt.Errorf("replay: scenario %q has no when block", wire.ID)
	}
	if wire.While != nil && wire.While.Mode != "" && wire.While.Mode != "none" && wire.While.Mode != "hold" {
		return nil, fmt.Errorf("%w: while mode %q", ErrNotReplayable, wire.While.Mode)
	}
	s := &replayScenario{id: wire.ID, enabled: wire.Enabled, minRunS: wire.MinRunS, cooldownS: wire.CooldownS}
	var err error
	if s.when, err = parseConditionList(wire.When.Conditions); err != nil {
		return nil, err
	}
	if wire.Until != nil {
		s.hasUntil = true
		if s.until, err = parseConditionList(wire.Until.Conditions); err != nil {
			return nil, err
		}
	}
	if wire.MaxDurationS != nil {
		s.hasMaxDuration = true
		s.maxDurationS = *wire.MaxDurationS
	}
	return s, nil
}

func parseConditionList(raws []json.RawMessage) ([]replayCond, error) {
	out := make([]replayCond, 0, len(raws))
	for _, c := range raws {
		parsed, err := parseReplayCond(c)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}
