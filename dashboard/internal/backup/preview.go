package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Change beschreibt eine Änderung in der Vorschau. Kind ist "mod", "new" oder "del".
// Label nennt die Datei oder die Art des Eintrags, Name den Eintrag selbst.
type Change struct {
	Kind  string
	Label string
	Name  string
}

// AreaPreview fasst die Änderungen eines Bereichs zusammen. Available ist false,
// wenn das Archiv keine Datei dieses Bereichs enthält.
type AreaPreview struct {
	Area      Area
	Available bool
	Changes   []Change
	Size      int64
	Count     int
	// PasswordsKept ist true, wenn die users.json im Archiv ohne Zugangsdaten ist.
	// Die vorhandenen Passwörter bleiben beim Wiederherstellen erhalten.
	PasswordsKept bool
}

// previewAreas legt die Reihenfolge der Bereiche in der Vorschau fest.
var previewAreas = []Area{AreaSettings, AreaDevices, AreaUsers, AreaSecrets, AreaStats, AreaHistory, AreaBattery}

// archiveFile ist eine Datei im Staging oder im Live-Knoten.
type archiveFile struct {
	path string
	size int64
}

// entrySpec beschreibt eine Liste mit Einträgen in einer JSON-Datei. key ist der
// Schlüssel der Liste, leer heißt, die Datei ist selbst eine Liste.
type entrySpec struct {
	key   string
	label string
}

// entry ist ein Eintrag einer Liste mit dem Namen aus "name" und dem Wert.
type entry struct {
	name  string
	value any
}

