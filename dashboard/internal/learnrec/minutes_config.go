package learnrec

import "strings"

// MinuteLocation ist der Ort, an dem die Minutenaufzeichnung liegt.
type MinuteLocation string

const (
	LocRAM    MinuteLocation = "ram"
	LocDrive  MinuteLocation = "drive"
	LocSystem MinuteLocation = "system"
)

// MinuteDaysDefault ist die Zahl der Tage, die die Minutenaufzeichnung behaelt,
// wenn die Einstellung 0 ist.
const MinuteDaysDefault = 60

// MinuteModePause ist der Modus, der die eigene Aufzeichnung pausiert, solange
// der Companion Minuten liefert.
const MinuteModePause = "pause_with_companion"

// MinuteSettings sind die gespeicherten Einstellungen der Minutenaufzeichnung.
// Mode ist auto, always, pause_with_companion oder off. Location ist auto, ram,
// drive oder system. ReconcileOff schaltet den Abgleich mit Home Assistant ab.
type MinuteSettings struct {
	Mode         string
	Location     string
	Days         int
	ReconcileOff bool
}

// MinutePlan ist die aufgeloeste Form der Einstellungen. AutoLocation meldet,
// dass der Ort der Lage folgt und nicht gewaehlt wurde. PauseMode meldet, dass
// die Aufzeichnung pausiert, sobald der Companion Minuten liefert. Paused setzt
// der Aufrufer je Minute. Reconcile meldet, dass der Abgleich laeuft.
type MinutePlan struct {
	On           bool
	Location     MinuteLocation
	AutoLocation bool
	PauseMode    bool
	Paused       bool
	Reconcile    bool
	Days         int
}

// ResolveMinutes uebersetzt die Einstellungen. Modus auto wird zu always, mit
// gekoppeltem Companion ohne Laufwerk zu pause_with_companion. Ausdruecklich
// always bleibt always. pause_with_companion ohne Companion rechnet wie always.
// Ort auto wird zu drive, wenn ein Laufwerk eingerichtet ist, sonst zu ram.
// Unbekannte Werte rechnen wie auto. Tage 0 heisst MinuteDaysDefault.
func ResolveMinutes(s MinuteSettings, driveConfigured, companionPaired bool) MinutePlan {
	plan := MinutePlan{Days: s.Days}
	if plan.Days <= 0 {
		plan.Days = MinuteDaysDefault
	}
	plan.Reconcile = companionPaired && !s.ReconcileOff
	switch strings.TrimSpace(s.Mode) {
	case "off":
		plan.On = false
	case "always":
		plan.On = true
	case MinuteModePause:
		plan.On = true
		plan.PauseMode = companionPaired
	default:
		plan.On = true
		plan.PauseMode = !driveConfigured && companionPaired
	}
	switch MinuteLocation(strings.TrimSpace(s.Location)) {
	case LocRAM, LocDrive, LocSystem:
		plan.Location = MinuteLocation(strings.TrimSpace(s.Location))
	default:
		plan.AutoLocation = true
		plan.Location = LocRAM
		if driveConfigured {
			plan.Location = LocDrive
		}
	}
	return plan
}

// RAMDays ist die Zahl der Tage, die im RAM bleiben: die Tage der Stufe, aber
// hoechstens das RAM-Budget durch die Bytes eines vollen Tages. Mindestens 1.
func RAMDays(l Limits, bytesPerDay int64) int {
	days := l.RAMMinuteDays
	if bytesPerDay > 0 {
		if fit := int(l.RAMMinuteBytes / bytesPerDay); fit < days {
			days = fit
		}
	}
	return max(1, days)
}
