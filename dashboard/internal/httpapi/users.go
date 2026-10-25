package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

const usersPathPrefix = "/api/v1/users"

// handleUsers serves /api/v1/users: the list with its catalog and guest
// settings (GET) and account creation (POST).
func handleUsers(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if !requireUsersAccess(w, r, manager, false) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			writeJSON(w, map[string]any{
				"users":            manager.Users(),
				"guests":           manager.Guests(),
				"guest_roles":      manager.GuestRoles(),
				"password_policy":  passwordPolicy(manager),
				"catalog":          usersCatalog(),
				"migration_notice": manager.MigrationNotice(),
				"self":             actor.Username,
			})
		case http.MethodPost:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			var request struct {
				Username string   `json:"username"`
				Password string   `json:"password"`
				Roles    []string `json:"roles"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
				return
			}
			user, err := manager.CreateUser(actor.Username, request.Username, request.Password, request.Roles)
			if err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, user)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

// handleUserSub serves /api/v1/users/ and everything below it. The fixed
// paths (me/password, me/http-password, guest-roles) are matched before the
// {name} segment, which the account names cannot collide with: "me" is too
// short and "guest-" is reserved.
func handleUserSub(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, usersPathPrefix), "/")
		switch {
		case rest == "":
			handleUsers(manager).ServeHTTP(w, r)
		case rest == "guest-roles":
			handleGuestRoles(manager).ServeHTTP(w, r)
		case rest == "password-policy":
			handlePasswordPolicy(manager).ServeHTTP(w, r)
		case rest == "me/password":
			handleOwnPassword(manager).ServeHTTP(w, r)
		case rest == "me/http-password":
			handleOwnHTTPPassword(manager).ServeHTTP(w, r)
		default:
			name, action, _ := strings.Cut(rest, "/")
			switch action {
			case "":
				handleUser(manager, name).ServeHTTP(w, r)
			case "password":
				handleUserPassword(manager, name).ServeHTTP(w, r)
			case "http-password":
				handleUserHTTPPassword(manager, name).ServeHTTP(w, r)
			default:
				http.NotFound(w, r)
			}
		}
	}
}

// handleUser serves PUT (roles and disabled) and DELETE for one account.
func handleUser(manager *auth.Manager, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			var request struct {
				Roles    []string `json:"roles"`
				Disabled bool     `json:"disabled"`
			}
			if err := decodeBody(r, &request); err != nil {
				writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
				return
			}
			user, err := manager.UpdateUser(actor.Username, name, request.Roles, request.Disabled)
			if err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, user)
		case http.MethodDelete:
			if !requireUsersAccess(w, r, manager, true) {
				return
			}
			actor, _ := auth.UserFromContext(r.Context())
			if err := manager.DeleteUser(actor.Username, name); err != nil {
				writeUserError(w, err)
				return
			}
			writeJSON(w, map[string]string{"status": "deleted"})
		default:
			methodNotAllowed(w, http.MethodPut, http.MethodDelete)
		}
	}
}

// handleUserPassword resets the password of an account by an admin.
func handleUserPassword(manager *auth.Manager, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		var request struct {
			Password string `json:"password"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetPassword(actor.Username, name, request.Password); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "password_set"})
	}
}

// handleUserHTTPPassword sets or, with an empty value, removes the HTTP
// password of an account.
func handleUserHTTPPassword(manager *auth.Manager, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		var request struct {
			Password string `json:"password"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetHTTPPassword(actor.Username, name, request.Password); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "http_password_set"})
	}
}

// handleGuestRoles sets the permissions every guest account gets.
func handleGuestRoles(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		var request struct {
			Roles []string `json:"roles"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetGuestRoles(actor.Username, request.Roles); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]any{"guest_roles": manager.GuestRoles()})
	}
}

// handlePasswordPolicy sets the minimum length for new passwords.
func handlePasswordPolicy(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		if !requireUsersAccess(w, r, manager, true) {
			return
		}
		actor, _ := auth.UserFromContext(r.Context())
		var request struct {
			MinLength int `json:"min_length"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetMinPasswordLength(actor.Username, request.MinLength); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]any{"password_policy": passwordPolicy(manager)})
	}
}

// passwordPolicy describes the minimum password length and the bounds the
// administrator may choose from.
func passwordPolicy(manager *auth.Manager) map[string]any {
	return map[string]any{
		"min_length": manager.MinPasswordLength(),
		"min":        auth.MinPasswordLengthLowest,
		"max":        auth.MinPasswordLengthHighest,
	}
}

// handleOwnPassword lets every signed-in account change its own password. It
// needs no user_management, only HTTPS and CSRF.
func handleOwnPassword(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		token, ok := requireOwnAccess(w, r, manager)
		if !ok {
			return
		}
		var request struct {
			OldPassword string `json:"old_password"`
			Password    string `json:"password"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.ChangeOwnPassword(token, request.OldPassword, request.Password); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "password_changed"})
	}
}

