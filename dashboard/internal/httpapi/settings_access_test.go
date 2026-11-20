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
