# Lokaler Smoke-Test des Dashboards

Startet das echte Dashboard am Entwicklungsrechner — ohne mosquitto, ohne
Zugriff auf den Pi, ohne echte Geräte — und prüft die Konfigurationsseite
end-to-end. Gedacht für Änderungen, die sich mit jsdom- oder Go-Unit-Tests
allein nicht überzeugend belegen lassen (Registry → HTTP-API → Formular).

```bash
dashboard/test/smoke/run-local-dashboard.sh            # prüfen, dann abbauen
dashboard/test/smoke/run-local-dashboard.sh --keep     # laufen lassen, im Browser ansehen
dashboard/test/smoke/run-local-dashboard.sh --devices src/tuya_mqtt
```

`--keep` gibt URL und Zugangsdaten aus und lässt alles stehen, bis Strg-C
kommt — der Weg, um sich das Formular tatsächlich anzusehen.

## Screenshot ohne Klicks

`screenshot.mjs` fährt einen headless Chromium (Playwright, echte
devDependency in `package.json` — kein Ad-hoc-Aufbau nötig) gegen ein
laufendes `--keep`-Dashboard, klickt bei Bedarf "Als Gast fortfahren" (über
HTTP ist nur der Gast-Zugang möglich, siehe `login.html`/`GuestOnly`) und
schreibt einen Screenshot. Gedacht für Sichtprüfungen an CSS/Layout, ohne
jedes Mal manuell einen Browser zu öffnen:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset geraete-kacheln &
node dashboard/test/smoke/screenshot.mjs --out /tmp/tiles.png
```

| Option | Wirkung |
|---|---|
| `--url URL` | Standard `http://localhost:18100/` |
| `--out PATH` | Standard `test/smoke/.run/screenshot.png` (bereits `.gitignore`t) |
| `--wait SELECTOR` | wartet zusätzlich auf ein Element, bevor der Screenshot geschrieben wird |
| `--width` / `--height` | Viewport, Standard 1280×900 (z. B. 390×844 für ein iPhone 13 Pro) |

| Option | Wirkung |
|---|---|
| `--keep` | läuft weiter statt abzubauen; URL und Zugangsdaten werden ausgegeben |
| `--devices DIR` | Gerätekonfigurationen aus `DIR` statt `src/battery_soc` |
| `--fixture FILE` | Retained-Nachrichten aus `FILE` statt `fixtures/battery-soc.json` |
| `--seed-data DIR` | `*.json` aus `DIR` ins Datenverzeichnis, **bevor** das Dashboard startet |
| `--theme NAME` | `mint`, `stromblau`, `signalgelb` oder `tageslicht` in `settings.json` |
| `--port N` | anderer HTTP-Port (Standard 18100) |
| `--simulate` | PV/Netz/Batterie/Hauslast bewegen sich per Sinuskurve statt fest zu stehen (nur mit `fixtures/energie-ueberschuss.json`, sonst No-Op) |
| `--preset NAME` | bündelt `--fixture`/`--seed-data`/`--simulate` für einen der unten dokumentierten Fälle (siehe „Presets“) |

Vier Themes mit allen Energie-Karten ansehen — ein Aufruf je Theme, ohne
einen einzigen Klick in der Oberfläche:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep \
  --preset energie --theme tageslicht
```

## Client-Kosten messen

`measure-client-cost.mjs` fährt denselben headless Chromium wie
`screenshot.mjs`, hängt sich per Chrome DevTools Protocol an die
`Performance`-Zähler und meldet, was ein offener Tab im Leerlauf kostet.
Gedacht für Belege statt Bauchgefühl bei Änderungen am Live-Update.

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset energie-simulate &
node dashboard/test/smoke/measure-client-cost.mjs --seconds 30 --out /tmp/vorher.json
```

| Option | Wirkung |
|---|---|
| `--url URL` | Standard `http://localhost:18100/` |
| `--seconds N` | Länge des Messfensters, Standard 30 |
| `--out PATH` | zusätzlich als JSON schreiben |
| `--swap-target ID` | welches Fragment gezählt wird, Standard `overview-live`; für den Geräte-Tab `devices-live` |
| `--width` / `--height` | Viewport, Standard 1280×900 |

Gemeldet werden `TaskDuration`, `ScriptDuration`, `LayoutDuration` und
`RecalcStyleDuration` als Differenz über das Fenster (jeweils auch als
CPU-Anteil), dazu die Endstände von `Nodes` und `LayoutCount` sowie die Zahl
der `htmx:afterSwap`-Ereignisse auf `#overview-live`.

