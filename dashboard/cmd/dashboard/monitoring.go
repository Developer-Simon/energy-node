package main

import (
	"strings"
	"sync"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/energy"
	"github.com/Developer-Simon/energy-node-dashboard/internal/httpapi"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/monitoring"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

// monitoringRunner sammelt je Minute (Erstsehen auch ohne offene Seite) und
// bei jedem Abruf. Speichern übernimmt der Hub über Save.
type monitoringRunner struct {
	mu      sync.Mutex
	store   *monitoring.Store
	collect func() monitoring.Input
}

func newMonitoringRunner(store *monitoring.Store, collect func() monitoring.Input) *monitoringRunner {
	return &monitoringRunner{store: store, collect: collect}
}

// Notices sammelt die Meldungen und setzt Erstsehen und Quittung.
func (m *monitoringRunner) Notices(now time.Time) []monitoring.Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	notices := monitoring.Collect(m.collect())
	m.store.Apply(notices, now)
	return notices
}

// ObserveMinute sammelt die Meldungen zum Minutenbeginn des Rahmens.
func (m *monitoringRunner) ObserveMinute(f learnrec.Frame) {
	m.Notices(f.Minute)
}

// Ack quittiert oder nimmt zurück und speichert sofort.
func (m *monitoringRunner) Ack(id string, ack bool, user string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.SetAck(id, ack, user, now); err != nil {
		return err
	}
	return m.store.Save()
}

// Save schreibt den Speicher, wenn sich etwas geändert hat.
func (m *monitoringRunner) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Save()
}

// monitoringDeviceIcon wählt das Icon eines Lerngeräts aus seiner Energie-Rolle.
func monitoringDeviceIcon(role energy.Role, switchable bool) string {
	switch {
	case role == energy.RoleHeatPump:
		return "ico-heatpump"
	case role == energy.RoleWallbox:
		return "ico-wallbox"
	case role == energy.RoleBattery:
		return "ico-battery"
	case role == energy.RolePV:
		return "ico-sun"
	case role == energy.RoleSwitch:
		return "ico-switch"
	case role == energy.RoleLoad, strings.HasPrefix(string(role), "custom:"):
		return "ico-load"
	case switchable:
		return "ico-switch"
	default:
		return "ico-device"
	}
}

// monitoringLearned ergänzt die Lernwerte um Gerätename (Registry) und Icon
// (Energie-Rolle der ersten Entität des Geräts, Schaltbarkeit aus learnDevices).
func monitoringLearned(learned httpapi.MonitoringLearned, devices []registry.DeviceView, snap energy.Snapshot, resolver *energy.Resolver) httpapi.MonitoringLearned {
	names := make(map[string]string, len(devices))
	for _, device := range devices {
		names[device.ID] = device.Name
	}
	roles := map[string]energy.Role{}
	for _, entity := range snap.Entities {
		if _, ok := roles[entity.DeviceID]; !ok {
			roles[entity.DeviceID] = entity.Role.Role
		}
	}
	infos := learnDevices(devices, resolver)
	for i := range learned.Values {
		id := learned.Values[i].Device
		if id == "" {
			continue
		}
		learned.Values[i].DeviceName = names[id]
		learned.Values[i].DeviceIcon = monitoringDeviceIcon(roles[id], infos[id].Switchable)
	}
	return learned
}
