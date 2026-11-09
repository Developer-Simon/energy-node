package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testSources legt einen kleinen Knoten im Temp-Verzeichnis an.
func testSources(t *testing.T) Sources {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	devices := filepath.Join(root, "devices")
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(data, "settings.json"), `{"theme":"dark"}`)
	write(filepath.Join(data, "users.json"), `{"users":[]}`)
	write(filepath.Join(data, "mqtt_credentials.json"), `{"password":"geheim"}`)
	write(filepath.Join(data, "energystats", "2026-11.json"), `{"kwh":12}`)
	write(filepath.Join(data, "sessions.json"), `{"tokens":["t"]}`)
	write(filepath.Join(devices, "meter_devices.json"), `[]`)
	appConfig := filepath.Join(root, "config.json")
	write(appConfig, `{"schema_version":2}`)
	battery := filepath.Join(root, "battery_state.json")
	write(battery, `{"soc":80}`)
	return Sources{
		DataDir:       data,
		DevicesDir:    devices,
		AppConfigPath: appConfig,
		BatteryStates: map[string]string{"soc": battery},
	}
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// rawEntry ist ein Tar-Eintrag für die Testarchive, die nicht Create baut.
type rawEntry struct {
	name     string
	typeflag byte
	linkname string
	size     int64
	body     io.Reader
}

func regEntry(name, body string) rawEntry {
	return rawEntry{name: name, typeflag: tar.TypeReg, size: int64(len(body)), body: strings.NewReader(body)}
}

// zeros liefert beliebig viele Nullbytes, ohne sie im Speicher zu halten.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func bigEntry(name string, size int64) rawEntry {
	return rawEntry{name: name, typeflag: tar.TypeReg, size: size, body: io.LimitReader(zeros{}, size)}
}

func manifestEntry(t *testing.T, m Manifest) rawEntry {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return regEntry("manifest.json", string(data))
}

// tarGz baut ein tar.gz aus den Einträgen, in der Reihenfolge der Argumente.
func tarGz(t *testing.T, entries ...rawEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o600, Typeflag: e.typeflag, Linkname: e.linkname, Size: e.size}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.body != nil {
			if _, err := io.CopyN(tw, e.body, e.size); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

// setNodeVersion setzt die eigene Version für einen Test und stellt sie danach wieder her.
func setNodeVersion(t *testing.T, v string) {
	t.Helper()
	old := NodeVersion
	NodeVersion = v
	t.Cleanup(func() { NodeVersion = old })
}

func allAreas() map[Area]bool {
	return map[Area]bool{
		AreaSettings: true, AreaDevices: true, AreaUsers: true, AreaSecrets: true,
		AreaStats: true, AreaHistory: true, AreaBattery: true,
	}
}

func TestRoundTripKeepsConfigAndChecksums(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	src := testSources(t)
	var buf bytes.Buffer
	opts := CreateOptions{
		Areas:       map[Area]bool{AreaSettings: true, AreaDevices: true, AreaUsers: true, AreaStats: true, AreaBattery: true},
		Trigger:     "manual",
		NodeVersion: "0.11.7",
	}
	created, err := Create(context.Background(), src, opts, &buf)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Format != FormatVersion || created.Trigger != "manual" || created.NodeVersion != "0.11.7" {
		t.Errorf("Manifest = %+v, want Format %d, Trigger manual, NodeVersion 0.11.7", created, FormatVersion)
	}

	staging := filepath.Join(t.TempDir(), "staging")
	got, err := ExtractToStaging(context.Background(), &buf, "", staging)
	if err != nil {
		t.Fatalf("ExtractToStaging: %v", err)
	}
	if !reflect.DeepEqual(got.Files, created.Files) {
		t.Errorf("Manifest.Files nach dem Lesen = %v, want %v", got.Files, created.Files)
	}

	cfg, err := os.ReadFile(filepath.Join(staging, "config", "config.json"))
	if err != nil {
		t.Fatalf("config/config.json fehlt im Staging: %v", err)
	}
	if string(cfg) != `{"schema_version":2}` {
		t.Errorf("config/config.json = %q", cfg)
	}

	for name, want := range got.Files {
		body, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("%s fehlt im Staging: %v", name, err)
			continue
		}
		if sum := sha(string(body)); sum != want {
			t.Errorf("Prüfsumme von %s = %s, want %s", name, sum, want)
		}
	}
	if _, ok := got.Files["battery/soc.json"]; !ok {
		t.Errorf("battery/soc.json fehlt im Manifest: %v", got.Files)
	}
	if _, ok := got.Files["data/mqtt_credentials.json"]; ok {
		t.Error("Geheimnis ohne Secrets im Archiv")
	}
}

