package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

type recordingSystemActionExecutor struct {
	action systemactions.Action
}

func (executor *recordingSystemActionExecutor) Execute(_ context.Context, action systemactions.Action) error {
	executor.action = action
	return nil
}

func TestAuthenticatedRouterSupportsGuestModeButProtectsSystemActions(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingSystemActionExecutor{}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, SystemActions: executor})

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/", nil))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "Als Gast fortfahren") {
		t.Fatalf("login page status %d: %s", login.Code, login.Body.String())
	}
	if !strings.Contains(login.Body.String(), "name=\"username\" autocomplete=\"username\" required disabled") ||
		!strings.Contains(login.Body.String(), "name=\"password\" type=\"password\" autocomplete=\"current-password\" required disabled") ||
		!strings.Contains(login.Body.String(), "type=\"submit\" disabled") {
		t.Fatal("HTTP login page does not render the disabled admin login form")
	}
	if !strings.Contains(login.Body.String(), "Funktionalität reduziert") {
		t.Fatal("HTTP login page does not explain the reduced functionality")
	}

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", guest.Code, guest.Body.String())
	}
	guestCookie := guest.Result().Cookies()[0]
	guestRequest := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
	guestRequest.AddCookie(guestCookie)
	guestAction := httptest.NewRecorder()
	router.ServeHTTP(guestAction, guestRequest)
	if guestAction.Code != http.StatusForbidden || executor.action != "" {
		t.Fatalf("guest action status %d, action=%q: %s", guestAction.Code, executor.action, guestAction.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Forwarded-Proto", "https")
	adminLogin := httptest.NewRecorder()
	router.ServeHTTP(adminLogin, loginRequest)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status %d: %s", adminLogin.Code, adminLogin.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(adminLogin.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	adminCookie := adminLogin.Result().Cookies()[0]
	actionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/system/reboot", nil)
	actionRequest.AddCookie(adminCookie)
	actionRequest.Header.Set("X-Forwarded-Proto", "https")
	actionRequest.Header.Set("X-CSRF-Token", session.CSRFToken)
	action := httptest.NewRecorder()
	router.ServeHTTP(action, actionRequest)
	if action.Code != http.StatusAccepted || executor.action != systemactions.Reboot {
		t.Fatalf("admin action status %d, action=%q: %s", action.Code, executor.action, action.Body.String())
	}
}

func TestSettingsPutProtectsLiveUpdateIntervalByRoleButAllowsOtherFields(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	store := settings.NewStore(t.TempDir())
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), store, nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest login status %d: %s", guest.Code, guest.Body.String())
	}
	guestCookie := guest.Result().Cookies()[0]

	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"live_update_interval_seconds":10}`))
	forbiddenRequest.AddCookie(guestCookie)
	router.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("guest interval change status %d: %s", forbidden.Code, forbidden.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || value.LiveUpdateIntervalSeconds != 3 {
		t.Fatalf("interval changed despite forbidden response: %#v (err=%v)", value, err)
	}

	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"live_update_interval_seconds":3,"show_runtime_status":false}`))
	allowedRequest.AddCookie(guestCookie)
	router.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK {
		t.Fatalf("guest unrelated-field change status %d: %s", allowed.Code, allowed.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || value.ShowRuntimeStatus {
		t.Fatalf("unrelated field was not saved: %#v (err=%v)", value, err)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Forwarded-Proto", "https")
	adminLogin := httptest.NewRecorder()
	router.ServeHTTP(adminLogin, loginRequest)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status %d: %s", adminLogin.Code, adminLogin.Body.String())
	}
	adminCookie := adminLogin.Result().Cookies()[0]

	adminChange := httptest.NewRecorder()
	adminRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"live_update_interval_seconds":10}`))
	adminRequest.AddCookie(adminCookie)
	adminRequest.Header.Set("X-Forwarded-Proto", "https")
	router.ServeHTTP(adminChange, adminRequest)
	if adminChange.Code != http.StatusOK {
		t.Fatalf("admin interval change status %d: %s", adminChange.Code, adminChange.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || value.LiveUpdateIntervalSeconds != 10 {
		t.Fatalf("interval not saved for admin: %#v (err=%v)", value, err)
	}
}
