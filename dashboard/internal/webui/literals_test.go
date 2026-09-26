package webui

import (
	"html"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Guard against UI text that bypasses the catalogs (localization A3).
// Templates are checked strictly: every visible text node and every visible
// attribute must come from {{t}} unless it consists of allowed tokens only.
// Scripts and Alpine expressions can only be checked heuristically, for
// German features (umlauts, a word list). A line with "i18n-ignore" is
// skipped. Files still waiting for their migration are listed in
// testdata/i18n-pending.txt.

type literal struct {
	line int
	text string
}

// jsStringLiterals returns the string literals of a script with the line
// they start on. Template literals contribute their text parts, ${...} is
// scanned as code. Comments are skipped. Regular expression literals are not
// recognised, which the dashboard's scripts do not need.
func jsStringLiterals(src string) []literal {
	const (
		code = iota
		single
		double
		backtick
		lineComment
		blockComment
	)
	var out []literal
	var buf strings.Builder
	var resume []int // mode to return to when a ${...} closes
	var depth []int  // open braces inside each ${...}
	mode, line, start := code, 1, 1
	flush := func() {
		out = append(out, literal{line: start, text: buf.String()})
		buf.Reset()
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\n' {
			line++
		}
		switch mode {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				mode = lineComment
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				mode = blockComment
				i++
			case c == '\'':
				mode, start = single, line
			case c == '"':
				mode, start = double, line
			case c == '`':
				mode, start = backtick, line
			case c == '{' && len(depth) > 0:
				depth[len(depth)-1]++
			case c == '}' && len(depth) > 0:
				if depth[len(depth)-1] > 0 {
					depth[len(depth)-1]--
					break
				}
				depth = depth[:len(depth)-1]
				mode, start = resume[len(resume)-1], line
				resume = resume[:len(resume)-1]
			}
		case single, double:
			quote := byte('\'')
			if mode == double {
				quote = '"'
			}
			switch {
			case c == '\\' && i+1 < len(src):
				i++
				buf.WriteByte(src[i])
				if src[i] == '\n' {
					line++
				}
			case c == quote:
				flush()
				mode = code
			case c == '\n':
				flush() // unterminated literal, resynchronise
				mode = code
			default:
				buf.WriteByte(c)
			}
		case backtick:
			switch {
			case c == '\\' && i+1 < len(src):
				i++
				buf.WriteByte(src[i])
				if src[i] == '\n' {
					line++
				}
			case c == '`':
				flush()
				mode = code
			case c == '$' && i+1 < len(src) && src[i+1] == '{':
				flush()
				resume = append(resume, backtick)
				depth = append(depth, 0)
				mode = code
				i++
			default:
				buf.WriteByte(c)
			}
		case lineComment:
			if c == '\n' {
				mode = code
			}
		case blockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				mode = code
				i++
			}
		}
	}
	return out
}

// germanWords are words that mark a string as German UI text. Words that are
// also common English or technical (die, am, an, in, was, hat, tag, ja,
// Status, Details, Name, Minute) are deliberately missing.
var germanWords = []string{
	"und", "oder", "nicht", "kein", "keine", "keinen", "keiner", "keines",
	"wird", "werden", "wurde", "wurden", "ist", "sind", "bitte", "noch",
	"bereits", "schon", "jetzt", "alle", "alles", "einen", "einer", "eines",
	"eine", "ein", "dem", "den", "des", "der", "das", "mit", "ohne", "beim",
	"zum", "zur", "vom", "auf", "aus", "bei", "nach", "unter", "zwischen",
	"gegen", "seit", "bis", "wenn", "weil", "dass", "damit", "auch", "nur",
	"mehr", "weniger", "gerade", "eben", "vor", "als", "wie", "heute",
	"gestern", "Sekunde", "Sekunden", "Minuten", "Stunde", "Stunden",
	"Tage", "Tagen", "Uhr", "Speichern", "Abbrechen", "Laden", "Lade",
	"Bearbeiten", "Entfernen", "Fehler", "Warnung", "Hinweis", "Einstellung",
	"Einstellungen", "Geraet", "Geraete", "Verbindung", "verbunden", "getrennt",
	"gespeichert", "geladen", "aktualisiert", "Anfrage", "fehlgeschlagen",
	"Seite", "Seiten", "Karte", "Karten", "Kachel", "Kacheln", "Wert", "Werte",
	"Leistung", "Verbrauch", "Einspeisung", "Netz", "Speicher", "Ladung",
	"Entladung", "Energie", "Regel", "Regeln", "Aktion", "Aktionen",
	"Bedingung", "Bedingungen", "Verlauf", "Konfiguration", "Automationen",
	"Anmelden", "Abmelden", "Passwort", "Benutzer", "Zeitraum", "Stand",
	"veraltet", "unbekannt", "Neu", "neue", "neuer", "neues", "Nein",
	"Zeile", "Spalte", "Breite", "Hoehe", "Titel", "Quelle", "Ziel", "Anzeige",
}