func TestAreasNotChosenStayOut(t *testing.T) {
	src := testSources(t)

	var without bytes.Buffer
	m, err := Create(context.Background(), src, CreateOptions{Areas: map[Area]bool{AreaSettings: true}}, &without)
	if err != nil {
		t.Fatalf("Create ohne Statistik: %v", err)
	}
	if _, ok := m.Files["data/energystats/2026-11.json"]; ok {
		t.Error("Statistik ist im Archiv, obwohl der Bereich nicht gewählt wurde")
	}
	staging := filepath.Join(t.TempDir(), "staging")
	if _, err := ExtractToStaging(context.Background(), &without, "", staging); err != nil {
		t.Fatalf("ExtractToStaging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "data", "energystats")); !os.IsNotExist(err) {
		t.Errorf("data/energystats/ existiert im Staging, obwohl nicht gewählt (err %v)", err)
	}

	var with bytes.Buffer
	m, err = Create(context.Background(), src, CreateOptions{Areas: map[Area]bool{AreaStats: true}}, &with)
	if err != nil {
		t.Fatalf("Create mit Statistik: %v", err)
	}
	if _, ok := m.Files["data/energystats/2026-11.json"]; !ok {
		t.Errorf("Statistik fehlt, obwohl gewählt: %v", m.Files)
	}
}

func TestSessionsNeverInArchive(t *testing.T) {
	src := testSources(t)
	var buf bytes.Buffer
	m, err := Create(context.Background(), src, CreateOptions{Areas: allAreas(), Secrets: true}, &buf)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for name := range m.Files {
		if strings.Contains(name, "sessions") {
			t.Errorf("Sitzungsdatei im Manifest: %s", name)
		}
	}
	staging := filepath.Join(t.TempDir(), "staging")
	if _, err := ExtractToStaging(context.Background(), &buf, "", staging); err != nil {
		t.Fatalf("ExtractToStaging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "data", "sessions.json")); !os.IsNotExist(err) {
		t.Errorf("data/sessions.json im Staging (err %v)", err)
	}
}

func TestRejectsTraversalAndSymlink(t *testing.T) {
	cases := map[string]rawEntry{
		"relativ": regEntry("../x", "boese"),
		"absolut": regEntry("/etc/x", "boese"),
		"symlink": {name: "data/settings.json", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			staging := filepath.Join(base, "staging")
			buf := tarGz(t, entry, manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{}}))
			_, err := ExtractToStaging(context.Background(), buf, "", staging)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if !reflect.DeepEqual(names, []string{"staging"}) {
				t.Errorf("außerhalb des Stagings angelegt: %v", names)
			}
		})
	}
}

func TestRejectsPathOutsideCatalog(t *testing.T) {
	buf := tarGz(t,
		regEntry("data/sessions.json", `{"tokens":[]}`),
		manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{}}),
	)
	_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestRejectsOversize(t *testing.T) {
	t.Run("datei", func(t *testing.T) {
		buf := tarGz(t,
			bigEntry("data/settings.json", MaxFileSize+1),
			manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{}}),
		)
		_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
	})
	t.Run("summe", func(t *testing.T) {
		// Vier Dateien an der Grenze ergeben genau MaxTotalSize, die fünfte sprengt sie.
		entries := []rawEntry{}
		for i := 1; i <= 4; i++ {
			entries = append(entries, bigEntry("battery/"+string(rune('0'+i))+".json", MaxFileSize))
		}
		entries = append(entries, regEntry("battery/5.json", "x"))
		entries = append(entries, manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{}}))
		buf := tarGz(t, entries...)
		_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
	})
}

func TestRejectsChecksumMismatch(t *testing.T) {
	original := `{"theme":"dark"}`
	tampered := `{"theme":"darm"}` // gleiche Länge, ein Byte anders
	buf := tarGz(t,
		regEntry("data/settings.json", tampered),
		manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{"data/settings.json": sha(original)}}),
	)
	_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if !strings.Contains(err.Error(), "data/settings.json") {
		t.Errorf("Meldung nennt die Datei nicht: %v", err)
	}
}

