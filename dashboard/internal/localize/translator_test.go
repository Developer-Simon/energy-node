package localize

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testCatalogs(deExtra string) fstest.MapFS {
	return fstest.MapFS{
		"de.json": {Data: []byte(`{"greet":"Hallo {name}","file.one":"{n} Datei","file.other":"{n} Dateien","only_de":"nur deutsch","meta.language_name":"Deutsch"` + deExtra + `}`)},
		"en.json": {Data: []byte(`{"greet":"Hello {name}","file.one":"{n} file","file.other":"{n} files","only_en":"english only","meta.language_name":"English"}`)},
	}
}

func testTranslator(t *testing.T) *Translator {
	t.Helper()
	translator, err := New(testCatalogs(""))
	if err != nil {
		t.Fatal(err)
	}
	return translator
}

func TestTFillsPlaceholders(t *testing.T) {
	translator := testTranslator(t)
	if got := translator.T("de", "greet", "name", "Sam"); got != "Hallo Sam" {
		t.Fatalf("T = %q", got)
	}
	if got := translator.T("en", "greet", "name", 7); got != "Hello 7" {
		t.Fatalf("T with a non-string value = %q", got)
	}
	if got := translator.T("en", "greet"); got != "Hello {name}" {
		t.Fatalf("a placeholder without a value must stay visible, got %q", got)
	}
	if got := translator.T("en", "greet", "name"); got != "Hello {name}" {
		t.Fatalf("an unpaired trailing key must be ignored, got %q", got)
	}
}

func TestTFallsBackPerKeyThenToTheKey(t *testing.T) {
	translator := testTranslator(t)
	if got := translator.T("de", "only_en"); got != "english only" {
		t.Fatalf("a key missing in de must come from en, got %q", got)
	}
	if got := translator.T("en", "only_de"); got != "only_de" {
		t.Fatalf("a key missing everywhere must show the key, got %q", got)
	}
	if got := translator.T("xx", "greet", "name", "Sam"); got != "Hello Sam" {
		t.Fatalf("an unknown language must use en, got %q", got)
	}
}

func TestTNPicksOneAndOther(t *testing.T) {
	translator := testTranslator(t)
	for n, want := range map[int]string{0: "0 Dateien", 1: "1 Datei", 2: "2 Dateien"} {
		if got := translator.TN("de", "file", n); got != want {
			t.Fatalf("TN(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFuncMapWorksInHTMLTemplates(t *testing.T) {
	translator := testTranslator(t)
	tmpl := template.Must(template.New("x").Funcs(translator.FuncMap("en")).Parse(
		`{{t "greet" "name" "Sam"}}|{{tn "file" 1}}|{{tn "file" 3}}`))
	var out strings.Builder
	if err := tmpl.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Hello Sam|1 file|3 files" {
		t.Fatalf("rendered %q", out.String())
	}
}

func TestOptionsListEveryLanguageWithItsOwnName(t *testing.T) {
	options := testTranslator(t).Options("en")
	want := []Option{{Code: "de", Short: "DE", Name: "Deutsch"}, {Code: "en", Short: "EN", Name: "English", Active: true}}
	if len(options) != len(want) {
		t.Fatalf("options = %+v", options)
	}
	for i := range want {
		if options[i] != want[i] {
			t.Fatalf("options[%d] = %+v, want %+v", i, options[i], want[i])
		}
	}
}

func TestVersionTracksCatalogContent(t *testing.T) {
	first := testTranslator(t)
	second := testTranslator(t)
	if first.Version() != second.Version() || len(first.Version()) != 12 {
		t.Fatalf("version must be stable and 12 characters: %q vs %q", first.Version(), second.Version())
	}
	changed, err := New(testCatalogs(`,"extra":"x"`))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Version() == first.Version() {
		t.Fatal("version must change when a catalog changes")
	}
}

func TestNewRequiresTheDefaultLanguage(t *testing.T) {
	_, err := New(fstest.MapFS{"en.json": {Data: []byte(`{"a":"b"}`)}})
	if err == nil || !strings.Contains(err.Error(), `"de"`) {
		t.Fatalf("expected an error naming the default language, got %v", err)
	}
}

func TestScriptHandlerServesTheMergedCatalog(t *testing.T) {
	handler := testTranslator(t).ScriptHandler()
	request := httptest.NewRequest(http.MethodGet, "/i18n/en.js", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.HasPrefix(body, "window.__I18N__=") {
		t.Fatalf("status %d body %q", recorder.Code, body)
	}
	for _, want := range []string{`"lang":"en"`, `"greet":"Hello {name}"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %s: %s", want, body)
		}
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/javascript") {
		t.Fatalf("Content-Type = %q", got)
	}
	if recorder.Header().Get("Cache-Control") == "" {
		t.Fatal("the catalog script must be cacheable")
	}
}

func TestScriptHandlerUnknownLanguageServesTheDefault(t *testing.T) {
	recorder := httptest.NewRecorder()
	testTranslator(t).ScriptHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/i18n/../xx.js", nil))
	if !strings.Contains(recorder.Body.String(), `"lang":"de"`) {
		t.Fatalf("unknown language must fall back to de: %s", recorder.Body.String())
	}
}

func TestScriptHandlerRejectsWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	testTranslator(t).ScriptHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/i18n/en.js", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", recorder.Code)
	}
}
