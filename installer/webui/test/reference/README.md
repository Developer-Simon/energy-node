# Reference drafts of the installer UI

Copied from `drafts/installer-ui/` (git-ignored, canvas built with the
`apple-design` skill). These files are the specification of how the installer
looks: `test/draft-fidelity.test.mjs` requires every CSS rule here to exist
verbatim in `static/css/`, and `test/e2e/layout.spec.mjs` compares the
rendered geometry and German texts at 1080x720.

Change a draft here first, in its own commit, then make the product follow.
Screen mapping: Main=connect, Vorpruefung=precheck, Konfiguration=configure,
Ausfuehrung=run, Ergebnis=result, Aktualisieren=preview, Diagnose=diagnose.
