package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/monitoring"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// monitoringRouter builds the authenticated router with the monitoring deps.
func monitoringRouter(t *testing.T, deps *MonitoringDeps) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, Monitoring: deps})
	return router, manager
}

// monitoringAckRequest posts an acknowledgement with the cookie and CSRF token.
func monitoringAckRequest(router http.Handler, cookie *http.Cookie, csrf, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, monitoringAckPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// monitoringGetRequest reads the monitoring page with the cookie.
func monitoringGetRequest(router http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, monitoringPath, nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestMonitoringGetCountsNew(t *testing.T) {
	notices := []monitoring.Notice{
		{ID: "energy:a", Source: "energy", Severity: "warn", Code: "a", Ack: &monitoring.Ack{At: 1, User: "x"}},
		{ID: "learn:b", Source: "learn", Severity: "info", Code: "b"},
	}
	deps := &MonitoringDeps{
		Notices: func(time.Time) []monitoring.Notice { return notices },
	}
	router, manager := monitoringRouter(t, deps)
	cookie, _ := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring})

	recorder := monitoringGetRequest(router, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Notices  []monitoring.Notice `json:"notices"`
		NewCount int                 `json:"new_count"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.NewCount != 1 {
		t.Fatalf("new_count = %d, want 1", body.NewCount)
	}
	if len(body.Notices) != 2 {
		t.Fatalf("notices = %+v, want both notices", body.Notices)
	}
	raw := recorder.Body.String()
	if strings.Contains(raw, `"notices":null`) || strings.Contains(raw, `"values":null`) {
		t.Fatalf("answer carries null lists: %s", raw)
	}

	// Without notices the lists stay empty, never null.
	deps.Notices = func(time.Time) []monitoring.Notice { return nil }
	recorder = monitoringGetRequest(router, cookie)
	if !strings.Contains(recorder.Body.String(), `"notices":[]`) || !strings.Contains(recorder.Body.String(), `"values":[]`) {
		t.Fatalf("empty answer = %s, want empty lists", recorder.Body.String())
	}
}

func TestMonitoringAckNeedsEditSetup(t *testing.T) {
	var acked []string
	deps := &MonitoringDeps{
		Notices: func(time.Time) []monitoring.Notice { return nil },
		Ack: func(id string, ack bool, user string, now time.Time) error {
			acked = append(acked, id)
			return nil
		},
	}
	router, manager := monitoringRouter(t, deps)

	// A user who sees the monitoring but may not set up must not acknowledge.
	viewCookie, viewCSRF := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring})
	recorder := monitoringAckRequest(router, viewCookie, viewCSRF, `{"id":"energy:a","ack":true}`)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"monitoring_ack_forbidden"`) {
		t.Fatalf("view only POST: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(acked) != 0 {
		t.Fatalf("acknowledged by a view only user: %v", acked)
	}

	// With edit_setup the same request is saved and answers the page.
	setupCookie, setupCSRF := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring, auth.RoleEditSetup})
	recorder = monitoringAckRequest(router, setupCookie, setupCSRF, `{"id":"energy:a","ack":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("setup POST: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(acked) != 1 || acked[0] != "energy:a" {
		t.Fatalf("acked = %v, want energy:a", acked)
	}
}

func TestMonitoringAckUnknownIs404(t *testing.T) {
	deps := &MonitoringDeps{
		Notices: func(time.Time) []monitoring.Notice { return nil },
		Ack: func(string, bool, string, time.Time) error {
			return monitoring.ErrUnknown
		},
	}
	router, manager := monitoringRouter(t, deps)
	cookie, csrf := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring, auth.RoleEditSetup})

	recorder := monitoringAckRequest(router, cookie, csrf, `{"id":"gone:x","ack":true}`)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"monitoring_ack_unknown"`) {
		t.Fatalf("unknown ID: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringAckEmptyIDIs400(t *testing.T) {
	var acked []string
	deps := &MonitoringDeps{
		Notices: func(time.Time) []monitoring.Notice { return nil },
		Ack: func(id string, ack bool, user string, now time.Time) error {
			acked = append(acked, id)
			return nil
		},
	}
	router, manager := monitoringRouter(t, deps)
	cookie, csrf := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring, auth.RoleEditSetup})

	recorder := monitoringAckRequest(router, cookie, csrf, `{"id":"","ack":true}`)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"monitoring_ack_invalid"`) {
		t.Fatalf("empty ID: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(acked) != 0 {
		t.Fatalf("Ack called for an empty ID: %v", acked)
	}
}

func TestMonitoringNilDepsIs501(t *testing.T) {
	router, manager := monitoringRouter(t, nil)
	cookie, csrf := guestCookie(t, router, manager, []string{auth.RoleViewMonitoring, auth.RoleEditSetup})

	if recorder := monitoringGetRequest(router, cookie); recorder.Code != http.StatusNotImplemented {
		t.Fatalf("GET without deps: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := monitoringAckRequest(router, cookie, csrf, `{"id":"energy:a","ack":true}`); recorder.Code != http.StatusNotImplemented {
		t.Fatalf("POST without deps: %d %s", recorder.Code, recorder.Body.String())
	}
}
