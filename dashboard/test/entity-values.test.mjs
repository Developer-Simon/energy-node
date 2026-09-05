// Die Primitive, die sich overview-values.js und device-tile-values.js
// teilen. Sie stehen in einer eigenen Datei, damit die beiden Nachzieh-
// Module nicht dieselbe Fallunterscheidung zweimal treffen - "fehlt im
// Push" und "Befehl haengt" muessen in beiden Ansichten dasselbe heissen.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'entity-values.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window;
}

test('merge fuellt fehlende Felder aus MISSING auf', () => {
  const window = load();
  const merged = window.entityValues.merge({ value: '42', has_value: true });
  assert.equal(merged.value, '42');
  assert.equal(merged.has_value, true);
  assert.equal(merged.available, false);
  assert.equal(merged.has_availability, false);
  assert.equal(merged.stale, false);
  assert.equal(merged.pending, false);
});

// Eine Entitaet, die im Push fehlt, ist nicht "unveraendert" - sie ist
// unbekannt. Sonst zeigte eine geloeschte Entitaet ihre letzte Zahl weiter.
test('merge macht aus undefined ein vollstaendiges Unbekannt', () => {
  const window = load();
  assert.deepEqual(window.entityValues.merge(undefined), window.entityValues.MISSING);
});

test('statusMessage meldet pending und timeout, sonst nichts', () => {
  const { entityValues } = load();
  assert.equal(entityValues.statusMessage(entityValues.merge({ pending: true })), entityValues.PENDING_MESSAGE);
  assert.equal(entityValues.statusMessage(entityValues.merge({ last_command_result: 'timeout' })), entityValues.TIMEOUT_MESSAGE);
  assert.equal(entityValues.statusMessage(entityValues.merge({ last_command_result: 'success' })), '');
});

test('availabilityState unterscheidet unbekannt von offline', () => {
  const { entityValues } = load();
  assert.equal(entityValues.availabilityState(entityValues.merge({})), 'unknown');
  assert.equal(entityValues.availabilityState(entityValues.merge({ has_availability: true, available: true })), 'ok');
  assert.equal(entityValues.availabilityState(entityValues.merge({ has_availability: true, available: false })), 'bad');
});

test('setDotClass tauscht den Zustand aus, statt Klassen anzuhaeufen', () => {
  const window = load();
  const dot = window.document.createElement('span');
  dot.className = 'entity-value-dot entity-value-dot-ok';
  window.entityValues.setDotClass(dot, 'entity-value-dot', 'bad');
  assert.match(dot.className, /entity-value-dot-bad/);
  assert.doesNotMatch(dot.className, /entity-value-dot-ok/);
});

// Der Rohwert muss im data-Attribut ueberleben, weil renderTimestamps() in
// dashboard.js den Text spaeter durch eine relative Angabe ersetzt.
test('timestampNode haelt den Rohwert im data-Attribut fest', () => {
  const window = load();
  const node = window.entityValues.timestampNode(window.document, '2026-08-28T10:00:00Z');
  assert.equal(node.tagName, 'TIME');
  assert.equal(node.dataset.relativeTimestamp, '2026-08-28T10:00:00Z');
  assert.equal(node.textContent, '2026-08-28T10:00:00Z');
});