func TestRejectsFileListMismatch(t *testing.T) {
	t.Run("datei ohne Eintrag", func(t *testing.T) {
		buf := tarGz(t,
			regEntry("data/settings.json", `{}`),
			manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{}}),
		)
		_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
	})
	t.Run("eintrag ohne Datei", func(t *testing.T) {
		buf := tarGz(t,
			manifestEntry(t, Manifest{Format: FormatVersion, Files: map[string]string{"data/settings.json": sha(`{}`)}}),
		)
		_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
	})
}

func TestRejectsNewerFormatAndVersion(t *testing.T) {
	setNodeVersion(t, "0.11.7")
	tests := []struct {
		name    string
		m       Manifest
		node    string
		wantErr error
	}{
		{"neueres Format", Manifest{Format: FormatVersion + 1, Files: map[string]string{}}, "0.11.7", ErrTooNew},
		{"neuere Version", Manifest{Format: FormatVersion, NodeVersion: "0.12.0", Files: map[string]string{}}, "0.11.7", ErrTooNew},
		{"gleiche Version", Manifest{Format: FormatVersion, NodeVersion: "0.11.7", Files: map[string]string{}}, "0.11.7", nil},
		{"ältere Version", Manifest{Format: FormatVersion, NodeVersion: "0.11.6", Files: map[string]string{}}, "0.11.7", nil},
		{"eigener Build dev", Manifest{Format: FormatVersion, NodeVersion: "99.0.0", Files: map[string]string{}}, "dev", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			NodeVersion = tc.node
			buf := tarGz(t, manifestEntry(t, tc.m))
			_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRejectsMissingManifest(t *testing.T) {
	buf := tarGz(t, regEntry("readme.txt", "hallo"))
	_, err := ExtractToStaging(context.Background(), buf, "", filepath.Join(t.TempDir(), "staging"))
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("fremdes tar.gz: err = %v, want ErrIncompatible", err)
	}

	_, err = ExtractToStaging(context.Background(), strings.NewReader("kein gzip"), "", filepath.Join(t.TempDir(), "staging"))
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("kein gzip: err = %v, want ErrIncompatible", err)
	}
}

func TestManifestSecretsReflectsWhatWasWritten(t *testing.T) {
	cases := []struct {
		name        string
		areas       map[Area]bool
		secrets     bool
		noCreds     bool
		noUsers     bool
		want        bool
		wantCredIn  bool
		wantUsersIn bool
	}{
		{name: "a secrets area with credential files", areas: allAreas(), secrets: true, want: true, wantCredIn: true, wantUsersIn: true},
		{name: "no credential files but users with hashes", areas: allAreas(), secrets: true, noCreds: true, want: true, wantUsersIn: true},
		{name: "no credential files and no users file", areas: allAreas(), secrets: true, noCreds: true, noUsers: true, want: false},
		{name: "secrets off with credential files", areas: allAreas(), secrets: false, want: false, wantUsersIn: true},
		{name: "users area only keeps hashes", areas: map[Area]bool{AreaUsers: true}, secrets: true, want: true, wantUsersIn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := testSources(t)
			if tc.noCreds {
				for _, f := range []string{"mqtt_credentials.json", "mqtt_bridge_credentials.json", "tinytuya_credentials.json"} {
					_ = os.Remove(filepath.Join(src.DataDir, f))
				}
			}
			if tc.noUsers {
				if err := os.Remove(filepath.Join(src.DataDir, "users.json")); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			m, err := Create(context.Background(), src, CreateOptions{Areas: tc.areas, Secrets: tc.secrets, NodeVersion: "dev"}, &buf)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if m.Secrets != tc.want {
				t.Errorf("Manifest.Secrets = %v, want %v", m.Secrets, tc.want)
			}
			_, credIn := m.Files["data/mqtt_credentials.json"]
			if credIn != tc.wantCredIn {
				t.Errorf("credential file in archive = %v, want %v", credIn, tc.wantCredIn)
			}
			_, usersIn := m.Files[usersArchiveName]
			if usersIn != tc.wantUsersIn {
				t.Errorf("users file in archive = %v, want %v", usersIn, tc.wantUsersIn)
			}
		})
	}
}
