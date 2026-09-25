package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// The login page needs the catalog script before anyone is signed in, just
// like /static/.
func TestCatalogScriptIsReachableWithoutSignIn(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager})

	script := httptest.NewRecorder()
	handler.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/i18n/en.js", nil))
	if script.Code != http.StatusOK || !strings.HasPrefix(script.Body.String(), "window.__I18N__=") {
		t.Fatalf("catalog script status %d body %q", script.Code, script.Body.String())
	}

	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("control: /api/v1/devices status %d, want 401", protected.Code)
	}
}
