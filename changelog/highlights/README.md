# Highlight translations

One file per pull request, named after its number, holds the texts of its
release highlights in other languages than English:

    {"de": {"*": "Text for every component", "dashboard": "Text for the dashboard only"}}

`*` applies to every component without its own line. Every other key is a
target of `scripts/generate_changelog.sh`. A PR file can also hold `details`
for a larger highlight, see
[details for bigger highlights](../../docs/developing/releasing.md#details-for-bigger-highlights).

When a final release is tagged, the release workflow
`archive-highlight-translations.yml` moves the files into a PR. The texts of one
release go to `v<MAJOR>.<MINOR>.<PATCH>.json`, the texts of all releases of one
major version to `archive-v<MAJOR>.json`. Both have the form
`{"<PR>": {"de": {...}}}`. A per-PR file overrides them for the languages it has.

While the PR is open, the `Highlights (de):` block of its description is the
source, and the Version bump workflow writes the file. After the merge the file
may be edited by hand. See
[translations in releasing.md](../../docs/developing/releasing.md#translations).
