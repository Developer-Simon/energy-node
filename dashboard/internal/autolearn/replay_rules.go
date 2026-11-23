package autolearn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// replayEnv is the fixed context of a replay: the local time zone and the
// site location for sun windows.
type replayEnv struct {
	loc     *time.Location
	site    sun.Location
	hasSite bool
}

// evalCtx is the input of one tick. balance is nil when it is stale.
type evalCtx struct {
	now     time.Time
	env     replayEnv
	balance map[string]float64
	learned map[string]float64
}

// replayRef is a number that a learned value sets, read as in the service:
// clamped to min and max, the offset is added, a missing value means fallback.
type replayRef struct {
	key                     string
	min, max, fallback, off float64
}

// replayCond is one condition as far as the replay evaluates it.
type replayCond struct {
	typ          string
	field        string
	comparison   string
	threshold    float64
	thresholdRef *replayRef
	hysteresis   float64
	hold         float64
	holdRef      *replayRef
	learned      string
	fallback     float64
	startH       int
	startM       int
	endH         int
	endM         int
	weekdays     []int
	monthDays    []int
	from, to     string
	fromOffset   float64
	toOffset     float64
}

// replayCondRT is the runtime of one condition: since is the start of the
// current raw episode, latched the hysteresis state.
type replayCondRT struct {
	since    time.Time
	hasSince bool
	latched  bool
}

// ruleMachine runs the state machine of one rule, as the service does. fired
// is the episode flag: true from the first fire until the conditions break.
type ruleMachine struct {
	rule    replayRule
	rts     []replayCondRT
	fired   bool
	hasLast bool
	last    time.Time
}

func newRuleMachine(r replayRule) *ruleMachine {
	return &ruleMachine{rule: r, rts: make([]replayCondRT, len(r.conds))}
}

// tick evaluates the rule at ec.now. It returns the result phase of the tick
// and whether the rule fired in it.
func (m *ruleMachine) tick(ec evalCtx) (string, bool) {
	if !m.rule.enabled {
		m.fired = false
		return "disabled", false
	}
	allRaw, allHeld, stale := evalConditions(m.rule.conds, m.rts, ec)
	if !allRaw {
		m.fired = false
	}
	inCooldown := false
	if m.hasLast {
		inCooldown = m.rule.cooldownS-ec.now.Sub(m.last).Seconds() > 0
	}
	switch {
	case stale:
		return "balance_stale", false
	case !allRaw:
		return "conditions_not_met", false
	case m.fired:
		if m.rule.repeatS > 0 && allHeld && m.hasLast && ec.now.Sub(m.last).Seconds() >= m.rule.repeatS {
			m.fire(ec.now)
			return "fired", true
		}
		return "fired", false
	case inCooldown:
		return "cooldown", false
	case !allHeld:
		return "hold_pending", false
	}
	m.fire(ec.now)
	return "fired", true
}

func (m *ruleMachine) fire(now time.Time) {
	m.fired = true
	m.hasLast = true
	m.last = now
}

// evalConditions evaluates every condition, as the service does, and reports
// whether all are met raw, all are held and whether a balance condition saw a
// stale balance.
func evalConditions(conds []replayCond, rts []replayCondRT, ec evalCtx) (allRaw, allHeld, stale bool) {
	allRaw, allHeld = true, true
	for i, c := range conds {
		raw, held, staleHere := evalCond(c, &rts[i], ec)
		allRaw = allRaw && raw
		allHeld = allHeld && held
		stale = stale || staleHere
	}
	return allRaw, allHeld, stale
}

// evalCond returns the raw state, the held state (raw for hold_seconds) and
// whether the condition saw a stale balance.
func evalCond(c replayCond, rt *replayCondRT, ec evalCtx) (raw, held, stale bool) {
	switch c.typ {
	case "balance_threshold":
		if ec.balance == nil {
			stale = true
			break
		}
		value, ok := ec.balance[c.field]
		if !ok {
			break
		}
		threshold := c.threshold
		if c.thresholdRef != nil {
			threshold = resolveRef(*c.thresholdRef, ec.learned)
		}
		if c.hysteresis > 0 {
			raw = thresholdMet(c.comparison, value, threshold, c.hysteresis, rt.latched)
			rt.latched = raw
		} else {
			raw = thresholdMet(c.comparison, value, threshold, 0, false)
		}
	case "learned_value":
		value, ok := ec.learned[c.learned]
		if !ok {
			value = c.fallback
		}
		threshold := c.threshold
		if c.thresholdRef != nil {
			threshold = resolveRef(*c.thresholdRef, ec.learned)
		}
		if c.hysteresis > 0 {
			raw = thresholdMet(c.comparison, value, threshold, c.hysteresis, rt.latched)
			rt.latched = raw
		} else {
			raw = thresholdMet(c.comparison, value, threshold, 0, false)
		}
	case "time_window", "sun_window":
		raw = windowActive(c, ec)
	}
	if !raw {
		rt.hasSince = false
		return false, false, stale
	}
	if !rt.hasSince {
		rt.since = ec.now
		rt.hasSince = true
	}
	hold := c.hold
	if c.holdRef != nil {
		hold = resolveRef(*c.holdRef, ec.learned)
	}
	return true, ec.now.Sub(rt.since).Seconds() >= hold, stale
}

