package autolearn

import (
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
)

// Origin sagt, wer einen Schaltvorgang ausgeloest hat.
type Origin string

const (
	OriginRule     Origin = "rule"
	OriginHand     Origin = "hand"
	OriginExternal Origin = "ext"
)

const (
	// originHistory haelt Befehle, Zustaende und Regelaktionen so lange vor.
	originHistory = time.Hour
	// originGrace ist die Zeit, die ein spaeter Zustand noch zaehlen darf.
	originGrace = 5 * time.Second
)

// SwitchEvent ist ein Schaltvorgang mit seiner Herkunft.
type SwitchEvent struct {
	At      time.Time
	Device  string
	Entity  string
	On      bool
	Origin  Origin
	RuleID  string
	House   float64
	Surplus float64
}

// RuleAction ist eine Aktion einer Regel auf ein Ziel, mit dem Ergebnis.
type RuleAction struct {
	At           time.Time
	RuleID       string
	Device       string
	Entity       string
	WantOn       *bool
	AlreadyThere bool
	Effective    bool
	Reaction     time.Duration
}

// Target ist ein Ziel einer Regelaktion. On ist nil bei toggle oder
// unbekanntem Payload.
type Target struct {
	Device string
	Entity string
	On     *bool
}

// CommandTarget ist ein Eintrag der Command-Topic-Tabelle mit den Payloads
// des Ziels.
type CommandTarget struct {
	Target
	PayloadOn  string
	PayloadOff string
}

type command struct {
	at      time.Time
	entity  string
	claimed bool
}

type change struct {
	at      time.Time
	device  string
	entity  string
	on      bool
	house   float64
	surplus float64
	emitted bool
	claimed bool
}

type ruleAct struct {
	act        RuleAction
	attributed bool
	resolved   bool
}

// Tracker ordnet Schaltvorgaenge ihrer Herkunft zu. Er ist sicher fuer
// gleichzeitigen Zugriff.
type Tracker struct {
	mu        sync.Mutex
	reactions *Reactions
	house     float64
	surplus   float64
	seeded    bool
	counts    map[string]int
	commands  []*command
	changes   []*change
	ruleActs  []*ruleAct
}

// NewTracker liefert einen Tracker, der Reaktionszeiten in reactions merkt.
func NewTracker(reactions *Reactions) *Tracker {
	if reactions == nil {
		reactions = NewReactions()
	}
	return &Tracker{reactions: reactions, counts: map[string]int{}}
}

// Command merkt einen Hand-Befehl auf das Ziel.
func (t *Tracker) Command(at time.Time, device, entity string, on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.commands = append(t.commands, &command{at: at, entity: entity})
}

// StateChange merkt einen gemeldeten Zustandswechsel der Schalt-Entitaet.
func (t *Tracker) StateChange(at time.Time, device, entity string, on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.changes = append(t.changes, &change{at: at, device: device, entity: entity, on: on,
		house: t.house, surplus: t.surplus})
}

// ObserveMinute merkt Haus und Ueberschuss des letzten Rahmens.
func (t *Tracker) ObserveMinute(f learnrec.Frame) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.house = f.Balance["load_total"]
	t.surplus = f.Balance["grid_export"] + f.Balance["battery_charge"]
}

// ruleStateDoc ist der Teil des Zustandsdokuments der Automationen, den der
// Tracker liest.
type ruleStateDoc struct {
	Rules map[string]struct {
		FireCount int     `json:"fire_count"`
		FiredAt   float64 `json:"fired_at"`
	} `json:"rules"`
}

// RuleState liest das Zustandsdokument (Rohes JSON). Der erste Aufruf merkt nur
// den Stand. Steigt fire_count einer Regel, entsteht je Ziel eine Regelaktion
// zur Zeit fired_at. Sinkt der Zaehler, etwa nach einem Neustart, gibt es keine
// Aktion.
func (t *Tracker) RuleState(at time.Time, payload string, targets func(ruleID string) []Target) {
	var doc ruleStateDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	first := !t.seeded
	t.seeded = true
	ids := make([]string, 0, len(doc.Rules))
	for id := range doc.Rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := doc.Rules[id]
		prev := t.counts[id]
		t.counts[id] = r.FireCount
		if first || r.FireCount <= prev || targets == nil {
			continue
		}
		firedAt := at
		if r.FiredAt > 0 {
			firedAt = unixFloat(r.FiredAt)
		}
		for _, tg := range targets(id) {
			t.ruleActs = append(t.ruleActs, &ruleAct{act: RuleAction{At: firedAt, RuleID: id,
				Device: tg.Device, Entity: tg.Entity, WantOn: tg.On}})
		}
	}
}

