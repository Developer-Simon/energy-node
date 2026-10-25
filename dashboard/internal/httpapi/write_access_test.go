package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestSettingsChangeRoles(t *testing.T) {
	current := settings.Default()
	next := current
	next.HistoryViews = []settings.HistoryView{{Name: "a"}}
	if got, _ := settingsChangeRoles(current, next); !reflect.DeepEqual(got, []string{auth.RoleViewHistory}) {
		t.Fatal(got)
	}
	next.Theme = settings.ThemeStromblau
	if got, _ := settingsChangeRoles(current, next); !reflect.DeepEqual(got, []string{auth.RoleEditSettings, auth.RoleViewHistory}) {
		t.Fatal(got)
	}
	if got, _ := settingsChangeRoles(current, current); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestSettingsPutWithoutEditSettings(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	handler := handleSettings(store)

	theme := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"theme":"stromblau"}`))
	theme = theme.WithContext(auth.WithUser(context.Background(), auth.User{Roles: []string{auth.RoleViewHistory}}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, theme)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"edit_settings_forbidden"`) {
		t.Fatalf("theme change status %d: %s", recorder.Code, recorder.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || value.Theme == settings.ThemeStromblau {
		t.Fatalf("theme saved despite 403: %#v (err=%v)", value, err)
	}

	views := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"theme":"mint","history_views":[{"id":"v1","name":"a"}]}`))
	views = views.WithContext(auth.WithUser(context.Background(), auth.User{Roles: []string{auth.RoleViewHistory}}))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, views)
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("history view change with view_history refused: %s", recorder.Body.String())
	}
}

func TestSettingsRevisionRestoreRequiresEditSettings(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	handler := handleSettingsRevision(store)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/restore", strings.NewReader(`{"revision":"missing"}`))
	request = request.WithContext(auth.WithUser(context.Background(), auth.User{Roles: []string{auth.RoleViewConfig}}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"edit_settings_forbidden"`) {
		t.Fatalf("restore status %d: %s", recorder.Code, recorder.Body.String())
	}
}

// writeAccessRouter serves the authenticated router with one command entity
// and returns the publisher that records published commands.
func writeAccessRouter(t *testing.T) (http.Handler, *auth.Manager, *recordingCommandPublisher) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.UpsertEntity(registry.Discovery{
		Device: registry.DeviceInfo{ID: "shelly"},
		Entity: registry.EntityInfo{UniqueID: "shelly_relay", Component: "switch", StateTopic: "state/shelly/relay", CommandTopic: "shelly/relay/0/set", PayloadOn: "ON", PayloadOff: "OFF"},
	})
	publisher := &recordingCommandPublisher{}
	dir := t.TempDir()
	router := NewAuthenticatedRouter(reg, config.NewManager(dir), settings.NewStore(dir), nil, nil, publisher, nil, nil, RouterDependencies{Auth: manager})
	return router, manager, publisher
}

// accountRequest builds a request with the session cookie of a logged-in
// account. An empty csrf leaves the CSRF header out.
func accountRequest(method, path, body string, cookie *http.Cookie, csrf string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	return request
}

// errorCode returns the code field of a JSON error response, or "".
func errorCode(recorder *httptest.ResponseRecorder) string {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		return ""
	}
	return body.Code
}

func TestEntityCommandRequiresOperateDevices(t *testing.T) {
	router, manager, publisher := writeAccessRouter(t)

	viewer, _ := loginWithRoles(t, router, manager, "viewer", []string{auth.RoleViewDevices})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", "", viewer, ""))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "operate_devices_forbidden" {
		t.Fatalf("view_devices command status %d: %s", recorder.Code, recorder.Body.String())
	}
	if publisher.topic != "" {
		t.Fatalf("command published despite 403: %s", publisher.topic)
	}

	operator, _ := loginWithRoles(t, router, manager, "operator", []string{auth.RoleViewDevices, auth.RoleOperateDevices})
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/entities/shelly_relay/command", "", operator, ""))
	if recorder.Code != http.StatusOK || publisher.topic != "shelly/relay/0/set" {
		t.Fatalf("operate_devices command status %d, topic %q: %s", recorder.Code, publisher.topic, recorder.Body.String())
	}
}

func TestConfigurationWriteRequiresEditConfig(t *testing.T) {
	router, manager, _ := writeAccessRouter(t)

	viewer, csrf := loginWithRoles(t, router, manager, "config-viewer", []string{auth.RoleViewConfig})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPut, "/api/v1/configurations/shelly", `{}`, viewer, csrf))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "edit_config_forbidden" {
		t.Fatalf("view_config PUT status %d: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodGet, "/api/v1/configurations/shelly", "", viewer, ""))
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("view_config GET refused: %s", recorder.Body.String())
	}
}

func TestTinyTuyaConfigureRequiresEditConfig(t *testing.T) {
	router, manager, _ := writeAccessRouter(t)

	viewer, csrf := loginWithRoles(t, router, manager, "tuya-viewer", []string{auth.RoleViewConfig})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/tiny-tuya/configure", `{}`, viewer, csrf))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "edit_config_forbidden" {
		t.Fatalf("view_config configure status %d: %s", recorder.Code, recorder.Body.String())
	}

	editor, editorCSRF := loginWithRoles(t, router, manager, "tuya-editor", []string{auth.RoleViewConfig, auth.RoleEditConfig})
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/tiny-tuya/configure", `{}`, editor, editorCSRF))
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("edit_config configure refused: %s", recorder.Body.String())
	}
}

func TestDeviceIgnoreRequiresViewDevices(t *testing.T) {
	router, manager, _ := writeAccessRouter(t)

	overview, csrf := loginWithRoles(t, router, manager, "overview-only", []string{auth.RoleViewOverview})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodPost, "/api/v1/devices/shelly/ignore", "", overview, csrf))
	if recorder.Code != http.StatusForbidden || errorCode(recorder) != "view_forbidden" {
		t.Fatalf("view_overview ignore status %d: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, accountRequest(http.MethodGet, "/api/v1/devices/shelly", "", overview, ""))
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("view_overview device detail refused: %s", recorder.Body.String())
	}
}
