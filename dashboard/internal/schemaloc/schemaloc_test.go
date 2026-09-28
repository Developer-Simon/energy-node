package schemaloc

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const sample = `{"title":"Root","type":"object","properties":{
 "zeta":{"type":"number","title":"Zeta","minimum":0.98,"maximum":1e-3},
 "alpha":{"type":"string","description":"Alpha help"},
 "list":{"type":"array","items":{"type":"object","properties":{"x":{"title":"X"}}}}},
 "allOf":[{"if":{"properties":{"alpha":{"const":"a","title":"ignored"}}},
  "then":{"properties":{"beta":{"title":"Beta"}}},
  "else":{"properties":{"beta":{"title":"Beta"}}}}]}`

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := New(fstest.MapFS{
		"de.json": {Data: []byte(`{"schema.s.title":"Wurzel","schema.s.zeta.title":"Zeta DE","schema.s.list.items.x.title":"X DE","schema.s.beta.title":"Beta DE"}`)},
		"en.json": {Data: []byte(`{}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEntriesWalksBranchesAndRejectsConflicts(t *testing.T) {
	entries, err := Entries("s", []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Key)
	}
	want := "schema.s.title schema.s.zeta.title schema.s.alpha.description schema.s.list.items.x.title schema.s.beta.title"
	if got := strings.Join(keys, " "); got != want {
		t.Fatalf("keys = %s\nwant   %s", got, want)
	}
	conflict := `{"allOf":[{"then":{"properties":{"b":{"title":"One"}}},"else":{"properties":{"b":{"title":"Two"}}}}]}`
	if _, err := Entries("s", []byte(conflict)); err == nil || !strings.Contains(err.Error(), "schema.s.b.title") {
		t.Fatalf("conflict error = %v, want one naming schema.s.b.title", err)
	}
}

func TestLocalizeKeepsKeyOrderAndNumbers(t *testing.T) {
	out, err := testCatalog(t).Localize("s", "de", []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, part := range []string{`"title":"Wurzel"`, `"title":"Zeta DE"`, `"minimum":0.98`, `"maximum":1e-3`, `"title":"X DE"`, `"description":"Alpha help"`, `"title":"ignored"`} {
		if !strings.Contains(got, part) {
			t.Errorf("output lacks %s: %s", part, got)
		}
	}
	if strings.Index(got, `"zeta"`) > strings.Index(got, `"alpha"`) {
		t.Errorf("property order changed: %s", got)
	}
	if strings.Count(got, `"title":"Beta DE"`) != 2 {
		t.Errorf("both branches must carry the translated text: %s", got)
	}
}

func TestLocalizeFallsBackToSchemaText(t *testing.T) {
	c := testCatalog(t)
	for _, lang := range []string{"en", "fr"} {
		out, err := c.Localize("s", lang, []byte(sample))
		if err != nil || string(out) != sample {
			t.Fatalf("Localize(%s) must return the schema byte-identical, got err=%v", lang, err)
		}
	}
	out, err := c.Localize("other", "de", []byte(sample))
	if err != nil || !strings.Contains(string(out), `"title":"Root"`) {
		t.Fatalf("an unknown schema id keeps its texts: err=%v out=%s", err, out)
	}
	if _, err := c.Localize("s", "de", []byte(`{"title":`)); err == nil {
		t.Fatal("invalid JSON must report an error")
	}
}

func TestEveryUILanguageHasASchemaCatalog(t *testing.T) {
	entries, err := os.ReadDir("../webui/catalogs")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, lang := range Default.Languages() {
		have[lang] = true
	}
	for _, e := range entries {
		lang := strings.TrimSuffix(e.Name(), ".json")
		if !have[lang] {
			t.Errorf("webui/catalogs/%s has no schemaloc/catalogs/%s.json", e.Name(), lang)
		}
	}
}
