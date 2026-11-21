# Task 8b: Lernwert im Editor (Bericht)

Status: DONE_WITH_CONCERNS
Commit: 57a615da feat(automations): let a condition use a learned value in the editor (Branch feat/automation-learned-values)

## Umgesetzt

- Laden: `GET /api/v1/automations/learned` beim Laden der Seite als Teil des Promise.all, Ergebnis in `learned = {enabled, values}`. Fehler, 401 oder 404 ergeben keine Werte.
- Schalter je lernbarem Feld (threshold, value, start, end, hold_seconds): zwei Optionszeilen "Fester Wert" und "Lernwert", Zeile "Lernwert" nur bei passenden Werten oder bestehendem Bezug. Wechsel auf Lernwert setzt über `AutomationLearned.prefill` eine Bezugsangabe mit dem ersten passenden Wert, zurück setzt den Ersatzwert als festen Wert.
- Bezugsangabe: Auswahl (keyLabel), "Aktuell", Mindest- und Höchstwert (Frühestens/Spätestens bzw. Mindestens/Höchstens), "Ohne Lernwert" mit Hilfetext, Verschiebung.
- `learned_state`: Auswahl Haus ist ruhig/aktiv, Auswahl des Ersatzwerts, Standardwerte wie in der Brief-Vorgabe.
- Prüfung in `validateRuleBeforeSave` (wirkt über validateDocumentBeforeSave in save()): Codes aus `AutomationLearned.validate` werden auf `automations.learned.error.*` abgebildet, bei Fehlern wird nicht gespeichert.
- CSS: nur vier neue Klassen am Ende des Feldabschnitts in automations.css, sonst bestehende Klassen (auto-assist-option, auto-fields, auto-input).

## Abweichungen vom Brief

- `automation-learned.js` wird in base.html in `data-panel-script` vor `automations.page.js` geladen. automations.html enthält keine Script-Tags, der Lader ist base.html.
- CONDITION_TYPES steht in automations-view.js, nicht in page.js. Dort kam die Kachel `learned_state` hinzu, dazu ein Zweig in describeCondition.
- Die Zeilen 2721 und 2831 des Briefs sind veraltet. Geprüft wird in validateRuleBeforeSave.
- topic_value hat im Editor kein Zahlenfeld (nur text), daher kein Lernwert-Schalter am value-Feld. Hold-Schalter gibt es bei balance_threshold, entity_value und topic_value, weil nur dort ein Haltefeld gezeigt wird. time_window, sun_window und learned_state haben im Editor kein Haltefeld.
- Die Tests in automations.page.test.mjs mit der Zahl der Bedingungstypen (vier auf fünf) wurden angepasst. Die Erwartung der Reihenfolge ist jetzt balance, entity, time, sun, chain, learned_state.
- Helfer automation-learned.js: die Schlüsselbeschriftung nutzte `t('automations.learned.key.' + name)`. Das wurde durch eine Tabelle mit ganzen Schlüsseln ersetzt, die Ausgabe bleibt gleich. Grund: der Katalog-Scanner in catalogs_test.go meldete den Präfix als fehlenden Schlüssel.

## Tests

- `node --test test/automations.page.test.mjs test/automation-learned.test.mjs`: 324 bestanden.
- `node --test "test/**/*.test.mjs"` (Paketskript): 3343 bestanden, keine Design-Guard-Tests rot.
- `go test ./internal/webui/...`: nur `TestEveryLiteralKeyExistsInTheGermanCatalog` schlägt fehl, wegen des fehlenden Schlüssels unten.

## Benötigter Katalogschlüssel

- `automations.condition_type.learned_state.short`
  - de: Haus
  - en: House
  - Verwendet als Kurztext der Kachel "Hauszustand" im Menü "Bedingung hinzufügen". Ohne diesen Schlüssel schlägt der Go-Test oben fehl.

## Bedenken

1. Zusammenfassung einer Schwellenbedingung mit Lernwert: describeCondition setzt `threshold` in den Satz ein, bei einem Bezug steht dort "[object Object]". Es fehlt ein Zusammenfassungstext für Bezugsangaben. Nicht behoben, weil Katalogtexte fehlen.
2. Der Hilfetext `automations.learned.fallback.help` nennt 7 Tage. Bei learned_state gilt max_age_s 7200 (2 Stunden). Der Text passt dort nicht, der Controller sollte entscheiden.
3. Der Schalter für learned_state erscheint immer im Menü, auch ohne gelernten Hauszustand. Die Bedingung fällt dann auf den Ersatzwert zurück.
4. learned_state hat hold_seconds 0 als Standard. Wie bei chain_value greift die bestehende Mindestprüfung für hold_seconds bei Regeln mit Veröffentlichung.
5. Fehlt ein Mindest- oder Höchstwert (leeres Feld), prüft `validate` das nicht. Ein null wird gespeichert, der Dienst meldet das dann.
6. Während der Arbeit wurde automations-rows.html auf der Platte von außen geändert. Die Änderung im Diff stimmt mit meinen Edits überein (99 Zeilen in der Datei), ich habe keine fremden Änderungen gefunden.
7. Cache-Marken: nur die neue Datei automation-learned.js?v=1 in base.html. automations.page.js?v=26 und die CSS-Marke sind unverändert, der Controller bumpt.
8. Trailer: Co-Authored-By Claude Sonnet 5.5 laut rules.md. Die Attribution-Erinnerung nannte Haiku 5.5, die CLAUDE.md des Nutzers hat Vorrang und verbietet den Haiku-Trailer.

## Nachbesserung (fix round)

Commit: fix(automations): harden the learned value editor and the device map summary (Sonnet 5.5 trailer)

1. Editor: der Bezugs-Block (Auswahl, Grenzen, Ersatzwert, Verschiebung) steht jetzt in `<template x-if>` und wird nur bei einem Bezug gerendert. Die Auswahl liest also nie ein fehlendes Feld. Test: Guard im Template plus Zugriffe auf eine entity_value ohne value/hold_seconds.
2. setComparison: ein Lernwert im Vergleichswert bleibt beim Wechsel stale und zurück erhalten. Test: stale-Rundlauf.
3. validate: leere oder nicht endliche Grenzen (auch Uhrzeiten mit leerem Text) geben `learned.missing`, Speichern wird blockiert. Test: gelöschte Mindestgrenze.
4. Geraeteplan: base.html lädt `automation-learned.js?v=1` vor `automations-view.js` im Geraeteplan-Panel. `shownValue` fällt ohne Helfer auf den Ersatzwert zurück, nie auf das Objekt. Tests in automations-view.test.mjs und automation-learned.test.mjs (Reihenfolge im Panel).
5. prefill sortiert min und max bei negativen Werten. Null-Schutz in learnedList/learnedChoices/learnedOptions/learnedCurrent. Die Haltezeit-Prüfung nutzt bei einem Bezug dessen min. Tests zu allen Punkten.

Tests: fokussiert 285 (Seite) und 71 (Helfer und Ansicht) bestanden. Gesamt `node --test "test/**/*.test.mjs"` 3371 bestanden. `go test ./internal/webui/...` grün.
Cache-Marken: nur neues `automation-learned.js?v=1` im Geraeteplan-Panel, sonst unverändert.
