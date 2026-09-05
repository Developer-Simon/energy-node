// device-tile-values.js zieht die vorhandene Kachel-DOM nach, statt sie neu
// zu bauen. Das Markup bleibt damit an genau einem Ort (device-tile.html);
// es gibt keinen zweiten Renderer, den jede CSS-Aenderung mitnehmen muesste.
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
const source = read('device-tile-values.js');

function load(html) {
  const dom = new JSDOM(`<!doctype html><html><body>${html}</body></html>`, { runScripts: 'outside-only' });
  vm.runInContext(primitives, dom.getInternalVMContext());
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window;
}

const apply = (window, entities) => window.deviceTileValues.applyDeviceTiles(window.document.body, entities);

// So rendert device-tile.html eine gewoehnliche Sensorzeile.
const sensorRow = (id, { unit = 'W', deviceClass = 'power', value = '42', stale = false } = {}) => `
  <article class="device-tile" data-device-id="node">
    <div class="device-tile-entities">
      <div class="device-tile-entity" data-entity-id="${id}" data-component="sensor" data-device-class="${deviceClass}" data-unit="${unit}">
        <div><strong>Leistung</strong><small>sensor</small></div>
        <span class="device-tile-entity-value${stale ? ' stale' : ''}">${value}${unit ? ` ${unit}` : ''}</span>
        <span class="entity-icon" role="img" aria-label="Leistung"></span>
        <small class="entity-command-status"></small>
      </div>
    </div>
  </article>`;

// Und so eine schaltbare Zeile: data-command-value traegt den aktuellen
// Wert, aus dem device-tile.js die Toggle-Nutzlast ableitet.
const switchRow = (id, value = 'ON') => `
  <div class="device-tile-entity" data-entity-id="${id}" data-component="switch" data-device-class="" data-unit="">
    <div><strong>Relais</strong><small>switch</small></div>
    <span class="device-tile-entity-value">${value}</span>
    <button class="entity-command-button" type="button" data-entity-id="${id}" data-command-value="${value}" data-payload-on="ON" data-payload-off="OFF"></button>
    <small class="entity-command-status"></small>
  </div>`;

const numberRow = (id, value = '30') => `
  <div class="device-tile-entity device-tile-entity-number" data-entity-id="${id}" data-component="number" data-device-class="" data-unit="%">
    <div><strong>Sollwert</strong><small>number</small></div>
    <div class="entity-number-control">
      <input type="range" class="entity-number-slider" data-command-type="number" data-entity-id="${id}" value="${value}" min="0" max="100"><output>${value}</output>
    </div>
    <small class="entity-command-status"></small>
  </div>`;

test('Wert und Einheit stehen wie im Template im selben Span', () => {
  const window = load(sensorRow('e1'));
  apply(window, { e1: { value: '77', has_value: true, has_availability: true, available: true } });
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, '77 W');
});

test('ohne Einheit steht nur der Wert', () => {
  const window = load(sensorRow('e1', { unit: '' }));
  apply(window, { e1: { value: 'ON', has_value: true } });
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, 'ON');
});

test('ohne Wert steht ein Strich und keine Einheit', () => {
  const window = load(sensorRow('e1'));
  apply(window, { e1: { value: '', has_value: false } });
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, '-');
});

// Eine Entitaet, die der Push nicht mehr kennt, darf nicht ihre letzte Zahl
// weiterzeigen, als waere sie aktuell.
test('eine im Push fehlende Entitaet faellt auf den Strich zurueck', () => {
  const window = load(sensorRow('e1'));
  apply(window, {});
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, '-');
});

test('device_class timestamp behaelt den Rohwert im time-Element', () => {
  const window = load(sensorRow('e1', { unit: '', deviceClass: 'timestamp' }));
  apply(window, { e1: { value: '2026-08-28T10:00:00Z', has_value: true } });
  const time = window.document.querySelector('.device-tile-entity-value time');
  assert.equal(time.dataset.relativeTimestamp, '2026-08-28T10:00:00Z');
});

test('stale wandert auf den Wert-Span und wieder weg', () => {
  const window = load(sensorRow('e1'));
  const span = () => window.document.querySelector('.device-tile-entity-value');
  apply(window, { e1: { value: '1', has_value: true, stale: true } });
  assert.ok(span().classList.contains('stale'));
  apply(window, { e1: { value: '1', has_value: true, stale: false } });
  assert.ok(!span().classList.contains('stale'));
});

// Die eine Klasse, die vom *Wert* abhaengt: device-tile.html verbreitert die
// Zeile, sobald ein einheitenloser Sensor laenger als 20 Zeichen wird.
// Ohne das hier spraenge das Raster erst beim naechsten Tausch.
test('langer einheitenloser Text verbreitert die Zeile', () => {
  const window = load(sensorRow('e1', { unit: '', value: 'kurz' }));
  const row = () => window.document.querySelector('.device-tile-entity');

  apply(window, { e1: { value: 'x'.repeat(25), has_value: true } });
  assert.ok(row().classList.contains('device-tile-entity-text'));

  apply(window, { e1: { value: 'kurz', has_value: true } });
  assert.ok(!row().classList.contains('device-tile-entity-text'));
});

