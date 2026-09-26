package webui

import (
	"encoding/json"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func readCatalogs(t *testing.T) map[string]map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(catalogFS, ".")
	if err != nil {
		t.Fatal(err)
	}
	catalogs := map[string]map[string]string{}
	for _, entry := range entries {
		data, err := fs.ReadFile(catalogFS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		catalog := map[string]string{}
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatalf("%s is not a flat JSON object: %v", entry.Name(), err)
		}
		catalogs[strings.TrimSuffix(entry.Name(), ".json")] = catalog
	}
	if catalogs["de"] == nil || catalogs["en"] == nil {
		t.Fatalf("de and en catalogs are required, found %v", len(catalogs))
	}
	return catalogs
}

var placeholderPattern = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(text string) string {
	var names []string
	for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		names = append(names, match[1])
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// German is the reference: every other catalog must carry exactly its keys,
// and each text exactly its {placeholders}.
func TestCatalogsCoverTheSameKeysAndPlaceholders(t *testing.T) {
	catalogs := readCatalogs(t)
	reference := catalogs["de"]
	for lang, catalog := range catalogs {
		for key, text := range reference {
			other, ok := catalog[key]
			if !ok {
				t.Errorf("%s.json misses key %q", lang, key)
				continue
			}
			if placeholders(text) != placeholders(other) {
				t.Errorf("%s.json key %q has placeholders {%s}, de has {%s}", lang, key, placeholders(other), placeholders(text))
			}
			if strings.TrimSpace(other) == "" {
				t.Errorf("%s.json key %q is empty", lang, key)
			}
		}
		for key := range catalog {
			if _, ok := reference[key]; !ok {
				t.Errorf("%s.json has key %q that de.json lacks", lang, key)
			}
		}
	}
}

func TestCatalogPluralsComeInPairs(t *testing.T) {
	for lang, catalog := range readCatalogs(t) {
		for key := range catalog {
			if base, ok := strings.CutSuffix(key, ".one"); ok {
				if _, ok := catalog[base+".other"]; !ok {
					t.Errorf("%s.json has %q without %q", lang, key, base+".other")
				}
			}
			if base, ok := strings.CutSuffix(key, ".other"); ok {
				if _, ok := catalog[base+".one"]; !ok {
					t.Errorf("%s.json has %q without %q", lang, key, base+".one")
				}
			}
		}
	}
}

var (
	// {{t "key" ...}} and {{tn "key" n}} in templates.
	templateKeyPattern = regexp.MustCompile(`\{\{-?\s*(tn?)\s+"([^"]+)"`)
	// $t('key'), $tn('key'), I18n.t('key') and a bare t('key') in JS and in
	// Alpine attributes. A preceding "." or word character excludes
	// unrelated methods like foo.t('x').
	jsKeyPattern = regexp.MustCompile(`(?:\$|\bI18n\.|(?:^|[^\w$.]))(tn?)\('([^']+)'`)
	// A comment "i18n-keys: a.b, c.d" declares keys a dynamic lookup builds.
	declaredKeysPattern = regexp.MustCompile(`i18n-keys:[ \t]*([\w.,\t -]+)`)
)

type usedKey struct {
	name   string
	plural bool
}

func extractKeys(src string) []usedKey {
	var keys []usedKey
	for _, match := range templateKeyPattern.FindAllStringSubmatch(src, -1) {
		keys = append(keys, usedKey{name: match[2], plural: match[1] == "tn"})
	}
	for _, match := range jsKeyPattern.FindAllStringSubmatch(src, -1) {
		keys = append(keys, usedKey{name: match[2], plural: match[1] == "tn"})
	}
	for _, match := range declaredKeysPattern.FindAllStringSubmatch(src, -1) {
		for _, name := range strings.FieldsFunc(match[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			keys = append(keys, usedKey{name: name})
		}
	}
	return keys
}

func TestKeyScannerRecognisesEveryCallForm(t *testing.T) {
	src := `<p>{{t "a.b"}}</p> {{- tn "c.d" .N}} <i x-text="$t('e.f', {x: 1})"></i>
	const s = I18n.t('g.h'); const u = t('i.j'); foo.t('not.this'); nott('nor.this');
	label = I18n.tn('k.l', 2); // i18n-keys: m.n, o.p
	`
	got := map[string]bool{}
	for _, key := range extractKeys(src) {
		got[key.name] = key.plural
	}
	want := map[string]bool{"a.b": false, "c.d": true, "e.f": false, "g.h": false, "i.j": false, "k.l": true, "m.n": false, "o.p": false}
	if len(got) != len(want) {
		t.Fatalf("scanner found %v, want %v", got, want)
	}
	for name, plural := range want {
		if p, ok := got[name]; !ok || p != plural {
			t.Fatalf("scanner found %v, want %v", got, want)
		}
	}
}

// pendingEnglishPrefix marks an en.json text that the extraction copied
// from German and that still needs its English wording (localization A3).
const pendingEnglishPrefix = "TODO(en): "

func TestEnglishCatalogHasNoPendingTexts(t *testing.T) {
	if os.Getenv("I18N_ALLOW_PENDING_EN") == "1" {
		t.Skip("I18N_ALLOW_PENDING_EN=1: extraction in progress")
	}
	for key, text := range readCatalogs(t)["en"] {
		if strings.HasPrefix(text, pendingEnglishPrefix) {
			t.Errorf("en.json key %q still waits for its English text", key)
		}
	}
}

// Sorted catalogs keep diffs of parallel migrations small.
func TestCatalogFilesAreSorted(t *testing.T) {
	entries, err := fs.ReadDir(catalogFS, ".")
	if err != nil {
		t.Fatal(err)
	}
	keyLine := regexp.MustCompile(`^\s*"([^"]+)":`)
	for _, entry := range entries {
		data, err := fs.ReadFile(catalogFS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		previous := ""
		for n, line := range strings.Split(string(data), "\n") {
			m := keyLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if m[1] < previous {
				t.Errorf("%s:%d: key %q is not sorted after %q", entry.Name(), n+1, m[1], previous)
			}
			previous = m[1]
		}
	}
}

func TestEveryLiteralKeyExistsInTheGermanCatalog(t *testing.T) {
	de := readCatalogs(t)["de"]
	check := func(file string, key usedKey) {
		names := []string{key.name}
		if key.plural {
			names = []string{key.name + ".one", key.name + ".other"}
		}
		for _, name := range names {
			if _, ok := de[name]; !ok {
				t.Errorf("%s uses key %q, which de.json lacks", file, name)
			}
		}
	}
	for _, dir := range []string{"templates", "static/js"} {
		entries, err := fs.ReadDir(templateFS, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := fs.ReadFile(templateFS, dir+"/"+entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range extractKeys(string(data)) {
				check(dir+"/"+entry.Name(), key)
			}
		}
	}
}
