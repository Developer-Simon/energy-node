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

// MinuteSettings sind die gespeicherten Einstellungen der Minutenaufzeichnung.
// Mode ist auto, always oder off. Location ist auto, ram, drive oder system.
type MinuteSettings struct {
	Mode     string
	Location string
	Days     int
}

// MinutePlan ist die aufgeloeste Form der Einstellungen. AutoLocation meldet,
// dass der Ort der Lage folgt und nicht gewaehlt wurde.
type MinutePlan struct {
	On           bool
	Location     MinuteLocation
	AutoLocation bool
	Days         int
}

// ResolveMinutes uebersetzt die Einstellungen. Modus auto wird zu always,
// Ort auto wird zu drive, wenn ein Laufwerk eingerichtet ist, sonst zu ram.
// Unbekannte Werte rechnen wie auto. Tage 0 heisst MinuteDaysDefault.
func ResolveMinutes(s MinuteSettings, driveConfigured bool) MinutePlan {
	plan := MinutePlan{Days: s.Days}
	if plan.Days <= 0 {
		plan.Days = MinuteDaysDefault
	}
	switch strings.TrimSpace(s.Mode) {
	case "off":
		plan.On = false
	default:
		plan.On = true
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
