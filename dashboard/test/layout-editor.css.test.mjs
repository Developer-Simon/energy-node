// Design-Sperre fuer den Layout-Editor. Kein DOM-Test: jsdom rechnet weder
// Grid noch Container Queries. Geprueft wird der Regeltext gegen die
// Bewegungstabelle der Spec - wer eine Dauer oder Kurve "anpasst", bricht hier.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = (...parts) => fs.readFileSync(path.join(here, '..', ...parts), 'utf8');
const css = read('internal', 'webui', 'static', 'css', 'layout-editor.css');
const base = read('internal', 'webui', 'static', 'css', 'base.css');
const manager = read('internal', 'webui', 'static', 'css', 'manager.css');
const editorHTML = read('internal', 'webui', 'templates', 'layout-editor.html');

test('die Bewegungskurven stehen als Token in base.css', () => {
  assert.match(base, /--layout-ease:\s*cubic-bezier\(\.32,\s*\.72,\s*0,\s*1\)/);
  assert.match(base, /--ease-in-out:\s*cubic-bezier\(\.77,\s*0,\s*\.175,\s*1\)/);
});

test('die Toolbox faehrt in 260ms auf der Entschleunigungskurve auf', () => {
  assert.match(css, /\.layout-toolbox\s*\{[^}]*transition:\s*transform \.26s var\(--layout-ease\)/s);
});

test('das Optionsmodal oeffnet in 200ms und skaliert aus .97', () => {
  assert.match(css, /\.layout-modal-sheet\s*\{[^}]*transform:\s*scale\(\.97\)/s);
  assert.match(css, /\.layout-modal-sheet\s*\{[^}]*transition:[^;]*\.2s var\(--ease-out\)/s);
});

test('das Karten-Chrome erscheint in 160ms und wird auf Touch nicht versteckt', () => {
  assert.match(css, /\.layout-card-chrome\s*\{[^}]*transition:\s*opacity \.16s var\(--ease-out\)/s);
  assert.match(css, /@media \(hover:\s*none\)\s*\{[^}]*\.layout-card-chrome\s*\{[^}]*opacity:\s*1/s);
});

