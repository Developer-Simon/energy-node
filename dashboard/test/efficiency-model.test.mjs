// Pure helpers of the efficiency tab. The server computes the numbers, the
// model only picks the state, the status line, the KPI tiles and the day rows.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

function loadModel(lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  installI18n(dom.window, { lang });
  const context = dom.getInternalVMContext();
  for (const name of ['theme.js', 'energy-model.js', 'energy-stats-model.js', 'devicemap-model.js', 'energy-chains.js', 'efficiency-model.js']) {
    vm.runInContext(read(name), context);
  }
  return dom.window.EfficiencyModel;
}

// A chain state as GET /api/v1/energy/... and registry-updated deliver it.
const state = (overrides = {}) => ({
  id: 'plug',
  label: 'Steckdose',
  role: 'battery',
  counting: 'ac',
  direction: 'auto',
  flow: 'down',
  points: [
    { entity_id: 'plug', label: 'Steckdose', value: 500, has_value: true, freshness: 'fresh' },
    { entity_id: 'ac', label: 'Speicher AC', value: 480, has_value: true, freshness: 'fresh' },
  ],
  segments: [{ from: 'plug', to: 'ac', available: true, loss_w: 20, efficiency: 0.96 }],
  no_current_values: false,
  ...overrides,
});

const efficiency = (overrides = {}) => ({
  id: 'plug',
  label: 'Steckdose',
  round_trip_30d: 0.8,
  standby_w_30d: 3.6,
  loss_month_kwh: 12.5,
  month: '2026-11',
  days: [],
  ...overrides,
});

test('stateKey', () => {
  const model = loadModel();
  assert.equal(model.stateKey(state({ role: 'battery', flow: 'down' })), 'charging');
  assert.equal(model.stateKey(state({ role: 'battery', flow: 'up' })), 'discharging');
  assert.equal(model.stateKey(state({ role: 'battery', flow: 'idle' })), 'idle');
  assert.equal(model.stateKey(state({ role: 'battery', flow: 'down', no_current_values: true })), 'no_values');

  const categories = { akku: { base: 'storage', label: 'Akku' } };
  assert.equal(model.stateKey(state({ role: 'custom:akku', flow: 'down' }), categories), 'charging');
  assert.equal(model.stateKey(state({ role: 'custom:akku', flow: 'up' }), categories), 'discharging');
  assert.equal(model.stateKey(state({ role: 'custom:verbraucher', flow: 'down' }), { verbraucher: { base: 'consumer' } }), 'active');

  assert.equal(model.stateKey(state({ role: 'load', flow: 'down' })), 'active');
  assert.equal(model.stateKey(state({ role: 'load', flow: 'up' })), 'active');

  // Without a sign a fixed direction does not say which way the energy goes.
  assert.equal(model.stateKey(state({ role: 'battery', direction: 'fixed_down', flow: 'down' })), 'active');
});

test('statusOf', () => {
  const model = loadModel();
  const stale = model.statusOf(state({ no_current_values: true }));
  assert.equal(stale.tone, 'warn');
  assert.equal(stale.key, 'efficiency.status.no_values');
  assert.match(model.statusOf(state({ no_current_values: true })).key, /no_values$/);

  const substitute = model.statusOf(state({ substitute_from: 'ac' }));
  assert.equal(substitute.tone, 'warn');
  assert.equal(substitute.key, 'efficiency.status.substitute');
  assert.equal(substitute.params.name, 'Speicher AC');

  const current = model.statusOf(state());
  assert.equal(current.tone, 'ok');
  assert.equal(current.key, 'efficiency.status.current');
});

test('overallPercent multiplies all segments', () => {
  const model = loadModel();
  const two = state({
    segments: [
      { available: true, efficiency: 0.97 },
      { available: true, efficiency: 0.92 },
    ],
  });
  assert.equal(model.overallPercent(two), 89.24);

  const missingEfficiency = state({
    segments: [
      { available: true, efficiency: 0.97 },
      { available: true },
    ],
  });
  assert.equal(model.overallPercent(missingEfficiency), null);

  const notAvailable = state({
    segments: [
      { available: true, efficiency: 0.97 },
      { available: false, efficiency: 0.92 },
    ],
  });
  assert.equal(model.overallPercent(notAvailable), null);

  assert.equal(model.overallPercent(state({ segments: [] })), null);
});

test('kpiTiles', () => {
  const model = loadModel();
  const segments = [
    { available: true, efficiency: 0.97 },
    { available: true, efficiency: 0.92 },
  ];
  const tiles = model.kpiTiles(state({ segments }), efficiency(), 'November');
  // Spread into this realm: the model builds its arrays inside the vm context.
  assert.deepEqual([...tiles.map(tile => tile.id)], ['now', 'round_trip', 'standby', 'loss_month']);
  assert.equal(tiles[0].value, '89 %');
  assert.equal(tiles[1].value, '80 %');
  assert.equal(tiles[2].value, '4 W');
  assert.equal(tiles[3].value, '12,5 kWh');
  assert.match(tiles[3].label, /November/);

  const empty = model.kpiTiles(state({ segments: [] }), efficiency({ round_trip_30d: null, standby_w_30d: null }), 'November');
  assert.equal(empty[0].value, '–');
  assert.equal(empty[1].value, '–');
  assert.equal(empty[2].value, '–');
});

test('dayRows', () => {
  const model = loadModel();
  const days = [
    { local: '2026-11-04', loss_kwh: 1, standby_kwh: 0.1, efficiency: 0.9, has_data: true },
    { local: '2026-11-05', loss_kwh: 2, standby_kwh: 0.1, efficiency: 0.8, has_data: true },
    { local: '2026-11-06', loss_kwh: 0, standby_kwh: 0, efficiency: null, has_data: false },
  ];
  const rows = model.dayRows(days);
  assert.equal(rows.length, 3);
  assert.equal(rows[1].width, 100);
  assert.equal(rows[0].width, 50);
  assert.equal(rows[0].lossText, '1,00 kWh');
  assert.equal(rows[0].efficiencyText, '90 %');
  assert.match(rows[0].label, /04/);

  assert.equal(rows[2].lossText, '–');
  assert.equal(rows[2].efficiencyText, '–');
  assert.equal(rows[2].width, 0);
});

test('dayRows without any data divides by nothing', () => {
  const model = loadModel();
  const rows = model.dayRows([
    { local: '2026-11-04', loss_kwh: 0, standby_kwh: 0, efficiency: null, has_data: false },
    { local: '2026-11-05', loss_kwh: 0, standby_kwh: 0, efficiency: null, has_data: false },
  ]);
  for (const row of rows) {
    assert.equal(row.width, 0);
    assert.equal(Number.isFinite(row.width), true);
  }
});

test('dayRows clamps a negative loss to width 0', () => {
  const model = loadModel();
  const rows = model.dayRows([
    { local: '2026-11-04', loss_kwh: 2, standby_kwh: 0, efficiency: 0.9, has_data: true },
    { local: '2026-11-05', loss_kwh: -1, standby_kwh: 0, efficiency: 1.01, has_data: true },
  ]);
  assert.equal(rows[0].width, 100);
  assert.equal(rows[1].width, 0);
});
