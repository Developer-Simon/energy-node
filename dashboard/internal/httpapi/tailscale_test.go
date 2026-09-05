package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
	"github.com/Developer-Simon/energy-node-dashboard/internal/tailscale"
)

type fakeTailscaleOutputRunner struct {
	out string
	err error
}

func (r fakeTailscaleOutputRunner) Output(context.Context, string, ...string) (string, error) {
	return r.out, r.err
}

func newTailscaleTestRouter(t *testing.T, executor SystemActionExecutor, client *tailscale.Client) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{
		Auth:          manager,
		SystemActions: executor,
		Tailscale:     client,
	})
	return router, manager
}

func TestTailscaleStatusRequiresNoRole(t *testing.T) {
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{"BackendState":"Running"}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, &fakeSystemExecutor{}, client)

	guestCookie := loginAsGuest(t, router)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tailscale/status", nil)
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("guest status status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Status struct {
			BackendState string `json:"backend_state"`
		} `json:"status"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status.BackendState != "Running" {
		t.Fatalf("backend_state = %q, want Running", body.Status.BackendState)
	}
}

func TestTailscaleLoginRequiresRoleHTTPSAndCSRF(t *testing.T) {
	executor := &fakeSystemExecutor{}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)

	guestCookie := loginAsGuest(t, router)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/login", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("guest login status %d, want 403: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 0 {
		t.Fatal("tailscale-up was executed despite the guest being forbidden")
	}
}

func TestTailscaleLoginStartsActionAndReturnsAccepted(t *testing.T) {
	executor := &fakeSystemExecutor{}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/xxx"}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/login", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("login status %d, want 202: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.TailscaleUp {
		t.Fatalf("executor.calls = %#v, want a single TailscaleUp call", executor.calls)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tailscale/status", nil)
	statusRequest.AddCookie(adminCookie)
	statusRequest.Header.Set("X-Forwarded-Proto", "https")
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusRequest)
	var body struct {
		LastAction struct {
			Action string `json:"action"`
			OK     bool   `json:"ok"`
		} `json:"last_action"`
	}
	if err := json.NewDecoder(statusRecorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.LastAction.Action != string(systemactions.TailscaleUp) || !body.LastAction.OK {
		t.Fatalf("last_action = %#v, want a successful tailscale-up record", body.LastAction)
	}
}

func TestTailscaleLoginWithoutConfirmIsRejected(t *testing.T) {
	executor := &fakeSystemExecutor{}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/login", strings.NewReader(`{"confirm":false}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed login status %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 0 {
		t.Fatal("tailscale-up was executed despite confirm=false")
	}
}

func TestTailscaleLogoutRequiresConfirm(t *testing.T) {
	executor := &fakeSystemExecutor{}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	unconfirmed := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/logout", strings.NewReader(`{"confirm":false}`))
	unconfirmed.AddCookie(adminCookie)
	unconfirmed.Header.Set("X-Forwarded-Proto", "https")
	unconfirmed.Header.Set("X-CSRF-Token", csrfToken)
	unconfirmedRecorder := httptest.NewRecorder()
	router.ServeHTTP(unconfirmedRecorder, unconfirmed)
	if unconfirmedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed logout status %d, want 400: %s", unconfirmedRecorder.Code, unconfirmedRecorder.Body.String())
	}

	confirmed := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/logout", strings.NewReader(`{"confirm":true}`))
	confirmed.AddCookie(adminCookie)
	confirmed.Header.Set("X-Forwarded-Proto", "https")
	confirmed.Header.Set("X-CSRF-Token", csrfToken)
	confirmedRecorder := httptest.NewRecorder()
	router.ServeHTTP(confirmedRecorder, confirmed)
	if confirmedRecorder.Code != http.StatusOK {
		t.Fatalf("confirmed logout status %d, want 200: %s", confirmedRecorder.Code, confirmedRecorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.TailscaleLogout {
		t.Fatalf("executor.calls = %#v, want a single TailscaleLogout call", executor.calls)
	}
}

func TestTailscaleRestartRequiresSystemActionsOnlyAndNoConfirm(t *testing.T) {
	executor := &fakeSystemExecutor{}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/restart", nil)
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restart status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.TailscaleRestart {
		t.Fatalf("executor.calls = %#v, want a single TailscaleRestart call", executor.calls)
	}
}

func TestTailscaleActionReportsBusy(t *testing.T) {
	executor := &fakeSystemExecutor{err: systemactions.ErrBusy}
	client := tailscale.NewClient("tailscale", fakeTailscaleOutputRunner{out: `{}`}, time.Second)
	router, _ := newTailscaleTestRouter(t, executor, client)
	adminCookie, csrfToken := loginAsAdmin(t, router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tailscale/login", strings.NewReader(`{"confirm":true}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("busy login status %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
}
