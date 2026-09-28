package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// The code is the third argument of every error writer.
	errorWriterPattern = regexp.MustCompile(`\b(?:writeError|writeErrorKey|writeErrorDetail|writeTinyTuyaError)\(\w+,\s*[^,]+,\s*([^,]+),`)
	// requireRole(w, r, manager, role, code, key, message)
	requireRolePattern = regexp.MustCompile(`\brequireRole\(\w+,\s*\w+,\s*[^,]+,\s*[^,]+,\s*([^,]+),\s*([^,]+),`)
	errorKeyPattern    = regexp.MustCompile(`"(error\.[a-z0-9_.]+)"`)
	// A catalog key followed by its German fallback: writeErrorKey(..., key, nil, "text")
	// and requireRole(..., key, "text").
	keyWithTextPattern = regexp.MustCompile(`"(error\.[a-z0-9_.]+)",\s*(?:nil,\s*)?"([^"]*)"\)`)
)

// Codes that reach a writer through a variable. requireRole's own
// parameter is checked at its call sites; bridgeApplyErrorCode returns these.
var dynamicErrorCodes = map[string][]string{
	"code":                           nil,
	"bridgeApplyErrorCode(applyErr)": {"bridge_restart_failed", "bridge_helper_failed"},
}

func loadCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "webui", "catalogs", lang+".json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]string{}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestEveryAPIErrorHasACatalogText(t *testing.T) {
	de, en := loadCatalog(t, "de"), loadCatalog(t, "en")
	files, _ := filepath.Glob("*.go")
	codes := map[string]string{} // code -> first position
	keys := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		addCode := func(arg string) {
			arg = strings.TrimSpace(arg)
			if strings.HasPrefix(arg, `"`) {
				codes[strings.Trim(arg, `"`)] = file
				return
			}
			known, ok := dynamicErrorCodes[arg]
			if !ok {
				t.Errorf("%s: error code %s is not a literal, add it to dynamicErrorCodes", file, arg)
			}
			for _, code := range known {
				codes[code] = file
			}
		}
		for _, match := range errorWriterPattern.FindAllStringSubmatch(source, -1) {
			addCode(match[1])
		}
		for _, match := range requireRolePattern.FindAllStringSubmatch(source, -1) {
			addCode(match[1])
			if !strings.HasPrefix(strings.TrimSpace(match[2]), `"error.`) {
				t.Errorf("%s: requireRole key %s must be a literal error.* key", file, match[2])
			}
		}
		for _, match := range errorKeyPattern.FindAllStringSubmatch(source, -1) {
			keys[match[1]] = file
		}
		for _, match := range keyWithTextPattern.FindAllStringSubmatch(source, -1) {
			if de[match[1]] != match[2] {
				t.Errorf("%s: de[%s] = %q, Go fallback is %q (E8: keep them identical)", file, match[1], de[match[1]], match[2])
			}
		}
	}
	if len(codes) < 100 {
		t.Fatalf("found only %d codes, the scanner is broken", len(codes))
	}
	for code, file := range codes {
		keys["error."+code] = file
	}
	keys["error.with_detail"] = "errors.go"
	for key, file := range keys {
		if de[key] == "" || en[key] == "" {
			t.Errorf("%s: %s is missing in de or en", file, key)
		}
	}
}
