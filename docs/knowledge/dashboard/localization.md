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
and placeholders, that plural forms come in pairs, and that every key used in
a template or script exists in `de.json`.

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
