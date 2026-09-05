// Der Home-Assistant-Adapter. Wie beim Dashboard-Adapter wird nur seine
// eigene Arbeit geprueft: hass.states -> BatteryInput und der Recorder als
// Verlaufsleser. Gerechnet wird im Kern, und den prueft
// battery-card-core.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.join(here, '..', '..');
const WWW = path.join(repo, 'integrations', 'homeassistant', 'custom_components', 'battery_soc', 'www');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(WWW, 'battery-card-core.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(WWW, 'battery-soc-card.js'), 'utf8'), context);
  return { window: dom.window, document: dom.window.document, Card: dom.window.customElements.get('battery-soc-card') };
}

const hass = {
  states: {
    'sensor.speicher_soc_combined': { state: '62', attributes: { unit_of_measurement: '%', device_class: 'battery' } },
    'sensor.speicher_net_power': { state: '-1240', attributes: { unit_of_measurement: 'W', device_class: 'power' } },
    'sensor.speicher_time_to_empty': { state: '4.5', attributes: { unit_of_measurement: 'h' } },
  },
};
const config = {
  soc_entity: 'sensor.speicher_soc_combined',
  power_entity: 'sensor.speicher_net_power',
  capacity_kwh: 12.8,
  reserve_percent: 15,
};

test('das Custom Element ist registriert und meldet sich bei Lovelace an', () => {
  const { Card, window: win } = load();
  assert.ok(Card, 'battery-soc-card ist nicht registriert');
  assert.ok((win.customCards || []).some(entry => entry.type === 'battery-soc-card'));
});

test('inputFromHass bildet die Zustände auf ein BatteryInput ab', () => {
  const { Card } = load();
  const input = Card.inputFromHass(hass, config, [], 1_757_000_000_000);
  assert.equal(input.soc, 62);
  assert.equal(input.capacity, 12.8);
  assert.equal(input.watts, -1240);
  assert.equal(input.reserve, 15);
});

// Manche Wechselrichter melden Entladen positiv. Die Karte dreht das per
// Option, statt eine zweite Entitaet zu verlangen.
test('inputFromHass dreht das Vorzeichen auf Wunsch', () => {
  const { Card } = load();
  assert.equal(Card.inputFromHass(hass, { ...config, invert_power: true }, [], 0).watts, 1240);
});

test('inputFromHass hält 0 als abgeschaltete Reserve und 10 als Vorgabe', () => {
  const { Card } = load();
  assert.equal(Card.inputFromHass(hass, { ...config, reserve_percent: 0 }, [], 0).reserve, 0);
  const withoutReserve = { ...config };
  delete withoutReserve.reserve_percent;
  assert.equal(Card.inputFromHass(hass, withoutReserve, [], 0).reserve, 10);
});

test('inputFromHass meldet unavailable als fehlenden Ladestand statt als 0', () => {
  const { Card } = load();
  const offline = { states: { 'sensor.speicher_soc_combined': { state: 'unavailable', attributes: {} } } };
  const input = Card.inputFromHass(offline, config, [], 0);
  assert.equal(input.soc, null);
  assert.equal(input.stale, true);
});

test('inputFromHass übernimmt die Restlaufzeit der Integration, wenn eine gesetzt ist', () => {
  const { Card } = load();
  assert.equal(Card.inputFromHass(hass, { ...config, runtime_entity: 'sensor.speicher_time_to_empty' }, [], 0).runtimeHours, 4.5);
  assert.equal(Card.inputFromHass(hass, config, [], 0).runtimeHours, null);
});

test('inputFromHass reicht window und projection_window als Stundenfenster weiter', () => {
  const { Card } = load();
  const paired = Card.inputFromHass(hass, { ...config, window: 12 }, [], 0);
  assert.equal(paired.historyHours, 12);
  assert.equal(paired.forecastHours, 12);

  const split = Card.inputFromHass(hass, { ...config, window: 24, projection_window: 6 }, [], 0);
  assert.equal(split.historyHours, 24);
  assert.equal(split.forecastHours, 6);

  // Ohne Angabe bleibt es beim Kern-Default.
  assert.equal(Card.inputFromHass(hass, config, [], 0).historyHours, undefined);
});

test('historyReader fragt den Recorder und übersetzt seine Antwort', async () => {
  const { Card } = load();
  const calls = [];
  const fake = { callWS: async message => { calls.push(message); return {
    'sensor.speicher_soc_combined': [{ s: '61', lu: 1_756_999_000 }, { s: '62', lu: 1_757_000_000 }],
  }; } };
  const rows = await Card.historyReader(fake, 'sensor.speicher_soc_combined')(1_756_000_000_000, 1_757_000_000_000);
  assert.equal(calls[0].type, 'history/history_during_period');
  assert.deepEqual(JSON.parse(JSON.stringify(calls[0].entity_ids)), ['sensor.speicher_soc_combined']);
  assert.equal(calls[0].minimal_response, true);
  assert.deepEqual(JSON.parse(JSON.stringify(rows)), [{ ts: 1_756_999_000_000, v: 61 }, { ts: 1_757_000_000_000, v: 62 }]);
});

// Ein abgeschalteter Recorder ist kein Kartenfehler - dann gibt es eben
// keinen Verlauf, und dafuer hat der Kern historyMode('none').
test('historyReader verschluckt einen Recorder-Fehler', async () => {
  const { Card } = load();
  const fake = { callWS: async () => { throw new Error('recorder disabled'); } };
  assert.deepEqual(JSON.parse(JSON.stringify(await Card.historyReader(fake, 'sensor.x')(0, 1))), []);
});

test('setConfig verlangt eine SoC-Entität und akzeptiert beide Darstellungen', () => {
  const { Card, document } = load();
  const card = document.createElement('battery-soc-card');
  assert.throws(() => card.setConfig({}), /soc_entity/);
  card.setConfig({ ...config, display: 'trajectory' });
  assert.equal(card.getCardSize(), 4);
  card.setConfig(config);
  assert.equal(card.getCardSize(), 3);
});

test('die Karte zeichnet nach dem ersten hass-Zustand in ihr Shadow-DOM', () => {
  const { Card, document } = load();
  const card = document.createElement('battery-soc-card');
  card.setConfig(config);
  document.body.append(card);
  card.hass = hass;
  const root = card.shadowRoot;
  assert.ok(root.querySelector('style[data-battery-card-style]'), 'Karten-CSS fehlt im Shadow-DOM');
  assert.equal(root.querySelector('.battery-column-pct').textContent, '62 %');
});