// Preview vergleicht die Dateien unter stagingDir mit den Live-Verzeichnissen
// in src. Dateien werden über den JSON-Vergleich bewertet, Geräte, Regeln und
// Layout-Seiten je Eintrag. Ein Bereich ohne Datei im Archiv ist nicht verfügbar.
func Preview(stagingDir string, src Sources) ([]AreaPreview, error) {
	staged, err := stagedFiles(stagingDir)
	if err != nil {
		return nil, err
	}
	live, err := liveFiles(src)
	if err != nil {
		return nil, err
	}

	names := map[Area][]string{}
	seen := map[string]bool{}
	for _, set := range []map[string]archiveFile{staged, live} {
		for name := range set {
			if seen[name] {
				continue
			}
			seen[name] = true
			if area, ok := areaOf(name); ok {
				names[area] = append(names[area], name)
			}
		}
	}

	out := make([]AreaPreview, 0, len(previewAreas))
	for _, area := range previewAreas {
		list := names[area]
		sort.Strings(list)
		p := AreaPreview{Area: area}
		for _, name := range list {
			if f, ok := staged[name]; ok {
				p.Count++
				p.Size += f.size
			}
		}
		p.Available = p.Count > 0
		if p.Available {
			for _, name := range list {
				s, inArchive := staged[name]
				l, inLive := live[name]
				changes, err := fileChanges(name, s, l, inArchive, inLive)
				if err != nil {
					return nil, err
				}
				p.Changes = append(p.Changes, changes...)
			}
		}
		if area == AreaUsers {
			if s, ok := staged[usersArchiveName]; ok {
				data, err := os.ReadFile(s.path)
				if err != nil {
					return nil, err
				}
				p.PasswordsKept = usersWithoutSecrets(data)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// stagedFiles listet die Dateien unter dir mit ihrem Archivnamen, etwa data/settings.json.
func stagedFiles(dir string) (map[string]archiveFile, error) {
	out := map[string]archiveFile{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = archiveFile{path: p, size: info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// liveFiles listet die Dateien der Live-Verzeichnisse, die der Katalog einem
// Bereich zuordnet, unter demselben Archivnamen wie stagedFiles.
func liveFiles(src Sources) (map[string]archiveFile, error) {
	out := map[string]archiveFile{}
	for _, tree := range src.Trees() {
		if tree.Dir == "" {
			continue
		}
		if _, err := os.Stat(tree.Dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(tree.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(tree.Dir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if e, ok := Lookup(tree.Root, rel); !ok || e.Area == "" || !src.Accepts(tree, e) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[tree.Prefix+rel] = archiveFile{path: p, size: info.Size()}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for id, state := range src.BatteryStates {
		base := id + ".json"
		if !validBatteryFile(base) {
			continue
		}
		info, err := os.Stat(state)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out["battery/"+base] = archiveFile{path: state, size: info.Size()}
	}
	return out, nil
}

// areaOf liefert den Bereich einer Archivdatei. Konfiguration und Dateien ohne
// Bereich zählen nicht zur Vorschau.
func areaOf(name string) (Area, bool) {
	switch {
	case strings.HasPrefix(name, "battery/"):
		return AreaBattery, true
	case strings.HasPrefix(name, "data/"):
		e, ok := Lookup(RootData, strings.TrimPrefix(name, "data/"))
		return e.Area, ok && e.Area != ""
	case strings.HasPrefix(name, "devices/"):
		e, ok := Lookup(RootDevices, strings.TrimPrefix(name, "devices/"))
		return e.Area, ok && e.Area != ""
	}
	return "", false
}

// entrySpecs liefert für Dateien mit Einträgen die Listen darin. Andere Dateien
// vergleicht fileChanges als Ganzes.
func entrySpecs(name string) []entrySpec {
	switch {
	case name == "data/layout.json":
		return []entrySpec{{key: "pages", label: "Seite"}}
	case name == "devices/automation_rules.json":
		return []entrySpec{{key: "rules", label: "Regel"}, {key: "scenarios", label: "Szenario"}}
	case strings.HasPrefix(name, "devices/") && strings.HasSuffix(name, "_devices.json"):
		return []entrySpec{{key: "", label: "Gerät"}}
	}
	return nil
}

// largeFileSize ist die Grenze, ab der eine Datei nicht mehr geparst wird. Der Pi hat
// wenig Speicher, und ein geparster Baum braucht ein Vielfaches der Dateigröße.
const largeFileSize = 4 << 20

// fileChanges bewertet eine Datei. Fehlt sie auf einer Seite, ist sie neu oder
// entfällt. Dateien mit Einträgen werden je Eintrag verglichen. Große Dateien
// auf beiden Seiten vergleicht nur ihr Inhalt über Größe und sha256.
func fileChanges(name string, s, l archiveFile, inArchive, inLive bool) ([]Change, error) {
	if inArchive && inLive && (s.size > largeFileSize || l.size > largeFileSize) {
		same, err := sameFileStreamed(s.path, l.path)
		if err != nil {
			return nil, err
		}
		if same {
			return nil, nil
		}
		return []Change{{Kind: "mod", Label: name}}, nil
	}
	var sData, lData []byte
	var err error
	if inArchive {
		if sData, err = os.ReadFile(s.path); err != nil {
			return nil, err
		}
	}
	if inLive {
		if lData, err = os.ReadFile(l.path); err != nil {
			return nil, err
		}
	}
	if inArchive && inLive {
		if bytes.Equal(sData, lData) {
			return nil, nil
		}
		if name == usersArchiveName && usersWithoutSecrets(sData) {
			// Ohne Zugangsdaten im Archiv zählen die Hashes des aktuellen Stands nicht als Änderung.
			if stripped, err := StripUserSecrets(lData); err == nil {
				lData = stripped
			}
		}
		if specs := entrySpecs(name); len(specs) > 0 {
			if changes, ok := entryChanges(specs, sData, lData); ok {
				return changes, nil
			}
		}
		if sameJSON(sData, lData) {
			return nil, nil
		}
		return []Change{{Kind: "mod", Label: name}}, nil
	}
	if specs := entrySpecs(name); len(specs) > 0 {
		if changes, ok := entryChanges(specs, sData, lData); ok {
			return changes, nil
		}
	}
	if inArchive {
		return []Change{{Kind: "new", Label: name}}, nil
	}
	return []Change{{Kind: "del", Label: name}}, nil
}

// usersWithoutSecrets meldet, ob users.json keine Passwort-Hashes und keine
// Companion-Kopplung enthält, also so aussieht, wie StripUserSecrets es erzeugt.
func usersWithoutSecrets(data []byte) bool {
	stripped, err := StripUserSecrets(data)
	if err != nil {
		return false
	}
	return sameJSON(data, stripped)
}

// sameFileStreamed vergleicht zwei Dateien über Größe und sha256, ohne sie ganz in den Speicher zu lesen.
func sameFileStreamed(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	if ai.Size() != bi.Size() {
		return false, nil
	}
	ha, err := fileSHA256(a)
	if err != nil {
		return false, err
	}
	hb, err := fileSHA256(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ha, hb), nil
}

// fileSHA256 liefert den sha256 einer Datei, gelesen in Teilen.
func fileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// entryChanges vergleicht die Listen je Eintrag über id. Ist eine Seite kein
// lesbares Dokument der erwarteten Form, liefert sie ok false.
func entryChanges(specs []entrySpec, sData, lData []byte) ([]Change, bool) {
	var out []Change
	for _, spec := range specs {
		s, err := entriesOf(sData, spec.key)
		if err != nil {
			return nil, false
		}
		l, err := entriesOf(lData, spec.key)
		if err != nil {
			return nil, false
		}
		ids := map[string]bool{}
		for id := range s {
			ids[id] = true
		}
		for id := range l {
			ids[id] = true
		}
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for _, id := range sorted {
			a, inA := s[id]
			b, inB := l[id]
			switch {
			case inA && inB:
				if !reflect.DeepEqual(a.value, b.value) {
					out = append(out, Change{Kind: "mod", Label: spec.label, Name: a.name})
				}
			case inA:
				out = append(out, Change{Kind: "new", Label: spec.label, Name: a.name})
			default:
				out = append(out, Change{Kind: "del", Label: spec.label, Name: b.name})
			}
		}
	}
	return out, true
}

// entriesOf liest die Einträge der Liste key aus data. Leere Daten sind eine leere Liste.
func entriesOf(data []byte, key string) (map[string]entry, error) {
	out := map[string]entry{}
	if len(data) == 0 {
		return out, nil
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	list := doc
	if key != "" {
		obj, ok := doc.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: kein Objekt", key)
		}
		list = obj[key]
		if list == nil {
			return out, nil
		}
	}
	items, ok := list.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: keine Liste", key)
	}
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Eintrag %d ist kein Objekt", i)
		}
		id, _ := obj["id"].(string)
		if id == "" {
			id = "#" + strconv.Itoa(i)
		}
		name, _ := obj["name"].(string)
		if name == "" {
			name = id
		}
		out[id] = entry{name: name, value: item}
	}
	return out, nil
}

// sameJSON vergleicht zwei Dokumente unabhängig von Schlüsselreihenfolge und
// Leerraum. Ist eines kein JSON, zählt der Bytevergleich.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}
