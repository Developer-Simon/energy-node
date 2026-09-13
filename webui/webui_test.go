package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui"
)

func TestAssetVersionIsTheVersionFileWithoutWhitespace(t *testing.T) {
	got := webui.AssetVersion()
	if got == "" || strings.ContainsAny(got, " \t\r\n") {
		t.Fatalf("AssetVersion() = %q, want a trimmed non-empty version", got)
	}
	if !strings.HasPrefix(got, "v") {
		t.Errorf("AssetVersion() = %q, want a leading v", got)
	}
}

func TestStaticHandlerServesEmbeddedFilesWithADayOfCaching(t *testing.T) {
	handler := webui.StaticHandler("/assets/")
	req := httptest.NewRequest(http.MethodGet, "/assets/css/probe.css", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// probe.css existiert nicht - geprueft wird hier nur, dass der Handler
	// den Praefix abschneidet und ueberhaupt in die eingebettete FS greift.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a file that is not embedded", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400" {
		t.Errorf("Cache-Control = %q, want the dashboard's one-day policy", got)
	}
}

func TestCatalogsContainBothLanguages(t *testing.T) {
	for _, name := range []string{"de.json", "en.json"} {
		if _, err := webui.Catalogs().Open(name); err != nil {
			t.Errorf("Catalogs().Open(%q): %v", name, err)
		}
	}
}
