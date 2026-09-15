package i18n_test

import (
	"testing"
	"testing/fstest"

	"github.com/Developer-Simon/energy-node-webui/i18n"
)

func testSet(t *testing.T) *i18n.Set {
	t.Helper()
	set, err := i18n.Load(fstest.MapFS{
		"en.json": {Data: []byte(`{"app.title":"Set up node","step.10":"System packages"}`)},
		"de.json": {Data: []byte(`{"app.title":"Node einrichten"}`)},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return set
}

func TestLoadReadsEveryCatalogInTheDirectory(t *testing.T) {
	set := testSet(t)
	got := set.Languages()
	if len(got) != 2 || got[0] != "de" || got[1] != "en" {
		t.Fatalf("Languages() = %v, want [de en]", got)
	}
	if !set.Has("de") || set.Has("fr") {
		t.Errorf("Has() reported the wrong languages")
	}
}

func TestLookupFallsBackPerKeyNotPerCatalog(t *testing.T) {
	set := testSet(t)
	if got, ok := set.Lookup("de", "app.title"); !ok || got != "Node einrichten" {
		t.Errorf(`Lookup("de","app.title") = %q,%v; want the German text`, got, ok)
	}
	// step.10 fehlt im deutschen Katalog - der englische Text muss kommen,
	// nicht ein leerer String und nicht der Schluessel.
	if got, ok := set.Lookup("de", "step.10"); !ok || got != "System packages" {
		t.Errorf(`Lookup("de","step.10") = %q,%v; want the English fallback`, got, ok)
	}
	if _, ok := set.Lookup("de", "does.not.exist"); ok {
		t.Errorf("an unknown key must report ok=false")
	}
}

func TestMergedLaysTheActiveLanguageOverEnglish(t *testing.T) {
	set := testSet(t)
	merged := set.Merged("de")
	if merged["app.title"] != "Node einrichten" {
		t.Errorf("app.title = %q, want the German text", merged["app.title"])
	}
	if merged["step.10"] != "System packages" {
		t.Errorf("step.10 = %q, want the English fallback", merged["step.10"])
	}
	// Merged darf den Katalog des Sets nicht veraendern.
	merged["app.title"] = "geaendert"
	if again := set.Merged("de"); again["app.title"] != "Node einrichten" {
		t.Errorf("Merged returned a shared map; the caller mutated the catalog")
	}
}

func TestMergedForAnUnknownLanguageIsPlainEnglish(t *testing.T) {
	set := testSet(t)
	if got := set.Merged("fr")["app.title"]; got != "Set up node" {
		t.Errorf("Merged(fr)[app.title] = %q, want the English text", got)
	}
}

func TestPreferredPicksTheLanguagePartOfTheLocale(t *testing.T) {
	available := []string{"de", "en"}
	cases := map[string]string{
		"de_DE.UTF-8": "de",
		"de-AT":       "de",
		"en_US.UTF-8": "en",
		"fr_FR.UTF-8": "en", // keine Uebersetzung vorhanden -> Rueckfallsprache
		"":            "en",
		"C":           "en",
	}
	for locale, want := range cases {
		if got := i18n.Preferred(locale, available); got != want {
			t.Errorf("Preferred(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestLoadRejectsAMalformedCatalog(t *testing.T) {
	_, err := i18n.Load(fstest.MapFS{
		"en.json": {Data: []byte(`{"a":`)},
	})
	if err == nil {
		t.Fatalf("Load accepted a truncated catalog")
	}
}
