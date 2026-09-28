# Localization

The dashboard's UI texts come from message catalogs, not from string literals.
German (`de`) is the default; English (`en`) is complete. A language is chosen
per browser.

## How a language is chosen

1. The `lang` cookie, if it names an available language (written by the
   language switcher in the masthead and on the login page).
2. The browser's `Accept-Language` header, by primary subtag.
3. `de`.

## Switching the language

- **Masthead and login page:** a compact `DE | EN` pill
  (`templates/lang-pill.html`), a radio group with a sliding thumb. The page
  reloads once the thumb has settled, at once with reduced motion. The pill
  shows the upper-case language code, screen readers hear the language's own
  name. It stays usable without Alpine because `i18n.js` handles it with one
  delegated `change` listener.
- **Settings → Darstellung → Formatierung:** the same choice as a segmented
  control. Like everything on that page it applies on *Speichern*, and the
  reload happens only after the settings were stored.
- The node setting `language_switch_hidden` hides the pill in the masthead and
  on the login page for everyone. The language is then only switchable in the
  settings. The pill stays in the page with `hidden`, so saving the settings
  shows it again without a reload.

The *Formatierung* card also holds the number format (see below).

## Catalogs

`dashboard/internal/webui/catalogs/<code>.json` — one flat JSON object per
language, embedded in the binary. Keys are lowercase, dot-separated and
semantic (`status.mqtt.connected`), never the German text. Placeholders are
`{name}`; plurals use `key.one` / `key.other` with `{n}`. A key missing in a
language falls back to English; a key missing everywhere shows the key itself.
Every catalog needs `meta.language_name` (the language's own name, shown in
the switcher).

## Using a text

- Template text: `{{t "key"}}`, `{{t "key" "name" .Name}}`, `{{tn "key" .Count}}`.
- **Inside an Alpine expression** (`x-text`, `x-bind:*`, …): always
  `$t('key')` / `$tn('key', n)`, never `{{t}}`. `html/template` escapes
  apostrophes in attributes and a text like "Don't" would break the JS string.
- JavaScript: `window.I18n.t('key', {name: value})`, `I18n.tn('key', n)`.
- A key built at run time (`t('status.' + state)`) needs a comment
  `i18n-keys: status.ok, status.error` listing every possible key, so the drift
  test can check them.

## Adding a text

When you write a new UI text, follow these rules:

1. **Semantic keys:** lowercase, dot-separated, never the German text. Example: `settings.storage_health.title`.
2. **German text:** must be correct German with real umlauts and „…" quotes, no semicolons, no dashes as connectors. Write English immediately in British English, sentence case. Both languages are ready when you commit.
3. **Reuse `common.*`:** only for words used identically on several pages (Save, Cancel, Yes, No). Create namespace keys instead (e.g. `settings.title`).
4. **Dynamically built keys:** add a comment `i18n-keys: status.ok, status.error` listing every possible key so the drift test can check them.
5. **Go delivers keys:** Go functions give a field `…Key` and optional `…Params map[string]any` alongside the old German field. Templates use `{{t .LabelKey}}` with a comment listing all possible keys.
6. **One sentence, one key:** never concatenate `t()` results or mix `t()` with other text. Optional parts get their own key variant (e.g. `status.succeeded` and `status.succeeded_by`).
7. **Plurals:** use `tn('key', n)` with keys ending in `.one` and `.other`. Don't write `n === 1 ? ... : ...` by hand.
8. **Composite text:** use placeholders `{name}` instead of string concatenation. Pass formatted numbers separately: `tn('x', n, {count: I18n.formatNumber(n)})`.
9. **Technical strings:** CSS classes, error codes, HTTP headers, event names, storage keys, MQTT topics. If the guard flags them, add `// i18n-ignore` at the line end. Never use `i18n-ignore` for visible text, aria-label counts as visible.
10. **Keep both catalogs sorted** after adding new keys.

## API errors