var (
	germanPattern = regexp.MustCompile(`[äöüÄÖÜß]|(?i:\b(?:` + strings.Join(germanWords, "|") + `)\b)`)
	// Sentence-like literals without a German feature, listed by I18N_WIDE
	// for a manual look.
	widePattern       = regexp.MustCompile(`^\p{Lu}\p{Ll}{2,}|\p{L}{2,}\s+\p{L}{2,}`)
	letterRunPattern  = regexp.MustCompile(`\p{L}{2,}`)
	templateAction    = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	templateSkip      = regexp.MustCompile(`(?is)<!--.*?-->|<script\b.*?</script>|<style\b.*?</style>|<svg\b.*?</svg>`)
	attributePattern  = regexp.MustCompile(`([^\s=/>"'<]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>"']+)))?`)
	visibleAttributes = map[string]bool{"title": true, "placeholder": true, "aria-label": true, "alt": true, "label": true, "aria-description": true, "aria-placeholder": true, "aria-valuetext": true}
	// {{t ...}} inside an Alpine attribute value (E4).
	alpineTemplateCallPattern = regexp.MustCompile(`\s(?:x-[\w:.-]+|:[\w.-]+|@[\w.-]+)="[^"]*\{\{-?\s*tn?\s`)
)

func germanText(s string) bool { return germanPattern.MatchString(s) }

type templateText struct {
	line int
	kind string // "text", "attr" or "alpine"
	name string // attribute name for attr and alpine
	text string
}

// blank replaces everything but newlines with spaces so line numbers stay.
func blank(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		return ' '
	}, s)
}

func lineAt(src string, offset int) int { return 1 + strings.Count(src[:offset], "\n") }

// templateTexts returns the visible texts of a template: text nodes,
// visible attributes, and string literals with a German feature inside
// Alpine attributes. Template actions, comments, scripts, styles and inline
// SVG are ignored.
func templateTexts(src string) []templateText {
	src = templateAction.ReplaceAllStringFunc(src, blank)
	src = templateSkip.ReplaceAllStringFunc(src, blank)
	var out []templateText
	for i := 0; i < len(src); {
		if src[i] != '<' {
			j := strings.IndexByte(src[i:], '<')
			if j < 0 {
				j = len(src) - i
			}
			raw := src[i : i+j]
			if text := strings.TrimSpace(html.UnescapeString(raw)); text != "" {
				lead := len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
				out = append(out, templateText{line: lineAt(src, i+lead), kind: "text", text: text})
			}
			i += j
			continue
		}
		j, quote := i+1, byte(0)
		for ; j < len(src); j++ {
			c := src[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '"' || c == '\'' {
				quote = c
				continue
			}
			if c == '>' {
				break
			}
		}
		end := min(j+1, len(src))
		tag := src[i:end]
		if !strings.HasPrefix(tag, "</") {
			for _, m := range attributePattern.FindAllStringSubmatchIndex(tag, -1) {
				if m[0] == 1 { // the tag name itself
					continue
				}
				name := strings.ToLower(tag[m[2]:m[3]])
				value := ""
				for g := 4; g <= 8; g += 2 {
					if m[g] >= 0 {
						value = html.UnescapeString(tag[m[g]:m[g+1]])
					}
				}
				line := lineAt(src, i+m[0])
				switch {
				case visibleAttributes[name]:
					if strings.TrimSpace(value) != "" {
						out = append(out, templateText{line: line, kind: "attr", name: name, text: value})
					}
				case strings.HasPrefix(name, "x-") || strings.HasPrefix(name, ":") || strings.HasPrefix(name, "@"):
					for _, lit := range jsStringLiterals(value) {
						if germanText(lit.text) {
							out = append(out, templateText{line: line, kind: "alpine", name: name, text: lit.text})
						}
					}
				}
			}
		}
		i = end
	}
	return out
}

type allowedTokens struct{ pattern *regexp.Regexp }

func compileAllowedTokens(tokens []string) allowedTokens {
	sort.Slice(tokens, func(a, b int) bool { return len(tokens[a]) > len(tokens[b]) })
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token = strings.TrimSpace(token); token != "" {
			quoted = append(quoted, regexp.QuoteMeta(token))
		}
	}
	if len(quoted) == 0 {
		return allowedTokens{}
	}
	return allowedTokens{pattern: regexp.MustCompile(`\b(?:` + strings.Join(quoted, "|") + `)\b`)}
}

