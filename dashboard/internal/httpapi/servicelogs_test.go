package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/servicelogs"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

type fakeServiceLogReader struct {
	query  servicelogs.Query
	calls  int
	result servicelogs.Result
	err    error
}

func (f *fakeServiceLogReader) Read(_ context.Context, q servicelogs.Query) (servicelogs.Result, error) {
	f.calls++
	f.query = q
	return f.result, f.err
}

func newServiceLogsTestRouter(t *testing.T, reader ServiceLogReader) http.Handler {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, ServiceLogs: reader})
}

// serviceLogsGet sends a GET as an admin over HTTPS. Guest cookies are issued
// over plain HTTP, so the guest test builds its request without the header.
func serviceLogsGet(t *testing.T, router http.Handler, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestServiceLogsRequiresAuth(t *testing.T) {
	reader := &fakeServiceLogReader{}
	router := newServiceLogsTestRouter(t, reader)
	recorder := serviceLogsGet(t, router, "/api/v1/system/logs?unit=dashboard", nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	if reader.calls != 0 {
		t.Fatal("reader was called without a session")
	}
}

func TestServiceLogsRequiresSystemActionsRole(t *testing.T) {
	reader := &fakeServiceLogReader{}
	router := newServiceLogsTestRouter(t, reader)
	guest := loginAsGuest(t, router)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/logs?unit=dashboard", nil)
	request.AddCookie(guest)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"system_actions_forbidden"`) {
		t.Fatalf("guest status %d: %s", recorder.Code, recorder.Body.String())
	}
	if reader.calls != 0 {
		t.Fatal("reader was called for a guest without the role")
	}
}

func TestServiceLogsRejectsPost(t *testing.T) {
	router := newServiceLogsTestRouter(t, &fakeServiceLogReader{})
	admin, _ := loginAsAdmin(t, router)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/logs?unit=dashboard", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(admin)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d, want 405: %s", recorder.Code, recorder.Body.String())
	}
}

func TestServiceLogsParsesQuery(t *testing.T) {
	reader := &fakeServiceLogReader{result: servicelogs.Result{Entries: []servicelogs.Entry{}, Cursor: "s=2"}}
	router := newServiceLogsTestRouter(t, reader)
	admin, _ := loginAsAdmin(t, router)

	recorder := serviceLogsGet(t, router, "/api/v1/system/logs?unit=caddy&since=360&lines=500&cursor=s%3D1&q=tls", admin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	want := servicelogs.Query{Unit: "caddy", SinceMinutes: 360, Lines: 500, Cursor: "s=1", Filter: "tls"}
	if reader.query != want {
		t.Fatalf("query = %#v, want %#v", reader.query, want)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	var body servicelogs.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Cursor != "s=2" {
		t.Fatalf("result body %s (err=%v)", recorder.Body.String(), err)
	}

	recorder = serviceLogsGet(t, router, "/api/v1/system/logs?unit=dashboard", admin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("defaults status %d: %s", recorder.Code, recorder.Body.String())
	}
	want = servicelogs.Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200}
	if reader.query != want {
		t.Fatalf("default query = %#v, want %#v", reader.query, want)
	}
}

func TestServiceLogsMapsErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid query", servicelogs.ErrInvalidQuery, http.StatusBadRequest, "log_query_invalid"},
		{"not permitted", servicelogs.ErrNotPermitted, http.StatusServiceUnavailable, "journal_not_permitted"},
		{"journal failed", servicelogs.ErrJournal, http.StatusBadGateway, "journal_failed"},
		{"wrapped journal failed", fmt.Errorf("helper: %w", servicelogs.ErrJournal), http.StatusBadGateway, "journal_failed"},
		{"timeout", servicelogs.ErrTimeout, http.StatusGatewayTimeout, "journal_timeout"},
		{"busy", servicelogs.ErrBusy, http.StatusTooManyRequests, "journal_busy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader := &fakeServiceLogReader{err: c.err}
			router := newServiceLogsTestRouter(t, reader)
			admin, _ := loginAsAdmin(t, router)
			recorder := serviceLogsGet(t, router, "/api/v1/system/logs?unit=dashboard", admin)
			if recorder.Code != c.status || !strings.Contains(recorder.Body.String(), `"code":"`+c.code+`"`) {
				t.Fatalf("status %d, want %d with code %s: %s", recorder.Code, c.status, c.code, recorder.Body.String())
			}
		})
	}
}

func TestServiceLogsNonNumericSince(t *testing.T) {
	reader := &fakeServiceLogReader{}
	router := newServiceLogsTestRouter(t, reader)
	admin, _ := loginAsAdmin(t, router)
	recorder := serviceLogsGet(t, router, "/api/v1/system/logs?unit=dashboard&since=abc", admin)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"log_query_invalid"`) {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if reader.calls != 0 {
		t.Fatal("reader was called for an invalid query")
	}
}

func TestServiceLogUnits(t *testing.T) {
	router := newServiceLogsTestRouter(t, &fakeServiceLogReader{})
	admin, _ := loginAsAdmin(t, router)
	recorder := serviceLogsGet(t, router, "/api/v1/system/logs/units", admin)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Units               []string `json:"units"`
		SinceMinutes        []int    `json:"since_minutes"`
		Lines               []int    `json:"lines"`
		DefaultUnit         string   `json:"default_unit"`
		DefaultSinceMinutes int      `json:"default_since_minutes"`
		DefaultLines        int      `json:"default_lines"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &keys); err != nil || len(keys) != 6 {
		t.Fatalf("units body has %d keys, want 6: %s", len(keys), recorder.Body.String())
	}
	if !slices.Equal(body.Units, servicelogs.Units) || !slices.Equal(body.SinceMinutes, servicelogs.SinceMinutes) || !slices.Equal(body.Lines, servicelogs.LineLimits) {
		t.Fatalf("allowlists differ: %s", recorder.Body.String())
	}
	if body.DefaultUnit != "dashboard" || body.DefaultSinceMinutes != 60 || body.DefaultLines != 200 {
		t.Fatalf("defaults differ: %s", recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
}

func TestServiceLogsHiddenWithoutReader(t *testing.T) {
	router := newServiceLogsTestRouter(t, nil)
	admin, _ := loginAsAdmin(t, router)
	// Without the reader the routes are not registered. The "/" catch-all
	// then answers with the overview page, so the check is that no JSON
	// from the service log handlers comes back.
	for _, path := range []string{"/api/v1/system/logs?unit=dashboard", "/api/v1/system/logs/units"} {
		recorder := serviceLogsGet(t, router, path, admin)
		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") || strings.Contains(recorder.Body.String(), `"entries"`) || strings.Contains(recorder.Body.String(), `"units"`) {
			t.Fatalf("%s served by a service log handler: %d %s", path, recorder.Code, recorder.Body.String())
		}
	}
}
