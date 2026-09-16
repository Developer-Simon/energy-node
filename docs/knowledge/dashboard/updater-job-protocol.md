# Der Auftrag zwischen Dashboard und energy-node-updater

Plan D der Installationsanwendungs-Spec (`.docs/superpowers/specs/2026-09-05-installationsanwendung-design.md`,
Komponente D) baut die Updater-Unit und den lokalen Schicht-2-Handler, aber
ausdruecklich nicht, wie ein neues Bundle ueberhaupt auf den Node kommt. Das
ist Sache der Dashboard-OTA-Spec. Diese Seite ist die Schnittstelle, die sie
beliefern muss.

## Woher der Updater sein Bundle nimmt

`internal/updaterhost.Config.CandidateBundleDir` zeigt auf ein bereits
entpacktes, aber noch **nicht verifiziertes** Bundle-Verzeichnis. Alles, was
dort ablegt, muss selbst dafuer sorgen, dass `manifest.json` und
`manifest.json.sig` zu genau diesem Bundle gehoeren -- die Pruefung selbst
uebernimmt ausschliesslich `energy-node-updater.sh` beim naechsten Lauf,
nie der Absender.

## Das Auftragsverzeichnis

`/var/lib/energy-node-installer/job/`:

| Datei | Wer schreibt sie | Wer liest sie |
|---|---|---|
| `bundle/` | `internal/updaterjob.Stage` | `energy-node-updater.sh` |
| `job.json` → `pending.json` | `internal/updaterjob.Stage` (atomarer Rename als letzter Schritt) | `energy-node-updater.path` (nur auf Existenz), `energy-node-updater.sh` |
| `current.json` | `energy-node-updater.sh` (erste Aktion: `pending.json` umbenannt) | `internal/updaterjob.InFlight` |
| `log` | `energy-node-updater.sh` (`<unix-millis> <Zeile>`) | `internal/updaterhost.tailJobLog` |
| `status.json` | `energy-node-updater.sh` (letzte Aktion) | `internal/updaterjob.ReadStatus` |

## Was eine OTA-Auslieferung selbst beitragen muss

- Ein neues Bundle irgendwie nach `CandidateBundleDir` bringen (Download,
  Upload, ...) -- das ist die einzige noch offene Luecke.
- Sonst nichts: Staging, Verifikation, Ausfuehrung, Fortschritt und
  Selbst-Update-Ueberleben sind bereits diese Spec.