// handleOwnHTTPPassword sets or removes the HTTP password of the signed-in
// account. The account needs http_login, checked by the manager.
func handleOwnHTTPPassword(manager *auth.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		token, ok := requireOwnAccess(w, r, manager)
		if !ok {
			return
		}
		var request struct {
			Password string `json:"password"`
		}
		if err := decodeBody(r, &request); err != nil {
			writeErrorDetail(w, http.StatusBadRequest, "invalid_body", err)
			return
		}
		if err := manager.SetOwnHTTPPassword(token, request.Password); err != nil {
			writeUserError(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "http_password_set"})
	}
}

// requireUsersAccess guards the account management routes: HTTPS always,
// CSRF for state changes, and user_management.
func requireUsersAccess(w http.ResponseWriter, r *http.Request, manager *auth.Manager, mutating bool) bool {
	if !requireHTTPS(w, r) {
		return false
	}
	if mutating && !requireCSRF(w, r, manager) {
		return false
	}
	return requireRole(w, r, manager, auth.RoleUserManagement, "user_management_forbidden", "error.user_management_forbidden", "Für die Benutzerverwaltung fehlt die Berechtigung user_management")
}

// requireOwnAccess guards the self-service routes: HTTPS, CSRF and a session.
// It returns the session token, which the manager needs for the change.
func requireOwnAccess(w http.ResponseWriter, r *http.Request, manager *auth.Manager) (string, bool) {
	if !requireHTTPS(w, r) || !requireCSRF(w, r, manager) {
		return "", false
	}
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return "", false
	}
	cookie, err := r.Cookie(sessionCookieName(isSecureRequest(r)))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication_required", "Anmeldung erforderlich")
		return "", false
	}
	return cookie.Value, true
}

// usersCatalog lists every permission with the flags the UI needs to lay out
// the role picker.
func usersCatalog() []map[string]any {
	roles := auth.Roles()
	out := make([]map[string]any, 0, len(roles))
	for _, info := range roles {
		out = append(out, map[string]any{
			"key":           info.Key,
			"category":      info.Category,
			"requires":      info.Requires,
			"https_only":    info.HTTPSOnly,
			"guest_allowed": info.GuestAllowed,
			"ha_allowed":    info.HAAllowed,
			"elevated":      info.Elevated,
		})
	}
	return out
}

// writeUserError maps the manager errors to the codes of spec section 7.
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrLastAdmin):
		writeErrorKey(w, http.StatusConflict, "last_admin", "error.last_admin", nil, "Der letzte Administrator kann nicht entfernt, gesperrt oder ohne user_management geändert werden")
	case errors.Is(err, auth.ErrSelfProtection):
		writeErrorKey(w, http.StatusConflict, "self_protection", "error.self_protection", nil, "Das eigene Konto kann nicht gelöscht oder gesperrt werden")
	case errors.Is(err, auth.ErrUserExists):
		writeErrorKey(w, http.StatusConflict, "user_exists", "error.user_exists", nil, "Dieser Benutzername ist schon vergeben")
	case errors.Is(err, auth.ErrUserNotFound):
		writeErrorKey(w, http.StatusNotFound, "user_not_found", "error.user_not_found", nil, "Der Benutzer wurde nicht gefunden")
	case errors.Is(err, auth.ErrHTTPPasswordReused):
		writeErrorKey(w, http.StatusBadRequest, "http_password_reused", "error.http_password_reused", nil, "Das HTTP-Passwort darf nicht dem Hauptpasswort gleichen")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeErrorKey(w, http.StatusForbidden, "old_password_invalid", "error.old_password_invalid", nil, "Das bisherige Passwort ist falsch")
	case errors.Is(err, auth.ErrInvalidUsername):
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.username", nil, "Der Benutzername braucht 3 bis 32 Zeichen aus a bis z, Ziffern, Punkt, Unterstrich oder Minus und darf nicht mit guest- beginnen")
	case errors.Is(err, auth.ErrInvalidPasswordPolicy):
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.password_policy", nil, "Die Mindestlänge des Passworts muss zwischen 8 und 64 Zeichen liegen")
	case errors.Is(err, auth.ErrWeakPassword):
		var short auth.PasswordTooShortError
		minLength := auth.DefaultMinPasswordLength
		if errors.As(err, &short) {
			minLength = short.MinLength
		}
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.password", map[string]any{"min_length": minLength}, fmt.Sprintf("Das Passwort ist zu kurz, es braucht mindestens %d Zeichen", minLength))
	case errors.Is(err, auth.ErrUnknownRole):
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.roles", nil, "Eine Berechtigung ist unbekannt")
	case errors.Is(err, auth.ErrGuestRoleNotAllowed):
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.guest_role", nil, "Diese Berechtigung ist für Gäste nicht erlaubt")
	case errors.Is(err, auth.ErrHTTPLoginRequired):
		writeErrorKey(w, http.StatusBadRequest, "invalid_user", "error.invalid_user.http_login", nil, "Ein HTTP-Passwort braucht die Berechtigung http_login")
	default:
		writeError(w, http.StatusInternalServerError, "user_save_failed", "Die Konten konnten nicht gespeichert werden")
	}
}
