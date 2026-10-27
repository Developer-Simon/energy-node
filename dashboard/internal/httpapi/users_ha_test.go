package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// pairingCodeFormat is the shape of a formatted pairing code, XXXX-XXXX-XXXX.
var pairingCodeFormat = regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`)

// haListing reads the Home Assistant and companion part of GET /api/v1/users.
type haListing struct {
	HAAccounts []struct {
		HAUserID    string   `json:"ha_user_id"`
		Username    string   `json:"username"`
		RolesCustom bool     `json:"roles_custom"`
		Roles       []string `json:"roles"`
	} `json:"ha_accounts"`
	HARoles *struct {
		Admin []string `json:"admin"`
		User  []string `json:"user"`
	} `json:"ha_roles"`
	Companion struct {
		Paired    bool `json:"paired"`
		Connected bool `json:"connected"`
	} `json:"companion"`
}

func listHA(t *testing.T, router http.Handler, cookie *http.Cookie) haListing {
	t.Helper()
	recorder := usersCall(router, http.MethodGet, "/api/v1/users", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var listing haListing
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	return listing
}

// TestHARoutesNeedUserManagementHTTPSAndCSRF: the pairing, preset and account
// routes check HTTPS, CSRF and user_management, in that order.
func TestHARoutesNeedUserManagementHTTPSAndCSRF(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	adminCookie, _ := sessionFor(t, router, "admin", usersAdminPassword)
	viewerCookie, viewerCSRF := loginWithRoles(t, router, manager, "viewer", []string{auth.RoleViewDevices})
	createRestrictedTestAccount(t, manager, "httpadmin", []string{auth.RoleHTTPLogin, auth.RoleUserManagement})
	httpCookie := loginOverHTTP(t, router, "httpadmin")

	routes := []struct {
		name, method, path, body string
	}{
		{name: "pairing create", method: http.MethodPost, path: "/api/v1/users/companion/pairing-code"},
		{name: "pairing drop", method: http.MethodDelete, path: "/api/v1/users/companion/pairing-code"},
		{name: "unpair", method: http.MethodDelete, path: "/api/v1/users/companion"},
		{name: "ha roles", method: http.MethodPut, path: "/api/v1/users/ha-roles", body: `{"admin":[],"user":[]}`},
		{name: "ha account update", method: http.MethodPut, path: "/api/v1/users/ha:u1", body: `{"roles":[],"roles_custom":false}`},
		{name: "ha account delete", method: http.MethodDelete, path: "/api/v1/users/ha:u1"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			// Without user_management, with HTTPS and CSRF.
			recorder := usersCall(router, route.method, route.path, route.body, viewerCookie, viewerCSRF, true)
			wantUsersError(t, recorder, http.StatusForbidden, "user_management_forbidden")
			// Without CSRF.
			recorder = usersCall(router, route.method, route.path, route.body, adminCookie, "", true)
			wantUsersError(t, recorder, http.StatusForbidden, "csrf_failed")
			// Over HTTP.
			recorder = usersCall(router, route.method, route.path, route.body, httpCookie, "", false)
			wantUsersError(t, recorder, http.StatusForbidden, "secure_connection_required")
		})
	}
}

// TestCreatePairingCodeReturnsFormattedCode: the code is only in the POST answer,
// the account list shows just the expiry time.
func TestCreatePairingCodeReturnsFormattedCode(t *testing.T) {
	router, _, _ := newCompanionTestRouter(t)
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)

	recorder := usersCall(router, http.MethodPost, "/api/v1/users/companion/pairing-code", "", cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var answer struct {
		Code      string `json:"code"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !pairingCodeFormat.MatchString(answer.Code) {
		t.Fatalf("pairing code %q does not match XXXX-XXXX-XXXX", answer.Code)
	}
	if answer.ExpiresAt == "" {
		t.Fatal("pairing code answer has no expires_at")
	}

	listed := usersCall(router, http.MethodGet, "/api/v1/users", "", cookie, "", true)
	wantUsersStatus(t, listed, http.StatusOK)
	body := listed.Body.String()
	if !strings.Contains(body, `"pairing_expires_at"`) {
		t.Fatalf("account list lacks pairing_expires_at: %s", body)
	}
	if strings.Contains(body, answer.Code) || strings.Contains(body, strings.ReplaceAll(answer.Code, "-", "")) {
		t.Fatalf("account list leaks the pairing code: %s", body)
	}
}

// TestUnpairRoute: unpairing a paired companion succeeds, a second unpair
// answers not_paired.
func TestUnpairRoute(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)

	wantUsersError(t, usersCall(router, http.MethodDelete, "/api/v1/users/companion", "", cookie, csrf, true), http.StatusConflict, "not_paired")

	pairCompanion(t, CompanionGate(manager, router), manager)
	recorder := usersCall(router, http.MethodDelete, "/api/v1/users/companion", "", cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	if listing := listHA(t, router, cookie); listing.Companion.Paired {
		t.Fatal("companion still paired after unpair")
	}
	wantUsersError(t, usersCall(router, http.MethodDelete, "/api/v1/users/companion", "", cookie, csrf, true), http.StatusConflict, "not_paired")
}

