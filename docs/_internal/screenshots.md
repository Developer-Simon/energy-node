# Reproducing the documentation screenshots

## Dashboard

### Reproducing these screenshots

The demo starts the real dashboard against a fixture broker, with no Raspberry
Pi, no Mosquitto and no devices. `demo/demo.sh --docs` uses the near-complete
`alle-funktionen.json` fixture: battery SoC devices, the seven energy entities
of a sunny moment, a fully modelled APsystems EZ1 with two strings and a
writable power limit. It adds the layout, role assignment, device map and the
automation rule seen above.

```bash
demo/demo.sh --docs --no-simulate
node dashboard/test/browser/docs-screenshots.mjs
```

The first command prints a URL and credentials and stays up until Ctrl-C. The
second command writes every image on this page into `docs/images/`. Some images
need their own run:

- **History:** `docs-screenshots.mjs --history` keeps the dashboard open for
  about twelve minutes so the browser records enough history, then takes the
  History tab.
- **Versions:** `docs-screenshots.mjs dashboard-settings-versions`.
- **Service logs:** `docs-screenshots.mjs dashboard-service-logs`. The script
  waits until no over-long test line is in view.
- **Overview cards:** with `demo/demo.sh --docs` running and the simulation on
  (the default), run `docs-screenshots.mjs --cards`. It adds a layout page with
  one card of each type at its minimum width, records about 16 minutes of
  history (the battery trajectory needs 15) and writes `dashboard-card-*.png`.
  The layout is restored afterwards.

For a single screenshot of any page, `screenshot.mjs` is the simpler tool:

```bash
node dashboard/test/browser/screenshot.mjs --url https://localhost:18200/ --out /tmp/overview.png --lang en
```

The full option list and the other browser tools are documented in
`dashboard/test/browser/README.md` in the repository. The fixtures are
described in `demo/README.md`.

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