Das Preset `uebersicht-push` ist für genau diese Messung gedacht: es zeigt
alle sieben Energiegrafiken plus `diagnostics`, `entity_value` und
`entity_group` — also jede Karte, die ihre Zahlen über den SSE-Push bekommt.

Für den Geräte-Tab muss zusätzlich das Panel in der URL stehen — der
`registry-updated`-Zuhörer in `dashboard.js` steigt aus, wenn sein Panel nicht
`active` ist, und dann gäbe es nichts zu messen:

```bash
node dashboard/test/smoke/measure-client-cost.mjs \
  --url 'http://localhost:18100/?panel=devices' --swap-target devices-live --seconds 30
```

### Presets

`--preset NAME` setzt `--fixture`, `--seed-data` und ggf. `--simulate` in
einem Rutsch auf eine der unten dokumentierten Kombinationen. Einzelne Flags
**nach** `--preset` überstimmen das Preset (dieselbe Reihenfolge-Regel wie
sonst im Skript) — `--preset energie --fixture eigene.json` nimmt z. B. das
Seed-Set von `energie`, aber die eigene Fixture.

| Preset | Entspricht |
|---|---|
| `battery-soc` | `--fixture fixtures/battery-soc.json` (Skript-Standard, kein Seed) |
| `energie` | `--fixture fixtures/energie-ueberschuss.json --seed-data fixtures/seed/energie-alle-karten` |
| `energie-simulate` | wie `energie`, zusätzlich `--simulate` |
| `uebersicht-push` | wie `energie-simulate`, zusätzlich `entity_value` und `entity_group` — für Performance-Messungen mit `measure-client-cost.mjs` |
| `energie-kombiniert` | `--fixture fixtures/energie-kombiniert.json --seed-data fixtures/seed/energie-kombiniert` — `load_mode "combined"`, Band und Board einzeln je Entität, Ring gesammelt |
| `alle-funktionen` | `--fixture fixtures/alle-funktionen.json --seed-data fixtures/seed/alle-funktionen` |
| `geraete-kacheln` | `--fixture fixtures/geraete-kacheln.json --seed-data fixtures/seed/geraete-kacheln` — drei device-Kacheln mit `span: "1"`, für Sichtprüfungen an `.device-tile-entity` (Slider-Breite, Titel-Umbruch bei `number`/`text`, Wert-Ausrichtung, unverändertes Raster bei allen anderen Entitätstypen) |

## Was hier gelöst ist

Drei Hürden stehen einem lokalen Start im Weg; alle drei sind im Skript erledigt:

1. **Das Dashboard beendet sich, wenn beim Start kein Broker erreichbar ist.**
   → `minibroker.py`, ein absichtlich unvollständiger MQTT-3.1.1-Broker
   (CONNECT/SUBSCRIBE/PINGREQ, Retained-Zustellung; kein QoS > 0, keine
   Wildcards, keine Weiterleitung zwischen Clients). Nicht ausserhalb von Tests
   verwenden.
2. **Die API verlangt eine Anmeldung.** → `DASHBOARD_ADMIN_PASSWORD` legt beim
   ersten Start einen Admin an, das Skript meldet sich an und benutzt die
   Sitzungs-Cookies.
3. **Die Anmeldung verlangt HTTPS.** → `isSecureRequest()` akzeptiert neben TLS
   auch `X-Forwarded-Proto: https`; genau den Header setzt das Skript.

Dazu: Konfigurationen werden in ein Wegwerf-Verzeichnis kopiert (die echten
Dateien unter `src/` werden **nicht** verändert), und belegte Ports aus einem
abgebrochenen Lauf werden vorher freigeräumt — sonst redet `curl` unbemerkt mit
einem alten Prozess.

Das Wegwerf-Verzeichnis liegt lokal unter `test/smoke/.run/` (per `.gitignore`
von git ausgenommen) statt im System-`/tmp` — leichter aufzuräumen und ohne
Seiteneffekte außerhalb des Repos.

Dazu kommt eine vierte Hürde, die `--seed-data`/`--theme` lösen: **jeder Lauf
bekommt ein frisches `mktemp`-Verzeichnis.** Layout, Farbschema und
Energie-Rollen stehen darin nicht — auch `--keep` rettet sie nicht über einen
Neustart, denn das Arbeitsverzeichnis ist beim nächsten Start ein anderes. Ohne
Vorbelegung muss man beides nach jedem Neustart erneut zusammenklicken.

## Die Fixtures

`fixtures/battery-soc.json` ist die Liste der Retained-Nachrichten, die jeder
Abonnent beim Verbinden bekommt: vier Discovery-Konfigurationen plus die
zugehörigen Zustands-Payloads. Sie ist so gebaut, dass die interessanten Fälle
alle vorkommen:

