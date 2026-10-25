package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
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
