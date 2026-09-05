// Der Dashboard-Adapter. Geprueft wird nur, was er selbst tut: den
// Schnappschuss auf ein BatteryInput abbilden und die IndexedDB als Leser
// reichen. Die Rechnung dahinter hat battery-card-core.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  vm.runInContext(read('theme.js'), context);
  vm.runInContext(read('energy-model.js'), context);
  vm.runInContext(read('energy-presentation.js'), context);
  vm.runInContext(read('battery-card-core.js'), context);
  vm.runInContext(read('battery-status.js'), context);
  return { factories, window: dom.window, document: dom.window.document };
}

const snapshot = {
  values: { battery_soc: 62, battery_capacity_kwh: 12.8, battery_discharge: 1240, pv: 0, load: 1240 },
  roles: [],
  interpretation: { battery_reserve_percent: 15 },
};

test('inputFrom bildet den Schnappschuss auf ein BatteryInput ab', () => {
  const { factories } = load();
  const input = factories.batteryColumnCard.inputFrom(snapshot, [], 1_757_000_000_000);
  assert.equal(input.soc, 62);
  assert.equal(input.capacity, 12.8);
  assert.equal(input.watts, -1240);
  assert.equal(input.reserve, 15);
  assert.equal(input.nowTs, 1_757_000_000_000);
});

test('inputFrom fällt ohne Interpretation auf die Vorgabe-Reserve zurück', () => {
  const { factories } = load();
  assert.equal(factories.batteryColumnCard.inputFrom({ values: {}, roles: [] }, [], 0).reserve, 10);
});

// 0 in der Interpretation heisst abgeschaltet und darf nicht auf 10
// zurueckfallen.
test('inputFrom übernimmt eine abgeschaltete Reserve', () => {
  const { factories } = load();
  const input = factories.batteryColumnCard.inputFrom(
    { ...snapshot, interpretation: { battery_reserve_percent: 0 } }, [], 0);
  assert.equal(input.reserve, 0);
});

test('inputFrom meldet einen fehlenden Ladestand als null', () => {
  const { factories } = load();
  assert.equal(factories.batteryColumnCard.inputFrom({ values: {}, roles: [] }, [], 0).soc, null);
});

test('inputFrom reicht das eingestellte Zeitfenster an den Kern weiter', () => {
  const { factories } = load();
  const input = factories.batteryTrajectoryCard.inputFrom(snapshot, [], 0, { historyHours: 12, forecastHours: 3 });
  assert.equal(input.historyHours, 12);
  assert.equal(input.forecastHours, 3);
  // Ohne Angabe bleibt es dem Kern-Default überlassen.
  const plain = factories.batteryTrajectoryCard.inputFrom(snapshot, [], 0);
  assert.equal(plain.historyHours, undefined);
});

test('windowHours liest data-battery-window und die optionale Projektion vom Layout-Item', () => {
  const { factories, document } = load();
  const item = document.createElement('div');
  item.setAttribute('data-layout-item-id', 'x');
  item.dataset.batteryWindow = '12';
  const root = document.createElement('section');
  item.append(root);
  const flat = obj => JSON.parse(JSON.stringify(obj));

  assert.deepEqual(flat(factories.batteryTrajectoryCard.windowHours(root)), { historyHours: 12, forecastHours: 12 });

  item.dataset.batteryProjectionWindow = '3';
  assert.deepEqual(flat(factories.batteryTrajectoryCard.windowHours(root)), { historyHours: 12, forecastHours: 3 });

  // Nichts gesetzt -> leeres Objekt, der Kern nimmt seinen Default.
  assert.deepEqual(flat(factories.batteryTrajectoryCard.windowHours(document.createElement('div'))), {});
});

test('historyReader steigt auf die nächste Verdichtungsstufe ab', async () => {
  const { factories, window: win } = load();
  const asked = [];
  win.HistoryStore = {
    readRange: async (tier, series) => {
      asked.push(tier);
      assert.equal(series, 'role:battery_soc');
      return tier === 'raw' ? [{ ts: 1, v: 62 }] : [{ ts: 1, avg: 61 }, { ts: 2, avg: 62 }];
    },
  };
  const rows = await factories.batteryColumnCard.historyReader()(0, 10);
  assert.deepEqual(asked, ['raw', '1m']);
  assert.equal(rows.length, 2);
});

test('historyReader liefert ohne HistoryStore eine leere Reihe', async () => {
  const { factories, window: win } = load();
  win.HistoryStore = undefined;
  assert.deepEqual(JSON.parse(JSON.stringify(await factories.batteryColumnCard.historyReader()(0, 10))), []);
});
