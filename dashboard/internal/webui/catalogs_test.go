package webui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/storagehealth"
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

// battery-card-core.js falls back to its own German BUILTIN_TEXTS table when
// neither an injected t() nor window.I18n is available (a bare page). That
// fallback text must stay byte-identical to de.json, or the two hosts (the
// dashboard and Home Assistant without hass.language yet) would show
// different German wording for the same key.
var batteryCardTextPattern = regexp.MustCompile(`'(battery\.card\.[\w.]+)':\s*'([^']*)'`)

func TestBatteryCardCoreBuiltinTextsMatchGerman(t *testing.T) {
	data, err := fs.ReadFile(templateFS, "static/js/battery-card-core.js")
	if err != nil {
		t.Fatal(err)
	}
	de := readCatalogs(t)["de"]
	matches := batteryCardTextPattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no battery.card.* entries found in battery-card-core.js's BUILTIN_TEXTS")
	}
	for _, match := range matches {
		key, text := match[1], match[2]
		if de[key] != text {
			t.Errorf("battery-card-core.js BUILTIN_TEXTS[%q] = %q, de.json has %q", key, text, de[key])
		}
	}
}

// TestStorageHealthCatalogKeysAndTexts verifies that storagehealth Go
// functions provide catalog keys, and that the German text with params
// substituted matches the expected label.
func TestStorageHealthCatalogKeysAndTexts(t *testing.T) {
	de := readCatalogs(t)["de"]

	// Test lifeTime codes 1-11
	for code := 1; code <= 11; code++ {
		lifetime, valid := storagehealth.TestableLifeTime(code)
		if !valid {
			t.Fatalf("lifeTime code %d should be valid", code)
		}
		if lifetime.LabelKey == "" {
			t.Errorf("lifeTime code %d: LabelKey is empty", code)
			continue
		}
		if _, ok := de[lifetime.LabelKey]; !ok {
			t.Errorf("lifeTime code %d: key %q not found in de.json", code, lifetime.LabelKey)
			continue
		}
		// Substitute params and check text matches
		catalogText := de[lifetime.LabelKey]
		expectedLabel := substituteParams(catalogText, lifetime.LabelParams)
		if expectedLabel != lifetime.Label {
			t.Errorf("lifeTime code %d: expected label %q, got %q", code, lifetime.Label, expectedLabel)
		}
	}

	// Test preEOL codes 1-3
	for code := 1; code <= 3; code++ {
		preeol, valid := storagehealth.TestablePreEOL(code)
		if !valid {
			t.Fatalf("preEOL code %d should be valid", code)
		}
		if preeol.LabelKey == "" {
			t.Errorf("preEOL code %d: LabelKey is empty", code)
			continue
		}
		if _, ok := de[preeol.LabelKey]; !ok {
			t.Errorf("preEOL code %d: key %q not found in de.json", code, preeol.LabelKey)
			continue
		}
		// Substitute params and check text matches
		catalogText := de[preeol.LabelKey]
		expectedLabel := substituteParams(catalogText, preeol.LabelParams)
		if expectedLabel != preeol.Label {
			t.Errorf("preEOL code %d: expected label %q, got %q", code, preeol.Label, expectedLabel)
		}
	}

	// Test remainingLabel with sample values (days < 365 and >= 365)
	testCases := []struct {
		name    string
		minDays float64
		maxDays float64
	}{
		{"days", 100, 200},
		{"days large", 300, 400},
		{"years", 400, 900},
	}
	for _, tc := range testCases {
		result := storagehealth.TestableRemainingLabel(tc.minDays, tc.maxDays)
		if result.Key == "" {
			t.Errorf("remainingLabel(%v, %v): Key is empty", tc.minDays, tc.maxDays)
			continue
		}
		if _, ok := de[result.Key]; !ok {
			t.Errorf("remainingLabel(%v, %v): key %q not found in de.json", tc.minDays, tc.maxDays, result.Key)
			continue
		}
		catalogText := de[result.Key]
		expectedLabel := substituteParams(catalogText, result.Params)
		if expectedLabel != result.Label {
			t.Errorf("remainingLabel(%v, %v): expected %q, got %q", tc.minDays, tc.maxDays, result.Label, expectedLabel)
		}
	}

	// Test consumedPercent with sample values
	consumedTestCases := []struct {
		name              string
		hostWritesBytes   float64
		enduranceMinBytes float64
		enduranceMaxBytes float64
	}{
		{"normal", 500_000_000_000, 1_000_000_000_000, 3_000_000_000_000},
		{"zero", 0, 1_000_000_000_000, 3_000_000_000_000},
	}
	for _, tc := range consumedTestCases {
		_, _, label, key, params := storagehealth.TestableConsumedPercent(tc.hostWritesBytes, tc.enduranceMinBytes, tc.enduranceMaxBytes)
		if key == "" {
			// Empty key is valid for zero bytes
			if label != "" {
				t.Errorf("consumedPercent(%v, ...): expected empty label for empty key, got %q", tc.hostWritesBytes, label)
			}
			continue
		}
		if _, ok := de[key]; !ok {
			t.Errorf("consumedPercent(%v, ...): key %q not found in de.json", tc.hostWritesBytes, key)
			continue
		}
		catalogText := de[key]
		expectedLabel := substituteParams(catalogText, params)
		if expectedLabel != label {
			t.Errorf("consumedPercent(%v, ...): expected %q, got %q", tc.hostWritesBytes, label, expectedLabel)
		}
	}

	// Test no_mmc reason key
	if _, ok := de["storage_health.reason.no_mmc"]; !ok {
		t.Error("storage_health.reason.no_mmc key not found in de.json")
	}
}

// substituteParams replaces {key} placeholders with values from the params map.
func substituteParams(text string, params map[string]interface{}) string {
	for key, value := range params {
		placeholder := "{" + key + "}"
		switch v := value.(type) {
		case int:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%d", v))
		case float64:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%.1f", v))
		default:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%v", v))
		}
	}
	return text
}
