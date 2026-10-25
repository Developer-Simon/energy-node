package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// settingsFieldRoles names the permission that a changed settings field needs
// instead of edit_settings.
var settingsFieldRoles = map[string]string{
	"live_update_interval_seconds": auth.RoleTuneLiveUpdates,
	"history_views":                auth.RoleViewHistory,
}

// requireRoleIfUser is requireRole for requests without a user in the context
// (no session): they stay open, as before accounts existed.
func requireRoleIfUser(w http.ResponseWriter, r *http.Request, role, code, key, message string) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return true
	}
	if !auth.HasRole(user, role) {
		writeErrorKey(w, http.StatusForbidden, code, key, nil, message)
		return false
	}
	return true
}

// settingsChangeRoles lists the permissions a settings write needs: every
// field whose value differs gets its role from settingsFieldRoles, all other
// changed fields need edit_settings. The result is sorted and without duplicates.
func settingsChangeRoles(current, next settings.Settings) ([]string, error) {
	before, err := settingsFields(current)
	if err != nil {
		return nil, err
	}
	after, err := settingsFields(next)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	roles := []string{}
	for key, value := range after {
		if bytes.Equal(before[key], value) {
			continue
		}
		role, ok := settingsFieldRoles[key]
		if !ok {
			role = auth.RoleEditSettings
		}
		if !seen[role] {
			seen[role] = true
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	return roles, nil
}

func settingsFields(value settings.Settings) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// requireSettingsRole checks one permission returned by settingsChangeRoles and
// writes the matching 403 when it is missing. The live update interval and the
// history views keep their own error codes.
func requireSettingsRole(w http.ResponseWriter, r *http.Request, role string) bool {
	switch role {
	case auth.RoleTuneLiveUpdates:
		return requireRoleIfUser(w, r, role, "live_update_interval_forbidden", "error.live_update_interval_forbidden", "Für das Anpassen des Live-Update-Intervalls fehlt die Berechtigung")
	case auth.RoleViewHistory:
		return requireRoleIfUser(w, r, role, "view_forbidden", "error.view_forbidden", "Für diese Seite fehlt die Berechtigung zum Ansehen.")
	default:
		return requireRoleIfUser(w, r, role, "edit_settings_forbidden", "error.edit_settings_forbidden", "Zum Ändern der Einstellungen fehlt die Berechtigung.")
	}
}