// visibleText reports whether text still contains words once the allowed
// tokens (product names, units, protocols) are removed.
func visibleText(text string, allowed allowedTokens) bool {
	if allowed.pattern != nil {
		text = allowed.pattern.ReplaceAllString(text, " ")
	}
	return letterRunPattern.MatchString(text)
}

type finding struct {
	line int
	text string
	wide bool // only listed with I18N_WIDE, never a failure
}

func readEmbedded(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(templateFS, dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		data, err := fs.ReadFile(templateFS, dir+"/"+entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[dir+"/"+entry.Name()] = string(data)
	}
	return files
}

func readList(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func ignoredLines(src string) map[int]bool {
	ignored := map[int]bool{}
	for n, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "i18n-ignore") {
			ignored[n+1] = true
		}
	}
	return ignored
}

// scanUITexts returns the findings of every template and script, keyed by
// the path relative to internal/webui.
func scanUITexts(t *testing.T) map[string][]finding {
	t.Helper()
	allowed := compileAllowedTokens(readList(t, "testdata/i18n-allowed-tokens.txt"))
	findings := map[string][]finding{}
	for file, src := range readEmbedded(t, "templates") {
		for _, text := range templateTexts(src) {
			if text.kind == "alpine" || visibleText(text.text, allowed) {
				findings[file] = append(findings[file], finding{line: text.line, text: text.text})
			}
		}
	}
	for file, src := range readEmbedded(t, "static/js") {
		ignored := ignoredLines(src)
		for _, lit := range jsStringLiterals(src) {
			switch {
			case ignored[lit.line]:
			case germanText(lit.text):
				findings[file] = append(findings[file], finding{line: lit.line, text: lit.text})
			case widePattern.MatchString(lit.text) && !strings.ContainsAny(lit.text, "<{=#/"):
				findings[file] = append(findings[file], finding{line: lit.line, text: lit.text, wide: true})
			}
		}
	}
	return findings
}

func hardFindings(list []finding) []finding {
	var hard []finding
	for _, f := range list {
		if !f.wide {
			hard = append(hard, f)
		}
	}
	return hard
}

