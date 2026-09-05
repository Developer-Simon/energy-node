// Package basepath teaches the dashboard to live under a reverse-proxy
// subpath (https://ha.example/node/) without any configuration of its own:
// the prefix is resolved per request from the proxy's X-Forwarded-Prefix
// (or X-Ingress-Path for Home-Assistant add-on ingress) header.
//
// Middleware strips the prefix from the request path before the router sees
// it, so every mux pattern and every TrimPrefix in internal/httpapi keeps
// matching "/api/v1/..." unchanged. Only the *output* side - templates,
// generated JS URLs, cookie paths - prefixes itself via From/Join/CookiePath.
// Without the header the prefix is "" and every generated URL is
// byte-identical to the direct-access (http://host:8080/) case.
package basepath

import (
	"context"
	"net/http"
	"strings"
)

type contextKey struct{}

var basePathKey contextKey

// unreserved reports whether c may appear inside a path segment. The rule is
// deliberately narrow: the header is client-controllable, so everything that
// could break out of an HTML attribute, a JS string literal or a Set-Cookie
// path - quotes, angle brackets, backslashes, ":", control characters - is
// rejected. Spelled out as a byte test rather than a regexp because Normalize
// runs on every proxied request.
func unreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '~' || c == '-'
}

// Normalize turns a raw header value into either "" or a clean prefix such as
// "/node": leading slash enforced, trailing slash removed. Anything suspicious
// (protocol-relative "//host", "..", absolute URLs, unexpected characters)
// yields "" so the dashboard falls back to root-relative URLs.
func Normalize(raw string) string {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "/") {
		if value == "" {
			return ""
		}
		value = "/" + value
	}
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	if value == "" {
		return ""
	}
	// One pass over the segments: rejects unexpected bytes, and an empty
	// segment covers both "//evil.com" (protocol-relative) and "/a//b".
	start := 1
	for i := 1; i <= len(value); i++ {
		if i < len(value) && value[i] != '/' {
			if !unreserved(value[i]) {
				return ""
			}
			continue
		}
		switch value[start:i] {
		case "", ".", "..":
			return ""
		}
		start = i + 1
	}
	return value
}

// Middleware resolves the prefix for this request, removes it from the URL
// path (proxies that pass the prefix through, e.g. proxy_pass without a
// trailing slash) and stores it in the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("X-Ingress-Path")
		if strings.TrimSpace(raw) == "" {
			raw = r.Header.Get("X-Forwarded-Prefix")
		}
		base := Normalize(raw)
		if base == "" {
			next.ServeHTTP(w, r)
			return
		}
		// WithContext already shallow-copies the request, so the URL is the
		// only thing left to clone before rewriting its path - r itself must
		// stay untouched for anything upstream that still holds it.
		stripped := r.WithContext(context.WithValue(r.Context(), basePathKey, base))
		url := *r.URL
		url.Path = trimPrefixPath(url.Path, base)
		if url.RawPath != "" {
			url.RawPath = trimPrefixPath(url.RawPath, base)
		}
		stripped.URL = &url
		next.ServeHTTP(w, stripped)
	})
}

// trimPrefixPath removes base from path when path is base itself or a
// subpath of it, always leaving a path that starts with "/".
func trimPrefixPath(path, base string) string {
	if path == base {
		return "/"
	}
	// Spelled out instead of HasPrefix(path, base+"/") so the check does not
	// allocate a joined string on every proxied request.
	if len(path) <= len(base) || path[len(base)] != '/' || !strings.HasPrefix(path, base) {
		return path
	}
	return path[len(base):]
}

// From returns the prefix resolved for r, or "" for direct access.
func From(r *http.Request) string {
	if r == nil {
		return ""
	}
	return FromContext(r.Context())
}

// FromContext returns the prefix stored by Middleware, or "".
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	base, _ := ctx.Value(basePathKey).(string)
	return base
}

// Join prefixes an absolute in-app path. Empty base or a non-absolute path
// (an external http(s):// reference, for instance) is returned unchanged.
func Join(base, path string) string {
	if base == "" || !strings.HasPrefix(path, "/") {
		return path
	}
	return base + path
}

// CookiePath scopes a session cookie to the proxied subtree, so a dashboard
// under /node/ does not write its cookie for the whole proxy host.
func CookiePath(r *http.Request) string {
	base := From(r)
	if base == "" {
		return "/"
	}
	return base + "/"
}
