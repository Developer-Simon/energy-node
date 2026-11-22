package webui

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// summaryCodeLiteral matches the Text{Code: "..."} literals of the autolearn detectors. A literal
// that ends in "." is a prefix that gets a direction suffix (switch_time.on, switch_time.off), one
// that ends in "sun_" gets the sun direction (sun_on, sun_off) and the after and before variants.
var summaryCodeLiteral = regexp.MustCompile(`Text\{Code: "([a-z_.]+)"`)

// learnLineGaps lists the summary codes that have a title and a text but no .line key. They are
// rule findings from the checker, which the band does not draw as a tile. Each entry must stay
// without a .line key, the test fails when one gets its key, so the list is kept current.
var learnLineGaps = map[string]bool{
	"check.competition":        true,
	"check.contradiction":      true,
	"check.duplicate":          true,
	"check.feedback":           true,
	"check.frequent":           true,
	"check.never_fired":        true,
	"check.orphan":             true,
	"check.regulator_conflict": true,
	"check.seasonal_rest":      true,
	"check.unsatisfiable":      true,
}

// autolearnSummaryKeys returns the catalog base keys (automations.learn.<code>) of every summary
// code the detectors emit, sun variants already split into after and before.
func autolearnSummaryKeys(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "autolearn"))
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("..", "autolearn", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range summaryCodeLiteral.FindAllStringSubmatch(string(src), -1) {
			code := match[1]
			switch {
			case strings.HasSuffix(code, "sun_"):
				codes[code+"on.after"] = true
				codes[code+"on.before"] = true
				codes[code+"off.after"] = true
				codes[code+"off.before"] = true
			case strings.HasSuffix(code, "."):
				codes[code+"on"] = true
				codes[code+"off"] = true
			default:
				codes[code] = true
			}
		}
	}
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	return keys
}

func TestEveryAutolearnSummaryCodeHasTitleAndText(t *testing.T) {
	catalogs := readCatalogs(t)
	keys := autolearnSummaryKeys(t)
	if len(keys) < 10 {
		t.Fatalf("found only %d summary codes, the scan is broken", len(keys))
	}
	for _, lang := range []string{"de", "en"} {
		for _, code := range keys {
			for _, part := range []string{"title", "text"} {
				key := "automations.learn." + code + "." + part
				if strings.TrimSpace(catalogs[lang][key]) == "" {
					t.Errorf("%s.json lacks %q, the detector emits %s", lang, key, code)
				}
			}
		}
	}
}

func TestEveryBandSummaryCodeHasLine(t *testing.T) {
	catalogs := readCatalogs(t)
	for _, code := range autolearnSummaryKeys(t) {
		key := "automations.learn." + code + ".line"
		for _, lang := range []string{"de", "en"} {
			has := strings.TrimSpace(catalogs[lang][key]) != ""
			if learnLineGaps[code] {
				if has {
					t.Errorf("%s now has %q, remove %s from learnLineGaps", lang, key, code)
				}
				continue
			}
			if !has {
				t.Errorf("%s.json lacks %q, the band tile needs it", lang, key)
			}
		}
	}
}
