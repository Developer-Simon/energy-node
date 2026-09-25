// Package localize picks the UI language of a request and turns catalog
// keys into text for templates and the browser. Catalog loading, per-key
// fallback and the plural naming scheme belong to the installer's shared
// i18n package; this package adds what only the dashboard needs: language
// negotiation and the glue to html/template and to the browser runtime.
package localize

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	// CookieName is the cookie the language switcher writes.
	CookieName = "lang"
	// DefaultLanguage applies when neither the cookie nor Accept-Language
	// names an available language.
	DefaultLanguage = "de"
)

// Resolve picks the language for a request: the cookie, then the browser's
// Accept-Language, then DefaultLanguage. Only languages in available count.
func Resolve(r *http.Request, available []string) string {
	if cookie, err := r.Cookie(CookieName); err == nil && contains(available, cookie.Value) {
		return cookie.Value
	}
	for _, tag := range acceptedLanguages(r.Header.Get("Accept-Language")) {
		if contains(available, tag) {
			return tag
		}
	}
	return DefaultLanguage
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

type weighted struct {
	tag string
	q   float64
}

// acceptedLanguages returns the primary language subtags of an
// Accept-Language header, best first. Entries with q=0, a malformed q or the
// wildcard are dropped.
func acceptedLanguages(header string) []string {
	var entries []weighted
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, param := range fields[1:] {
			if value, ok := strings.CutPrefix(strings.TrimSpace(param), "q="); ok {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil {
					parsed = 0
				}
				q = parsed
			}
		}
		if q <= 0 {
			continue
		}
		if i := strings.IndexAny(tag, "-_"); i >= 0 {
			tag = tag[:i]
		}
		entries = append(entries, weighted{tag: tag, q: q})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].q > entries[j].q })
	tags := make([]string, len(entries))
	for i, entry := range entries {
		tags[i] = entry.tag
	}
	return tags
}
