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
	"github.com/Developer-Simon/energy-node-dashboard/internal/autolearn"
	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// fakeAutomationFindings answers fixed findings and records the ignore calls.
type fakeAutomationFindings struct {
	findings   []autolearn.Finding
	ignored    []string
	unignored  []string
	ignoreErr  error
	findingsAt []time.Time
	// preview is answered for every valid draft, previewed counts the calls.
	preview   []autolearn.Finding
	previewed int
}

func (f *fakeAutomationFindings) Preview(raw []byte) ([]autolearn.Finding, error) {
	if _, err := autolearn.ParseDoc(raw); err != nil {
		return nil, err
	}
	f.previewed++
	return f.preview, nil
}

func (f *fakeAutomationFindings) Findings(now time.Time) []autolearn.Finding {
	f.findingsAt = append(f.findingsAt, now)
	return f.findings
}

func (f *fakeAutomationFindings) Ignore(id string) error {
	if f.ignoreErr != nil {
		return f.ignoreErr
	}
	f.ignored = append(f.ignored, id)
	return nil
}

func (f *fakeAutomationFindings) Unignore(id string) error {
	if f.ignoreErr != nil {
		return f.ignoreErr
	}
	f.unignored = append(f.unignored, id)
	return nil
}

func TestAutomationFindingsGet(t *testing.T) {
	checker := &fakeAutomationFindings{findings: []autolearn.Finding{{ID: "duplicate:a:b", Kind: "duplicate", RuleIDs: []string{"a", "b"}, Severity: "warning"}}}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	recorder := doChainRequest(router, http.MethodGet, automationFindingsPath, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Findings []autolearn.Finding `json:"findings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Findings) != 1 || body.Findings[0].ID != "duplicate:a:b" || len(body.Findings[0].RuleIDs) != 2 {
		t.Fatalf("findings = %+v", body.Findings)
	}
	if len(checker.findingsAt) != 1 {
		t.Fatalf("Findings called %d times, want 1", len(checker.findingsAt))
	}

	// No findings answer an empty list, never null.
	checker.findings = nil
	recorder = doChainRequest(router, http.MethodGet, automationFindingsPath, "")
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"findings":[]}` {
		t.Fatalf("empty answer = %s, want an empty list", got)
	}

	// Without a checker the route answers 501.
	bare := NewRouter(registry.New(), nil, settings.NewStore(t.TempDir()))
	recorder = doChainRequest(bare, http.MethodGet, automationFindingsPath, "")
	if recorder.Code != http.StatusNotImplemented || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_unavailable"`) {
		t.Fatalf("GET without checker: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationFindingsIgnoreNeedsAutomationsRole(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewAutomations, auth.RoleAutomations}); err != nil {
		t.Fatal(err)
	}
	checker := &fakeAutomationFindings{}

	// A user who sees the automations but may not change the findings must not ignore.
	viewOnly := httptest.NewRequest(http.MethodPost, automationFindingsIgnorePath, strings.NewReader(`{"id":"orphan:a:x"}`))
	viewOnly = viewOnly.WithContext(auth.WithUser(viewOnly.Context(), auth.User{Username: "tester", Roles: []string{auth.RoleViewAutomations}}))
	recorder := httptest.NewRecorder()
	handleAutomationFindingIgnore(manager, checker).ServeHTTP(recorder, viewOnly)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_forbidden"`) {
		t.Fatalf("view only: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(checker.ignored) != 0 {
		t.Fatalf("ignored by a view only user: %v", checker.ignored)
	}

	// The full router: a POST without CSRF is refused, with it 204.
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, AutomationFindings: checker})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	cookie := guest.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	without := httptest.NewRequest(http.MethodPost, automationFindingsIgnorePath, strings.NewReader(`{"id":"orphan:a:x"}`))
	without.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, without)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("POST without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(checker.ignored) != 0 {
		t.Fatalf("ignored without CSRF: %v", checker.ignored)
	}

	with := httptest.NewRequest(http.MethodPost, automationFindingsIgnorePath, strings.NewReader(`{"id":"orphan:a:x"}`))
	with.AddCookie(cookie)
	with.Header.Set("X-CSRF-Token", session.CSRFToken)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, with)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("POST with CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(checker.ignored) != 1 || checker.ignored[0] != "orphan:a:x" {
		t.Fatalf("ignored = %v, want orphan:a:x", checker.ignored)
	}
}

