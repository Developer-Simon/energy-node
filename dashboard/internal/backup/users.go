package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoAdmin meldet ein Ergebnis ohne aktives lokales Konto mit user_management.
var ErrNoAdmin = errors.New("kein aktives Konto mit user_management")

// roleUserManagement ist der Berechtigungsschlüssel aus dem Auth-Paket.
const roleUserManagement = "user_management"

// Die Zugangsdaten in users.json. ha_users enthält keine, bleibt also unberührt.
const (
	userPasswordHash     = "password_hash"
	userHTTPPasswordHash = "http_password_hash"
	companionKey         = "companion"
)

func init() {
	stripUsers = StripUserSecrets
}

// StripUserSecrets entfernt aus users.json die Passwort-Hashes und die
// Companion-Kopplung. Unbekannte Felder bleiben erhalten.
func StripUserSecrets(usersJSON []byte) ([]byte, error) {
	doc, err := decodeUsers(usersJSON)
	if err != nil {
		return nil, err
	}
	delete(doc, companionKey)
	for _, u := range userList(doc) {
		delete(u, userPasswordHash)
		delete(u, userHTTPPasswordHash)
	}
	return encodeUsers(doc)
}

// MergeUsers baut aus dem Archiv und dem aktuellen Stand die neue users.json.
// Mit Zugangsdaten im Archiv ersetzt das Archiv den aktuellen Stand. Ohne sie
// übernimmt die Zusammenführung die Hashes je Benutzername und die Kopplung aus
// dem aktuellen Stand. Benutzer ohne Treffer werden gesperrt und in disabled
// aufgeführt. Ein Ergebnis ohne aktiven Admin wird abgelehnt, dann bleibt der
// aktuelle Stand bestehen.
func MergeUsers(current, archived []byte, archiveHasSecrets bool) (merged []byte, disabled []string, err error) {
	cur, err := decodeUsers(current)
	if err != nil {
		return current, nil, err
	}
	arc, err := decodeUsers(archived)
	if err != nil {
		return current, nil, err
	}

	if archiveHasSecrets {
		if err := requireAdmin(arc); err != nil {
			return current, nil, err
		}
		return archived, nil, nil
	}

	known := make(map[string]map[string]any)
	for _, u := range userList(cur) {
		known[normalizedName(u)] = u
	}
	for _, u := range userList(arc) {
		name := normalizedName(u)
		delete(u, userPasswordHash)
		delete(u, userHTTPPasswordHash)
		have, ok := known[name]
		if !ok {
			u["disabled"] = true
			disabled = append(disabled, name)
			continue
		}
		for _, key := range []string{userPasswordHash, userHTTPPasswordHash} {
			if value, ok := have[key]; ok {
				u[key] = value
			}
		}
	}
	if companion, ok := cur[companionKey]; ok {
		arc[companionKey] = companion
	} else {
		delete(arc, companionKey)
	}
	if err := requireAdmin(arc); err != nil {
		return current, nil, err
	}
	merged, err = encodeUsers(arc)
	if err != nil {
		return current, nil, err
	}
	return merged, disabled, nil
}

// decodeUsers liest users.json als map. Zahlen bleiben json.Number, damit
// nichts beim Umkodieren verrutscht.
func decodeUsers(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("users.json: %w", err)
	}
	if doc == nil {
		return nil, errors.New("users.json ist leer")
	}
	return doc, nil
}

func encodeUsers(doc map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("users.json: %w", err)
	}
	return append(out, '\n'), nil
}

// userList liefert die Benutzereinträge von users.json. Fehlt die Liste oder
// ist sie kein Array, gibt es keine Einträge.
func userList(doc map[string]any) []map[string]any {
	items, _ := doc["users"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if u, ok := item.(map[string]any); ok {
			out = append(out, u)
		}
	}
	return out
}

func normalizedName(u map[string]any) string {
	name, _ := u["username"].(string)
	return strings.ToLower(name)
}

// requireAdmin verlangt mindestens ein aktives, lokales Konto mit user_management.
func requireAdmin(doc map[string]any) error {
	for _, u := range userList(doc) {
		if guest, _ := u["guest"].(bool); guest {
			continue
		}
		if disabled, _ := u["disabled"].(bool); disabled {
			continue
		}
		roles, _ := u["roles"].([]any)
		for _, role := range roles {
			if role == roleUserManagement {
				return nil
			}
		}
	}
	return ErrNoAdmin
}
