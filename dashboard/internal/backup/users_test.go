package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const usersWithSecrets = `{
  "version": 1,
  "guest_roles": ["view"],
  "future_field": {"keep": true},
  "users": [
    {"username": "simon", "password_hash": "pw-simon", "http_password_hash": "http-simon", "roles": ["user_management"], "disabled": false, "created_at": "2026-01-01T00:00:00Z", "last_login_at": "2026-01-02T00:00:00Z"},
    {"username": "anna", "password_hash": "pw-anna", "roles": ["view"], "disabled": false, "future_field": "keep"}
  ],
  "companion": {"label": "Home Assistant", "token_hash": "companion-hash", "paired_at": "2026-01-03T00:00:00Z", "last_seen_at": "2026-01-04T00:00:00Z"},
  "ha_users": [{"ha_user_id": "ha-1", "username": "hausr", "disabled": false, "roles": ["view"]}]
}`

// decodeTestDoc liest JSON als map, damit Tests Felder prüfen können.
func decodeTestDoc(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("users.json ist kein JSON: %v", err)
	}
	return doc
}

// testUser liefert den Eintrag mit dem Namen aus der users-Liste.
func testUser(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	list, _ := doc["users"].([]any)
	for _, item := range list {
		u, _ := item.(map[string]any)
		if u["username"] == name {
			return u
		}
	}
	t.Fatalf("Benutzer %q fehlt", name)
	return nil
}

func TestStripRemovesAllHashes(t *testing.T) {
	out, err := StripUserSecrets([]byte(usersWithSecrets))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"pw-simon", "http-simon", "pw-anna", "companion-hash"} {
		if bytes.Contains(out, []byte(secret)) {
			t.Fatalf("Geheimnis %q steht noch im Ergebnis: %s", secret, out)
		}
	}
	doc := decodeTestDoc(t, out)
	if _, ok := doc["companion"]; ok {
		t.Fatal("companion bleibt nach dem Entfernen der Zugangsdaten stehen")
	}
	for _, name := range []string{"simon", "anna"} {
		u := testUser(t, doc, name)
		if _, ok := u["password_hash"]; ok {
			t.Fatalf("password_hash von %s bleibt stehen", name)
		}
		if _, ok := u["http_password_hash"]; ok {
			t.Fatalf("http_password_hash von %s bleibt stehen", name)
		}
	}
}

func TestStripKeepsUnknownFields(t *testing.T) {
	out, err := StripUserSecrets([]byte(usersWithSecrets))
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeTestDoc(t, out)
	if !reflect.DeepEqual(doc["future_field"], map[string]any{"keep": true}) {
		t.Fatalf("unbekanntes Feld oben fehlt: %v", doc["future_field"])
	}
	if doc["version"] != float64(1) {
		t.Fatalf("version verändert: %v", doc["version"])
	}
	if anna := testUser(t, doc, "anna"); anna["future_field"] != "keep" {
		t.Fatalf("unbekanntes Feld am Benutzer fehlt: %v", anna)
	}
	if simon := testUser(t, doc, "simon"); simon["last_login_at"] != "2026-01-02T00:00:00Z" {
		t.Fatalf("last_login_at verändert: %v", simon["last_login_at"])
	}
	if _, ok := doc["ha_users"]; !ok {
		t.Fatal("ha_users fehlt, HAAccount hat keine Zugangsdaten und bleibt unberührt")
	}
}

