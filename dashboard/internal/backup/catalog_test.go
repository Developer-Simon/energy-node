package backup

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// jsonLiteral findet Dateinamen wie "settings.json" oder "energystats/x.json"
// in Quelltexten. Ein Stamm ist Pflicht, damit ".json" allein nicht trifft.
// Das \b verhindert Treffer wie "spec.jsonKeyField".
var jsonLiteral = regexp.MustCompile(`[A-Za-z0-9_./-]+\.json\b`)

// notNodeData sind JSON-Dateinamen im Code, die nicht zum Knotenzustand
// gehoeren. Jeder Eintrag ist entweder ein Updater- oder Installer-Artefakt
// (liegt nicht in Data oder Devices), eine eingebettete Ressource, eine
// Testdaten-Referenz in einem Kommentar oder ein Pfad aus Task 2.
var notNodeData = map[string]bool{
	// Updater- und Installer-Artefakte im Staging- und Statusverzeichnis.
	"announced.json":          true,
	"current.json":            true,
	"pending.json":            true,
	"status.json":             true,
	"manifest.json":           true,
	"installed-manifest.json": true,
	"previous-manifest.json":  true,
	"selection.json":          true,
	"onboarding-seed.json":    true,
	"_devices.json":           true,
	"changelog.json":          true,
	"components.json":         true,
	"job.json":                true,
	// Eingebettete Ressourcen und Testdaten, keine Dateien im Datenverzeichnis.
	"de.json":            true,
	"en.json":            true,
	"icons.source.json":  true,
	"cases.json":         true,
	"balance-cases.json": true,
	// config.json ist die App-Konfiguration unter /etc/energy-node, nicht Teil
	// des Datenverzeichnisses. Das Archiv nimmt sie unter config/config.json auf (Task 2).
	"config.json": true,
}

// classifiedName meldet, ob ein Name oder Pfad im Katalog steht.
func classifiedName(tok, name string) bool {
	for _, e := range Catalog() {
		if ok, _ := path.Match(e.Pattern, tok); ok {
			return true
		}
		if ok, _ := path.Match(e.Pattern, name); ok {
			return true
		}
		// Ein Eintrag in einem Unterordner nennt die Datei mit Pfad, der Code
		// oft nur mit ihrem Namen.
		if base := path.Base(e.Pattern); base == name && !strings.ContainsAny(base, "*?[") {
			return true
		}
	}
	return false
}

func TestLookupClassifies(t *testing.T) {
	tests := []struct {
		root  Root
		rel   string
		class Class
		area  Area
	}{
		{RootData, "settings.json", ClassConfig, AreaSettings},
		{RootData, "energystats/2026-11.json", ClassRuntime, AreaStats},
		{RootData, "energystats/chains/learned.json", ClassRuntime, AreaStats},
		{RootData, "energystats/chains/house.json", ClassRuntime, AreaStats},
		{RootData, "sessions.json", ClassDisposable, ""},
	}
	for _, tc := range tests {
		e, ok := Lookup(tc.root, tc.rel)
		if !ok {
			t.Fatalf("Lookup(%q, %q): kein Katalogeintrag", tc.root, tc.rel)
		}
		if e.Class != tc.class || e.Area != tc.area {
			t.Errorf("Lookup(%q, %q) = Klasse %q, Bereich %q; want %q, %q",
				tc.root, tc.rel, e.Class, e.Area, tc.class, tc.area)
		}
	}
	if _, ok := Lookup(RootData, "unbekannt.json"); ok {
		t.Error("Lookup(RootData, unbekannt.json) meldet einen Treffer, want keinen")
	}
}

func TestArchiveEntriesAlwaysHasConfigNeverDisposable(t *testing.T) {
	all := map[Area]bool{
		AreaSettings: true, AreaDevices: true, AreaUsers: true, AreaSecrets: true,
		AreaStats: true, AreaHistory: true, AreaBattery: true,
	}
	selections := []map[Area]bool{nil, {}, {AreaStats: true}, {AreaSecrets: true, AreaHistory: true}, all}

	configCount := 0
	for _, e := range Catalog() {
		if e.Class == ClassConfig {
			configCount++
		}
	}

	for _, areas := range selections {
		entries := ArchiveEntries(areas)
		configInArchive := 0
		for _, e := range entries {
			if e.Class == ClassDisposable {
				t.Errorf("ArchiveEntries(%v) enthaelt Disposable-Eintrag %q", areas, e.Pattern)
			}
			if e.Class == ClassConfig {
				configInArchive++
			}
		}
		if configInArchive != configCount {
			t.Errorf("ArchiveEntries(%v) enthaelt %d Config-Eintraege, want %d", areas, configInArchive, configCount)
		}
	}
}

func TestEveryJSONFileInCodeIsClassified(t *testing.T) {
	var unclassified []string
	for _, dir := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, tok := range jsonLiteral.FindAllString(string(src), -1) {
				// Fuehrende Punkte sind Temp- oder Versteckt-Praefixe wie ".config.json".
				name := strings.TrimLeft(path.Base(tok), ".")
				if notNodeData[name] || notNodeData[tok] || classifiedName(tok, name) {
					continue
				}
				unclassified = append(unclassified, p+": "+tok)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Durchlauf von %s: %v", dir, err)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("JSON-Dateien ohne Einordnung. Bitte in catalog.go einordnen, nicht in notNodeData schieben, "+
			"ausser die Datei liegt nachweislich nicht im Daten- oder Geraetverzeichnis:\n%s",
			strings.Join(unclassified, "\n"))
	}
}
