package basepath

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"root", "/", ""},
		{"plain", "/node", "/node"},
		{"trailing slash", "/node/", "/node"},
		{"missing leading slash", "node", "/node"},
		{"surrounding whitespace", "  /node/  ", "/node"},
		{"nested", "/ha/ingress/node", "/ha/ingress/node"},
		{"ingress token", "/api/hassio_ingress/abc_DEF-123", "/api/hassio_ingress/abc_DEF-123"},
		{"protocol relative", "//evil.com", ""},
		{"absolute url", "http://evil.com", ""},
		{"double quote", `/node"`, ""},
		{"single quote", "/node'", ""},
		{"angle bracket", "/node<script>", ""},
		{"backslash", `/node\evil`, ""},
		{"colon", "/node:8080", ""},
		{"newline", "/node\nX-Bad: 1", ""},
		{"dot dot", "/..", ""},
		{"dot dot inside", "/node/../etc", ""},
		{"single dot", "/./node", ""},
		{"empty segment", "/node//sub", ""},
		{"query", "/node?a=1", ""},
		{"space inside", "/no de", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.raw); got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// seen records what the wrapped handler actually observed, which is the whole
// point of the middleware: the router must keep seeing root-relative paths.
type seen struct {
	path string
	base string
}

func serve(t *testing.T, target string, header map[string]string) seen {
	t.Helper()
	var got seen
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{path: r.URL.Path, base: From(r)}
	}))
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for key, value := range header {
		request.Header.Set(key, value)
	}
	handler.ServeHTTP(httptest.NewRecorder(), request)
	return got
}

func TestMiddlewareWithoutHeaderIsAPassthrough(t *testing.T) {
	got := serve(t, "/api/v1/health", nil)
	if got.path != "/api/v1/health" || got.base != "" {
		t.Fatalf("got %+v, want path /api/v1/health and empty base", got)
	}
}

func TestMiddlewareStripsPrefixFromPath(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   string
	}{
		// proxy_pass without a trailing slash forwards the prefix.
		{"prefixed subpath", "/node/api/v1/health", "/api/v1/health"},
		{"prefix only", "/node", "/"},
		{"prefix with slash", "/node/", "/"},
		// proxy_pass *with* a trailing slash already removed it.
		{"already stripped", "/api/v1/health", "/api/v1/health"},
		// a path that merely starts with the same characters is not a subpath.
		{"lookalike sibling", "/nodes/api", "/nodes/api"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serve(t, tc.target, map[string]string{"X-Forwarded-Prefix": "/node"})
			if got.path != tc.want {
				t.Fatalf("path = %q, want %q", got.path, tc.want)
			}
			if got.base != "/node" {
				t.Fatalf("base = %q, want /node", got.base)
			}
		})
	}
}

func TestMiddlewareStripsEscapedPath(t *testing.T) {
	got := serve(t, "/node/api/v1/devices/a%2Fb", map[string]string{"X-Forwarded-Prefix": "/node"})
	if got.path != "/api/v1/devices/a/b" {
		t.Fatalf("path = %q, want /api/v1/devices/a/b", got.path)
	}
}

func TestMiddlewarePrefersIngressHeader(t *testing.T) {
	got := serve(t, "/", map[string]string{
		"X-Ingress-Path":     "/api/hassio_ingress/tok3n",
		"X-Forwarded-Prefix": "/node",
	})
	if got.base != "/api/hassio_ingress/tok3n" {
		t.Fatalf("base = %q, want the ingress path", got.base)
	}
}

func TestMiddlewareRejectsHostileHeader(t *testing.T) {
	got := serve(t, "/api/v1/health", map[string]string{"X-Forwarded-Prefix": "//evil.com"})
	if got.base != "" || got.path != "/api/v1/health" {
		t.Fatalf("got %+v, want an empty base and an untouched path", got)
	}
}

func TestMiddlewareDoesNotLeakIntoTheNextRequest(t *testing.T) {
	var bases []string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bases = append(bases, From(r))
	}))
	prefixed := httptest.NewRequest(http.MethodGet, "/node/", nil)
	prefixed.Header.Set("X-Forwarded-Prefix", "/node")
	handler.ServeHTTP(httptest.NewRecorder(), prefixed)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if len(bases) != 2 || bases[0] != "/node" || bases[1] != "" {
		t.Fatalf("bases = %q, want [/node \"\"]", bases)
	}
}

func TestFromWithoutMiddleware(t *testing.T) {
	if got := From(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("From = %q, want empty", got)
	}
	if got := From(nil); got != "" {
		t.Fatalf("From(nil) = %q, want empty", got)
	}
}

func TestJoin(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"", "/static/css/base.css", "/static/css/base.css"},
		{"/node", "/static/css/base.css", "/node/static/css/base.css"},
		{"/node", "https://example.test/x", "https://example.test/x"},
		{"/node", "relative.css", "relative.css"},
	}
	for _, tc := range cases {
		if got := Join(tc.base, tc.path); got != tc.want {
			t.Fatalf("Join(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestCookiePath(t *testing.T) {
	if got := CookiePath(httptest.NewRequest(http.MethodGet, "/", nil)); got != "/" {
		t.Fatalf("CookiePath without prefix = %q, want /", got)
	}
	var got string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = CookiePath(r)
	}))
	request := httptest.NewRequest(http.MethodGet, "/node/", nil)
	request.Header.Set("X-Forwarded-Prefix", "/node")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got != "/node/" {
		t.Fatalf("CookiePath = %q, want /node/", got)
	}
}