test('mit Einheit bleibt die Zeile schmal, egal wie lang der Wert ist', () => {
  const window = load(sensorRow('e1', { unit: 'W', value: '1' }));
  apply(window, { e1: { value: 'x'.repeat(25), has_value: true } });
  assert.ok(!window.document.querySelector('.device-tile-entity').classList.contains('device-tile-entity-text'));
});

// Echte text-Entitaeten haben die Klasse im Template fest - sie darf nicht
// vom Wert abhaengig wieder verschwinden.
test('eine text-Entitaet behaelt ihre Breite auch bei kurzem Wert', () => {
  const window = load(`<div class="device-tile-entity device-tile-entity-text" data-entity-id="e1" data-component="text" data-device-class="" data-unit=""><span class="device-tile-entity-value">x</span></div>`);
  apply(window, { e1: { value: 'ok', has_value: true } });
  assert.ok(window.document.querySelector('.device-tile-entity').classList.contains('device-tile-entity-text'));
});

// data-command-value ist kein Anzeigewert: device-tile.js leitet daraus die
// Toggle-Nutzlast ab. Bleibt es stehen, schaltet der naechste Klick in die
// falsche Richtung.
test('der Schaltknopf bekommt den neuen Wert fuer die Toggle-Nutzlast', () => {
  const window = load(switchRow('e1', 'ON'));
  apply(window, { e1: { value: 'OFF', has_value: true } });
  assert.equal(window.document.querySelector('.entity-command-button').dataset.commandValue, 'OFF');
});

test('pending sperrt den Schaltknopf, danach wird er wieder frei', () => {
  const window = load(switchRow('e1'));
  const button = () => window.document.querySelector('.entity-command-button');

  apply(window, { e1: { value: 'ON', has_value: true, pending: true } });
  assert.equal(button().disabled, true);
  assert.equal(button().getAttribute('aria-busy'), 'true');

  apply(window, { e1: { value: 'ON', has_value: true, pending: false } });
  assert.equal(button().disabled, false);
  assert.equal(button().getAttribute('aria-busy'), null);
});

test('der Zahlenregler folgt dem Wert samt output', () => {
  const window = load(numberRow('e1', '30'));
  apply(window, { e1: { value: '55', has_value: true } });
  assert.equal(window.document.querySelector('.entity-number-slider').value, '55');
  assert.equal(window.document.querySelector('output').textContent, '55');
});

// Der Push kommt bis zu einmal je Sekunde, ein Ziehen dauert laenger. Beim
// Tausch war das kein Thema (htmx ersetzte das Element ohnehin); beim
// Nachziehen wuerde der Regler unter der Hand wegspringen.
test('ein Regler mit Fokus wird nicht angefasst', () => {
  const window = load(numberRow('e1', '30'));
  const slider = window.document.querySelector('.entity-number-slider');
  slider.focus();
  apply(window, { e1: { value: '55', has_value: true } });
  assert.equal(slider.value, '30');
});

test('die Statustexte kommen als Abbildung zurueck, nicht in die DOM', () => {
  const window = load(switchRow('e1'));
  const messages = apply(window, { e1: { value: 'ON', has_value: true, pending: true } });
  assert.equal(messages.e1, window.entityValues.PENDING_MESSAGE);
  assert.equal(window.document.querySelector('.entity-command-status').textContent, '');
});

test('ohne entities passiert nichts', () => {
  const window = load(sensorRow('e1'));
  // In ein Objektliteral dieses Realms spreizen: node:assert/strict vergleicht
  // sonst die Prototypen, und die eines im vm-Kontext gebauten {} sind nicht
  // dieselben wie hier. Gemeint ist nur "leere Abbildung".
  assert.deepEqual({ ...window.deviceTileValues.applyDeviceTiles(window.document.body, null) }, {});
  assert.equal(window.document.querySelector('.device-tile-entity-value').textContent, '42 W');
});

test('Delta-Modus fasst nur die genannten Zeilen an', () => {
  const window = load(sensorRow('e1') + sensorRow('e2') + sensorRow('e3'));
  const before = window.document.querySelector('[data-entity-id="e3"] .device-tile-entity-value').textContent;

  window.deviceTileValues.applyDeviceTiles(window.document.body, {
    e2: { value: '99', has_value: true, available: true, has_availability: true },
  }, true);

  assert.equal(window.document.querySelector('[data-entity-id="e2"] .device-tile-entity-value').textContent, '99 W');
  assert.equal(window.document.querySelector('[data-entity-id="e3"] .device-tile-entity-value').textContent, before);
});

test('Voll-Modus (Vorgabe) fasst weiter alle Zeilen an', () => {
  const window = load(sensorRow('e1') + sensorRow('e2'));
  window.deviceTileValues.applyDeviceTiles(window.document.body, {
    e1: { value: '5', has_value: true, available: true, has_availability: true },
  });
  // e2 fehlt im Push -> im Voll-Modus MISSING -> "-"
  assert.equal(window.document.querySelector('[data-entity-id="e2"] .device-tile-entity-value').textContent, '-');
});
