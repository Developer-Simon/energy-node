// overview-values.js zieht die vorhandene Karten-DOM nach, statt sie neu zu
// bauen. Der Unterschied ist der Kern von Stufe 3: das Markup bleibt an
// genau einem Ort (overview.html), und es gibt keinen zweiten Renderer, den
// jede CSS-Aenderung mitnehmen muesste.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const primitives = read('entity-values.js');
const source = read('overview-values.js');

function load(html) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only' });
  vm.runInContext(primitives, dom.getInternalVMContext());
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window;
}

const valueCard = (id, extra = '') => `
  <article class="entity-value-card" data-entity-id="${id}" data-device-class="power" data-unit="W"${extra}>
    <header class="entity-value-top">
      <span class="entity-value-label">Leistung</span>
      <span class="entity-value-dot entity-value-dot-ok"></span>
    </header>
    <div class="entity-value-num"><strong>42</strong><span class="entity-value-unit">W</span></div>
    <small class="entity-command-status"></small>
  </article>`;

test('setzt Wert und Einheit', () => {
  const window = load(valueCard('e1'));
  window.overviewValues.applyEntityValues(window.document.body, {
    e1: { value: '77', has_value: true, available: true, has_availability: true },
  });
  const card = window.document.querySelector('[data-entity-id="e1"]');
  assert.equal(card.querySelector('.entity-value-num strong').textContent, '77');
  assert.equal(card.querySelector('.entity-value-unit').textContent, 'W');
});

test('ohne Wert steht ein Strich und keine Einheit', () => {
  const window = load(valueCard('e1'));
  window.overviewValues.applyEntityValues(window.document.body, {
    e1: { value: '', has_value: false, available: true, has_availability: true },
  });
  const card = window.document.querySelector('[data-entity-id="e1"]');
  assert.equal(card.querySelector('.entity-value-num strong').textContent, '-');
  assert.equal(card.querySelector('.entity-value-unit'), null);
});

test('Statuspunkt folgt available/has_availability', () => {
  const window = load(valueCard('e1'));
  const dot = () => window.document.querySelector('.entity-value-dot').className;

  window.overviewValues.applyEntityValues(window.document.body, { e1: { available: false, has_availability: true } });
  assert.match(dot(), /entity-value-dot-bad/);
  assert.doesNotMatch(dot(), /entity-value-dot-ok/);

  window.overviewValues.applyEntityValues(window.document.body, { e1: { available: true, has_availability: true } });
  assert.match(dot(), /entity-value-dot-ok/);

  window.overviewValues.applyEntityValues(window.document.body, { e1: { available: true, has_availability: false } });
  assert.match(dot(), /entity-value-dot-unknown/);
});

test('stale schaltet die Klasse an der Karte', () => {
  const window = load(valueCard('e1'));
  const card = window.document.querySelector('[data-entity-id="e1"]');

  window.overviewValues.applyEntityValues(window.document.body, { e1: { stale: true } });
  assert.ok(card.classList.contains('stale'));

  window.overviewValues.applyEntityValues(window.document.body, { e1: { stale: false } });
  assert.ok(!card.classList.contains('stale'));
});

// device_class "timestamp" wird von renderTimestamps() in dashboard.js in
// eine relative Angabe umgeschrieben - der Rohwert muss dafuer im
// data-Attribut stehen, nicht nur im Text.
test('timestamp bekommt ein time-Element mit data-relative-timestamp', () => {
  const window = load(valueCard('e1').replace('data-device-class="power"', 'data-device-class="timestamp"'));
  window.overviewValues.applyEntityValues(window.document.body, {
    e1: { value: '2026-08-26T10:00:00Z', has_value: true },
  });
  const time = window.document.querySelector('.entity-value-num time');
  assert.equal(time.dataset.relativeTimestamp, '2026-08-26T10:00:00Z');
});

