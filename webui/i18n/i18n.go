// Package i18n haelt die Nachrichtenkataloge der Oberflaeche. Es uebersetzt
// nichts von sich aus: es laedt Kataloge, mischt sie je Schluessel ueber die
// Rueckfallsprache und liefert die fertige Karte an Schicht 3. Uebersetzt wird
// ausschliesslich im Browser - der SSE-Strom bleibt sprachneutral.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Fallback ist die Sprache, aus der jeder fehlende Schluessel bedient wird.
const Fallback = "en"

// Catalog ist eine flache Zuordnung von Schluessel auf Text. Flach, weil
// Schicht 3 sie unveraendert als JSON bekommt und ein data-i18n-Attribut
// genau einen Schluessel traegt.
type Catalog map[string]string

// Set haelt alle geladenen Kataloge.
type Set struct {
	catalogs map[string]Catalog
}

// Load liest jede *.json-Datei des uebergebenen FS als Katalog; der Dateiname
// ohne Endung ist das Sprachkuerzel.
func Load(fsys fs.FS) (*Set, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("i18n: Katalogverzeichnis nicht lesbar: %w", err)
	}
	set := &Set{catalogs: map[string]Catalog{}}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || path.Ext(name) != ".json" {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("i18n: %s nicht lesbar: %w", name, err)
		}
		catalog := Catalog{}
		if err := json.Unmarshal(data, &catalog); err != nil {
			return nil, fmt.Errorf("i18n: %s ist kein flaches JSON-Objekt: %w", name, err)
		}
		set.catalogs[strings.TrimSuffix(name, ".json")] = catalog
	}
	if len(set.catalogs) == 0 {
		return nil, fmt.Errorf("i18n: kein Katalog gefunden")
	}
	if _, ok := set.catalogs[Fallback]; !ok {
		return nil, fmt.Errorf("i18n: die Rueckfallsprache %q fehlt", Fallback)
	}
	return set, nil
}

// Languages liefert alle geladenen Sprachkuerzel, sortiert.
func (s *Set) Languages() []string {
	out := make([]string, 0, len(s.catalogs))
	for lang := range s.catalogs {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// Has meldet, ob es fuer eine Sprache einen eigenen Katalog gibt.
func (s *Set) Has(lang string) bool {
	_, ok := s.catalogs[lang]
	return ok
}

// Lookup schlaegt einen einzelnen Schluessel nach und faellt je Schluessel auf
// die Rueckfallsprache zurueck.
func (s *Set) Lookup(lang, key string) (string, bool) {
	if catalog, ok := s.catalogs[lang]; ok {
		if text, ok := catalog[key]; ok {
			return text, true
		}
	}
	text, ok := s.catalogs[Fallback][key]
	return text, ok
}

// Merged liefert den Katalog der Sprache ueber der Rueckfallsprache, als
// frische Karte - der Aufrufer darf sie veraendern.
func (s *Set) Merged(lang string) Catalog {
	out := Catalog{}
	for key, text := range s.catalogs[Fallback] {
		out[key] = text
	}
	if lang != Fallback {
		for key, text := range s.catalogs[lang] {
			out[key] = text
		}
	}
	return out
}

// Keys liefert die Schluessel eines einzelnen Katalogs, sortiert, ohne
// Rueckfall. Damit pruefen die Drift-Tests, ob eine Uebersetzung vollstaendig
// ist.
func (s *Set) Keys(lang string) []string {
	out := make([]string, 0, len(s.catalogs[lang]))
	for key := range s.catalogs[lang] {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Preferred waehlt aus einer OS-Locale ("de_DE.UTF-8", "de-AT", "C") die
// Sprache, fuer die ein Katalog existiert - sonst die Rueckfallsprache.
func Preferred(locale string, available []string) string {
	lang := locale
	for _, sep := range []string{".", "_", "-", "@"} {
		if i := strings.Index(lang, sep); i >= 0 {
			lang = lang[:i]
		}
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	for _, candidate := range available {
		if candidate == lang {
			return lang
		}
	}
	return Fallback
}
