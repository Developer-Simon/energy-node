package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
)

// guestAccessListing reads guest_access from GET /api/v1/users.
func guestAccessListing(t *testing.T, router http.Handler, cookie *http.Cookie) bool {
	t.Helper()
	recorder := usersCall(router, http.MethodGet, "/api/v1/users", "", cookie, "", true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var listing struct {
		GuestAccess *bool `json:"guest_access"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.GuestAccess == nil {
		t.Fatalf("listing has no guest_access: %s", recorder.Body.String())
	}
	return *listing.GuestAccess
}

// TestGuestAccessEndpoint: the admin switches guest access off. Guests can no
// longer sign in, the listing reports the switch, and an empty body is rejected.
func TestGuestAccessEndpoint(t *testing.T) {
	router, _ := newRestrictedTestRouter(t, nil)
	adminCookie, adminCSRF := sessionFor(t, router, "admin", usersAdminPassword)

	wantUsersError(t, usersCall(router, http.MethodPut, "/api/v1/users/guest-access", `{}`, adminCookie, adminCSRF, true), http.StatusBadRequest, "invalid_body")

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/guest-access", `{"enabled":false}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	var body struct {
		GuestAccess *bool `json:"guest_access"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.GuestAccess == nil || *body.GuestAccess {
		t.Fatalf("PUT response guest_access = %v, want false: %s", body.GuestAccess, recorder.Body.String())
	}

	wantUsersError(t, usersCall(router, http.MethodPost, "/api/v1/auth/guest", "", nil, "", true), http.StatusForbidden, "guest_access_disabled")

	if guestAccessListing(t, router, adminCookie) {
		t.Fatal("listing reports guest_access true after switching it off")
	}

	recorder = usersCall(router, http.MethodPut, "/api/v1/users/guest-access", `{"enabled":true}`, adminCookie, adminCSRF, true)
	wantUsersStatus(t, recorder, http.StatusOK)
	wantUsersStatus(t, usersCall(router, http.MethodPost, "/api/v1/auth/guest", "", nil, "", true), http.StatusOK)
}

// TestGuestAccessEndpointNeedsUserManagement: a guest with a valid guest preset
// but without user_management cannot switch guest access.
func TestGuestAccessEndpointNeedsUserManagement(t *testing.T) {
	router, manager := newRestrictedTestRouter(t, nil)
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewOverview}); err != nil {
		t.Fatal(err)
	}
	guest := usersCall(router, http.MethodPost, "/api/v1/auth/guest", "", nil, "", true)
	wantUsersStatus(t, guest, http.StatusOK)
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	guestCookie := guest.Result().Cookies()[0]

	recorder := usersCall(router, http.MethodPut, "/api/v1/users/guest-access", `{"enabled":false}`, guestCookie, session.CSRFToken, true)
	wantUsersError(t, recorder, http.StatusForbidden, "user_management_forbidden")
	if !manager.GuestAccessEnabled() {
		t.Fatal("guest switched guest access off without user_management")
	}
}