| Topic | Zweck |
|---|---|
| `shelly/netz_meanwell/status` | JSON-Objekt mit mehreren Zahlenfeldern → Key-Vorschläge |
| `shelly/netz_lumentree/status` | zweites JSON-Objekt, für das zweite Feldpaar |
| `bms/bank_a/voltage` | **nackte Zahl** → Warnung „kein JSON-Objekt" |
| `bms/bank_b/voltage` | aus der Discovery bekannt, **sendet nie** → „(noch keine Daten)" |

Eine eigene Fixture geht per `--fixture` (bzw. als zweites Argument direkt an
`minibroker.py`).

`--simulate` lässt vier ihrer Leistungswerte (PV, Netz, Batterie, Hauslast)
alle drei Sekunden per Sinuskurve um die oben genannten Basiswerte pendeln,
gesendet als echte MQTT-`PUBLISH`-Pakete auf der bereits offenen Verbindung —
kein Energiebilanz-Modell, nur genug Bewegung, damit die zeitbasierten
Energie-Karten (`energy_band`, `energy_day`) im Browser eine echte Kurve statt
einer flachen Linie zeigen:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset energie-simulate
```

`energy_day` gruppiert dabei nach Poll (Browser fragt alle 10 s
`/api/v1/energy` ab, siehe `history-recorder.js`) und braucht mindestens zwei
Punkte, bevor überhaupt eine Fläche gezeichnet wird — die Karte bleibt kurz
nach dem Start also noch leer.

`fixtures/energie-ueberschuss.json` ist die zweite Fixture: sieben Entitäten
mit `unit_of_measurement: W`, gerechnet auf einen sonnigen Moment — PV 4200 W,
Einspeisung 1250 W, Batterie lädt mit 900 W, Hausverbrauch 2050 W (darin
Wallbox 600 W und Wärmepumpe 450 W). Die Bilanz geht damit exakt auf
(`gap_applied == 0`, „Übriger Verbrauch" 1000 W), sodass die Energie-Karten
echte Werte und den Zustand „gut" zeigen statt des Leerzustands „kein
Energiefluss", den `battery-soc.json` erzeugt. Sie gehört mit dem Seed-Set
unten zusammen — die Rollen sind auf die `unique_id`s dieser Fixture
zugewiesen.

`fixtures/alle-funktionen.json` ist **teuer, aber nahezu vollständig**: die
Battery-SoC-Geräte aus `battery-soc.json`, die sieben Energie-Entitäten aus
`energie-ueberschuss.json` (ohne den einzelnen `pv_wr_power`-Sensor) und
zusätzlich ein vollständig nachgebildeter AP Systems EZ1-Wechselrichter
(`apsystems_dach`, zwei PV-Strings, Tages-/Gesamtertrag, ein schaltbares
Power-Limit und ein Betriebsstatus-Schalter mit `command_topic`). Gedacht für
Sichtprüfungen und Handtests, die möglichst viele Dashboard-Funktionen auf
einmal sehen wollen, ohne dass man dafür eine eigene Fixture bauen muss —
nicht dafür, jede einzelne Funktion isoliert und günstig zu prüfen; dafür
bleiben die schlankeren Fixtures oben die bessere Wahl. Gehört mit
`fixtures/seed/alle-funktionen/` zusammen:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset alle-funktionen
```

`fixtures/geraete-kacheln.json` ist schlank und auf die Stellschrauben von
`.device-tile-entity`/`.device-tile-entities` (Titel-Umbruch, Slider-Breite,
Ausrichtung der Wert-Spalte über mehrere Zeilen) zugeschnitten, drei Geräte:

- `apsys_nord` — ein `Power-Limit`-Slider (`number`, Härtefall für die
  Slider-Zeile: die 4-Spalten-Sonderbehandlung `.device-tile-entity-number`),
  eine `Standort-Notiz` (`text`, Härtefall für lange freie Textwerte:
  `.device-tile-entity-text`), ein `Betriebsstatus`-Schalter (`switch`, kurzer
  Wert ohne Einheit, bleibt bewusst beim normalen Raster) und ein Sensor mit
  langem, ungetrenntem Namen (`Wechselrichtertemperatur`) — bleibt beim
  ursprünglichen `1fr`/`auto`/`auto`-Raster, weil weder `number` noch `text`
  noch ein langer Wert ohne Einheit.