// unixFloat wandelt Unix-Sekunden mit Bruchteil in eine Zeit.
func unixFloat(f float64) time.Time {
	sec := math.Floor(f)
	return time.Unix(int64(sec), int64((f-sec)*1e9))
}

// Drain liefert die Schaltvorgaenge, deren Frist abgelaufen ist, und die
// Regelaktionen, deren Auswertung faellig ist.
func (t *Tracker) Drain(now time.Time) ([]SwitchEvent, []RuleAction) {
	t.mu.Lock()
	defer t.mu.Unlock()
	events := t.classifyLocked(now)
	actions := t.resolveLocked(now)
	t.pruneLocked(now)
	return events, actions
}

// classifyLocked ordnet faellige Schaltvorgaenge zu. Der Aufrufer haelt mu.
func (t *Tracker) classifyLocked(now time.Time) []SwitchEvent {
	var due []*change
	for _, c := range t.changes {
		if c.emitted {
			continue
		}
		r := t.reactions.Typical(c.entity)
		if now.Before(c.at.Add(3*r + originGrace)) {
			continue
		}
		due = append(due, c)
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })

	var events []SwitchEvent
	for _, c := range due {
		start := c.at.Add(-3 * t.reactions.Typical(c.entity))
		ev := SwitchEvent{At: c.at, Device: c.device, Entity: c.entity, On: c.on,
			Origin: OriginExternal, House: c.house, Surplus: c.surplus}
		if ra := t.claimRuleActLocked(c, start); ra != nil {
			ev.Origin = OriginRule
			ev.RuleID = ra.act.RuleID
		} else if cm := t.claimCommandLocked(c, start); cm != nil {
			ev.Origin = OriginHand
			t.reactions.Observe(c.entity, c.at.Sub(cm.at))
		}
		c.emitted = true
		events = append(events, ev)
	}
	return events
}

// claimRuleActLocked sucht eine noch nicht zugeordnete Regelaktion auf das
// Ziel im Fenster [start, c.at].
func (t *Tracker) claimRuleActLocked(c *change, start time.Time) *ruleAct {
	for _, ra := range t.ruleActs {
		if ra.attributed || ra.resolved || ra.act.Entity != c.entity {
			continue
		}
		if ra.act.At.Before(start) || ra.act.At.After(c.at) {
			continue
		}
		if ra.act.WantOn != nil && *ra.act.WantOn != c.on {
			continue
		}
		ra.attributed = true
		return ra
	}
	return nil
}

// claimCommandLocked sucht den spaetesten noch nicht zugeordneten Befehl auf das
// Ziel im Fenster [start, c.at].
func (t *Tracker) claimCommandLocked(c *change, start time.Time) *command {
	var best *command
	for _, cm := range t.commands {
		if cm.claimed || cm.entity != c.entity {
			continue
		}
		if cm.at.Before(start) || cm.at.After(c.at) {
			continue
		}
		if best == nil || cm.at.After(best.at) {
			best = cm
		}
	}
	if best != nil {
		best.claimed = true
	}
	return best
}

// settleLocked liefert die Zeit, ab der eine Regelaktion ausgewertet werden
// darf. Das ist mindestens a+2·P95 (effectEnd) und die Frist jedes noch nicht
// zugeordneten Schaltvorgangs im Fenster [a, a+3R], der ihr zugeordnet werden
// koennte. So wird eine Aktion nie als wirkungslos gemeldet und danach als
// Ursache eines Schaltvorgangs beansprucht.
func (t *Tracker) settleLocked(a RuleAction, effectEnd time.Time) time.Time {
	settle := effectEnd
	r := t.reactions.Typical(a.Entity)
	last := a.At.Add(3 * r)
	for _, c := range t.changes {
		if c.emitted || c.entity != a.Entity || c.at.Before(a.At) || c.at.After(last) {
			continue
		}
		if due := c.at.Add(3*r + originGrace); due.After(settle) {
			settle = due
		}
	}
	return settle
}

