// Package backup beschreibt, welche Dateien des Knotens ein Backup enthaelt.
// Der Katalog ordnet jede Datei einer Wurzel, einer Klasse und einem Bereich
// zu. Config- und Secret-Dateien gehoeren ins Backup, Disposable-Dateien nie.
package backup

import "path"

// Class bestimmt, wie eine Datei gesichert oder verworfen wird.
type Class string

const (
	// ClassConfig ist Konfiguration, die immer ins Backup gehoert.
	ClassConfig Class = "config"
	// ClassSecret sind Zugangsdaten, die nur mit ihrem Bereich ins Backup kommen.
	ClassSecret Class = "secret"
	// ClassRuntime ist Laufzeitzustand, etwa Statistik oder Verlauf.
	ClassRuntime Class = "runtime"
	// ClassDisposable wird nie gesichert, weil der Knoten sie neu erzeugt.
	ClassDisposable Class = "disposable"
	// ClassEnvironment beschreibt Dateien der Umgebung des Knotens. Sie werden
	// nie archiviert.
	ClassEnvironment Class = "environment"
)

// Area fasst Dateien zu einem Bereich zusammen, den man beim Backup waehlt.
type Area string

const (
	AreaSettings Area = "settings"
	AreaDevices  Area = "devices"
	AreaUsers    Area = "users"
	AreaSecrets  Area = "secrets"
	AreaStats    Area = "stats"
	AreaHistory  Area = "history"
	AreaBattery  Area = "battery"
)

// Root ist das Verzeichnis, in dem eine Datei liegt.
type Root string

const (
	// RootData ist das Datenverzeichnis des Dashboards.
	RootData Root = "data"
	// RootDevices ist das Verzeichnis der Geraetedaten.
	RootDevices Root = "devices"
)

// Entry ordnet ein Muster innerhalb einer Wurzel einer Klasse und einem Bereich zu.
// Das Muster wird mit path.Match gegen den relativen Pfad geprueft, Verzeichnisse
// stehen als "verzeichnis/*".
type Entry struct {
	Root    Root
	Pattern string
	Class   Class
	Area    Area
}

var catalog = []Entry{
	{RootData, "settings.json", ClassConfig, AreaSettings},
	{RootData, "layout.json", ClassConfig, AreaSettings},
	{RootData, "device-map.json", ClassConfig, AreaSettings},
	{RootData, "device-prefs.json", ClassConfig, AreaSettings},
	{RootData, "energy.json", ClassConfig, AreaSettings},
	{RootData, "onboarding.json", ClassConfig, AreaSettings},
	{RootData, "ignored_devices.json", ClassConfig, AreaSettings},
	{RootData, "mqtt.json", ClassConfig, AreaSettings},
	{RootData, "bridge.json", ClassConfig, AreaSettings},
	// users.json enthaelt auch Hashes, die Task 4 als Secret behandelt.
	{RootData, "users.json", ClassConfig, AreaUsers},

	{RootDevices, "*_devices.json", ClassConfig, AreaDevices},
	{RootDevices, "automation_rules.json", ClassConfig, AreaDevices},
	{RootDevices, "shelly_presets.json", ClassConfig, AreaDevices},

	{RootData, "mqtt_credentials.json", ClassSecret, AreaSecrets},
	{RootData, "mqtt_bridge_credentials.json", ClassSecret, AreaSecrets},
	{RootData, "tinytuya_credentials.json", ClassSecret, AreaSecrets},

	{RootData, "energystats/*", ClassRuntime, AreaStats},
	{RootDevices, "automation_history.json", ClassRuntime, AreaHistory},

	{RootData, "energydiscovery/*", ClassDisposable, ""},
	{RootData, "runtime.json", ClassDisposable, ""},
	{RootData, "storage-health.json", ClassDisposable, ""},
	{RootData, "sessions.json", ClassDisposable, ""},
	{RootData, "redeploy-candidate/*", ClassDisposable, ""},
	{RootData, "revisions/*", ClassDisposable, ""},
	{RootData, "backups/*", ClassDisposable, ""},
	{RootData, "backup-pending/*", ClassDisposable, ""},
	{RootData, "backup-result.json", ClassDisposable, ""},
	{RootData, "journal.json", ClassDisposable, ""},
	// Die Staging-Datei fuer apply-app-config, die nach dem Einspielen entfernt wird.
	{RootData, "energy-node.config.staged.json", ClassDisposable, ""},
	{RootDevices, "*.schema.json", ClassDisposable, ""},
	{RootDevices, "revisions/*", ClassDisposable, ""},
}

// Catalog liefert alle Katalogeintraege in ihrer Reihenfolge.
func Catalog() []Entry {
	out := make([]Entry, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup sucht den ersten Eintrag, der rel in der Wurzel root zugeordnet ist.
func Lookup(root Root, rel string) (Entry, bool) {
	for _, e := range catalog {
		if e.Root != root {
			continue
		}
		if ok, _ := path.Match(e.Pattern, rel); ok {
			return e, true
		}
	}
	return Entry{}, false
}

// ArchiveEntries liefert die Eintraege fuer ein Backup. Config wird immer
// gesichert. Secret und Runtime nur, wenn ihr Bereich in areas gewaehlt ist.
// Disposable und Environment fehlen immer.
func ArchiveEntries(areas map[Area]bool) []Entry {
	var out []Entry
	for _, e := range catalog {
		switch e.Class {
		case ClassConfig:
			out = append(out, e)
		case ClassSecret, ClassRuntime:
			if areas[e.Area] {
				out = append(out, e)
			}
		}
	}
	return out
}
