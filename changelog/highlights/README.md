# Highlight translations

One file per pull request, named after its number, holds the texts of its
release highlights in other languages than English:

    {"de": {"*": "Text for every component", "dashboard": "Text for the dashboard only"}}

`*` applies to every component without its own line. Every other key is a
target of `scripts/generate_changelog.sh`.

The texts of already released highlights are collected in `archive.json`, in
the form `{"<PR>": {"de": {...}}}`. A per-PR file overrides the archive for the
languages it has. While the PR is open, the `Highlights (de):` block of its description is the source and the Version bump
workflow writes the file. After the merge the file may be edited by hand. See
[releasing.md](../../docs/developing/releasing.md#translations). When a final release is tagged, the release workflow `archive-highlight-translations.yml` moves these files into `archive.json` through a PR.
