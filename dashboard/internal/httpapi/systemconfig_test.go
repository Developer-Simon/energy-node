package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/appconfig"
	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/localize"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// newSystemConfigTestRouter folgt newMQTTTestRouter aus mqtt_test.go:
// derselbe Bootstrap-Admin ("admin"/"secret"), der zugleich die Rolle
// system_actions haelt, und derselbe Router-Konstruktor.
func newSystemConfigTestRouter(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	return newSystemConfigTestRouterWith(t, nil)
}

func newSystemConfigTestRouterWith(t *testing.T, executor SystemActionExecutor) (http.Handler, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	template, err := os.ReadFile(filepath.Join("..", "..", "..", "services", "energy-node.config.json"))
	if err != nil {
		t.Fatalf("Vorlage lesen: %v", err)
	}
	if err := os.WriteFile(configPath, template, 0o664); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(filepath.Join(dataDir, "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(dataDir), nil, nil, nil, nil, nil, RouterDependencies{
		Auth: manager, DataDir: dataDir, AppConfigPath: configPath, SystemActions: executor,
	})
	return router, configPath, dataDir
}

func putSystemConfig(t *testing.T, router http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrfToken := loginAsAdmin(t, router)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/system/config", strings.NewReader(body))
	request.AddCookie(cookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// documentWith liefert die Vorlage mit einer geaenderten Stelle, als
// JSON-Text fuer den Request-Body.
func documentWith(t *testing.T, configPath string, mutate func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestSystemConfigGetReturnsFileWithoutSecrets(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	cookie, _ := loginAsAdmin(t, router)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Config struct {
			MQTT struct {
				PasswordFile string `json:"password_file"`
			} `json:"mqtt"`
		} `json:"config"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Antwort entpacken: %v", err)
	}
	if payload.Config.MQTT.PasswordFile != "/etc/energy-node/mqtt.pw" {
		t.Fatalf("password_file = %q, erwartet den Pfad aus der Datei", payload.Config.MQTT.PasswordFile)
	}
	if strings.Contains(recorder.Body.String(), `"password"`) {
		t.Fatal("die Antwort enthaelt ein password-Feld")
	}
	_ = configPath
}

func TestSystemConfigSchemaIsServed(t *testing.T) {
	router, _, _ := newSystemConfigTestRouter(t)
	// Nicht rollengeschuetzt: ein Gast-Login reicht, wie bei GET des Dokuments.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/config/schema", nil)
	request.AddCookie(loginAsGuest(t, router))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET schema status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type %q, want application/json", got)
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &schema); err != nil {
		t.Fatalf("Schema entpacken: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("schema.type = %q, want object", schema.Type)
	}
	for _, section := range []string{"schema_version", "mqtt", "paths", "dashboard"} {
		if _, ok := schema.Properties[section]; !ok {
			t.Fatalf("Schema beschreibt %q nicht", section)
		}
	}
}

func TestSystemConfigSchemaRejectsPut(t *testing.T) {
	router, _, _ := newSystemConfigTestRouter(t)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/system/config/schema", strings.NewReader("{}"))
	request.AddCookie(loginAsGuest(t, router))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT schema status %d, want 405", recorder.Code)
	}
}

func TestSystemConfigPutRejectedForGuest(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	before, _ := os.ReadFile(configPath)

	guestCookie := loginAsGuest(t, router)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/system/config", strings.NewReader(string(before)))
	request.AddCookie(guestCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusOK {
		t.Fatalf("Gast-PUT status %d, erwartet Ablehnung", recorder.Code)
	}
	after, _ := os.ReadFile(configPath)
	if string(after) != string(before) {
		t.Fatal("die Datei wurde trotz Ablehnung geaendert")
	}
}

func TestSystemConfigPutRejectedWithoutHTTPSAndWithoutCSRF(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	body, _ := os.ReadFile(configPath)
	cookie, csrfToken := loginAsAdmin(t, router)

	// Ohne X-Forwarded-Proto erkennt authMiddleware das Admin-Cookie gar
	// nicht erst - der Weg ueber HTTP ist damit durchgehend gesperrt.
	insecure := httptest.NewRequest(http.MethodPut, "/api/v1/system/config", strings.NewReader(string(body)))
	insecure.AddCookie(cookie)
	insecure.Header.Set("X-CSRF-Token", csrfToken)
	insecureRecorder := httptest.NewRecorder()
	router.ServeHTTP(insecureRecorder, insecure)
	if insecureRecorder.Code == http.StatusOK {
		t.Fatalf("PUT ueber HTTP status %d, erwartet Ablehnung", insecureRecorder.Code)
	}

	noCSRF := httptest.NewRequest(http.MethodPut, "/api/v1/system/config", strings.NewReader(string(body)))
	noCSRF.AddCookie(cookie)
	noCSRF.Header.Set("X-Forwarded-Proto", "https")
	noCSRFRecorder := httptest.NewRecorder()
	router.ServeHTTP(noCSRFRecorder, noCSRF)
	if noCSRFRecorder.Code != http.StatusForbidden {
		t.Fatalf("PUT ohne CSRF status %d, erwartet 403", noCSRFRecorder.Code)
	}
}

func TestSystemConfigPutRejectsPathOutsideAllowlist(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	before, _ := os.ReadFile(configPath)
	body := documentWith(t, configPath, func(document map[string]any) {
		document["mqtt"].(map[string]any)["password_file"] = "/etc/shadow"
	})

	recorder := putSystemConfig(t, router, body)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, erwartet 400: %s", recorder.Code, recorder.Body.String())
	}
	after, _ := os.ReadFile(configPath)
	if string(after) != string(before) {
		t.Fatal("die Datei wurde trotz abgelehntem Pfad geaendert")
	}
}

func TestSystemConfigPutRejectsSchemaViolation(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	before, _ := os.ReadFile(configPath)
	body := documentWith(t, configPath, func(document map[string]any) {
		document["dashboard"].(map[string]any)["port"] = "8080"
	})

	recorder := putSystemConfig(t, router, body)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, erwartet 400: %s", recorder.Code, recorder.Body.String())
	}
	after, _ := os.ReadFile(configPath)
	if string(after) != string(before) {
		t.Fatal("die Datei wurde trotz Schemaverletzung geaendert")
	}
}

func TestSystemConfigPutWritesAndKeepsRevision(t *testing.T) {
	router, configPath, dataDir := newSystemConfigTestRouter(t)
	body := documentWith(t, configPath, func(document map[string]any) {
		document["logging"].(map[string]any)["level"] = "DEBUG"
	})

	recorder := putSystemConfig(t, router, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, erwartet 200: %s", recorder.Code, recorder.Body.String())
	}

	written, _ := os.ReadFile(configPath)
	if !strings.Contains(string(written), "DEBUG") {
		t.Fatal("der neue Wert steht nicht in der Datei")
	}
	revisions, err := os.ReadDir(filepath.Join(dataDir, "revisions", "system-config"))
	if err != nil {
		t.Fatalf("Revisionsverzeichnis: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("%d Revisionen, erwartet 1", len(revisions))
	}
}

func TestSystemConfigPutReportsRestartRequiredFields(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)

	logLevelBody := documentWith(t, configPath, func(document map[string]any) {
		document["logging"].(map[string]any)["level"] = "DEBUG"
	})
	var payload struct {
		RestartRequired []string `json:"restart_required"`
	}
	recorder := putSystemConfig(t, router, logLevelBody)
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.RestartRequired) != 0 {
		t.Fatalf("restart_required = %v, erwartet leer", payload.RestartRequired)
	}

	hostBody := documentWith(t, configPath, func(document map[string]any) {
		document["mqtt"].(map[string]any)["host"] = "broker.local"
	})
	recorder = putSystemConfig(t, router, hostBody)
	payload.RestartRequired = nil
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.RestartRequired) == 0 {
		t.Fatal("restart_required ist leer, erwartet den Eintrag mqtt")
	}
}

func TestSystemConfigSchemaFollowsTheLanguage(t *testing.T) {
	router, _, _ := newSystemConfigTestRouter(t)
	session := loginAsGuest(t, router)
	keys := regexp.MustCompile(`"[A-Za-z_]+":`)
	var source bytes.Buffer
	if err := json.Compact(&source, appconfig.Schema()); err != nil {
		t.Fatal(err)
	}
	wantKeys := keys.FindAllString(source.String(), -1)
	for lang, want := range map[string]string{"de": "Broker-Host", "en": "Broker host"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/system/config/schema", nil)
		request.AddCookie(session)
		request.AddCookie(&http.Cookie{Name: localize.CookieName, Value: lang})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d", lang, recorder.Code)
		}
		var body bytes.Buffer
		if err := json.Compact(&body, recorder.Body.Bytes()); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if !strings.Contains(body.String(), `"title":"`+want+`"`) {
			t.Errorf("%s: schema lacks %q", lang, want)
		}
		if got := keys.FindAllString(body.String(), -1); !reflect.DeepEqual(got, wantKeys) {
			t.Errorf("%s: keys or key order changed", lang)
		}
	}
}

// installingExecutor steht fuer apply-app-config: es merkt sich die
// gestagete Datei zum Aufrufzeitpunkt.
type installingExecutor struct {
	stagedPath string
	staged     string
	calls      []systemactions.Action
	err        error
}

func (e *installingExecutor) Execute(_ context.Context, action systemactions.Action) error {
	e.calls = append(e.calls, action)
	data, _ := os.ReadFile(e.stagedPath)
	e.staged = string(data)
	return e.err
}

// readOnlyConfigDir macht das Verzeichnis der config.json so unbeschreibbar
// wie /etc/energy-node mit 0755 fuer die Dienstgruppe. Als root greift das
// nicht, dann ueberspringt der Test.
func readOnlyConfigDir(t *testing.T, configPath string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("als root blockiert chmod 0555 nichts")
	}
	dir := filepath.Dir(configPath)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestSystemConfigPutUsesHelperWhenDirectWriteIsDenied(t *testing.T) {
	executor := &installingExecutor{}
	router, configPath, dataDir := newSystemConfigTestRouterWith(t, executor)
	executor.stagedPath = filepath.Join(dataDir, systemactions.StagedAppConfigName)
	readOnlyConfigDir(t, configPath)

	body := documentWith(t, configPath, func(doc map[string]any) {})
	recorder := putSystemConfig(t, router, body)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 1 || executor.calls[0] != systemactions.ApplyAppConfig {
		t.Fatalf("calls = %v, want [apply-app-config]", executor.calls)
	}
	if executor.staged != body {
		t.Fatalf("gestagete Datei weicht vom Request-Body ab")
	}
	if _, err := os.Stat(executor.stagedPath); !os.IsNotExist(err) {
		t.Fatalf("Staging-Datei nicht aufgeraeumt (%v)", err)
	}
}

func TestSystemConfigPutDoesNotCallHelperWhenDirectWriteWorks(t *testing.T) {
	executor := &installingExecutor{}
	router, configPath, _ := newSystemConfigTestRouterWith(t, executor)

	recorder := putSystemConfig(t, router, documentWith(t, configPath, func(doc map[string]any) {}))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", recorder.Code, recorder.Body.String())
	}
	if len(executor.calls) != 0 {
		t.Fatalf("Helper darf nicht laufen, calls = %v", executor.calls)
	}
}

func TestSystemConfigPutReportsHelperFailureAndLeavesNoStagingFile(t *testing.T) {
	executor := &installingExecutor{err: errors.New("exit status 65")}
	router, configPath, dataDir := newSystemConfigTestRouterWith(t, executor)
	executor.stagedPath = filepath.Join(dataDir, systemactions.StagedAppConfigName)
	readOnlyConfigDir(t, configPath)

	recorder := putSystemConfig(t, router, documentWith(t, configPath, func(doc map[string]any) {}))

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "config_not_writable") {
		t.Fatalf("status = %d, body %s, want 500 config_not_writable", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(executor.stagedPath); !os.IsNotExist(err) {
		t.Fatalf("Staging-Datei nach Helper-Fehler nicht aufgeraeumt (%v)", err)
	}
}

func TestSystemConfigPutWithoutExecutorKeepsConfigNotWritable(t *testing.T) {
	router, configPath, _ := newSystemConfigTestRouter(t)
	readOnlyConfigDir(t, configPath)

	recorder := putSystemConfig(t, router, documentWith(t, configPath, func(doc map[string]any) {}))

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "config_not_writable") {
		t.Fatalf("status = %d, body %s, want 500 config_not_writable", recorder.Code, recorder.Body.String())
	}
}

func TestSystemConfigPutDoesNotCallHelperForNonPermissionErrors(t *testing.T) {
	executor := &installingExecutor{}
	dataDir := t.TempDir()
	// Elternpfad ist eine Datei: der direkte Schreibversuch scheitert mit
	// ENOTDIR, nicht mit einem Rechtefehler.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeSystemConfig(context.Background(), filepath.Join(blocker, "config.json"), dataDir, []byte(`{}`), executor)
	if err == nil {
		t.Fatal("erwartet Fehler")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("Helper darf bei Nicht-Rechtefehler nicht laufen, calls = %v", executor.calls)
	}
}
