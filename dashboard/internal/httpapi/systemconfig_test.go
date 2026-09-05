package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// newSystemConfigTestRouter folgt newMQTTTestRouter aus mqtt_test.go:
// derselbe Bootstrap-Admin ("admin"/"secret"), der zugleich die Rolle
// system_actions haelt, und derselbe Router-Konstruktor.
func newSystemConfigTestRouter(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	template, err := os.ReadFile(filepath.Join("..", "..", "..", "src", "energy-node.config.json"))
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
		Auth: manager, DataDir: dataDir, AppConfigPath: configPath,
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