test('die Zielbreite arbeitet mit zoom, damit Container-Queries echt rechnen', () => {
  // Eine blosse Verschmaelerung saehe gleich aus, wuerde die Karten aber in
  // echte Enge zwingen - der Unterschied ist der ganze Punkt.
  assert.match(css, /\.layout-canvas-stage\s*\{[^}]*width:\s*var\(--target-w[^}]*zoom:\s*var\(--target-zoom/s);
  assert.match(css, /\.layout-canvas-stage\s*\{[^}]*transition:\s*width \.28s var\(--ease-in-out\)/s);
});

test('neue Karte und neuer Tab materialisieren in 300ms', () => {
  assert.match(css, /\.layout-edit-slot\.is-entering\s*\{[^}]*animation:\s*card-in \.3s var\(--layout-ease\)/s);
  // .tab.is-fresh lebt in base.css: die Navigation steht bei jedem Aufruf.
  assert.match(base, /\.tab\.is-fresh\s*\{[^}]*animation:\s*tab-in \.3s var\(--layout-ease\)/s);
});

test('jede Bewegung liegt hinter prefers-reduced-motion', () => {
  // Die Regel muss die Bewegungen abschalten, die es wirklich gibt - eine
  // Blockueberschrift allein hat hier schon einmal Sicherheit vorgetaeuscht.
  const block = css.match(/@media \(prefers-reduced-motion:\s*reduce\)\s*\{([\s\S]*)\}/);
  assert.ok(block, 'kein prefers-reduced-motion-Block');
  assert.match(block[1], /\.layout-edit-slot\.is-entering[^{]*\{[^}]*animation:\s*none/s);
  const baseBlock = base.match(/@media \(prefers-reduced-motion:\s*reduce\)\s*\{\s*\.tab\.is-fresh[^}]*\}/);
  assert.ok(baseBlock, '.tab.is-fresh ist in base.css nicht abgeschaltet');
  assert.match(block[1], /\.layout-toolbox[^{]*\{[^}]*transition:\s*none/s);
});

test('der Bearbeitungsmodus haengt am Uebersichts-Panel, nicht am entfernten Layout-Panel', () => {
  assert.doesNotMatch(css, /#layout-panel/);
  assert.match(css, /#overview-panel\[data-mode="view"\][^{]*\.layout-card-chrome[^{]*\{[^}]*display:\s*none/s);
  assert.match(css, /#overview-panel\[data-mode="edit"\][^{]*\[data-view-only\][^{]*\{[^}]*display:\s*none/s);
  assert.match(css, /#overview-panel\[data-mode="edit"\]\s+\.layout-grid\s*\{[^}]*color-mix\(in srgb,\s*var\(--accent\) 4%/s);
});

// --- Deckungsgleichheit Ansicht <-> Editor --------------------------------
// Der Editor darf die Geometrie der Uebersicht nicht anfassen. Diese drei
// Tests sind die Sperre dagegen; sie entstanden aus drei echten Befunden.

test('die Layout-Seite ist fuer das Raster durchsichtig', () => {
  // .layout-grid ordnet nur direkte Kinder an. Der Seiten-Wrapper aus
  // overview.html steht dazwischen - ohne display:contents ist die ganze
  // Uebersicht einspaltig und jedes grid-column: span N wirkungslos.
  assert.match(base, /\[data-layout-page\]\s*\{[^}]*display:\s*contents/s);
});

test('der Ungespeichert-Hinweis laesst sich per hidden abschalten', () => {
  // .layout-status setzt display:inline-flex; eine Autorenregel schlaegt das
  // [hidden]{display:none} des UA-Stylesheets immer. Ohne diese Regel klebt
  // "Ungespeichert" dauerhaft in der Werkzeugleiste.
  assert.match(css, /\.layout-status\[hidden\]\s*\{[^}]*display:\s*none/s);
});

test('der Editor stylt .layout-grid-item nur im Bearbeitungsmodus um', () => {
  // Die Datei wird lazy geladen und bleibt danach im Dokument. Eine
  // ungescopte .layout-grid-item-Regel wuerde die Uebersicht auch nach
  // "Speichern & schliessen" dauerhaft anders aussehen lassen.
  const bare = css.replace(/\/\*[\s\S]*?\*\//g, '');
  for (const rule of bare.matchAll(/(^|\})\s*([^{}@]*\.layout-grid-item[^{}]*)\{/g)) {
    const selector = rule[2].trim();
    assert.match(selector, /\[data-mode="edit"\]/,
      `ungescopte Regel auf .layout-grid-item: ${selector}`);
  }
});

test('der Bearbeitungsmodus laesst das Raster ein Raster bleiben', () => {
  // display:block war die Anpassung an GridStacks absolute Anordnung. Bleibt
  // sie stehen, faellt im Editor der ganze CSS-Grid-Fluss samt
  // Container-Queries weg - und der Moduswechsel verschiebt wieder alles.
  const rule = css.match(/#overview-panel\[data-mode="edit"\]\s+\.layout-grid\s*\{([^}]*)\}/s);
  assert.ok(rule, 'die Editor-Regel auf .layout-grid fehlt');
  assert.doesNotMatch(rule[1], /display\s*:/, 'der Editor darf das display des Rasters nicht anfassen');
});

test('das Kartenmodal zentriert im Fenster, nicht im Editor', () => {
  // position:absolute zentriert im naechsten positionierten Vorfahren - das
  // ist das Panel. Bei langer Uebersicht landet das Modal dann irgendwo im
  // Scrollbereich statt vor den Augen.
  assert.match(css, /\.layout-modal-scrim\s*\{[^}]*position:\s*fixed/s);
});

test('die Werkzeugleiste der Uebersicht steht in base.css, nicht in der Editor-Datei', () => {
  // .layout-toolbar samt "Editieren"-Knopf rendert overview.html bei JEDEM
  // Aufruf; layout-editor.css kommt erst beim ersten Editieren. Lagen die
  // Grundregeln dort, war der Knopf bis dahin ein nackter <button> und sprang
  // beim ersten Klick in Form.
  assert.match(base, /\.layout-toolbar\s*\{/);
  assert.match(base, /\.layout-btn\s*\{/);
  assert.match(base, /\.layout-count\s*\{/);
  assert.match(base, /\.layout-spacer\s*\{/);
  assert.doesNotMatch(css, /\.layout-toolbar\s*\{/);
  assert.doesNotMatch(css, /\.layout-btn\s*\{/);
});

test('der Knopf auf Akzentflaeche nimmt seine Schriftfarbe aus dem Farbschema', () => {
  // #0f1216 war im hellen "tageslicht" schwarz auf Gruen; --accent-ink
  // wechselt mit dem Schema mit (dieselbe Paarung wie .icon-button.primary).
  assert.match(base, /\.layout-btn\.primary\s*\{[^}]*color:\s*var\(--accent-ink\)/s);
  assert.doesNotMatch(base + css, /\.layout-btn[^{]*\{[^}]*#0f1216/s);
});

test('die Hover-Regeln der Knoepfe schlagen das generische button:hover', () => {
  // button:hover:not(:disabled) in base.css hat (0,2,1) und gewinnt gegen ein
  // blosses .layout-btn:hover (0,2,0) - der Speichern-Knopf wurde beim
  // Ueberfahren grau, mit dunkler Schrift darauf.
  for (const [file, text] of [['base.css', base], ['layout-editor.css', css]]) {
    const bare = text.replace(/\/\*[\s\S]*?\*\//g, '');
    for (const rule of bare.matchAll(/(^|\})\s*(\.layout-[a-z-]*(?:btn|chip|tab|item)[^{}]*:hover[^{}]*)\{/g)) {
      assert.match(rule[2].trim(), /:not\(:disabled\)|\.layout-edit-slot:hover/,
        `Hover-Regel ohne :not(:disabled) in ${file}: ${rule[2].trim()}`);
    }
  }
});

test('der Apple-Schalter steht in base.css, damit ihn auch das Optionsmodal bekommt', () => {
  // Das Modal des Editors laedt manager.css nicht - dort blieben die Schalter
  // nackte Checkboxen.
  assert.match(base, /\.settings-toggle\s*\{[^}]*width:\s*3rem/s);
  assert.match(base, /\.settings-toggle-track\s*\{/);
  assert.doesNotMatch(manager, /\.settings-toggle\s*\{/);
});

test('der Trennstrich bekommt eine Hoehe, sonst ist seine Kante 0px hoch', () => {
  assert.match(base, /\.tab-divider\s*\{[^}]*align-self:\s*stretch/s);
});

test('Chrome und Griff werden nicht auf Kartenhoehe gestreckt', () => {
  // base.css streckt jedes Kind einer .layout-grid-item auf die Zeilenhoehe -
  // die Optionsleiste stand dadurch als Band ueber der ganzen Kartenkante.
  assert.match(base, /\.layout-grid-item\s*>\s*\*\s*\{[^}]*height:\s*100%/s);
  assert.match(css, /\.layout-edit-slot\s*>\s*\.layout-card-chrome\s*\{[^}]*height:\s*auto/s);
  assert.match(css, /\.layout-edit-slot\s*>\s*\.layout-resize-grip\s*\{[^}]*height:/s);
});

test('das Modal traegt keine Querscrollleiste', () => {
  // Ohne globales border-box und mit min-width:auto auf 1fr-Spuren brachte ein
  // <input type="number"> seine Eigenbreite mit, sprengte die Spalte um wenige
  // Pixel - und overflow:auto machte daraus eine waagerechte Scrollleiste.
  assert.match(css, /\.layout-modal-field select, \.layout-modal-field input\s*\{[^}]*box-sizing:\s*border-box/s);
  assert.match(css, /\.layout-modal-field-row\s*>\s*\*\s*\{[^}]*min-width:\s*0/s);
});

test('die Ikonen des Editors kommen aus dem globalen Sprite', () => {
  // Kein eigener Ikonensatz im Editor: dieselben <symbol> und derselbe
  // Klassenname wie in Automationen, Verlaeufen und der Uebersicht.
  assert.match(base, /\.automation-icon\s*\{[^}]*stroke:\s*currentColor/s);
  assert.match(editorHTML, /class="automation-icon"[^>]*><use href="#ico-save"/);
  assert.doesNotMatch(editorHTML, /<svg viewBox="0 0 24 24" fill="none"/);
});

test('die Navigationszusaetze stehen in base.css, nicht in der Editor-Datei', () => {
  // layout-editor.css wird erst beim ersten "Editieren" nachgeladen. Der
  // Trennstrich und der Punkt vor Seitennamen gehoeren zur Navigation, die
  // bei jedem Seitenaufruf steht - sie waren dort unsichtbar.
  assert.match(base, /\.tab-divider\s*\{[^}]*border-left/s);
  assert.match(base, /\.tab\.is-page\s*>\s*i\s*\{/);
  assert.match(base, /@keyframes tab-in/);
  assert.doesNotMatch(css, /\.tab-divider\s*\{/);
  assert.doesNotMatch(css, /\.tab\.is-page\s*>\s*i\s*\{/);
});

test('die Toolbox haengt am Fenster, nicht am Dokumentfluss', () => {
  // Als position:absolute stand sie im Panel: wer nach dem Ablegen einer
  // Karte nach unten scrollte und sie erneut oeffnete, sah nichts - sie ging
  // weit oberhalb des Sichtbereichs auf. Fixed plus --toolbox-top (aus
  // syncToolboxOffset()) haelt sie unter der klebenden Werkzeugleiste und
  // laesst sie bis zur Fensterunterkante reichen.
  const rule = css.match(/\.layout-toolbox\s*\{([^}]*)\}/s);
  assert.ok(rule, 'die Toolbox-Regel fehlt');
  assert.match(rule[1], /position:\s*fixed/);
  assert.match(rule[1], /top:\s*var\(--toolbox-top/);
  assert.match(rule[1], /bottom:\s*0/);
});

test('die Links/Rechts-Auswahl der Toolbox ist raus', () => {
  // Sie stammte aus dem Entwurf, in dem beide Seiten verglichen werden
  // sollten. In der Anwendung gibt es nur die rechte.
  assert.doesNotMatch(css, /layout-toolbox-sidepick/);
  assert.doesNotMatch(css, /\.layout-toolbox\[data-side/);
});