- `apsys_sued` — dieselbe Kombination ohne die `text`-Entität, als Kontrolle,
  dass ein normaler `Power-Limit`-Slider ohne Geschwister-Text-Zeile genauso
  aussieht.
- `energie_automationen` — ein einzelner Text-*Sensor* (Komponente `sensor`,
  nicht `text`!) mit einem langen, unformatierten Wert ohne Einheit
  (`Letzte Energie-Automatisierung`) — der Grund, warum `.device-tile-entity`
  nicht allein an der MQTT-Komponente hängt: `device-tile.html` hebt jede
  Zeile ohne `unit_of_measurement` auf `.device-tile-entity-text`, deren Wert
  über 20 Zeichen lang ist (siehe `$isLongTextSensor`). Kurze unit-lose Werte
  wie `Betriebsstatus`s `ON`/`OFF` bleiben unter der Schwelle und damit beim
  normalen Raster - nur echte Freitext-Sensoren wie dieser wechseln.

Gehört mit `fixtures/seed/geraete-kacheln/layout.json` zusammen, das alle
drei Geräte mit `span: "1"` auf der Übersichtsseite platziert: das Raster
(`.layout-grid`, `minmax(min(18rem, 100%), 1fr)`) zwingt die Kacheln damit
unabhängig von der Fensterbreite immer auf die 18rem-Untergrenze — genau die
Breite, bei der der Slider vorher die Titel-Spalte auf fast 0 gestaucht hat:

```bash
dashboard/test/smoke/run-local-dashboard.sh --keep --preset geraete-kacheln
```

## Seed-Daten

`--seed-data DIR` kopiert `DIR/*.json` ins Datenverzeichnis, bevor das
Dashboard startet — dasselbe Muster wie `--devices`, nur für `settings.json`,
`layout.json`, `energy.json`, `device-map.json`. Ein teilweise gefülltes
`settings.json` genügt: `Settings.UnmarshalJSON` startet bei `Default()` und
ergänzt jedes fehlende Feld.

`fixtures/seed/energie-alle-karten/` ist das mitgelieferte Set:

| Datei | Inhalt |
|---|---|
| `layout.json` | alle acht Kartentypen sichtbar (`energy_status`, `energy_summary`, `energy_flow`, `energy_ring`, `energy_schema`, `energy_band`, `energy_day`, `energy_board`) plus `diagnostics` |
| `energy.json` | Rollen `pv`/`grid`/`battery`/`load`/`wallbox`/`heat_pump` auf die Entitäten von `fixtures/energie-ueberschuss.json` |
| `settings.json` | Theme `mint` — `--theme` überstimmt das nach dem Kopieren |

`fixtures/seed/alle-funktionen/` ist das Gegenstück zu `alle-funktionen.json`
— ebenfalls **teuer, aber nahezu vollständig**:

| Datei | Inhalt |
|---|---|
| `layout.json` | wie `energie-alle-karten/`, plus eine zweite Gruppe „Geräte" mit den bisher ungenutzten Layout-Typen `device` (AP-Systems-Wechselrichter) und `entity` (einzelne Spannungs-Entität) |
| `energy.json` | sieben der elf Rollen, u. a. `battery_soc` (mit `capacity_kwh`); `pv` kommt bewusst aus zwei Entitäten (`apsystems_dach_pv1_power` + `apsystems_dach_pv2_power`), um die Summierung mehrerer Quellen pro Rolle zu zeigen — die Gesamtleistung des Wechselrichters bleibt absichtlich unzugewiesen und erscheint dadurch in der Liste der nicht zugeordneten Entitäten |
| `settings.json` | zusätzlich `device_view_mode: analysis` sowie die Diagnose-/Konfigurations- und Discovery-Tooltip-Anzeige aktiviert |
| `device-map.json` | Positionen für alle zehn Geräte plus eine Kante, damit die Geräte-Karte nicht leer ist |

Bewusst **nicht** abgedeckt: der Automations-Tab (hängt am separaten
Python-Service über MQTT, den dieser Harness nicht startet) und die
gespiegelten Rollen `grid_import`/`grid_export`/`battery_charge`/
`battery_discharge` (würden zweite, redundante Sensoren neben `grid`/
`battery` brauchen).

`users.json` lässt sich mitkopieren, aber dann legt `DASHBOARD_ADMIN_PASSWORD`
keinen Admin mehr an: passt der Hash nicht zum Passwort im Skript, scheitert
schon die Anmeldung.

## Zwei Fallstricke beim Ansehen im Browser