// thresholdMet compares value with threshold. With a hysteresis and a latched
// state, the state holds until the value leaves the band.
func thresholdMet(comparison string, value, threshold, hysteresis float64, latched bool) bool {
	held := latched && hysteresis > 0
	switch comparison {
	case "above":
		if held {
			return value >= threshold-hysteresis
		}
		return value > threshold
	case "below":
		if held {
			return value <= threshold+hysteresis
		}
		return value < threshold
	}
	return false
}

// resolveRef reads a learned number. A missing value means fallback, a value
// outside min and max is clamped. The offset is added in both cases.
func resolveRef(ref replayRef, learned map[string]float64) float64 {
	value, ok := learned[ref.key]
	if !ok {
		return ref.fallback + ref.off
	}
	if value < ref.min {
		return ref.min + ref.off
	}
	if value > ref.max {
		return ref.max + ref.off
	}
	return value + ref.off
}

// windowActive reports whether a time or sun window holds at ec.now. It looks
// at today's window and yesterday's, and a window belongs to its start day
// for the weekday and month day filters.
func windowActive(c replayCond, ec evalCtx) bool {
	now := ec.now.In(ec.env.loc)
	y, m, d := now.Date()
	for _, back := range []int{0, -1} {
		day := time.Date(y, m, d+back, 0, 0, 0, 0, ec.env.loc)
		start, end, ok := windowBounds(c, day, ec)
		if !ok || ec.now.Before(start) || !ec.now.Before(end) {
			continue
		}
		if len(c.weekdays) > 0 && !containsInt(c.weekdays, pyWeekday(day)) {
			continue
		}
		if len(c.monthDays) > 0 && !containsInt(c.monthDays, day.Day()) {
			continue
		}
		return true
	}
	return false
}

func windowBounds(c replayCond, day time.Time, ec evalCtx) (time.Time, time.Time, bool) {
	y, m, d := day.Date()
	loc := ec.env.loc
	if c.typ == "time_window" {
		start := time.Date(y, m, d, c.startH, c.startM, 0, 0, loc)
		end := time.Date(y, m, d, c.endH, c.endM, 0, 0, loc)
		if !end.After(start) {
			end = time.Date(y, m, d+1, c.endH, c.endM, 0, 0, loc)
		}
		return start, end, true
	}
	if !ec.env.hasSite {
		return time.Time{}, time.Time{}, false
	}
	startEvent, ok := sunEvent(day, c.from, ec.env.site)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	endEvent, ok := sunEvent(day, c.to, ec.env.site)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	start := startEvent.Add(offsetDuration(c.fromOffset))
	end := endEvent.Add(offsetDuration(c.toOffset))
	if end.Before(start) {
		next, ok := sunEvent(day.AddDate(0, 0, 1), c.to, ec.env.site)
		if !ok {
			return time.Time{}, time.Time{}, false
		}
		end = next.Add(offsetDuration(c.toOffset))
	}
	return start, end, true
}

// sunEvent returns sunrise or sunset of the calendar day of day.
func sunEvent(day time.Time, event string, site sun.Location) (time.Time, bool) {
	rise, set, ok := sun.Times(day, site.Lat, site.Lon)
	if !ok {
		return time.Time{}, false
	}
	if event == "sunrise" {
		return rise, true
	}
	return set, true
}

func offsetDuration(minutes float64) time.Duration {
	return time.Duration(minutes * float64(time.Minute))
}