func TestMergeKeepsExistingHashes(t *testing.T) {
	current := []byte(usersWithSecrets)
	archived, err := StripUserSecrets([]byte(usersWithSecrets))
	if err != nil {
		t.Fatal(err)
	}
	merged, disabled, err := MergeUsers(current, archived, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(disabled) != 0 {
		t.Fatalf("keine Benutzer sollten gesperrt sein, gesperrt: %v", disabled)
	}
	doc := decodeTestDoc(t, merged)
	simon := testUser(t, doc, "simon")
	if simon["password_hash"] != "pw-simon" || simon["http_password_hash"] != "http-simon" {
		t.Fatalf("Hashes von simon nicht aus dem aktuellen Stand: %v", simon)
	}
	if anna := testUser(t, doc, "anna"); anna["password_hash"] != "pw-anna" {
		t.Fatalf("Hash von anna nicht aus dem aktuellen Stand: %v", anna)
	}
	companion, _ := doc["companion"].(map[string]any)
	if companion["token_hash"] != "companion-hash" {
		t.Fatalf("Kopplung kommt nicht aus dem aktuellen Stand: %v", doc["companion"])
	}
}

func TestMergeDisablesNewUsersWithoutHash(t *testing.T) {
	current := []byte(usersWithSecrets)
	archived := []byte(`{"users": [
      {"username": "simon", "roles": ["user_management"], "disabled": false},
      {"username": "neu", "roles": ["view"], "disabled": false}
    ]}`)
	merged, disabled, err := MergeUsers(current, archived, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(disabled, []string{"neu"}) {
		t.Fatalf("disabled = %v, erwartet [neu]", disabled)
	}
	doc := decodeTestDoc(t, merged)
	neu := testUser(t, doc, "neu")
	if neu["disabled"] != true {
		t.Fatalf("neuer Benutzer ohne Hash ist nicht gesperrt: %v", neu)
	}
	if _, ok := neu["password_hash"]; ok {
		t.Fatalf("neuer Benutzer bekommt einen Hash: %v", neu)
	}
}

func TestMergeNeverLocksOutLastAdmin(t *testing.T) {
	current := []byte(usersWithSecrets)
	// Im Archiv ist der einzige Admin gesperrt, also bliebe kein aktiver Admin.
	archived := []byte(`{"users": [
      {"username": "simon", "roles": ["user_management"], "disabled": true},
      {"username": "anna", "roles": ["view"], "disabled": false}
    ]}`)
	merged, disabled, err := MergeUsers(current, archived, false)
	if !errors.Is(err, ErrNoAdmin) {
		t.Fatalf("err = %v, erwartet ErrNoAdmin", err)
	}
	if !bytes.Equal(merged, current) {
		t.Fatal("bei Fehler muss der aktuelle Stand bleiben")
	}
	if len(disabled) != 0 {
		t.Fatalf("bei Fehler keine Liste gesperrter Benutzer erwartet: %v", disabled)
	}
}

func TestMergeWithSecretsReplaces(t *testing.T) {
	current := []byte(usersWithSecrets)
	archived := []byte(`{"users": [
      {"username": "simon", "password_hash": "archiv-simon", "roles": ["user_management"], "disabled": false}
    ], "companion": {"label": "Archiv", "token_hash": "archiv-companion"}}`)
	merged, disabled, err := MergeUsers(current, archived, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(disabled) != 0 {
		t.Fatalf("mit Zugangsdaten werden keine Benutzer gesperrt: %v", disabled)
	}
	if !reflect.DeepEqual(decodeTestDoc(t, merged), decodeTestDoc(t, archived)) {
		t.Fatalf("mit Zugangsdaten muss das Archiv den aktuellen Stand ersetzen: %s", merged)
	}
}

// readTarEntry liest eine Datei aus einem nicht verschlüsselten Backup-Archiv.
func readTarEntry(t *testing.T, archive []byte, name string) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("%s fehlt im Archiv", name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == name {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
}

func createWithUsers(t *testing.T, secrets bool) []byte {
	t.Helper()
	src := testSources(t)
	if err := os.WriteFile(filepath.Join(src.DataDir, "users.json"), []byte(usersWithSecrets), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	opts := CreateOptions{Areas: allAreas(), Secrets: secrets, NodeVersion: "dev"}
	if _, err := Create(context.Background(), src, opts, &buf); err != nil {
		t.Fatal(err)
	}
	return readTarEntry(t, buf.Bytes(), usersArchiveName)
}

func TestCreateStripsUserSecretsWithoutSecrets(t *testing.T) {
	users := createWithUsers(t, false)
	if bytes.Contains(users, []byte("password_hash")) || bytes.Contains(users, []byte("companion")) {
		t.Fatalf("Archiv ohne Zugangsdaten enthält Hashes: %s", users)
	}
	if !strings.Contains(string(users), "future_field") {
		t.Fatalf("unbekannte Felder gehen beim Archivieren verloren: %s", users)
	}
}

func TestCreateKeepsUserSecretsWithSecrets(t *testing.T) {
	users := createWithUsers(t, true)
	if !bytes.Contains(users, []byte("pw-simon")) || !bytes.Contains(users, []byte("companion-hash")) {
		t.Fatalf("Archiv mit Zugangsdaten verliert die Hashes: %s", users)
	}
}
