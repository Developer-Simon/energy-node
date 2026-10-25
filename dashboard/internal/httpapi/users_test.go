package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

const usersAdminPassword = "secret"

type usersListing struct {
	Users []struct {
		Username        string   `json:"username"`
		Roles           []string `json:"roles"`
		Disabled        bool     `json:"disabled"`
		HasHTTPPassword bool     `json:"has_http_password"`
	} `json:"users"`
	Guests []struct {
		Username string `json:"username"`
	} `json:"guests"`
	GuestRoles []string `json:"guest_roles"`
	Catalog    []struct {
		Key          string `json:"key"`
		Category     string `json:"category"`
		Requires     string `json:"requires"`
		HTTPSOnly    bool   `json:"https_only"`
		GuestAllowed bool   `json:"guest_allowed"`
		HAAllowed    bool   `json:"ha_allowed"`
		Elevated     bool   `json:"elevated"`
	} `json:"catalog"`
	PasswordPolicy struct {
		MinLength int `json:"min_length"`
		Min       int `json:"min"`
		Max       int `json:"max"`
	} `json:"password_policy"`
	MigrationNotice bool   `json:"migration_notice"`
	Self            string `json:"self"`
}

// sessionFor logs in over HTTPS and returns the session cookie and CSRF token.
func sessionFor(t *testing.T, router http.Handler, username, password string) (*http.Cookie, string) {
	t.Helper()
	recorder := usersCall(router, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"username":%q,"password":%q}`, username, password), nil, "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTPS login of %s status %d: %s", username, recorder.Code, recorder.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return recorder.Result().Cookies()[0], session.CSRFToken
}

// usersCall sends one request. secure adds the forwarded HTTPS header, csrf
// (when not empty) goes into X-CSRF-Token.
func usersCall(router http.Handler, method, path, body string, cookie *http.Cookie, csrf string, secure bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if secure {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func wantUsersStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
}

func wantUsersError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != status || body.Code != code {
		t.Fatalf("status %d code %q, want %d %q: %s", recorder.Code, body.Code, status, code, recorder.Body.String())
	}
}

func listUsers(t *testing.T, router http.Handler, cookie *http.Cookie) usersListing {
	t.Helper()
	recorder := usersCall(router, http.MethodGet, "/api/v1/users", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var listing usersListing
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	return listing
}

func findListedUser(listing usersListing, name string) (idx int, found bool) {
	for i, user := range listing.Users {
		if user.Username == name {
			return i, true
		}
	}
	return -1, false
}

// TestUsersRoutesRequireUserManagementHTTPSAndCSRF: jede Route verlangt HTTPS,
// bei Schreibzugriffen CSRF und user_management. Die eigenen Passwortrouten
// verlangen kein user_management, dort prüft der Test nur HTTPS und CSRF.
func TestUsersRoutesRequireUserManagementHTTPSAndCSRF(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	createRestrictedTestAccount(t, manager, "anna", []string{auth.RoleViewDevices})
	viewerCookie, viewerCSRF := loginWithRoles(t, router, manager, "viewer", []string{auth.RoleViewDevices})
	createRestrictedTestAccount(t, manager, "httpadmin", []string{auth.RoleHTTPLogin, auth.RoleUserManagement})
	httpCookie := loginOverHTTP(t, router, "httpadmin")

	routes := []struct {
		name, method, path, body string
		self, write              bool
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/users"},
		{name: "create", method: http.MethodPost, path: "/api/v1/users", body: `{"username":"gast2","password":"gast2-pass-1","roles":[]}`, write: true},
		{name: "update", method: http.MethodPut, path: "/api/v1/users/anna", body: `{"roles":["view_devices"],"disabled":false}`, write: true},
		{name: "password", method: http.MethodPut, path: "/api/v1/users/anna/password", body: `{"password":"anna-pass-2"}`, write: true},
		{name: "http-password", method: http.MethodPut, path: "/api/v1/users/anna/http-password", body: `{"password":"anna-http-1"}`, write: true},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/users/anna", write: true},
		{name: "guest-roles", method: http.MethodPut, path: "/api/v1/users/guest-roles", body: `{"roles":["view_overview"]}`, write: true},
		{name: "password-policy", method: http.MethodPut, path: "/api/v1/users/password-policy", body: `{"min_length":12}`, write: true},
		{name: "me/password", method: http.MethodPut, path: "/api/v1/users/me/password", body: `{"old_password":"` + restrictedTestPassword + `","password":"viewer-pass-2"}`, self: true, write: true},
		{name: "me/http-password", method: http.MethodPut, path: "/api/v1/users/me/http-password", body: `{"password":"viewer-http-1"}`, self: true, write: true},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			if !route.self {
				// Ohne user_management, mit HTTPS und CSRF.
				recorder := usersCall(router, route.method, route.path, route.body, viewerCookie, viewerCSRF, true)
				wantUsersError(t, recorder, http.StatusForbidden, "user_management_forbidden")
			}
			if route.write {
				cookie := adminCookie
				if route.self {
					cookie = viewerCookie
				}
				recorder := usersCall(router, route.method, route.path, route.body, cookie, "", true)
				wantUsersError(t, recorder, http.StatusForbidden, "csrf_failed")
			}
			recorder := usersCall(router, route.method, route.path, route.body, httpCookie, "", false)
			wantUsersError(t, recorder, http.StatusForbidden, "secure_connection_required")
		})
	}
}

// TestUsersHappyPath: anlegen, ändern, Passwort, HTTP-Passwort, Gastvorgabe,
// löschen. Nach jedem Schritt spiegelt GET die Änderung.
func TestUsersHappyPath(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)

	recorder := usersCall(router, http.MethodPost, "/api/v1/users", `{"username":"Anna","password":"anna-pass-1","roles":["view_devices"]}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing := listUsers(t, router, adminCookie)
	if idx, ok := findListedUser(listing, "anna"); !ok || len(listing.Users[idx].Roles) != 1 || listing.Users[idx].Roles[0] != auth.RoleViewDevices {
		t.Fatalf("after create, users = %+v", listing.Users)
	}
	if listing.Self != "admin" || len(listing.Catalog) == 0 {
		t.Fatalf("self = %q, catalog entries = %d", listing.Self, len(listing.Catalog))
	}

	recorder = usersCall(router, http.MethodPut, "/api/v1/users/anna", `{"roles":["view_devices","http_login"],"disabled":false}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing = listUsers(t, router, adminCookie)
	if idx, _ := findListedUser(listing, "anna"); !containsString(listing.Users[idx].Roles, auth.RoleHTTPLogin) {
		t.Fatalf("after update, anna roles = %v", listing.Users[idx].Roles)
	}

	recorder = usersCall(router, http.MethodPut, "/api/v1/users/anna/password", `{"password":"anna-pass-2"}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	if login := usersCall(router, http.MethodPost, "/api/v1/auth/login", `{"username":"anna","password":"anna-pass-2"}`, nil, "", true); login.Code != http.StatusOK {
		t.Fatalf("login with the new password status %d", login.Code)
	}
	if login := usersCall(router, http.MethodPost, "/api/v1/auth/login", `{"username":"anna","password":"anna-pass-1"}`, nil, "", true); login.Code != http.StatusUnauthorized {
		t.Fatalf("login with the old password status %d, want 401", login.Code)
	}

	recorder = usersCall(router, http.MethodPut, "/api/v1/users/anna/http-password", `{"password":"anna-http-1"}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing = listUsers(t, router, adminCookie)
	if idx, _ := findListedUser(listing, "anna"); !listing.Users[idx].HasHTTPPassword {
		t.Fatalf("has_http_password is false after set")
	}
	recorder = usersCall(router, http.MethodPut, "/api/v1/users/anna/http-password", `{"password":""}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing = listUsers(t, router, adminCookie)
	if idx, _ := findListedUser(listing, "anna"); listing.Users[idx].HasHTTPPassword {
		t.Fatalf("has_http_password is still true after removal")
	}

	recorder = usersCall(router, http.MethodPut, "/api/v1/users/guest-roles", `{"roles":["view_overview"]}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing = listUsers(t, router, adminCookie)
	if len(listing.GuestRoles) != 1 || listing.GuestRoles[0] != auth.RoleViewOverview {
		t.Fatalf("guest_roles = %v", listing.GuestRoles)
	}

	recorder = usersCall(router, http.MethodDelete, "/api/v1/users/anna", "", adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	listing = listUsers(t, router, adminCookie)
	if _, ok := findListedUser(listing, "anna"); ok {
		t.Fatalf("anna is still listed after delete")
	}
	if n := len(manager.Users()); n != 1 {
		t.Fatalf("manager still holds %d accounts, want the admin only", n)
	}
}

// TestOwnPasswordWithoutUserManagement: ein Konto ohne user_management ändert
// sein Passwort über me/password, die Sitzung bleibt gültig.
func TestOwnPasswordWithoutUserManagement(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, csrf := loginWithRoles(t, router, manager, "lena", []string{auth.RoleViewDevices})

	wrong := usersCall(router, http.MethodPut, "/api/v1/users/me/password", `{"old_password":"falsch-falsch-1","password":"lena-new-pass-2"}`, cookie, csrf, true)
	wantUsersError(t, wrong, http.StatusForbidden, "old_password_invalid")

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/me/password", `{"old_password":"`+restrictedTestPassword+`","password":"lena-new-pass-2"}`, cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)

	session := usersCall(router, http.MethodGet, "/api/v1/auth/session", "", cookie, "", true)
	wantUsersStatus(t, session, http.StatusOK)
	if login := usersCall(router, http.MethodPost, "/api/v1/auth/login", `{"username":"lena","password":"lena-new-pass-2"}`, nil, "", true); login.Code != http.StatusOK {
		t.Fatalf("login with the new password status %d", login.Code)
	}
}

// TestOwnHTTPPasswordWithoutUserManagement: me/http-password ohne user_management.
// Ohne http_login gibt es nichts zu setzen, das ist invalid_user.
func TestOwnHTTPPasswordWithoutUserManagement(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	cookie, csrf := loginWithRoles(t, router, manager, "lena", []string{auth.RoleHTTPLogin, auth.RoleViewDevices})

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/me/http-password", `{"password":"lena-http-1"}`, cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	if idx := accountIndex(manager.Users(), "lena"); idx < 0 || !manager.Users()[idx].HasHTTPPassword {
		t.Fatalf("lena has no HTTP password after own change")
	}
	recorder = usersCall(router, http.MethodPut, "/api/v1/users/me/http-password", `{"password":""}`, cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	if idx := accountIndex(manager.Users(), "lena"); idx < 0 || manager.Users()[idx].HasHTTPPassword {
		t.Fatalf("lena still has an HTTP password after removal")
	}

	createRestrictedTestAccount(t, manager, "kai", []string{auth.RoleViewDevices})
	kaiCookie, kaiCSRF := sessionFor(t, router, "kai", restrictedTestPassword)
	recorder = usersCall(router, http.MethodPut, "/api/v1/users/me/http-password", `{"password":"kai-http-pass-1"}`, kaiCookie, kaiCSRF, true)
	wantUsersError(t, recorder, http.StatusBadRequest, "invalid_user")
}

func accountIndex(users []auth.User, name string) int {
	for i, user := range users {
		if user.Username == name {
			return i
		}
	}
	return -1
}

// TestUsersErrorMapping: die Fehler des Managers kommen mit dem Code aus Spec
// Abschnitt 7 und dem passenden Status an.
func TestUsersErrorMapping(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	createRestrictedTestAccount(t, manager, "anna", []string{auth.RoleViewDevices})

	post := func(body string) *httptest.ResponseRecorder {
		return usersCall(router, http.MethodPost, "/api/v1/users", body, adminCookie, adminCSRF, true)
	}
	put := func(path, body string) *httptest.ResponseRecorder {
		return usersCall(router, http.MethodPut, path, body, adminCookie, adminCSRF, true)
	}

	wantUsersError(t, post(`{"username":"anna","password":"anna-pass-1","roles":[]}`), http.StatusConflict, "user_exists")
	wantUsersError(t, post(`{"username":"ANNA","password":"anna-pass-1","roles":[]}`), http.StatusConflict, "user_exists")
	wantUsersError(t, post(`{"username":"ab","password":"anna-pass-1","roles":[]}`), http.StatusBadRequest, "invalid_user")
	wantUsersError(t, post(`{"username":"neu","password":"kurz","roles":[]}`), http.StatusBadRequest, "invalid_user")
	wantUsersError(t, post(`{"username":"neu","password":"neu-pass-1","roles":["gibt_es_nicht"]}`), http.StatusBadRequest, "invalid_user")
	wantUsersError(t, put("/api/v1/users/ghost", `{"roles":[],"disabled":false}`), http.StatusNotFound, "user_not_found")
	wantUsersError(t, put("/api/v1/users/guest-roles", `{"roles":["user_management"]}`), http.StatusBadRequest, "invalid_user")

	// anna hat kein http_login: das HTTP-Passwort wird abgewiesen.
	wantUsersError(t, put("/api/v1/users/anna/http-password", `{"password":"anna-pass-1"}`), http.StatusBadRequest, "invalid_user")
	wantUsersStatus(t, put("/api/v1/users/anna", `{"roles":["view_devices","http_login"],"disabled":false}`), http.StatusOK)
	// Gleich dem Hauptpasswort ist abgewiesen.
	wantUsersError(t, put("/api/v1/users/anna/http-password", `{"password":"`+restrictedTestPassword+`"}`), http.StatusBadRequest, "http_password_reused")

	// Der eigene Account lässt sich nicht löschen oder sperren.
	wantUsersError(t, usersCall(router, http.MethodDelete, "/api/v1/users/admin", "", adminCookie, adminCSRF, true), http.StatusConflict, "self_protection")
	wantUsersError(t, put("/api/v1/users/admin", `{"roles":[],"disabled":true}`), http.StatusConflict, "self_protection")

	// Der letzte Admin verliert user_management nicht.
	wantUsersError(t, put("/api/v1/users/admin", `{"roles":[],"disabled":false}`), http.StatusConflict, "last_admin")
}

// TestUpdateUserIsLastWriteWins: zwei PUT nacheinander, GET zeigt den zweiten Stand.
func TestUpdateUserIsLastWriteWins(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)
	createRestrictedTestAccount(t, manager, "anna", []string{auth.RoleViewDevices})

	first := usersCall(router, http.MethodPut, "/api/v1/users/anna", `{"roles":["view_devices","view_history"],"disabled":false}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, first, http.StatusOK)
	second := usersCall(router, http.MethodPut, "/api/v1/users/anna", `{"roles":["view_overview"],"disabled":false}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, second, http.StatusOK)

	listing := listUsers(t, router, adminCookie)
	idx, ok := findListedUser(listing, "anna")
	if !ok {
		t.Fatal("anna is not listed")
	}
	if roles := listing.Users[idx].Roles; len(roles) != 1 || roles[0] != auth.RoleViewOverview {
		t.Fatalf("anna roles = %v, want [%s] from the second PUT", roles, auth.RoleViewOverview)
	}
}

// TestPasswordPolicyRoute: PUT setzt die Mindestlänge, GET und die Sitzung
// zeigen sie, ein Wert außerhalb von 8 bis 64 kommt als invalid_user zurück,
// und das Anlegen eines Kontos mit zu kurzem Passwort scheitert.
func TestPasswordPolicyRoute(t *testing.T) {
	router, _ := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)

	if listing := listUsers(t, router, adminCookie); listing.PasswordPolicy.MinLength != 10 || listing.PasswordPolicy.Min != 8 || listing.PasswordPolicy.Max != 64 {
		t.Fatalf("default policy: %+v", listing.PasswordPolicy)
	}

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/password-policy", `{"min_length":12}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var put struct {
		PasswordPolicy struct {
			MinLength int `json:"min_length"`
			Min       int `json:"min"`
			Max       int `json:"max"`
		} `json:"password_policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if put.PasswordPolicy.MinLength != 12 || put.PasswordPolicy.Min != 8 || put.PasswordPolicy.Max != 64 {
		t.Fatalf("PUT answer: %+v", put.PasswordPolicy)
	}
	if listing := listUsers(t, router, adminCookie); listing.PasswordPolicy.MinLength != 12 {
		t.Fatalf("GET after PUT: %+v", listing.PasswordPolicy)
	}

	for _, body := range []string{`{"min_length":7}`, `{"min_length":65}`, `{"min_length":0}`} {
		wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/users/password-policy", body, adminCookie, adminCSRF, true), http.StatusBadRequest, "invalid_user")
	}
	if listing := listUsers(t, router, adminCookie); listing.PasswordPolicy.MinLength != 12 {
		t.Fatalf("rejected value changed the policy: %+v", listing.PasswordPolicy)
	}

	wantUsersError(t, usersCall(router, http.MethodPost, "/api/v1/users", `{"username":"anna","password":"abcdefghijk","roles":[]}`, adminCookie, adminCSRF, true), http.StatusBadRequest, "invalid_user")
	wantUsersStatus(t, usersCall(router, http.MethodPost, "/api/v1/users", `{"username":"anna","password":"abcdefghijkl","roles":[]}`, adminCookie, adminCSRF, true), http.StatusOK)

	// Die Sitzung trägt die Mindestlänge, damit die Oberfläche sie ohne GET kennt.
	login := usersCall(router, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"username":"admin","password":%q}`, usersAdminPassword), nil, "", true)
	wantUsersStatus(t, login, http.StatusOK)
	var session struct {
		PasswordMinLength int `json:"password_min_length"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.PasswordMinLength != 12 {
		t.Fatalf("password_min_length: got %d, want 12", session.PasswordMinLength)
	}
}

// TestUsersCatalogMarksElevatedRoles: GET liefert elevated für jede
// Berechtigung, genau die sechs erhöhten sind true.
func TestUsersCatalogMarksElevatedRoles(t *testing.T) {
	router, _ := newRestrictedTestRouter(t, nil)
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	want := map[string]bool{
		auth.RoleUserManagement: true, "system_actions": true, auth.RoleUpdateSettings: true,
		auth.RoleInstallUpdates: true, auth.RoleTailscale: true, "mqtt_config": true,
	}
	listing := listUsers(t, router, adminCookie)
	if len(listing.Catalog) == 0 {
		t.Fatal("empty catalog")
	}
	seen := map[string]bool{}
	for _, entry := range listing.Catalog {
		seen[entry.Key] = true
		if entry.Elevated != want[entry.Key] {
			t.Errorf("%s: elevated %v, want %v", entry.Key, entry.Elevated, want[entry.Key])
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("catalog lacks %s", key)
		}
	}
}
