package hostapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
	"github.com/Developer-Simon/energy-node-webui/i18n"
)

const testToken = "t0ken-for-tests"

func newTestServer(t *testing.T, mutate func(*hostapi.Options)) (*hostapi.Server, *hostapitest.FakeBackend) {
	t.Helper()
	set, err := i18n.Load(webui.Catalogs())
	if err != nil {
		t.Fatalf("i18n.Load: %v", err)
	}
	fake := hostapitest.NewFake()
	opts := hostapi.Options{Backend: fake, Catalogs: set, Token: testToken, Language: "de"}
	if mutate != nil {
		mutate(&opts)
	}
	server, err := hostapi.New(opts)
	if err != nil {
		t.Fatalf("hostapi.New: %v", err)
	}
	return server, fake
}

func do(t *testing.T, server *hostapi.Server, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("X-Installer-Token", testToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func TestNewRejectsOptionsWithoutABackend(t *testing.T) {
	if _, err := hostapi.New(hostapi.Options{}); err == nil {
		t.Fatalf("New accepted options without a backend")
	}
}

func TestTheShellCarriesTheAssetVersionAndTheToken(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "?v="+webui.AssetVersion()) {
		t.Errorf("the shell does not carry the central cache-bust mark:\n%s", body)
	}
	if !strings.Contains(body, testToken) {
		t.Errorf("the shell does not hand the token to the page")
	}
}

func TestAnAssetIsServedWithoutAToken(t *testing.T) {
	server, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/assets/js/does-not-exist.js", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("assets must not require the token")
	}
}

func TestEveryAPIPathRequiresTheToken(t *testing.T) {
	server, _ := newTestServer(t, nil)
	for _, path := range []string{"/", "/api/bootstrap", "/api/catalog/de", "/api/precheck"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: status %d, want 401", path, rec.Code)
		}
	}
}

func TestTheTokenIsAlsoAcceptedAsAQueryParameter(t *testing.T) {
	server, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap?token="+testToken, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 - EventSource cannot set headers", rec.Code)
	}
}

func TestAnEmptyTokenSwitchesTheCheckOff(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) { o.Token = "" })
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a host that authenticates on its own", rec.Code)
	}
}

func TestBootstrapDescribesTheHostAndTheLanguages(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got hostapi.Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.Host != hostapi.HostInstaller {
		t.Errorf("host = %q, want installer", got.Host)
	}
	if got.Language != "de" || got.LanguageFixed {
		t.Errorf("language = %q fixed=%v, want de and switchable", got.Language, got.LanguageFixed)
	}
	if len(got.Languages) < 2 {
		t.Errorf("languages = %v, want at least de and en", got.Languages)
	}
	if got.AssetVersion != webui.AssetVersion() {
		t.Errorf("asset_version = %q, want %q", got.AssetVersion, webui.AssetVersion())
	}
	if !got.NeedsConnection {
		t.Errorf("the installer host needs a connection screen")
	}
}

func TestBootstrapReportsAFixedLanguageForTheDashboardHost(t *testing.T) {
	server, fake := newTestServer(t, func(o *hostapi.Options) {
		o.Language = "de"
		o.LanguageFixed = true
	})
	fake.Description.Host = hostapi.HostDashboard
	fake.Description.NeedsConnection = false

	rec := do(t, server, http.MethodGet, "/api/bootstrap", "")
	var got hostapi.Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if !got.LanguageFixed || got.Language != "de" {
		t.Errorf("the dashboard host must report a fixed German: %+v", got)
	}
	if got.NeedsConnection {
		t.Errorf("the dashboard host must not ask for a connection")
	}
}

func TestCatalogReturnsTheMergedCatalog(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/catalog/de", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var catalog map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if catalog["app.title"] == "" {
		t.Errorf("app.title is missing from the catalog")
	}
	if catalog["fault.ARCH_MISMATCH.message"] == "" {
		t.Errorf("the fault texts must travel in the same catalog")
	}
}

func TestAnUnknownCatalogFallsBackToEnglishInsteadOf404(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/catalog/fr", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the English catalog", rec.Code)
	}
}

func TestAWrongMethodIsRejectedWithACode(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/bootstrap", "{}")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("an error body must be JSON: %v", err)
	}
	if payload["error"] != "METHOD_NOT_ALLOWED" {
		t.Errorf("error = %q, want METHOD_NOT_ALLOWED", payload["error"])
	}
}

func TestBootstrapCarriesTheBundleArchitecture(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.Description.BundleArch = "armv6"
	rec := do(t, server, http.MethodGet, "/api/bootstrap", "")
	var got hostapi.Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.BundleArch != "armv6" {
		t.Errorf("bundle_arch = %q, want armv6 - the connection screen shows it before any connection exists", got.BundleArch)
	}
}
