# Browser-Werkzeuge

Playwright-Skripte fuer Sichtpruefungen und Messungen am laufenden Dashboard.
Sie laufen gegen die Demo, nicht gegen den Smoke-Test:

```bash
demo/demo.sh                     # Demo starten, auf https://localhost:18200
node dashboard/test/browser/screenshot.mjs
```

Die Demo nutzt ein selbstsigniertes Zertifikat. Die Skripte akzeptieren es
(`ignoreHTTPSErrors`). Anmeldung als `admin` mit dem Demo-Passwort aus der
Konsole des Starts.

| Skript | Zweck | Aufruf |
|---|---|---|
| `screenshot.mjs` | Ein Bild einer Seite, vollstaendig gescrollt | `node dashboard/test/browser/screenshot.mjs --url https://localhost:18200/geraete --out /tmp/geraete.png` |
| `docs-screenshots.mjs` | Die Bilder fuer `docs/images/` neu aufnehmen | `demo/demo.sh --docs --no-simulate`, dann `node dashboard/test/browser/docs-screenshots.mjs`. Mit `--history` zusaetzlich den Verlauf (ca. 12 min). Die Karten fuer `docs/dashboard/overview-cards.md`: `demo/demo.sh --docs`, dann `node dashboard/test/browser/docs-screenshots.mjs --cards` |
| `history-view-tile.mjs` | Prueft, dass Verlaufskacheln den Live-Tausch ueberleben (Gruppe Verlauf und Statistik) | `node dashboard/test/browser/history-view-tile.mjs` |
| `mobile-overflow.mjs` | Prueft Tabs und Breiten auf horizontales Ueberlaufen | `node dashboard/test/browser/mobile-overflow.mjs --widths 400,1280 overview settings energy devicemap stats` |
| `measure-client-cost.mjs` | Misst die Kosten eines offenen Uebersichts-Tabs im Leerlauf | `node dashboard/test/browser/measure-client-cost.mjs --seconds 30` |

Bilder und Messwerte landen standardmaessig in `dashboard/test/browser/.run/`
(nicht eingecheckt).