// TestHARolesRoute: http_login is refused as a preset, a valid preset is stored
// and listed.
func TestHARolesRoute(t *testing.T) {
	router, _, _ := newCompanionTestRouter(t)
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)

	refused := fmt.Sprintf(`{"admin":[%q],"user":[]}`, auth.RoleHTTPLogin)
	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/users/ha-roles", refused, cookie, csrf, true), http.StatusBadRequest, "invalid_user")

	valid := fmt.Sprintf(`{"admin":[%q],"user":[%q]}`, auth.RoleUserManagement, auth.RoleViewDevices)
	recorder := usersCall(router, http.MethodPut, "/api/v1/users/ha-roles", valid, cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var answer struct {
		HARoles struct {
			Admin []string `json:"admin"`
			User  []string `json:"user"`
		} `json:"ha_roles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if !containsString(answer.HARoles.Admin, auth.RoleUserManagement) || !containsString(answer.HARoles.User, auth.RoleViewDevices) {
		t.Fatalf("answer presets = %+v", answer.HARoles)
	}

	listing := listHA(t, router, cookie)
	if listing.HARoles == nil || !containsString(listing.HARoles.Admin, auth.RoleUserManagement) || !containsString(listing.HARoles.User, auth.RoleViewDevices) {
		t.Fatalf("list presets = %+v, want the stored ones", listing.HARoles)
	}
}

// TestHAAccountRoutes: a Home Assistant account gets its own roles, unknown
// accounts answer 404, the password routes do not exist for them, and delete
// removes the account.
func TestHAAccountRoutes(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	pairCompanion(t, CompanionGate(manager, router), manager)
	if _, err := manager.CompanionSession(auth.HAIdentity{UserID: "u1", Username: "anna", Name: "Anna"}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, router, "admin", usersAdminPassword)

	update := fmt.Sprintf(`{"roles":[%q],"disabled":false,"roles_custom":true}`, auth.RoleViewDevices)
	recorder := usersCall(router, http.MethodPut, "/api/v1/users/ha:u1", update, cookie, csrf, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var account struct {
		RolesCustom bool     `json:"roles_custom"`
		Roles       []string `json:"roles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	if !account.RolesCustom || !containsString(account.Roles, auth.RoleViewDevices) {
		t.Fatalf("updated account = %+v", account)
	}
	if listing := listHA(t, router, cookie); len(listing.HAAccounts) != 1 || !listing.HAAccounts[0].RolesCustom {
		t.Fatalf("list ha_accounts = %+v", listing.HAAccounts)
	}

	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/users/ha:unknown", update, cookie, csrf, true), http.StatusNotFound, "user_not_found")
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/users/ha:u1/password", `{"password":"anna-pass-1"}`, cookie, csrf, true), http.StatusNotFound)
	wantUsersStatus(t, usersCall(router, http.MethodPut, "/api/v1/users/ha:u1/http-password", `{"password":"anna-http-1"}`, cookie, csrf, true), http.StatusNotFound)

	wantUsersStatus(t, usersCall(router, http.MethodDelete, "/api/v1/users/ha:u1", "", cookie, csrf, true), http.StatusOK)
	if accounts := manager.HAAccounts(); len(accounts) != 0 {
		t.Fatalf("accounts after delete = %+v", accounts)
	}
}

// TestUsersListIncludesHAData: before pairing and presets the list carries
// null presets and an unconnected companion, afterwards the stored values.
func TestUsersListIncludesHAData(t *testing.T) {
	router, manager, _ := newCompanionTestRouter(t)
	cookie, _ := sessionFor(t, router, "admin", usersAdminPassword)

	recorder := usersCall(router, http.MethodGet, "/api/v1/users", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	if body := recorder.Body.String(); !strings.Contains(body, `"ha_roles":null`) || !strings.Contains(body, `"ha_accounts":[]`) {
		t.Fatalf("fresh list lacks null presets or empty ha_accounts: %s", body)
	}
	if listing := listHA(t, router, cookie); listing.Companion.Paired || listing.Companion.Connected {
		t.Fatalf("fresh companion = %+v, want unpaired and not connected", listing.Companion)
	}

	pairCompanion(t, CompanionGate(manager, router), manager)
	listing := listHA(t, router, cookie)
	if !listing.Companion.Paired || listing.Companion.Connected {
		t.Fatalf("paired companion = %+v, want paired and not connected (no stream open)", listing.Companion)
	}

	if err := manager.SetHARoles("admin", []string{auth.RoleViewDevices}, nil); err != nil {
		t.Fatal(err)
	}
	listing = listHA(t, router, cookie)
	if listing.HARoles == nil || !containsString(listing.HARoles.Admin, auth.RoleViewDevices) {
		t.Fatalf("list presets = %+v, want the admin preset", listing.HARoles)
	}
}