// pyWeekday numbers the days as the service does, Monday is 0.
func pyWeekday(day time.Time) int {
	return (int(day.Weekday()) + 6) % 7
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// parseReplayCond reads one condition. A condition outside the energy core
// wraps ErrNotReplayable.
func parseReplayCond(raw json.RawMessage) (replayCond, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return replayCond{}, fmt.Errorf("%w: %v", ErrNotReplayable, err)
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return replayCond{}, fmt.Errorf("%w: %v", ErrNotReplayable, err)
	}
	c := replayCond{typ: head.Type}
	var err error
	if c.hold, c.holdRef, err = numberOrRef(fields["hold_seconds"]); err != nil {
		return replayCond{}, err
	}
	switch head.Type {
	case "balance_threshold", "learned_value":
		if err := decodeString(fields["comparison"], &c.comparison); err != nil {
			return replayCond{}, err
		}
		if c.comparison != "above" && c.comparison != "below" {
			return replayCond{}, fmt.Errorf("%w: comparison %q", ErrNotReplayable, c.comparison)
		}
		if len(bytes.TrimSpace(fields["threshold"])) == 0 {
			return replayCond{}, fmt.Errorf("%w: threshold missing", ErrNotReplayable)
		}
		if c.threshold, c.thresholdRef, err = numberOrRef(fields["threshold"]); err != nil {
			return replayCond{}, err
		}
		if c.hysteresis, err = optionalNumber(fields["hysteresis"]); err != nil {
			return replayCond{}, err
		}
		if head.Type == "balance_threshold" {
			if err := decodeString(fields["field"], &c.field); err != nil || !balanceFields[c.field] {
				return replayCond{}, fmt.Errorf("%w: balance field %q", ErrNotReplayable, c.field)
			}
			return c, nil
		}
		if err := decodeString(fields["learned"], &c.learned); err != nil || c.learned == "" {
			return replayCond{}, fmt.Errorf("%w: learned", ErrNotReplayable)
		}
		if c.fallback, err = optionalNumber(fields["fallback"]); err != nil {
			return replayCond{}, err
		}
		return c, nil
	case "time_window":
		var start, end string
		if err := decodeString(fields["start"], &start); err != nil {
			return replayCond{}, err
		}
		if err := decodeString(fields["end"], &end); err != nil {
			return replayCond{}, err
		}
		if c.startH, c.startM, err = parseClock(start); err != nil {
			return replayCond{}, err
		}
		if c.endH, c.endM, err = parseClock(end); err != nil {
			return replayCond{}, err
		}
		if err := decodeInts(fields["weekdays"], &c.weekdays); err != nil {
			return replayCond{}, err
		}
		if err := decodeInts(fields["month_days"], &c.monthDays); err != nil {
			return replayCond{}, err
		}
		return c, nil
	case "sun_window":
		if err := decodeString(fields["from"], &c.from); err != nil {
			return replayCond{}, err
		}
		if err := decodeString(fields["to"], &c.to); err != nil {
			return replayCond{}, err
		}
		if (c.from != "sunrise" && c.from != "sunset") || (c.to != "sunrise" && c.to != "sunset") {
			return replayCond{}, fmt.Errorf("%w: sun event", ErrNotReplayable)
		}
		if c.fromOffset, err = optionalNumber(fields["from_offset_min"]); err != nil {
			return replayCond{}, err
		}
		if c.toOffset, err = optionalNumber(fields["to_offset_min"]); err != nil {
			return replayCond{}, err
		}
		return c, nil
	}
	return replayCond{}, fmt.Errorf("%w: condition type %q", ErrNotReplayable, head.Type)
}

// numberOrRef reads a number or a learned reference. An absent or null value
// is 0 and no reference.
func numberOrRef(raw json.RawMessage) (float64, *replayRef, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return 0, nil, nil
	}
	var n float64
	if err := json.Unmarshal(trimmed, &n); err == nil {
		return n, nil, nil
	}
	var ref struct {
		Learned  string   `json:"learned"`
		Min      *float64 `json:"min"`
		Max      *float64 `json:"max"`
		Fallback *float64 `json:"fallback"`
		Offset   float64  `json:"offset"`
	}
	if err := json.Unmarshal(trimmed, &ref); err != nil || ref.Learned == "" ||
		ref.Min == nil || ref.Max == nil || ref.Fallback == nil {
		return 0, nil, fmt.Errorf("%w: reference", ErrNotReplayable)
	}
	return 0, &replayRef{key: ref.Learned, min: *ref.Min, max: *ref.Max,
		fallback: *ref.Fallback, off: ref.Offset}, nil
}

func optionalNumber(raw json.RawMessage) (float64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return 0, nil
	}
	var n float64
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return 0, fmt.Errorf("%w: number expected", ErrNotReplayable)
	}
	return n, nil
}

func decodeString(raw json.RawMessage, out *string) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: text expected", ErrNotReplayable)
	}
	return nil
}

func decodeInts(raw json.RawMessage, out *[]int) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return fmt.Errorf("%w: list of numbers expected", ErrNotReplayable)
	}
	return nil
}

// balanceFields are the balance fields of the service (BALANCE_FIELDS). Custom
// categories (custom:<id>) and unknown names are not replayable.
var balanceFields = map[string]bool{
	"grid_export": true, "grid_import": true, "pv": true, "load_total": true,
	"base": true, "wallbox": true, "heat_pump": true, "battery_charge": true,
	"battery_discharge": true, "gap_applied": true, "autarkie": true,
	"eigenverbrauch": true, "battery_soc": true, "battery_capacity_kwh": true,
	"battery_energy_kwh": true,
}

// parseClock reads "HH:MM".
func parseClock(text string) (int, int, error) {
	var h, m int
	if _, err := fmt.Sscanf(text, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("%w: clock %q", ErrNotReplayable, text)
	}
	return h, m, nil
}
