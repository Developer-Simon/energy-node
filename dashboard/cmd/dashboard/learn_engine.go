package main

import (
	"context"
	"sort"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/energydiscovery"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// engineRunner haengt die Automationen-Engine an den Minutentakt des Hubs.
// ObserveMinute laeuft im Hub und damit im MQTT-Pfad, also legt es nur einen
// Tick in einen Kanal der Laenge 1 und kehrt sofort zurueck. Ist der Kanal
// schon voll, faellt der Tick weg, die naechste Minute holt ihn nach. Run
// rechnet in einer eigenen Goroutine.
type engineRunner struct {
	engine      *autolearn.Engine
	groups      func() map[string]bool
	onDowngrade func()
	ticks       chan time.Time
}

func newEngineRunner(engine *autolearn.Engine, groups func() map[string]bool, onDowngrade func()) *engineRunner {
	return &engineRunner{
		engine:      engine,
		groups:      groups,
		onDowngrade: onDowngrade,
		ticks:       make(chan time.Time, 1),
	}
}

// ObserveMinute gibt den Minutenbeginn an die Engine weiter, ohne zu warten.
func (r *engineRunner) ObserveMinute(f learnrec.Frame) {
	select {
	case r.ticks <- f.Minute:
	default:
	}
}

// Run verarbeitet die Ticks, bis ctx endet. Meldet die Engine eine Ueberlast,
// ruft es onDowngrade auf.
func (r *engineRunner) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-r.ticks:
			r.engine.Tick(at, r.groups())
			if r.engine.Downgrade() {
				r.onDowngrade()
			}
		}
	}
}

// learnDevices baut die Geraete, die die Erkenner sehen. Der Dienst der
// Automationen selbst fehlt, wie im Hub. Die Energierolle kommt aus dem
// Resolver, also aus den Zuordnungen und der Heuristik der Energieansicht.
func learnDevices(devices []registry.DeviceView, resolver *energy.Resolver) map[string]autolearn.DeviceInfo {
	out := make(map[string]autolearn.DeviceInfo, len(devices))
	for _, dev := range devices {
		if dev.ID == energydiscovery.DeviceIdentifier {
			continue
		}
		info := autolearn.DeviceInfo{ID: dev.ID, Name: dev.Name, Role: deviceRole(resolver, dev.Entities)}
		for _, e := range dev.Entities {
			info.Entities = append(info.Entities, e.UniqueID)
			if e.Component == "light" {
				info.Light = true
			}
			if isSwitchComponent(e.Component) {
				info.Switchable = true
			}
		}
		sort.Strings(info.Entities)
		out[dev.ID] = info
	}
	return out
}

// deviceRole returns the energy role of a device from its entities. A wallbox
// or heat pump role of any entity wins, otherwise the first role that is not
// the switch role. Without a resolver or a role the result is empty.
func deviceRole(resolver *energy.Resolver, entities []registry.EntityView) energy.Role {
	if resolver == nil {
		return ""
	}
	var role energy.Role
	for _, e := range entities {
		resolved, ok := resolver.Resolve(e)
		if !ok || resolved.Role == "" || resolved.Role == energy.RoleSwitch {
			continue
		}
		if resolved.Role == energy.RoleWallbox || resolved.Role == energy.RoleHeatPump {
			return resolved.Role
		}
		if role == "" {
			role = resolved.Role
		}
	}
	return role
}
