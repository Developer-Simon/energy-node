# Highlight translations

One file per pull request, named after its number, holds the English and the
German text of its release highlights:

    {"de": {"*": "Text für jede Komponente", "dashboard": "Text nur für das Dashboard"},
     "en": {"*": "Text for every component"}}

`*` applies to every component without its own line. Every other key is a
target of `scripts/generate_changelog.sh`. A PR file can also hold `details`
for a larger highlight, see
[details for bigger highlights](../../docs/developing/releasing.md#details-for-bigger-highlights).
Every highlight of the dashboard also needs a `tour`, see
[tours for web UI highlights](../../docs/developing/releasing.md#tours-for-web-ui-highlights).

When a final release is tagged, the release workflow
`archive-highlight-translations.yml` moves the files into a PR. The texts of one
release go to `v<MAJOR>.<MINOR>.<PATCH>.json`, the texts of all releases of one
major version to `archive-v<MAJOR>.json`. Both have the form
`{"<PR>": {"de": {...}, "en": {...}}}`. A per-PR file overrides them for the languages it has.

While the PR is open, its description is the source: the Version bump workflow
copies the English text of the changelog entry once (or the `Highlights (en):`
block every time) and the `Highlights (de):` block into the file. After the
merge the file may be edited by hand, for example to shorten the English text.
The `CHANGELOG.md` files stay as they are. See
[translations in releasing.md](../../docs/developing/releasing.md#translations).
