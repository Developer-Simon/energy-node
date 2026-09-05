// compact-card-values.js zieht die read-only Kompakt-Karten des Geraete-Tabs
// nach. Das Markup bleibt an genau einem Ort (compact-card in devices.html);
// diese Tests halten fest, dass das Nachziehen dieselbe DOM ergibt wie der
// Server-Render.
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
const source = read('compact-card-values.js');

function load(html) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only' });
  vm.runInContext(primitives, dom.getInternalVMContext());
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window;
}

const apply = (window, entities, delta = false) => window.compactCardValues.applyCompactCards(window.document.body, entities, delta);

// So rendert compact-card eine Zeile: Wert und Einheit im selben Span, ein
// Leerzeichen dazwischen.
const cardRow = (id, { unit = 'W', deviceClass = 'power', value = '42', stale = false } = {}) => `
  <button class="compact-card is-online" data-device-id="node">
    <span class="compact-card-rows">
      <span class="compact-card-row" data-entity-id="${id}" data-device-class="${deviceClass}" data-unit="${unit}">
        <span class="compact-card-row-label">Leistung</span>
        <span class="compact-card-row-value${stale ? ' stale' : ''}">${value}${unit ? ` ${unit}` : ''}</span>
      </span>
    </span>
  </button>`;

test('Wert und Einheit stehen wie im Template im selben Span', () => {
  const window = load(cardRow('e1'));
  apply(window, { e1: { value: '77', has_value: true } });
  assert.equal(window.document.querySelector('.compact-card-row-value').textContent, '77 W');
});

test('ohne Einheit steht nur der Wert', () => {
  const window = load(cardRow('e1', { unit: '' }));
  apply(window, { e1: { value: 'ON', has_value: true } });
  assert.equal(window.document.querySelector('.compact-card-row-value').textContent, 'ON');
});

// Der Halbgeviertstrich (U+2013), nicht der Bindestrich - so rendert das
// Template den fehlenden Wert.
test('ohne Wert steht der Halbgeviertstrich des Templates, keine Einheit', () => {
  const window = load(cardRow('e1'));
  apply(window, { e1: { value: '', has_value: false } });
  assert.equal(window.document.querySelector('.compact-card-row-value').textContent, '–');
});

test('stale wird gesetzt und wieder entfernt', () => {
  const window = load(cardRow('e1'));
  apply(window, { e1: { value: '5', has_value: true, stale: true } });
  assert.ok(window.document.querySelector('.compact-card-row-value').classList.contains('stale'));
  apply(window, { e1: { value: '6', has_value: true, stale: false } });
  assert.ok(!window.document.querySelector('.compact-card-row-value').classList.contains('stale'));
});

// Der Rohwert muss im data-Attribut ueberleben, weil renderTimestamps() in
// dashboard.js den Text spaeter durch eine relative Angabe ersetzt.
test('ein Zeitstempel behaelt den Rohwert im data-Attribut', () => {
  const window = load(cardRow('e1', { deviceClass: 'timestamp', unit: '' }));
  apply(window, { e1: { value: '2026-08-30T10:00:00Z', has_value: true } });
  const time = window.document.querySelector('.compact-card-row-value time');
  assert.equal(time.dataset.relativeTimestamp, '2026-08-30T10:00:00Z');
});

// Fehlt eine Entitaet im vollen Push, gilt sie als unbekannt - nicht als
// unveraendert. Dieselbe Fallunterscheidung wie in device-tile-values.js.
test('eine im vollen Push fehlende Entitaet faellt auf den Strich zurueck', () => {
  const window = load(cardRow('e1'));
  apply(window, {});
  assert.equal(window.document.querySelector('.compact-card-row-value').textContent, '–');
});

test('im Delta-Modus werden nur die genannten Zeilen angefasst', () => {
  const window = load(cardRow('e1', { value: '42' }) + cardRow('e2', { value: '10' }));
  apply(window, { e1: { value: '99', has_value: true } }, true);
  const [first, second] = window.document.querySelectorAll('.compact-card-row-value');
  assert.equal(first.textContent, '99 W');
  assert.equal(second.textContent, '10 W');
});
