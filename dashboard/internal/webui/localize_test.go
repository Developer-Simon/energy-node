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

func TestLoginTextsFollowTheLanguage(t *testing.T) {
	german := renderLogin(t, nil)
	for _, want := range []string{
		"<title>Anmeldung | Energy Node Dashboard</title>",
		"Bitte anmelden oder als Gast fortfahren.",
		"Benutzername <input",
		"Passwort <input",
		">Anmelden</button>",
		">Als Gast fortfahren</button>",
		`data-failed="Anfrage fehlgeschlagen"`,
	} {
		if !strings.Contains(german, want) {
			t.Errorf("German login page lacks %q", want)
		}
	}
	english := renderLogin(t, func(r *http.Request) { r.Header.Set("Accept-Language", "en") })
	for _, want := range []string{
		"<title>Sign in | Energy Node Dashboard</title>",
		"Please sign in or continue as a guest.",
		"Username <input",
		"Password <input",
		">Sign in</button>",
		">Continue as guest</button>",
		`data-failed="Request failed"`,
	} {
		if !strings.Contains(english, want) {
			t.Errorf("English login page lacks %q", want)
		}
	}
	if strings.Contains(english, "Bitte anmelden") || strings.Contains(english, "Benutzername") {
		t.Errorf("English login page still contains German text: %s", english)
	}
}

func TestLoginOffersTheLanguageSwitcher(t *testing.T) {
	body := renderLogin(t, func(r *http.Request) { r.Header.Set("Accept-Language", "en") })
	for _, want := range []string{
		`<select data-lang-select>`,
		`<option value="de">Deutsch</option>`,
		`<option value="en" selected>English</option>`,
		`static/js/i18n.js?v=1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page lacks %q", want)
		}
	}
}

func TestLoginGuestOnlyHintFollowsTheLanguage(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Accept-Language", "en")
	Login(true, nil, "").ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "Functionality is reduced over HTTP.") {
		t.Fatalf("guest-only hint is not English: %s", recorder.Body.String())
	}
}

func renderOverviewWithLang(t *testing.T, lang string) string {
	t.Helper()
	request := httptest.NewRequest("GET", "/", nil)
	if lang != "" {
		request.AddCookie(&http.Cookie{Name: "lang", Value: lang})
	}
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, nil).ServeHTTP(recorder, request)
	return recorder.Body.String()
}

func TestMastheadAndStatusBarFollowTheLanguage(t *testing.T) {
	german := renderOverviewWithLang(t, "de")
	for _, want := range []string{
		"<small>Discovery-getriebene Live-Ansicht und lokaler Gerätemanager</small>",
		`<span class="runtime-status-label">Systemstatus</span>`,
		"$t('status.mqtt.connected')",
		"$t('masthead.update_available', {version: latest})",
	} {
		if !strings.Contains(german, want) {
			t.Errorf("German overview lacks %q", want)
		}
	}
	english := renderOverviewWithLang(t, "en")
	for _, want := range []string{
		"<small>Discovery-driven live view and local device manager</small>",
		`<span class="runtime-status-label">System status</span>`,
	} {
		if !strings.Contains(english, want) {
			t.Errorf("English overview lacks %q", want)
		}
	}
	for _, gone := range []string{"'verbunden'", "'nicht verfügbar'", "' verfügbar'", "'⚠ Unterspannung'"} {
		if strings.Contains(german, gone) {
			t.Errorf("overview still hard-codes %s in an Alpine expression", gone)
		}
	}
}

func TestOverviewOffersTheLanguageSwitcherAndRuntime(t *testing.T) {
	body := renderOverviewWithLang(t, "en")
	for _, want := range []string{
		`<select data-lang-select>`,
		`<option value="en" selected>English</option>`,
		`<script src="/static/js/i18n.js?v=1"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview lacks %q", want)
		}
	}
	if strings.Index(body, `/i18n/en.js`) > strings.Index(body, `static/js/i18n.js`) {
		t.Error("the catalog script must load before i18n.js")
	}
}