1. **`go:embed` bindet Templates, CSS und JS beim Kompilieren ein.** Ein
   laufendes `go run ./cmd/dashboard` sieht spätere Quelländerungen nie — auch
   keine neu angelegten Dateien. Der Neustart des Smoke-Tests ist deshalb der
   **letzte** Schritt vor einer Sichtprüfung, nicht der erste.
2. **Panel-Skripte (`data-panel-script` in `base.html`) tragen keinen
   `?v=`-Cache-Buster**, anders als `base.css`, `manager.css` und
   `devicemap.page.js`. Der Browser cached sie mit `max-age=86400` und liefert
   sie bei jedem Tab-Klick aus dem Disk-Cache — über Serverneustarts hinweg.
   `Ctrl+Shift+R` hilft nicht, weil der Panel-Loader sie erst nach dem
   `load`-Event nachzieht. Wer eine Änderung an einem Panel-Skript sehen will,
   leert den Cache für die Seite (DevTools → Network → „Disable cache") oder
   umgeht den Panel-Loader ganz: Zustand per `curl -X PUT` setzen (mit
   `X-Forwarded-Proto: https` und dem Cookie aus `$WORK/cookies.txt`) und die
   Seite neu laden. Für Theme und Layout ist genau das jetzt `--theme` bzw.
   `--seed-data`.

## Was geprüft wird

- `/api/v1/topics/samples` liefert den letzten Payload je Topic, sortiert
- ein Topic ohne Nachricht bleibt ohne Payload (statt eines erfundenen Werts)
- `battery_soc_devices` erscheint als verwaltete Konfiguration
- **Kommazahlen lassen sich speichern und zurücklesen** (`charge_efficiency`
  auf 0,95) — die Regression, wegen der dieser Harness entstanden ist
- der Server lehnt einen Wert ausserhalb des Schemas (1,5) mit HTTP 400 ab

Die ersten beiden Punkte nennen Topics aus `fixtures/battery-soc.json` und
laufen deshalb nur ohne `--fixture`. Dazu kommen Prüfungen, sobald vorbelegt
wurde — ein Seed, der stillschweigend nicht ankommt, sieht im Browser wie ein
Renderfehler aus:

- ein geseedetes `layout.json` erscheint mit sichtbaren Karten unter
  `/api/v1/layout`
- ein geseedetes `energy.json` erscheint mit Zuweisungen unter
  `/api/v1/energy/roles`
- `--theme` steht in `/api/v1/settings`
- mit `fixtures/energie-ueberschuss.json` **und** geseedeten Rollen, aber
  **ohne** `--simulate`: die Bilanz unter `/api/v1/energy` geht ohne Rest auf
  (PV 4200 W, Einspeisung 1250 W, Batterie 900 W, „Übriger Verbrauch" 1000 W,
  `gap_applied == 0`) — die exakten Werte, gegen die `--simulate` bewusst
  verstößt, daher laufen beide Prüfungen nie gleichzeitig
- mit `--simulate` (und `fixtures/energie-ueberschuss.json`): die PV-Leistung
  unter `/api/v1/topics/samples` ändert sich innerhalb von zehn Sekunden —
  belegt, dass die simulierten Werte tatsächlich als MQTT-`PUBLISH` auf der
  offenen Verbindung ankommen, nicht nur, dass der Broker-Prozess lebt

### Verlauf-Austausch

Das Skript prüft zusätzlich die Austausch-Schnittstelle: die Ankündigung
unter `/api/v1/history/exchange` nennt Protokollversion 1 und genau die
Stufen `1m` und `5m` (die Rohstufe wird nie getauscht), und ein Angebot des
einen Peers erreicht über zwei gleichzeitig offene SSE-Ströme genau den
anderen — nicht den Absender selbst. Das ist der einzige Prüfschritt, der
zwei Clients am echten Server zusammenbringt; Hub und Ringpuffer sind in
`internal/historyexchange`, der Browser-Teil in `test/history-exchange.test.mjs`
für sich geprüft.

Der Prüfschritt spritzt sein Angebot synthetisch per `curl` ein und
umgeht damit die Verdichtung im Browser. Beim manuellen Test mit zwei
echten Tabs ist das ein Unterschied: 1m-/5m-Sätze entstehen erst, wenn
`history-recorder.js` seine Rohdaten verdichtet, und das geschieht nur
für Daten, die älter als `history_raw_window_hours` sind (Standard 24h,
siehe `compact()` in `history-maintenance.js`). Zwei frisch geöffnete
Tabs haben also zunächst nichts zu tauschen — das ist kein Fehler in der
Austausch-Schnittstelle, sondern eine Folge der Standard-Aufbewahrung.

Der Exit-Code ist 0, wenn alle Prüfungen bestanden sind.