// resolveLocked wertet Regelaktionen aus, sobald sie ausgewertet werden duerfen
// (siehe settleLocked).
func (t *Tracker) resolveLocked(now time.Time) []RuleAction {
	var out []RuleAction
	for _, ra := range t.ruleActs {
		if ra.resolved {
			continue
		}
		a := ra.act
		effectEnd := a.At.Add(2 * t.reactions.P95(a.Entity))
		if now.Before(t.settleLocked(a, effectEnd)) {
			continue
		}
		ra.resolved = true
		a.AlreadyThere = t.alreadyThereLocked(a)
		if c := t.claimEffectLocked(a, effectEnd); c != nil {
			a.Effective = true
			a.Reaction = c.at.Sub(a.At)
		}
		out = append(out, a)
	}
	return out
}

// alreadyThereLocked prueft, ob der letzte bekannte Zustand vor der Aktion schon
// am Ziel war.
func (t *Tracker) alreadyThereLocked(a RuleAction) bool {
	if a.WantOn == nil {
		return false
	}
	var last *change
	for _, c := range t.changes {
		if c.entity != a.Entity || !c.at.Before(a.At) {
			continue
		}
		if last == nil || c.at.After(last.at) {
			last = c
		}
	}
	return last != nil && last.on == *a.WantOn
}

// claimEffectLocked sucht den ersten noch nicht beanspruchten Wechsel in
// Richtung WantOn im Fenster [a.At, deadline] und beansprucht ihn.
func (t *Tracker) claimEffectLocked(a RuleAction, deadline time.Time) *change {
	var best *change
	for _, c := range t.changes {
		if c.claimed || c.entity != a.Entity {
			continue
		}
		if c.at.Before(a.At) || c.at.After(deadline) {
			continue
		}
		if a.WantOn != nil && c.on != *a.WantOn {
			continue
		}
		if best == nil || c.at.Before(best.at) {
			best = c
		}
	}
	if best != nil {
		best.claimed = true
	}
	return best
}

// pruneLocked wirft Eintraege weg, die aelter als originHistory sind und nicht
// mehr gebraucht werden. Der Aufrufer haelt mu.
func (t *Tracker) pruneLocked(now time.Time) {
	cutoff := now.Add(-originHistory)
	var cmds []*command
	for _, cm := range t.commands {
		if !cm.at.Before(cutoff) {
			cmds = append(cmds, cm)
		}
	}
	t.commands = cmds
	var changes []*change
	for _, c := range t.changes {
		if !c.emitted || !c.at.Before(cutoff) {
			changes = append(changes, c)
		}
	}
	t.changes = changes
	var acts []*ruleAct
	for _, ra := range t.ruleActs {
		if !ra.resolved || !ra.act.At.Before(cutoff) {
			acts = append(acts, ra)
		}
	}
	t.ruleActs = acts
}

// RuleTargets liefert je Regel die Ziele ihrer Aktionen. Eine Aktion mit
// entity_id zielt direkt auf die Entitaet, sonst wird ihr Command-Topic in der
// Tabelle gesucht. Der Payload wird gegen PayloadOn und PayloadOff des Ziels
// gelesen, sonst ist On nil.
func RuleTargets(doc Doc, byCommandTopic map[string]CommandTarget) func(ruleID string) []Target {
	rules := make(map[string]Rule, len(doc.Rules))
	for _, r := range doc.Rules {
		rules[r.ID] = r
	}
	byEntity := make(map[string]CommandTarget, len(byCommandTopic))
	for _, ct := range byCommandTopic {
		if ct.Entity != "" {
			byEntity[ct.Entity] = ct
		}
	}
	return func(ruleID string) []Target {
		rule, ok := rules[ruleID]
		if !ok {
			return nil
		}
		var out []Target
		for _, a := range rule.Actions {
			switch {
			case a.EntityID != "":
				ct := byEntity[a.EntityID]
				tg := Target{Device: ct.Device, Entity: a.EntityID, On: payloadState(a.Payload, ct)}
				out = append(out, tg)
			case a.Topic != "":
				ct, ok := byCommandTopic[a.Topic]
				if !ok {
					continue
				}
				tg := ct.Target
				tg.On = payloadState(a.Payload, ct)
				out = append(out, tg)
			}
		}
		return out
	}
}

func boolPtr(b bool) *bool { return &b }

// payloadState liest den Payload gegen die Payloads des Ziels. Leere Payloads
// passen nie.
func payloadState(payload string, ct CommandTarget) *bool {
	switch {
	case ct.PayloadOn != "" && payload == ct.PayloadOn:
		return boolPtr(true)
	case ct.PayloadOff != "" && payload == ct.PayloadOff:
		return boolPtr(false)
	default:
		return nil
	}
}
