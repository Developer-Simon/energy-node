package localize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-webui/i18n"
)

// Translator renders catalog texts. It holds no per-request state, so one
// instance serves every request.
type Translator struct {
	set     *i18n.Set
	version string
}

// New loads every *.json in catalogs (file name = language code). The
// default language must have a catalog: a UI without texts is unusable.
func New(catalogs fs.FS) (*Translator, error) {
	set, err := i18n.Load(catalogs)
	if err != nil {
		return nil, err
	}
	if !set.Has(DefaultLanguage) {
		return nil, fmt.Errorf("localize: default language %q has no catalog", DefaultLanguage)
	}
	hash := sha256.New()
	for _, lang := range set.Languages() {
		// json.Marshal sorts map keys, so the digest is deterministic.
		data, err := json.Marshal(set.Merged(lang))
		if err != nil {
			return nil, err
		}
		hash.Write([]byte(lang))
		hash.Write(data)
	}
	return &Translator{set: set, version: hex.EncodeToString(hash.Sum(nil))[:12]}, nil
}

// Languages lists the available language codes, sorted.
func (t *Translator) Languages() []string { return t.set.Languages() }

// Version is a short digest of all catalogs. It goes into the ?v= of the
// catalog script so a text change busts the browser cache by itself.
func (t *Translator) Version() string { return t.version }

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// T returns the text for key in lang. pairs are name/value pairs for the
// {name} placeholders. A key missing in lang falls back to English, a key
// missing everywhere is shown as the key itself so gaps stay visible.
func (t *Translator) T(lang, key string, pairs ...any) string {
	text, ok := t.set.Lookup(lang, key)
	if !ok {
		return key
	}
	return fill(text, params(pairs))
}

// TN picks key+".one" for n == 1 and key+".other" otherwise; {n} is filled
// automatically.
func (t *Translator) TN(lang, key string, n int, pairs ...any) string {
	suffix := ".other"
	if n == 1 {
		suffix = ".one"
	}
	values := params(pairs)
	values["n"] = strconv.Itoa(n)
	text, ok := t.set.Lookup(lang, key+suffix)
	if !ok {
		return key + suffix
	}
	return fill(text, values)
}

// FuncMap binds t and tn to one language, so templates never need the
// request or the root context to translate.
func (t *Translator) FuncMap(lang string) template.FuncMap {
	return template.FuncMap{
		"t":  func(key string, pairs ...any) string { return t.T(lang, key, pairs...) },
		"tn": func(key string, n int, pairs ...any) string { return t.TN(lang, key, n, pairs...) },
	}
}

// Option is one entry of the language switcher. Short is the upper-case
// code the compact switcher shows ("DE"), Name the language's own name for
// screen readers and the settings page.
type Option struct {
	Code   string
	Short  string
	Name   string
	Active bool
}

// Options lists every language under its own name (catalog key
// meta.language_name), so a new catalog describes itself.
func (t *Translator) Options(active string) []Option {
	codes := t.set.Languages()
	options := make([]Option, 0, len(codes))
	for _, code := range codes {
		name, ok := t.set.Lookup(code, "meta.language_name")
		if !ok {
			name = code
		}
		options = append(options, Option{Code: code, Short: strings.ToUpper(code), Name: name, Active: code == active})
	}
	return options
}

// ScriptHandler serves <prefix>/<lang>.js: the merged catalog as a classic
// script that sets window.__I18N__. The page loads it as a blocking script
// in <head>, so window.I18n works from the first deferred script on. An
// unknown language gets the default catalog rather than an error.
func (t *Translator) ScriptHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lang := strings.TrimSuffix(path.Base(r.URL.Path), ".js")
		if !t.set.Has(lang) {
			lang = DefaultLanguage
		}
		payload, err := json.Marshal(struct {
			Lang    string       `json:"lang"`
			Catalog i18n.Catalog `json:"catalog"`
		}{Lang: lang, Catalog: t.set.Merged(lang)})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = fmt.Fprintf(w, "window.__I18N__=%s;\n", payload)
	})
}

func params(pairs []any) map[string]string {
	values := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values[fmt.Sprint(pairs[i])] = fmt.Sprint(pairs[i+1])
	}
	return values
}

func fill(text string, values map[string]string) string {
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		if value, ok := values[match[1:len(match)-1]]; ok {
			return value
		}
		return match
	})
}