API errors keep their `{"code","message"}` form (see
[API documentation](api-documentation.md#response-format)). The dashboard
translates them by code.

- **Go:** `writeError(w, status, code, message)` for a code with one text.
  `writeErrorKey(w, status, code, "error.<code>.<variant>", params, message)`
  when a code has several texts, `message` is the German fallback and must
  equal `de.json` for that key. `writeErrorDetail(w, status, code, err)` for a
  passed-through error: `err.Error()` travels as `detail`, unless the chain
  holds a `*uierror.Error`, whose `Key` and `Params` become `message_key`
  and `params`. `requireRole` takes the variant key before its message.
- **`internal/uierror`:** `uierror.New(key, text, params)` is an error whose
  `Error()` is exactly `text`, so logs and tests keep the old wording.
  `uierror.From(err)` finds it through `fmt.Errorf("…: %w", err)` wrapping.
- **Keys:** `error.<code>` for every code (the general text), plus
  `error.<code>.<variant>` for each variant. `error.with_detail` is
  `{message}: {detail}`.
- **Guard:** `internal/httpapi/error_catalog_test.go` scans every error writer
  and `requireRole` call and fails when a code or key is missing in `de` or
  `en`, or when a Go fallback differs from `de.json`. A code passed through a
  variable must be listed in `dynamicErrorCodes`.
- **Browser:** `I18n.error(body, fallbackKey)` picks `message_key`, then
  `error.<code>` (with `detail` appended), then the server's `message`, then
  the fallback key (default `common.request_failed`). Page scripts wrap it in
  a local `apiError` helper and read the body with
  `response.json().catch(() => ({}))`, so an HTML error page from a proxy
  shows the fallback text instead of a `SyntaxError`.

## Adding a language

Add `dashboard/internal/webui/catalogs/<code>.json` with every key of `de.json`
and the same placeholders, plus `meta.locale`, `meta.number.decimal` and
`meta.number.group`. Copy flatpickr's locale file for the language to
`static/js-deps/flatpickr-l10n-<lang>.js` (from
`node_modules/flatpickr/dist/l10n/<lang>.js`) and add it to
`static/js-deps/THIRD-PARTY-NOTICES.md`. `go test ./internal/webui/` fails
until the catalog is complete. The switcher picks it up automatically.

## Guards

`internal/webui/catalogs_test.go` checks that all catalogs have the same keys
and placeholders, that plural forms come in pairs, that every key used in a
template or script exists in `de.json`, that the catalog files are sorted,
and that no English text still starts with `TODO(en): `.

`internal/webui/literals_test.go` rejects UI text that bypasses the catalogs:

- **Templates** strictly: every text node and every `title`, `placeholder`,
  `aria-label`, `alt` and `label` must come from `{{t}}`, unless it consists
  of tokens from `testdata/i18n-allowed-tokens.txt` (product names, units,
  protocols). `{{t}}` inside an Alpine attribute is an error too.
- **Scripts and Alpine expressions** heuristically: a string literal with an
  umlaut or a German word from the list in `literals_test.go`. A technical
  string that trips it gets `// i18n-ignore` at the end of its line.

Listing the findings of a file: `I18N_INVENTORY=static/js/notify.js go test
./internal/webui/ -run TestNoUntranslatedUIText -v`
(`I18N_INVENTORY=all` for everything, `I18N_WIDE=1` also lists sentence-like
literals without a German feature).

Sorting a catalog (from the repository root):
`.venv/bin/python -c 'import json,sys; p=sys.argv[1]; d=json.load(open(p)); open(p,"w").write(json.dumps(d, ensure_ascii=False, indent=2, sort_keys=True)+"\n")' dashboard/internal/webui/catalogs/de.json`

## Numbers, dates and sorting

Numbers do not follow the language. The operator picks a global number
format in Settings → Darstellung (`number_format`: `auto`, `comma`, `point`,
and `number_grouping`: `match`, `thin`, stored in `settings.json`).
`auto` takes the separators from the active catalog's `meta.number.decimal`
and `meta.number.group`. Groups start at five integer digits, so `1234 W`
stays compact and `12.345 W` is grouped. A changed format reloads the page.

Device values from MQTT are formatted only when they carry a unit and are a
plain decimal (`-12.50`). Their decimals are kept exactly. Values without a
unit (counters, years, IDs), text states and timestamps stay as reported.

Dates, times and sorting follow the catalog's `meta.locale` through `Intl`.

In code, never call `toLocaleString`, `localeCompare` or `Intl.*` directly.
Use `I18n.formatNumber(value, decimals)`, `I18n.formatValue(raw, unit)`,
`I18n.formatDateTime/formatDate/formatTime(value, options)` and
`I18n.compare(a, b)`. In Go templates use `{{formatValue .Value .Unit}}` and
`{{formatNumber .Value 0}}`. `dashboard/test/format-guard.test.mjs` enforces
this. The Go package `internal/numfmt` and `i18n.js` share their test cases
(`internal/numfmt/testdata/cases.json`), extend both together.

A new catalog needs `meta.locale`, `meta.number.decimal` and
`meta.number.group`. A test checks the separators against `Intl` for that
locale. flatpickr loads `static/js-deps/flatpickr-l10n-<lang>.js` when that
file exists (English is built in), ApexCharts builds its month and day
names from `Intl`.