func TestNoUntranslatedTextOutsidePendingFiles(t *testing.T) {
	findings := scanUITexts(t)
	files := make([]string, 0, len(findings))
	for file := range findings {
		files = append(files, file)
	}
	sort.Strings(files)

	if os.Getenv("I18N_WRITE_PENDING") == "1" {
		var b strings.Builder
		b.WriteString("# Files whose UI texts are not in the catalogs yet (localization A3).\n# A migrated file is removed from this list. See literals_test.go.\n")
		for _, file := range files {
			if len(hardFindings(findings[file])) > 0 {
				b.WriteString(file + "\n")
			}
		}
		if err := os.WriteFile("testdata/i18n-pending.txt", []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if filter := os.Getenv("I18N_INVENTORY"); filter != "" {
		wide := os.Getenv("I18N_WIDE") == "1"
		for _, file := range files {
			if filter != "all" && !strings.Contains(file, filter) {
				continue
			}
			for _, f := range findings[file] {
				if !f.wide || wide {
					t.Logf("%s:%d: %q", file, f.line, f.text)
				}
			}
		}
	}

	pending := map[string]bool{}
	for _, file := range readList(t, "testdata/i18n-pending.txt") {
		pending[file] = true
		if _, err := fs.Stat(templateFS, file); err != nil {
			t.Errorf("testdata/i18n-pending.txt lists %s, which does not exist", file)
		} else if len(hardFindings(findings[file])) == 0 {
			t.Errorf("%s has no untranslated text left, remove it from testdata/i18n-pending.txt", file)
		}
	}
	for _, file := range files {
		if pending[file] {
			continue
		}
		for _, f := range hardFindings(findings[file]) {
			t.Errorf("%s:%d: text %q is not from the catalog (use {{t}}, $t() or I18n.t, see docs/knowledge/dashboard/localization.md)", file, f.line, f.text)
		}
	}
}

func TestJSStringLiteralScanner(t *testing.T) {
	src := "const a = 'Lädt'; // 'kein Literal'\n" +
		"/* 'auch keins' */ const b = \"x\\\"y\";\n" +
		"const c = `Update ${version ? 'neu' : `v${n}`} verfügbar`;\n" +
		"const d = 'http://host/path';\n"
	got := jsStringLiterals(src)
	want := []literal{
		{line: 1, text: "Lädt"},
		{line: 2, text: `x"y`},
		{line: 3, text: "Update "},
		{line: 3, text: "neu"},
		{line: 3, text: "v"},
		{line: 3, text: ""},
		{line: 3, text: " verfügbar"},
		{line: 4, text: "http://host/path"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestGermanTextHeuristic(t *testing.T) {
	for _, s := range []string{"Lädt", "Bitte warten", "keine Daten", "Werte veraltet", "Speichern", "Gerät", "vor 5 s"} {
		if !germanText(s) {
			t.Errorf("germanText(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"status.ok", "Details", "Status", "active", "is-visible", "Die hard", "MQTT", "application/json", ""} {
		if germanText(s) {
			t.Errorf("germanText(%q) = true, want false", s)
		}
	}
}

func TestTemplateTextScanner(t *testing.T) {
	src := "<h2>Einstellungen</h2>\n" +
		"<p>{{t \"a.b\"}}</p>\n" +
		"<button aria-label=\"Schließen\" x-text=\"busy ? 'Prüfe ...' : $t('c.d')\">MQTT</button>\n" +
		"<span title=\"{{t \"e.f\"}}\" x-show=\"a > b\">Hallo</span>\n" +
		"<!-- Kommentar --><script>const x = 'Nein';</script><svg><title>Icon</title></svg>\n" +
		"<input placeholder=\"z. B. 192.168.1.10\">\n"
	got := templateTexts(src)
	want := []templateText{
		{line: 1, kind: "text", text: "Einstellungen"},
		{line: 3, kind: "attr", name: "aria-label", text: "Schließen"},
		{line: 3, kind: "alpine", name: "x-text", text: "Prüfe ..."},
		{line: 3, kind: "text", text: "MQTT"},
		{line: 4, kind: "text", text: "Hallo"},
		{line: 6, kind: "attr", name: "placeholder", text: "z. B. 192.168.1.10"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestTemplateFindingsHonourAllowedTokens(t *testing.T) {
	allowed := compileAllowedTokens([]string{"MQTT", "Home Assistant", "kWh"})
	cases := map[string]bool{"MQTT": false, "Home Assistant": false, "12 kWh": false, "MQTT Broker": true, "Hallo": true, "−": false, "": false}
	for text, want := range cases {
		if got := visibleText(text, allowed); got != want {
			t.Errorf("visibleText(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestAlpineAttributesUseMagicNotTemplateCall(t *testing.T) {
	for _, dir := range []string{"templates"} {
		for file, src := range readEmbedded(t, dir) {
			for _, match := range alpineTemplateCallPattern.FindAllStringIndex(src, -1) {
				t.Errorf("%s:%d uses {{t}} inside an Alpine attribute, use $t() instead (E4)", file, lineAt(src, match[0]))
			}
		}
	}
}
