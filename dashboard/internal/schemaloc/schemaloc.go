// Package schemaloc translates the title and description texts of the JSON
// schemas the dashboard renders as forms. The English schema text is the
// source and the fallback, other languages come from catalogs/<lang>.json
// under schema.<schema-id>.<path>.<field>. The path joins property names
// with dots, an array element adds the segment "items", allOf/then/else add
// nothing and if is never entered.
package schemaloc

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"

	"github.com/Developer-Simon/energy-node-webui/i18n"
)

// SystemSchemaID names the central configuration schema (appconfig).
const SystemSchemaID = "system"

//go:embed catalogs/*.json
var embedded embed.FS

// Default holds the embedded catalogs.
var Default = mustDefault()

func mustDefault() *Catalog {
	sub, err := fs.Sub(embedded, "catalogs")
	if err != nil {
		panic(err)
	}
	catalog, err := New(sub)
	if err != nil {
		panic(err)
	}
	return catalog
}

// Entry is one schema text under its catalog key.
type Entry struct {
	Key  string
	Text string
}

// Catalog looks up schema texts per language.
type Catalog struct {
	set *i18n.Set
}

func New(fsys fs.FS) (*Catalog, error) {
	set, err := i18n.Load(fsys)
	if err != nil {
		return nil, err
	}
	return &Catalog{set: set}, nil
}

func (c *Catalog) Languages() []string { return c.set.Languages() }

// Lookup returns the catalog text of key in lang. English has no entries,
// the schema text is the English text.
func (c *Catalog) Lookup(lang, key string) (string, bool) {
	if lang == i18n.Fallback || !c.set.Has(lang) {
		return "", false
	}
	return c.set.Lookup(lang, key)
}

// Localize returns raw with every text that has an entry in lang replaced.
// Without a catalog for lang the schema comes back byte-identical.
func (c *Catalog) Localize(schemaID, lang string, raw []byte) ([]byte, error) {
	if lang == i18n.Fallback || !c.set.Has(lang) {
		return raw, nil
	}
	root, err := decode(raw)
	if err != nil {
		return nil, err
	}
	walk(root, "", func(obj *object, field, path string) {
		if text, ok := c.Lookup(lang, key(schemaID, path)); ok {
			obj.values[field] = text
		}
	})
	var buf bytes.Buffer
	if err := encode(&buf, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Entries lists every text of the schema under its key, in document order.
// The same key with two different texts is an error: one catalog entry
// could not serve both.
func Entries(schemaID string, raw []byte) ([]Entry, error) {
	root, err := decode(raw)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	seen := map[string]string{}
	var conflict error
	walk(root, "", func(obj *object, field, path string) {
		k, text := key(schemaID, path), obj.values[field].(string)
		if previous, ok := seen[k]; ok {
			if previous != text && conflict == nil {
				conflict = fmt.Errorf("schemaloc: %s has two different texts: %q and %q", k, previous, text)
			}
			return
		}
		seen[k] = text
		entries = append(entries, Entry{Key: k, Text: text})
	})
	return entries, conflict
}

func key(schemaID, path string) string { return "schema." + schemaID + "." + path }

// walk calls visit for every string title and description. path already
// ends in the field name ("mqtt.host.title").
func walk(node any, path string, visit func(obj *object, field, path string)) {
	obj, ok := node.(*object)
	if !ok {
		return
	}
	for _, field := range []string{"title", "description"} {
		if _, ok := obj.values[field].(string); ok {
			visit(obj, field, join(path, field))
		}
	}
	if properties, ok := obj.values["properties"].(*object); ok {
		for _, name := range properties.keys {
			walk(properties.values[name], join(path, name), visit)
		}
	}
	walk(obj.values["items"], join(path, "items"), visit)
	if all, ok := obj.values["allOf"].([]any); ok {
		for _, entry := range all {
			walk(entry, path, visit)
		}
	}
	walk(obj.values["then"], path, visit)
	walk(obj.values["else"], path, visit)
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
