package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// applyRoot legt Datenverzeichnis, Geräteverzeichnis und Batteriezustände im Temp-Verzeichnis an.
func applyRoot(t *testing.T) Sources {
	t.Helper()
	root := t.TempDir()
	return Sources{
		DataDir:       filepath.Join(root, "data"),
		DevicesDir:    filepath.Join(root, "devices"),
		BatteryStates: map[string]string{},
	}
}

// writeTestFile schreibt eine Datei samt Verzeichnissen.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// readTestFile liest eine Datei und meldet einen Fehler, wenn sie fehlt.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// stageArchive legt eine Staging-Tree mit den Dateien an und bucht sie über WritePending ein.
func stageArchive(t *testing.T, src Sources, files map[string]string, p Pending) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "stage")
	for name, content := range files {
		writeTestFile(t, filepath.Join(stage, filepath.FromSlash(name)), content)
	}
	if err := WritePending(src.DataDir, stage, p); err != nil {
		t.Fatal(err)
	}
}

func TestApplyReplacesChosenAreasOnly(t *testing.T) {
	src := applyRoot(t)
	writeTestFile(t, filepath.Join(src.DataDir, "settings.json"), `{"old":1}`)
	writeTestFile(t, filepath.Join(src.DataDir, "mqtt_credentials.json"), `{"secret":"old"}`)
	writeTestFile(t, filepath.Join(src.DevicesDir, "shelly_devices.json"), `{"devices":"old"}`)

	stageArchive(t, src, map[string]string{
		"data/settings.json":          `{"new":1}`,
		"data/mqtt_credentials.json":  `{"secret":"new"}`,
		"devices/shelly_devices.json": `{"devices":"new"}`,
	}, Pending{Areas: map[Area]bool{AreaSettings: true}})

	res, err := ApplyPending(src)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.OK || res.Code != "" {
		t.Fatalf("Result = %+v, want OK", res)
	}
	if len(res.Applied) != 1 || res.Applied[0] != AreaSettings {
		t.Fatalf("Applied = %v, want [settings]", res.Applied)
	}
	if got := readTestFile(t, filepath.Join(src.DataDir, "settings.json")); got != `{"new":1}` {
		t.Errorf("settings.json = %s, want the staged content", got)
	}
	if got := readTestFile(t, filepath.Join(src.DataDir, "mqtt_credentials.json")); got != `{"secret":"old"}` {
		t.Errorf("mqtt_credentials.json = %s, want the old content", got)
	}
	if got := readTestFile(t, filepath.Join(src.DevicesDir, "shelly_devices.json")); got != `{"devices":"old"}` {
		t.Errorf("shelly_devices.json = %s, want the old content", got)
	}
	if _, err := os.Stat(filepath.Join(src.DataDir, "backup-pending")); !os.IsNotExist(err) {
		t.Errorf("backup-pending should be removed after a successful apply, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(src.DataDir, "backup-result.json")); err != nil {
		t.Errorf("backup-result.json missing: %v", err)
	}
}

func TestApplyNoPendingIsNoOp(t *testing.T) {
	src := applyRoot(t)
	res, err := ApplyPending(src)
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("Result = %+v, want nil without pending import", res)
	}
	if _, err := os.Stat(src.DataDir); !os.IsNotExist(err) {
		t.Errorf("data dir was created by a no-op apply, stat err = %v", err)
	}
}

func TestInterruptedApplyRollsBackOnNextStart(t *testing.T) {
	src := applyRoot(t)
	pendingDir := filepath.Join(src.DataDir, pendingDirName)
	writeTestFile(t, filepath.Join(pendingDir, oldDirName, "file-0"), "old settings")
	// Die Live-Datei settings.json ist halb geschrieben, fresh.json war vorher nicht da.
	writeTestFile(t, filepath.Join(src.DataDir, "settings.json"), "half writ")
	writeTestFile(t, filepath.Join(src.DataDir, "fresh.json"), "half")
	writeTestFile(t, filepath.Join(pendingDir, pendingFile), `{"areas":{"settings":true}}`)
	err := writeJournal(pendingDir, journal{Entries: []journalEntry{
		{Target: filepath.Join(src.DataDir, "settings.json"), Existed: true, Old: "file-0"},
		{Target: filepath.Join(src.DataDir, "fresh.json")},
	}})
	if err != nil {
		t.Fatal(err)
	}

	res, err := ApplyPending(src)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.OK || res.Code != "interrupted" {
		t.Fatalf("Result = %+v, want interrupted", res)
	}
	if got := readTestFile(t, filepath.Join(src.DataDir, "settings.json")); got != "old settings" {
		t.Errorf("settings.json = %q, want the old content", got)
	}
	if _, err := os.Stat(filepath.Join(src.DataDir, "fresh.json")); !os.IsNotExist(err) {
		t.Errorf("fresh.json should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(pendingDir); !os.IsNotExist(err) {
		t.Errorf("backup-pending should be removed, stat err = %v", err)
	}
	stored, err := ReadResult(src.DataDir)
	if err != nil || stored == nil || stored.Code != "interrupted" {
		t.Errorf("stored result = %+v, err %v, want interrupted", stored, err)
	}
}

func TestApplyWritesBatteryStateById(t *testing.T) {
	src := applyRoot(t)
	state := filepath.Join(t.TempDir(), "state.json")
	src.BatteryStates["bat1"] = state

	stageArchive(t, src, map[string]string{
		"battery/bat1.json": `{"soc":50}`,
	}, Pending{Areas: map[Area]bool{AreaBattery: true}})

	res, err := ApplyPending(src)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.OK {
		t.Fatalf("Result = %+v, want OK", res)
	}
	if got := readTestFile(t, state); got != `{"soc":50}` {
		t.Errorf("state file = %s, want the staged content", got)
	}
}

func TestApplySkipsBatteryWithoutEntry(t *testing.T) {
	src := applyRoot(t)
	base := t.TempDir()
	src.BatteryStates["bat1"] = filepath.Join(base, "bat1.json")

	stageArchive(t, src, map[string]string{
		"battery/bat1.json":  `{"soc":1}`,
		"battery/other.json": `{"soc":2}`,
	}, Pending{Areas: map[Area]bool{AreaBattery: true}})

	res, err := ApplyPending(src)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.OK {
		t.Fatalf("Result = %+v, want OK", res)
	}
	if got := readTestFile(t, filepath.Join(base, "bat1.json")); got != `{"soc":1}` {
		t.Errorf("bat1 state = %s, want the staged content", got)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("battery without entry was written, base dir has %d entries", len(entries))
	}
}
