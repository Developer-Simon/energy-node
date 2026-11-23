package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func TestSettingsAccessLearningFieldsNeedEditSetup(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	handler := handleSettings(store)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"learning_acknowledged":["auto_patterns"]}`))
	request = request.WithContext(auth.WithUser(context.Background(), auth.User{Roles: []string{auth.RoleEditSettings}}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("learning_acknowledged without edit_setup status %d: %s", recorder.Code, recorder.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || len(value.LearningAcknowledged) != 0 {
		t.Fatalf("learning_acknowledged saved despite 403: %#v (err=%v)", value.LearningAcknowledged, err)
	}
}

func TestSettingsAccessAutomationGroupSwitchesNeedEditSetup(t *testing.T) {
	for _, key := range []string{"automation_habits_disabled", "automation_reliability_disabled", "automation_energy_disabled", "learning_season_year_disabled", "learning_ha_reconcile_disabled", "automation_outdoor_entity"} {
		if role := settingsFieldRoles[key]; role != auth.RoleEditSetup {
			t.Fatalf("%s role = %q, want %q", key, role, auth.RoleEditSetup)
		}
	}

	store := settings.NewStore(t.TempDir())
	handler := handleSettings(store)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"automation_habits_disabled":true}`))
	request = request.WithContext(auth.WithUser(context.Background(), auth.User{Roles: []string{auth.RoleEditSettings}}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("automation_habits_disabled without edit_setup status %d: %s", recorder.Code, recorder.Body.String())
	}
	if value, err := store.LoadSettings(); err != nil || value.AutomationHabitsDisabled {
		t.Fatalf("automation_habits_disabled saved despite 403: %+v (err=%v)", value.AutomationHabitsDisabled, err)
	}
}

// TestSettingsRestoreChecksFieldRoles covers the restore path: the roles come
// from the fields that differ between the revision and the current settings,
// the same way the PUT path decides them.
func TestSettingsRestoreChecksFieldRoles(t *testing.T) {
	restore := func(t *testing.T, store *settings.Store, roles []string) *httptest.ResponseRecorder {
		t.Helper()
		revisions, err := store.SettingsRevisions()
		if err != nil || len(revisions) != 1 {
			t.Fatalf("revisions = %+v (err=%v), want exactly one", revisions, err)
		}
		body := `{"revision":"` + revisions[0].Name + `"}`
		request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/restore", strings.NewReader(body))
		request = request.WithContext(auth.WithUser(context.Background(), auth.User{Roles: roles}))
		recorder := httptest.NewRecorder()
		handleSettingsRevision(store).ServeHTTP(recorder, request)
		return recorder
	}

	t.Run("setup field needs edit_setup", func(t *testing.T) {
		store := settings.NewStore(t.TempDir())
		base, err := store.LoadSettings()
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSettings(base); err != nil {
			t.Fatal(err)
		}
		changed := base
		changed.AutomationPatternsDisabled = true
		if err := store.SaveSettings(changed); err != nil {
			t.Fatal(err)
		}

		if recorder := restore(t, store, []string{auth.RoleEditSettings}); recorder.Code != http.StatusForbidden {
			t.Fatalf("restore of automation_patterns_disabled with edit_settings only: %d %s", recorder.Code, recorder.Body.String())
		}
		if value, err := store.LoadSettings(); err != nil || !value.AutomationPatternsDisabled {
			t.Fatalf("setting restored despite 403: %+v (err=%v)", value.AutomationPatternsDisabled, err)
		}

		if recorder := restore(t, store, []string{auth.RoleEditSettings, auth.RoleEditSetup}); recorder.Code != http.StatusOK {
			t.Fatalf("restore with edit_setup: %d %s", recorder.Code, recorder.Body.String())
		}
		if value, err := store.LoadSettings(); err != nil || value.AutomationPatternsDisabled {
			t.Fatalf("setting not restored with edit_setup: %+v (err=%v)", value.AutomationPatternsDisabled, err)
		}
	})

	t.Run("design field needs only edit_settings", func(t *testing.T) {
		store := settings.NewStore(t.TempDir())
		base, err := store.LoadSettings()
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSettings(base); err != nil {
			t.Fatal(err)
		}
		changed := base
		changed.ShowRuntimeStatus = !base.ShowRuntimeStatus
		if err := store.SaveSettings(changed); err != nil {
			t.Fatal(err)
		}

		if recorder := restore(t, store, []string{auth.RoleEditSettings}); recorder.Code != http.StatusOK {
			t.Fatalf("restore of a design field with edit_settings: %d %s", recorder.Code, recorder.Body.String())
		}
		if value, err := store.LoadSettings(); err != nil || value.ShowRuntimeStatus != base.ShowRuntimeStatus {
			t.Fatalf("design field not restored: %+v (err=%v)", value.ShowRuntimeStatus, err)
		}
	})
}
