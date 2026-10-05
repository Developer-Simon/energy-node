# Reproducing the documentation screenshots

## Dashboard

### Reproducing these screenshots

The smoke test starts the real dashboard against a fixture broker, with no
Raspberry Pi, no Mosquitto and no devices. The `docs-screenshots` preset is
built on the near-complete `alle-funktionen` fixture: battery SoC devices, the
seven energy entities of a sunny moment, a fully modelled APsystems EZ1 with
two strings and a writable power limit. It adds the layout, role assignment,
device map and the automation rule seen above.

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset docs-screenshots --simulate-package &
node dashboard/test/smoke/docs-screenshots.mjs
```

The first command prints a URL and credentials and stays up until Ctrl-C.
`--simulate-package` makes a fake GitHub offer version 9.9.9 with a small
package, which is what the update screenshots show. The second command writes
every image on this page into `docs/images/`. Two images need their own run:

- **History:** `docs-screenshots.mjs --history` keeps the dashboard open for
  about twelve minutes so the browser records enough history, then takes the
  History tab.
- **Versions:** restart the first command with `--simulate-installed` instead
  of `--simulate-package`, then run
  `docs-screenshots.mjs dashboard-settings-versions`.

For a single screenshot of any page, `screenshot.mjs` is the simpler tool:

```bash
node dashboard/test/smoke/screenshot.mjs --out /tmp/overview.png --lang en
```

The full option list, the other presets and the fixtures behind them are
documented in `dashboard/test/smoke/README.md` in the repository.

## Installer

### Reproducing these screenshots

The installer's web UI has a demo host that serves the real screens against a
fake backend, so no Pi is needed:

```sh
cd installer/webui
go run ./cmd/fakehost --lang en --port 8099
```

Open the printed address and connect with any address, user and password.
`--scenario vorlage-update` serves the update preview, `--hold-step <id>` stops
a run at that step, and `--fail-step <id>:<CODE>` makes one fail.

The update preview and the changelog page are taken by a script that drives
the demo host itself (it needs Go and Playwright):

```sh
cd installer/webui
node test/e2e/docs-screenshots.mjs
```
