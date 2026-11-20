// Package learnrec sammelt Minutenrahmen der Geraete und lernt daraus
// Automationsvorschlaege. Dieses Teil legt Stufen und Grenzen fest.
package learnrec

import (
	"strings"
	"time"
)

// Level ist die Lernstufe, die der Nutzer waehlt oder die automatisch
// aus der Kernzahl abgeleitet wird.
type Level string

const (
	LevelEco      Level = "eco"
	LevelBalanced Level = "balanced"
	LevelFull     Level = "full"
)

// Limits bündelt die Grenzen einer Stufe. Weitere Felder fuegen spaetere
// Teilauftraege an.
type Limits struct {
	Workers       int
	Budget        time.Duration
	LightEvery    time.Duration
	MaxDevices    int
	RAMMinuteDays int
}

// LimitsFor liefert die Grenzen aus Spec 5.7 fuer die Stufe und die Kernzahl.
// Unbekannte Stufen rechnen wie balanced.
func LimitsFor(l Level, cores int) Limits {
	workers := max(1, cores-1)
	switch l {
	case LevelEco:
		return Limits{Workers: 1, Budget: 2 * time.Second, LightEvery: 15 * time.Minute, MaxDevices: 64, RAMMinuteDays: 3}
	case LevelFull:
		return Limits{Workers: workers, Budget: 60 * time.Second, LightEvery: 1 * time.Minute, MaxDevices: 256, RAMMinuteDays: 14}
	default:
		return Limits{Workers: workers, Budget: 10 * time.Second, LightEvery: 5 * time.Minute, MaxDevices: 128, RAMMinuteDays: 7}
	}
}

// AutoLevel waehlt die Stufe ohne Nutzerwahl: eco auf einem Kern, sonst balanced.
func AutoLevel(cores int) Level {
	if cores <= 1 {
		return LevelEco
	}
	return LevelBalanced
}

// ResolveLevel uebersetzt die Einstellung. "auto" und leer folgen AutoLevel,
// gueltige Stufen gelten direkt, Unbekanntes wie auto.
func ResolveLevel(setting string, cores int) Level {
	switch Level(strings.TrimSpace(setting)) {
	case LevelEco, LevelBalanced, LevelFull:
		return Level(strings.TrimSpace(setting))
	default:
		return AutoLevel(cores)
	}
}
