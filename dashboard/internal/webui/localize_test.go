package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func renderLogin(t *testing.T, mutate func(*http.Request)) string {
	t.Helper()
	request := httptest.NewRequest("GET", "/", nil)
	if mutate != nil {
		mutate(request)
	}
	recorder := httptest.NewRecorder()
	Login(false, nil, "").ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("got status %d", recorder.Code)
	}
	return recorder.Body.String()
}

func TestLoginDefaultsToGerman(t *testing.T) {
	if body := renderLogin(t, nil); !strings.Contains(body, `<html lang="de" data-base-path="" data-theme="mint">`) {
		t.Fatalf("unexpected html tag: %s", body)
	}
}

func TestLoginFollowsAcceptLanguage(t *testing.T) {
	body := renderLogin(t, func(r *http.Request) { r.Header.Set("Accept-Language", "en-GB,en;q=0.9") })
	if !strings.Contains(body, `<html lang="en" data-base-path="" data-theme="mint">`) {
		t.Fatalf("Accept-Language en did not select English: %s", body)
	}
}

func TestLanguageCookieBeatsAcceptLanguage(t *testing.T) {
	body := renderLogin(t, func(r *http.Request) {
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	})
	if !strings.Contains(body, `<html lang="de"`) {
		t.Fatalf("cookie de must beat Accept-Language en: %s", body)
	}
}

func TestUnknownLanguageCookieIsIgnored(t *testing.T) {
	body := renderLogin(t, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "lang", Value: "xx"}) })
	if !strings.Contains(body, `<html lang="de"`) {
		t.Fatalf("an unknown cookie must be ignored: %s", body)
	}
}

func TestOverviewLoadsTheCatalogScriptBeforeAnyDeferredScript(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(&http.Cookie{Name: "lang", Value: "en"})
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, `<html lang="en"`) {
		t.Fatalf("overview does not render English: %s", body)
	}
	script := `<script src="/i18n/en.js?v=` + translator.Version() + `"></script>`
	at := strings.Index(body, script)
	if at < 0 {
		t.Fatalf("overview does not load %s", script)
	}
	if first := strings.Index(body, "defer>"); first >= 0 && first < at {
		t.Fatal("the catalog script must precede every deferred script")
	}
}

func TestOverviewPrefixesTheCatalogScriptBehindAProxy(t *testing.T) {
	body := renderWithBasePath(t, Overview(registry.New(), nil, nil), "/node/")
	if !strings.Contains(body, `<script src="/node/i18n/de.js?v=`) {
		t.Fatalf("catalog script is not prefixed: %s", body)
	}
}
