package diagnostics

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
)

func TestRuleTextsMatchTheGermanCatalog(t *testing.T) {
	data, err := os.ReadFile("../webui/catalogs/de.json")
	if err != nil {
		t.Fatal(err)
	}
	de := map[string]string{}
	if err := json.Unmarshal(data, &de); err != nil {
		t.Fatal(err)
	}
	for key, text := range ruleTexts {
		for part, want := range map[string]string{"message": text.Message, "hint": text.Hint} {
			name := "diagnostics.rule." + key + "." + part
			if de[name] != want {
				t.Errorf("de[%s] = %q, Go has %q", name, de[name], want)
			}
		}
	}
}

var (
	// warning(rule.RuleID(), "key", ...) and warningWithDetails(...)
	helperKeyPattern = regexp.MustCompile(`\bwarning(?:WithDetails)?\([^,]+,\s*"([a-z_]+)"`)
	// Warning{... Key: "key", ...} for the rules that build a Warning directly
	literalKeyPattern  = regexp.MustCompile(`\bKey:\s*"([a-z_]+)"`)
	literalTextPattern = regexp.MustCompile(`\b(?:Message|Hint):\s*"`)
)

func TestRulesUseExactlyTheKnownTexts(t *testing.T) {
	data, err := os.ReadFile("diagnostics.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	used := map[string]bool{}
	for _, pattern := range []*regexp.Regexp{helperKeyPattern, literalKeyPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			used[match[1]] = true
			if _, ok := ruleTexts[match[1]]; !ok {
				t.Errorf("diagnostics.go uses key %q that ruleTexts does not define", match[1])
			}
		}
	}
	for key := range ruleTexts {
		if !used[key] {
			t.Errorf("ruleTexts[%q] is not used by any rule", key)
		}
	}
	if literalTextPattern.MatchString(source) {
		t.Error("diagnostics.go sets Message or Hint from a literal, take it from ruleTexts")
	}
}

func TestWarningCarriesKeyAndTexts(t *testing.T) {
	device := registry.DeviceView{ID: "node", Entities: []registry.EntityView{{UniqueID: "temp", Component: "sensor"}}}
	warnings := MissingTopicRule{}.Evaluate(device, Context{Now: time.Now().UTC()})
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings", len(warnings))
	}
	got := warnings[0]
	if got.Key != "missing_topic" || got.Message != ruleTexts["missing_topic"].Message || got.Hint != ruleTexts["missing_topic"].Hint {
		t.Fatalf("got %+v", got)
	}
}
