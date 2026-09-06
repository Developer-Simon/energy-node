# Third-party notices

The dashboard embeds these libraries directly (no CDN, no build step — see
[`dashboard/AGENTS.md`](../../../../AGENTS.md)). Versions below are what is
actually embedded, read from each file itself (a version string, or an
embedded semver where no header states one); this can trail the `^`-range in
[`dashboard/package.json`](../../../../package.json) for the four packages
managed there until the next vendoring pass.

| File | Library | Version | License |
|---|---|---|---|
| `alpine.min.js` | [Alpine.js](https://alpinejs.dev/) | 3.14.9 | MIT |
| `alpine-collapse.min.js` | [`@alpinejs/collapse`](https://alpinejs.dev/plugins/collapse) | same release line as `alpine.min.js` (no version string in this build) | MIT |
| `alpine-mask.min.js` | [`@alpinejs/mask`](https://alpinejs.dev/plugins/mask) | same release line as `alpine.min.js` (no version string in this build) | MIT |
| `apexcharts.min.js` | [ApexCharts](https://apexcharts.com/) | 4.7.0 | MIT |
| `choices.min.js` | [Choices.js](https://github.com/Choices-js/Choices) | 11.2.3 | MIT |
| `cytoscape.min.js` | [Cytoscape.js](https://js.cytoscape.org/) | 3.34.0 | MIT |
| `flatpickr.min.js` | [flatpickr](https://flatpickr.js.org/) | 4.6.13 | MIT |
| `flatpickr-l10n-de.js` | flatpickr — German locale | matches `flatpickr.min.js` | MIT |
| `gridstack-all.js` | [GridStack.js](https://gridstackjs.com/) | 13.0.1 | MIT |
| `htmx.min.js` | [htmx](https://htmx.org/) | 2.0.6 | BSD 2-Clause |
| `popper.min.js` | [`@popperjs/core`](https://popper.js.org/) | 2.11.8 | MIT |
| `tippy.umd.min.js` | [Tippy.js](https://atomiks.github.io/tippyjs/) | 6.3.7 | MIT |

Each file above carries its own copyright; none of these projects are
affiliated with Energy Node. `alpinejs`, `choices.js`, `cytoscape`, `flatpickr`
and `htmx.org` are vendored by hand (not listed in `package.json`, which
only tracks the four dependencies installed via npm and re-bundled here).