test('pending sperrt den Knopf, danach wird wieder freigegeben', () => {
  const window = load(`<button class="entity-value-card" data-entity-id="e1" data-device-class="" data-unit=""><div class="entity-value-num"><strong>-</strong></div><small class="entity-command-status"></small></button>`);
  const button = window.document.querySelector('button');

  window.overviewValues.applyEntityValues(window.document.body, { e1: { pending: true } });
  assert.equal(button.disabled, true);
  assert.equal(button.getAttribute('aria-busy'), 'true');

  window.overviewValues.applyEntityValues(window.document.body, { e1: { pending: false } });
  assert.equal(button.disabled, false);
  assert.equal(button.hasAttribute('aria-busy'), false);
});

test('meldet die Statustexte zurueck statt sie selbst zu setzen', () => {
  const window = load(valueCard('e1') + valueCard('e2') + valueCard('e3'));
  const messages = window.overviewValues.applyEntityValues(window.document.body, {
    e1: { pending: true },
    e2: { last_command_result: 'timeout' },
    e3: {},
  });
  assert.equal(messages.e1, 'Warte auf Bestätigung über MQTT ...');
  assert.match(messages.e2, /^Zeitüberschreitung/);
  assert.equal(messages.e3, '');
});

test('Chips einer entity_group werden genauso nachgezogen', () => {
  const window = load(`
    <section class="entity-group-card">
      <div class="entity-group-grid">
        <article class="entity-group-chip" data-entity-id="e1" data-device-class="power" data-unit="W">
          <div class="entity-group-chip-top"><span class="entity-group-chip-dot entity-group-chip-dot-ok"></span></div>
          <div class="entity-group-chip-val">42<span>W</span></div>
          <div class="entity-group-chip-label">Leistung</div>
        </article>
      </div>
    </section>`);
  window.overviewValues.applyEntityValues(window.document.body, {
    e1: { value: '5', has_value: true, available: false, has_availability: true },
  });
  const chip = window.document.querySelector('[data-entity-id="e1"]');
  assert.equal(chip.querySelector('.entity-group-chip-val').textContent, '5W');
  assert.match(chip.querySelector('.entity-group-chip-dot').className, /entity-group-chip-dot-bad/);
});

// Verschwindet eine Entitaet aus der Registry, bleibt ihre Karte bis zum
// naechsten Voll-Aufbau stehen. Sie darf dann nicht die letzte Zahl
// weiterzeigen, als waere sie aktuell.
test('eine Karte ohne Eintrag im Push wird auf unbekannt gesetzt', () => {
  const window = load(valueCard('e1'));
  window.overviewValues.applyEntityValues(window.document.body, {});
  const card = window.document.querySelector('[data-entity-id="e1"]');
  assert.equal(card.querySelector('.entity-value-num strong').textContent, '-');
  assert.match(card.querySelector('.entity-value-dot').className, /entity-value-dot-unknown/);
});

test('ohne Wurzel oder ohne Daten passiert nichts', () => {
  const window = load(valueCard('e1'));
  // Spread kopiert das Ergebnis in ein Objekt dieses (ausseren) Node-Realms:
  // ein {} aus dem vm-Kontext von jsdom hat ein anderes Object.prototype als
  // das {} hier im Test, und assert.deepEqual (= deepStrictEqual unter
  // node:assert/strict) lehnt strukturgleiche Objekte aus verschiedenen
  // Realms ohne den Spread als "nicht referenzgleich" ab.
  assert.deepEqual({ ...window.overviewValues.applyEntityValues(null, { e1: {} }) }, {});
  assert.deepEqual({ ...window.overviewValues.applyEntityValues(window.document.body, null) }, {});
});

const diagnosticsCard = () => `
  <button type="button" class="diagnostics-summary-card">
    <header class="diagnostics-summary-top">
      <span class="diagnostics-summary-label">Diagnosen</span>
      <span class="diagnostics-summary-dot diagnostics-summary-dot-ok"></span>
    </header>
    <p class="diagnostics-summary-ok">Alles in Ordnung</p>
  </button>`;

