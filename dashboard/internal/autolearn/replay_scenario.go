package autolearn

import "time"

// replayScenario is a scenario as far as the replay needs it. Only the modes
// none and hold of the while block are replayable, so the block has no effect
// on the replay.
type replayScenario struct {
	id             string
	enabled        bool
	when           []replayCond
	until          []replayCond
	hasUntil       bool
	minRunS        float64
	maxDurationS   float64
	hasMaxDuration bool
	cooldownS      float64
}

// scenarioMachine runs the state machine of one scenario, as the service does:
// idle, active and disabled.
type scenarioMachine struct {
	s        replayScenario
	phase    string
	since    time.Time
	hasSince bool
	endedAt  time.Time
	hasEnded bool
	rearm    bool
	when     []replayCondRT
	until    []replayCondRT
}

func newScenarioMachine(s replayScenario) *scenarioMachine {
	return &scenarioMachine{s: s, phase: "idle"}
}

// tick evaluates the scenario at ec.now. It returns the phase after the tick
// and whether the scenario started in it.
func (m *scenarioMachine) tick(ec evalCtx) (string, bool) {
	now := ec.now
	if !m.s.enabled {
		if m.phase == "active" {
			m.end(now)
		}
		m.phase = "disabled"
		m.when, m.until = nil, nil
		return m.phase, false
	}
	if len(m.when) != len(m.s.when) {
		m.when = make([]replayCondRT, len(m.s.when))
	}
	allRawWhen, allHeldWhen, staleWhen := evalConditions(m.s.when, m.when, ec)
	untilHeld := false
	if !m.s.hasUntil {
		m.until = nil
	} else {
		if len(m.until) != len(m.s.until) {
			m.until = make([]replayCondRT, len(m.s.until))
		}
		_, untilHeld, _ = evalConditions(m.s.until, m.until, ec)
	}

	if m.phase != "active" {
		if m.rearm && !allRawWhen {
			m.rearm = false
		}
		switch {
		case staleWhen, m.rearm:
		case m.hasEnded && now.Sub(m.endedAt).Seconds() < m.s.cooldownS:
		case allHeldWhen:
			m.start(now)
			return m.phase, true
		}
		return m.phase, false
	}

	if m.s.hasMaxDuration && now.Sub(m.since).Seconds() >= m.s.maxDurationS {
		m.end(now)
		m.rearm = true
		return m.phase, false
	}
	var ends bool
	if m.s.hasUntil {
		ends = untilHeld
	} else {
		ends = !m.s.hasMaxDuration && !allRawWhen && !staleWhen
	}
	if ends && now.Sub(m.since).Seconds() >= m.s.minRunS {
		m.end(now)
	}
	return m.phase, false
}

func (m *scenarioMachine) start(now time.Time) {
	m.phase = "active"
	m.since = now
	m.hasSince = true
}

// end returns to idle and drops the condition runtimes, as the service does.
func (m *scenarioMachine) end(now time.Time) {
	m.phase = "idle"
	m.hasSince = false
	m.endedAt = now
	m.hasEnded = true
	m.when, m.until = nil, nil
}
