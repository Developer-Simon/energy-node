package hostapi_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestOpenURLPassesAnHTTPSAddressToTheHost(t *testing.T) {
	var opened []string
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.OpenURL = func(u string) error { opened = append(opened, u); return nil }
	})
	rec := do(t, server, http.MethodPost, "/api/open-url", `{"url":"https://login.tailscale.com/a/abc123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(opened) != 1 || opened[0] != "https://login.tailscale.com/a/abc123" {
		t.Errorf("opened = %v", opened)
	}
}

func TestOpenURLRefusesAnythingButHTTP(t *testing.T) {
	called := false
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.OpenURL = func(string) error { called = true; return nil }
	})
	for _, bad := range []string{`file:///etc/passwd`, `javascript:alert(1)`, `ftp://example.org/x`, `https://`, `not a url`, `-o x`} {
		rec := do(t, server, http.MethodPost, "/api/open-url", `{"url":"`+bad+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", bad, rec.Code)
		}
	}
	long := `{"url":"https://example.org/` + strings.Repeat("a", 3000) + `"}`
	if rec := do(t, server, http.MethodPost, "/api/open-url", long); rec.Code != http.StatusBadRequest {
		t.Errorf("long address: status = %d, want 400", rec.Code)
	}
	if called {
		t.Errorf("the host was asked to open a refused address")
	}
}

func TestOpenURLIsNotSupportedWithoutAHostOpener(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/open-url", `{"url":"https://example.org/"}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if strings.Contains(boot.Body.String(), "can_open_url") {
		t.Errorf("bootstrap advertises an opener that is not there: %s", boot.Body.String())
	}
}

func TestOpenURLReportsAHostError(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.OpenURL = func(string) error { return errors.New("no browser") }
	})
	rec := do(t, server, http.MethodPost, "/api/open-url", `{"url":"https://example.org/"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if !strings.Contains(boot.Body.String(), `"can_open_url":true`) {
		t.Errorf("bootstrap does not advertise the opener: %s", boot.Body.String())
	}
}