test('aus "alles in Ordnung" werden Zaehler, wenn eine Warnung auftaucht', () => {
  const window = load(diagnosticsCard());
  window.overviewValues.applyDiagnostics(window.document.body, {
    critical: 0, warning: 2, info: 1, status_class: 'warn',
  });
  const card = window.document.querySelector('.diagnostics-summary-card');
  assert.match(card.querySelector('.diagnostics-summary-dot').className, /diagnostics-summary-dot-warn/);
  assert.equal(card.querySelector('.diagnostics-summary-ok'), null);
  const counts = [...card.querySelectorAll('.diagnostics-summary-count strong')].map(el => el.textContent);
  assert.deepEqual(counts, ['0', '2', '1']);
});

test('und zurueck: ohne Warnungen steht wieder der Satz da', () => {
  const window = load(diagnosticsCard());
  window.overviewValues.applyDiagnostics(window.document.body, { critical: 0, warning: 1, info: 0, status_class: 'warn' });
  window.overviewValues.applyDiagnostics(window.document.body, { critical: 0, warning: 0, info: 0, status_class: 'ok' });
  const card = window.document.querySelector('.diagnostics-summary-card');
  assert.equal(card.querySelector('.diagnostics-summary-ok').textContent, 'Alles in Ordnung');
  assert.equal(card.querySelector('.diagnostics-summary-counts'), null);
  assert.match(card.querySelector('.diagnostics-summary-dot').className, /diagnostics-summary-dot-ok/);
});

test('das schlechteste Geraet erscheint und verschwindet mit den Daten', () => {
  const window = load(diagnosticsCard());
  const card = window.document.querySelector('.diagnostics-summary-card');

  window.overviewValues.applyDiagnostics(window.document.body, {
    critical: 1, warning: 0, info: 0, status_class: 'bad',
    worst_device: { name: 'Speicher', score: 40, status_class: 'warn' },
  });
  const worst = card.querySelector('.diagnostics-summary-worst');
  assert.equal(worst.querySelector('.diagnostics-summary-worst-name').textContent, 'Speicher');
  assert.equal(worst.querySelector('.diagnostics-summary-worst-score').textContent, '40');
  assert.match(worst.className, /diagnostics-summary-worst-warn/);

  window.overviewValues.applyDiagnostics(window.document.body, { critical: 1, warning: 0, info: 0, status_class: 'bad' });
  assert.equal(card.querySelector('.diagnostics-summary-worst'), null);
});

test('ohne Karte oder ohne Daten passiert nichts', () => {
  const window = load('<div></div>');
  window.overviewValues.applyDiagnostics(window.document.body, { status_class: 'ok' });
  const withCard = load(diagnosticsCard());
  withCard.overviewValues.applyDiagnostics(withCard.document.body, null);
  assert.equal(withCard.document.querySelector('.diagnostics-summary-ok').textContent, 'Alles in Ordnung');
});

test('Delta-Modus fasst nur die genannten Karten an', () => {
  const window = load(valueCard('e1') + valueCard('e2') + valueCard('e3'));
  const before = window.document.querySelector('[data-entity-id="e3"] .entity-value-num strong').textContent;

  window.overviewValues.applyEntityValues(window.document.body, {
    e2: { value: '99', has_value: true, available: true, has_availability: true },
  }, true);

  assert.equal(window.document.querySelector('[data-entity-id="e2"] .entity-value-num strong').textContent, '99');
  assert.equal(window.document.querySelector('[data-entity-id="e3"] .entity-value-num strong').textContent, before);
});

test('Voll-Modus (Vorgabe) fasst weiter alle Karten an', () => {
  const window = load(valueCard('e1') + valueCard('e2'));
  window.overviewValues.applyEntityValues(window.document.body, {
    e1: { value: '5', has_value: true, available: true, has_availability: true },
  });
  // e2 fehlt im Push -> im Voll-Modus MISSING -> "-"
  assert.equal(window.document.querySelector('[data-entity-id="e2"] .entity-value-num strong').textContent, '-');
});