func TestAutomationFindingsIgnoreAndUndo(t *testing.T) {
	checker := &fakeAutomationFindings{}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	recorder := doChainRequest(router, http.MethodPost, automationFindingsIgnorePath, `{"id":"frequent:a","undo":false}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("ignore: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = doChainRequest(router, http.MethodPost, automationFindingsIgnorePath, `{"id":"frequent:a","undo":true}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("undo: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(checker.ignored) != 1 || checker.ignored[0] != "frequent:a" {
		t.Fatalf("ignored = %v", checker.ignored)
	}
	if len(checker.unignored) != 1 || checker.unignored[0] != "frequent:a" {
		t.Fatalf("unignored = %v", checker.unignored)
	}
}

func TestAutomationFindingsIgnoreRejectsMissingID(t *testing.T) {
	checker := &fakeAutomationFindings{}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	for _, body := range []string{`{}`, `{"id":""}`, `{"undo":true}`, `{`} {
		recorder := doChainRequest(router, http.MethodPost, automationFindingsIgnorePath, body)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"automation_finding_invalid"`) {
			t.Fatalf("%s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if len(checker.ignored) != 0 || len(checker.unignored) != 0 {
		t.Fatalf("invalid bodies changed the findings: %v %v", checker.ignored, checker.unignored)
	}
}

func TestAutomationFindingsIgnoreSaveFailureIsServerError(t *testing.T) {
	checker := &fakeAutomationFindings{ignoreErr: errSaveFailed}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	recorder := doChainRequest(router, http.MethodPost, automationFindingsIgnorePath, `{"id":"frequent:a"}`)
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_save_failed"`) {
		t.Fatalf("save failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationFindingsIgnoreWithoutChecker(t *testing.T) {
	bare := NewRouter(registry.New(), nil, settings.NewStore(t.TempDir()))
	recorder := doChainRequest(bare, http.MethodPost, automationFindingsIgnorePath, `{"id":"frequent:a"}`)
	if recorder.Code != http.StatusNotImplemented || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_unavailable"`) {
		t.Fatalf("POST without checker: %d %s", recorder.Code, recorder.Body.String())
	}
}

const duplicateDraft = `{"rules":[{"id":"first","name":"Pump first","enabled":true,"conditions":[],"actions":[]},{"id":"second","name":"Pump second","enabled":true,"conditions":[],"actions":[]}]}`

func TestAutomationFindingsPreviewAnswersDraftFindings(t *testing.T) {
	checker := &fakeAutomationFindings{preview: []autolearn.Finding{{ID: "duplicate:first:second", Kind: "duplicate", RuleIDs: []string{"first", "second"}, Severity: "info"}}}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	recorder := doChainRequest(router, http.MethodPost, automationFindingsPreviewPath, duplicateDraft)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Findings []autolearn.Finding `json:"findings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Findings) != 1 || body.Findings[0].ID != "duplicate:first:second" {
		t.Fatalf("findings = %+v", body.Findings)
	}
	if checker.previewed != 1 {
		t.Fatalf("Preview called %d times, want 1", checker.previewed)
	}

	// No findings answer an empty list, never null.
	checker.preview = nil
	recorder = doChainRequest(router, http.MethodPost, automationFindingsPreviewPath, duplicateDraft)
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"findings":[]}` {
		t.Fatalf("empty answer = %s, want an empty list", got)
	}
}

func TestAutomationFindingsPreviewBrokenDocumentIs400(t *testing.T) {
	checker := &fakeAutomationFindings{}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	recorder := doChainRequest(router, http.MethodPost, automationFindingsPreviewPath, `{"rules":`)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_preview_invalid"`) {
		t.Fatalf("broken JSON: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationFindingsPreviewBodyLimit(t *testing.T) {
	checker := &fakeAutomationFindings{}
	router := NewRouterWithDependencies(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{AutomationFindings: checker})

	huge := `{"rules":[],"pad":"` + strings.Repeat("x", 1<<20) + `"}`
	recorder := doChainRequest(router, http.MethodPost, automationFindingsPreviewPath, huge)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d %s", recorder.Code, recorder.Body.String())
	}
	if checker.previewed != 0 {
		t.Fatalf("oversized body was previewed")
	}
}

func TestAutomationFindingsPreviewWithoutChecker(t *testing.T) {
	bare := NewRouter(registry.New(), nil, settings.NewStore(t.TempDir()))
	recorder := doChainRequest(bare, http.MethodPost, automationFindingsPreviewPath, duplicateDraft)
	if recorder.Code != http.StatusNotImplemented || !strings.Contains(recorder.Body.String(), `"code":"automation_findings_unavailable"`) {
		t.Fatalf("POST preview without checker: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomationFindingsPreviewNeedsViewRoleAndCSRF(t *testing.T) {
	manager, err := auth.NewManager(filepath.Join(t.TempDir(), "users.json"), "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGuestRoles("admin", []string{auth.RoleViewAutomations}); err != nil {
		t.Fatal(err)
	}
	checker := &fakeAutomationFindings{}
	router := NewAuthenticatedRouter(registry.New(), config.NewManager(t.TempDir()), settings.NewStore(t.TempDir()), nil, nil, nil, nil, nil, RouterDependencies{Auth: manager, AutomationFindings: checker})
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", nil))
	cookie := guest.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	// A view only user has no automations role but may preview, with CSRF.
	without := httptest.NewRequest(http.MethodPost, automationFindingsPreviewPath, strings.NewReader(duplicateDraft))
	without.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, without)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_failed"`) {
		t.Fatalf("POST preview without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
	if checker.previewed != 0 {
		t.Fatalf("previewed without CSRF")
	}

	with := httptest.NewRequest(http.MethodPost, automationFindingsPreviewPath, strings.NewReader(duplicateDraft))
	with.AddCookie(cookie)
	with.Header.Set("X-CSRF-Token", session.CSRFToken)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, with)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST preview with CSRF: %d %s", recorder.Code, recorder.Body.String())
	}
}
