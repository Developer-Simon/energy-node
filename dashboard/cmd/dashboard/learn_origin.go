package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// automationDeviceID ist das Geraet des Automations-Dienstes, dessen Objekt
// "state" das Zustandsdokument der Regeln traegt.
const automationDeviceID = "automation"

// switchEntry ist eine Schalt-Entitaet mit ihrem Geraet, wie sie beim letzten
// Minutentakt bekannt war.
type switchEntry struct {
	device string
	view   registry.EntityView
}

// originTables sind die je Minute neu gebauten Tabellen. Sie werden nie
// veraendert, nur ersetzt.
type originTables struct {
	switches map[string]switchEntry
	stateUID string
	rules    func(ruleID string) []autolearn.Target
}

// originLearner fuettert den Tracker aus dem MQTT-Takt und den HTTP-Befehlen.
type originLearner struct {
	tracker   *autolearn.Tracker
	reg       *registry.Registry
	readRules func() ([]byte, error)
	tables    atomic.Pointer[originTables]
}

func newOriginLearner(tracker *autolearn.Tracker, reg *registry.Registry, readRules func() ([]byte, error)) *originLearner {
	return &originLearner{tracker: tracker, reg: reg, readRules: readRules}
}

func isSwitchComponent(component string) bool {
	switch component {
	case "switch", "light", "fan":
		return true
	}
	return false
}

// rebuild liest Regeldokument und Schalt-Entitaeten neu. Ein fehlendes oder
// kaputtes Regeldokument ergibt Ziele ohne Zuordnung.
func (o *originLearner) rebuild() {
	t := &originTables{
		switches: map[string]switchEntry{},
		rules:    func(string) []autolearn.Target { return nil },
	}
	byTopic := map[string]autolearn.CommandTarget{}
	for _, dev := range o.reg.Snapshot() {
		for _, e := range dev.Entities {
			if dev.ID == automationDeviceID && e.ObjectID == "state" {
				t.stateUID = e.UniqueID
			}
			if !isSwitchComponent(e.Component) {
				continue
			}
			t.switches[e.UniqueID] = switchEntry{device: dev.ID, view: e}
			if e.CommandTopic == "" {
				continue
			}
			byTopic[e.CommandTopic] = autolearn.CommandTarget{
				Target:     autolearn.Target{Device: dev.ID, Entity: e.UniqueID},
				PayloadOn:  e.PayloadOn,
				PayloadOff: e.PayloadOff,
			}
		}
	}
	if raw, err := o.readRules(); err == nil {
		if doc, err := autolearn.ParseDoc(raw); err == nil {
			t.rules = autolearn.RuleTargets(doc, byTopic)
		}
	}
	o.tables.Store(t)
}

// observeStates merkt Schaltwechsel und Regelzustaende. Laeuft auf dem
// MQTT-Goroutine.
func (o *originLearner) observeStates(now time.Time, changes []registry.StateChange) {
	t := o.tables.Load()
	if t == nil {
		return
	}
	for _, c := range changes {
		if c.UniqueID == "" {
			continue
		}
		if c.DeviceID == automationDeviceID && c.UniqueID == t.stateUID {
			if c.HasValue {
				o.tracker.RuleState(now, c.Payload, t.rules)
			}
			continue
		}
		sw, ok := t.switches[c.UniqueID]
		if !ok {
			continue
		}
		view := sw.view
		view.Value = c.Value
		view.HasValue = c.HasValue
		if on, ok := learnrec.SwitchOf(view); ok {
			o.tracker.StateChange(now, sw.device, c.UniqueID, on)
		}
	}
}

// observeCommand merkt einen erfolgreich veroeffentlichten Befehl, sofern er
// einen Schalter schaltet.
func (o *originLearner) observeCommand(at time.Time, uniqueID, payload string) {
	info, ok := o.reg.Command(uniqueID)
	if !ok {
		return
	}
	on, ok := switchOfPayload(info, payload)
	if !ok {
		return
	}
	device, ok := o.reg.DeviceIDForUnique(uniqueID)
	if !ok {
		return
	}
	o.tracker.Command(at, device, uniqueID, on)
}

// switchOfPayload liest einen Befehls-Payload gegen die Payloads der
// Entitaet. Nur Schalt-Komponenten ergeben einen Zustand.
func switchOfPayload(info registry.CommandInfo, payload string) (bool, bool) {
	return learnrec.SwitchOf(registry.EntityView{
		Component:  info.Component,
		PayloadOn:  info.PayloadOn,
		PayloadOff: info.PayloadOff,
		Value:      payload,
		HasValue:   true,
	})
}

// run baut die Tabellen je Minute neu und zieht den Tracker leer. Bis der
// Tagesspeicher angebunden ist, werden die Ergebnisse verworfen.
func (o *originLearner) run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			o.rebuild()
			events, actions := o.tracker.Drain(now)
			// Task 5 feeds the day store here.
			_, _ = events, actions
		}
	}
}
