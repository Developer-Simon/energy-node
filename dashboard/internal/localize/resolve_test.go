package localize

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolve(t *testing.T) {
	available := []string{"de", "en"}
	tests := []struct {
		name   string
		cookie string
		accept string
		want   string
	}{
		{"nothing set falls back to the default", "", "", "de"},
		{"cookie wins", "en", "", "en"},
		{"cookie beats Accept-Language", "en", "de", "en"},
		{"unknown cookie is ignored", "xx", "en", "en"},
		{"Accept-Language primary subtag", "", "en-GB,en;q=0.9,de;q=0.8", "en"},
		{"q-values order the candidates", "", "de;q=0.5,en;q=0.9", "en"},
		{"unsupported language is skipped", "", "fr,de;q=0.5", "de"},
		{"only unsupported languages use the default", "", "fr", "de"},
		{"q=0 excludes a language", "", "en;q=0,de", "de"},
		{"wildcard is ignored", "", "*", "de"},
		{"malformed q excludes the entry", "", "en;q=abc,de", "de"},
		{"underscore separator", "", "en_US", "en"},
		{"case is ignored", "", "EN", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != "" {
				request.AddCookie(&http.Cookie{Name: CookieName, Value: tt.cookie})
			}
			if tt.accept != "" {
				request.Header.Set("Accept-Language", tt.accept)
			}
			if got := Resolve(request, available); got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}
