// Package storageusage misst, wie viel Platz die Systemplatte für Installation,
// Laufzeitdaten und Backups belegt. Die Messung läuft über den Dateibaum und
// folgt keinen Symlinks, damit ein eingehängtes Zusatzlaufwerk nicht mitgezählt
// wird.
package storageusage

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Sources nennt die Ordner der Systemplatte je Teil.
type Sources struct {
	Ref     string // Pfad auf der Systemplatte, bestimmt das Gerät
	Install []string
	Runtime []string
	Backups []string
	Exclude []string // Ordner, die nie zu Install zählen
}

// Parts ist der belegte Platz je Teil in Byte.
type Parts struct{ Install, Runtime, Backups int64 }

// Measure misst die Teile der Systemplatte. Fehlt Ref, gibt Parts{} zurück.
func Measure(src Sources) Parts {
	if src.Ref == "" {
		return Parts{}
	}
	info, err := os.Stat(src.Ref)
	if err != nil {
		return Parts{}
	}
	dev, ok := deviceOf(info)
	if !ok {
		return Parts{}
	}
	sameDev := func(fi fs.FileInfo) bool {
		d, ok := deviceOf(fi)
		return ok && d == dev
	}
	return measure(src, sameDev)
}

// measure zählt die Teile mit einer eigenen Prüfung des Geräts. Nur Install
// bekommt die Ausschlussliste, denn Runtime und Backups liegen oft genau in
// einem ausgeschlossenen Ordner.
func measure(src Sources, sameDev func(fs.FileInfo) bool) Parts {
	return Parts{
		Install: sumTree(src.Install, src.Exclude, sameDev),
		Runtime: sumTree(src.Runtime, nil, sameDev),
		Backups: sumTree(src.Backups, nil, sameDev),
	}
}

// sumTree addiert den belegten Platz aller regulären Dateien unter roots.
// Ordner aus exclude und Ordner von einem anderen Gerät werden übersprungen.
// Symlinks werden nicht verfolgt, Lesefehler überspringen nur den Eintrag.
func sumTree(roots, exclude []string, sameDev func(fs.FileInfo) bool) int64 {
	skip := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		skip[filepath.Clean(e)] = true
	}

	var total int64
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				if skip[filepath.Clean(path)] {
					return fs.SkipDir
				}
				fi, err := d.Info()
				if err != nil || !sameDev(fi) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			total += allocatedBytes(fi)
			return nil
		})
	}
	return total
}

// allocatedBytes liefert den tatsächlich belegten Platz einer Datei in Byte.
func allocatedBytes(fi fs.FileInfo) int64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return int64(st.Blocks) * 512
}

// deviceOf liefert die Gerätenummer eines Eintrags.
func deviceOf(fi fs.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
