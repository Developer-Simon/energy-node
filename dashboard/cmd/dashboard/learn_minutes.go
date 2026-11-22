package main

import (
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/learnrec"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
)

// automationServiceID ist der Dienst der Automationen. Seine Reglerschritte
// kommen auf outstation/automation/scenario/<id>/step.
const automationServiceID = "automation"

// minuteSnapshot ist, was die Minutenaufzeichnung aus Einstellungen, Laufwerk
// und Lernstufe braucht. Der Minutenweg liest ihn atomar, ohne Datei und ohne Lock.
type minuteSnapshot struct {
	plan    learnrec.MinutePlan
	dirs    learnrec.MinuteDirs
	driveUp bool
	limits  learnrec.Limits
}

// minuteSnapshotFor baut den Schnappschuss. Das Laufwerksverzeichnis ist nur
// gesetzt, wenn der Speicher im Modus Laufwerk steht. Die Aufzeichnung legt
// learnrec/minutes selbst unter dem Basisverzeichnis an.
func minuteSnapshotFor(value settings.Settings, driveUUID string, status storage.Status, dataDir, mount string, limits learnrec.Limits) *minuteSnapshot {
	plan := learnrec.ResolveMinutes(learnrec.MinuteSettings{
		Mode:     value.LearningMinutesMode,
		Location: value.LearningMinutesLocation,
		Days:     value.LearningMinutesDays,
	}, driveUUID != "")
	snap := &minuteSnapshot{plan: plan, dirs: learnrec.MinuteDirs{System: dataDir}, limits: limits}
	if status.Mode == storage.ModeDrive {
		snap.driveUp = true
		snap.dirs.Drive = storage.DriveRuntimeDir(storage.Paths{Mount: mount})
	}
	return snap
}

// stepDir ist das Basisverzeichnis der Schrittdateien am Ort der Minuten. Leer
// heisst RAM, also am Laufwerk ohne Laufwerk.
func (s *minuteSnapshot) stepDir() string {
	switch s.plan.Location {
	case learnrec.LocDrive:
		if s.driveUp {
			return s.dirs.Drive
		}
	case learnrec.LocSystem:
		return s.dirs.System
	}
	return ""
}

// minuteSources sind die Quellen, aus denen Refresh den Schnappschuss baut.
type minuteSources struct {
	settings func() (settings.Settings, error)
	status   func() storage.Status
	limits   func() learnrec.Limits
	dataDir  string
	mount    string
}

// minuteRecording buendelt den MinuteStore und den StepLog. Beide lesen ihre
// Einstellungen aus dem Schnappschuss, den Refresh im Zehn-Sekunden-Takt erneuert.
type minuteRecording struct {
	store *learnrec.MinuteStore
	steps *autolearn.StepLog
	src   minuteSources
	snap  atomic.Pointer[minuteSnapshot]
}

// newMinuteRecording legt die Aufzeichnung an und liest den ersten Schnappschuss.
func newMinuteRecording(src minuteSources) *minuteRecording {
	r := &minuteRecording{src: src}
	r.Refresh()
	r.store = learnrec.NewMinuteStore(time.Local, r.plan, r.dirs, r.driveUp, r.limits, log.Printf)
	r.steps = autolearn.NewStepLog(func() int { return r.snap.Load().plan.Days }, r.stepDir).WithRingDays(r.stepRingDays)
	return r
}

// stepRingDays ist die Zahl der Tage im RAM-Ring der Schritte. Sie folgt dem
// RAM-Budget wie die Minuten im RAM: die Bytes eines vollen Tages sind die
// Kanaele mal 1440 Minuten mal 4 Byte je Wert. Der Ort aendert den Ring nicht.
func (r *minuteRecording) stepRingDays() int {
	const bytesPerValue = 4
	const minutesPerDay = 1440
	keys := r.store.Usage().Keys
	return learnrec.RAMDays(r.limits(), int64(keys)*minutesPerDay*bytesPerValue)
}

// Refresh liest Einstellungen, Laufwerk und Lernstufe und ersetzt den
// Schnappschuss. Ohne lesbare Einstellungen gelten die Standardwerte.
func (r *minuteRecording) Refresh() {
	value, err := r.src.settings()
	if err != nil {
		value = settings.Default()
	}
	driveUUID := ""
	if cfg, err := storage.LoadConfig(r.src.dataDir); err == nil {
		driveUUID = cfg.UUID
	}
	r.snap.Store(minuteSnapshotFor(value, driveUUID, r.src.status(), r.src.dataDir, r.src.mount, r.src.limits()))
}

func (r *minuteRecording) plan() learnrec.MinutePlan { return r.snap.Load().plan }
func (r *minuteRecording) dirs() learnrec.MinuteDirs { return r.snap.Load().dirs }
func (r *minuteRecording) driveUp() bool             { return r.snap.Load().driveUp }
func (r *minuteRecording) limits() learnrec.Limits   { return r.snap.Load().limits }
func (r *minuteRecording) stepDir() string           { return r.snap.Load().stepDir() }

// Flush schreibt Minuten und Schritte an den Ort. Beide laufen, auch wenn der
// eine einen Fehler meldet.
func (r *minuteRecording) Flush(now time.Time) error {
	return errors.Join(r.store.Flush(now), r.steps.Flush(now))
}

// Reset loescht die Minuten und die Schritte am Ort, am Laufwerk und im System.
func (r *minuteRecording) Reset() error {
	drive := storage.DriveRuntimeDir(storage.Paths{Mount: r.src.mount})
	return errors.Join(r.store.Reset(), r.steps.Reset(r.src.dataDir, drive))
}

// stepTopic ist das Topic der Reglerschritte aller Szenarien des Dienstes.
func stepTopic(serviceID string) string {
	return "outstation/" + serviceID + "/scenario/+/step"
}

// stepScenario liefert die Szenario-ID eines Schritt-Topics des Dienstes. ok ist
// false bei jedem anderen Topic.
func stepScenario(serviceID, topic string) (string, bool) {
	id, ok := strings.CutPrefix(topic, "outstation/"+serviceID+"/scenario/")
	if !ok {
		return "", false
	}
	id, ok = strings.CutSuffix(id, "/step")
	if !ok || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// watchHandler verteilt die Nachrichten von mqttclient.WatchTopics. Schritte gehen
// an den StepLog, alles andere an other. Ein unlesbarer Schritt faellt in Add weg.
func watchHandler(serviceID string, steps *autolearn.StepLog, other func(topic string, payload []byte)) func(topic string, payload []byte) {
	return func(topic string, payload []byte) {
		if id, ok := stepScenario(serviceID, topic); ok {
			_ = steps.Add(id, payload)
			return
		}
		other(topic, payload)
	}
}
