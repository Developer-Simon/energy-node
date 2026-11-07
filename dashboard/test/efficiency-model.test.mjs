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

function loadModelDom(lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  installI18n(dom.window, { lang });
  const context = dom.getInternalVMContext();
  for (const name of ['theme.js', 'energy-model.js', 'energy-stats-model.js', 'devicemap-model.js', 'energy-chains.js', 'efficiency-model.js']) {
    vm.runInContext(read(name), context);
  }
  return { model: dom.window.EfficiencyModel, dom };
}

function loadModel(lang = 'de') {
  return loadModelDom(lang).model;
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

// Plain copies: objects built inside the vm realm fail deepStrictEqual on prototypes.
const plain = value => JSON.parse(JSON.stringify(value));

const finding = overrides => ({
  kind: 'drift',
  chain_id: 'plug',
  chain_label: 'Steckdose',
  since: '2026-11-06T10:00:00Z',
  from: 'plug',
  to: 'ac',
  ...overrides,
});

test('statusOf ranks findings over substitute and learning', () => {
  const model = loadModel();

  const one = model.statusOf(state({ findings: [finding()], substitute_from: 'ac', learning: true }));
  assert.equal(one.tone, 'warn');
  assert.equal(one.key, 'chains.finding.drift');
  assert.match(one.text, /Steckdose/);

  const two = plain(model.statusOf(state({ findings: [finding(), finding({ kind: 'sign' })] })));
  assert.equal(two.tone, 'warn');
  assert.equal(two.key, 'efficiency.status.findings');
  assert.equal(two.params.n, 2);
  assert.match(two.text, /2 Auffälligkeiten/);

  const substitute = model.statusOf(state({ substitute_from: 'ac' }));
  assert.equal(substitute.tone, 'warn');
  assert.equal(substitute.key, 'efficiency.status.substitute');
  assert.equal(substitute.params.name, 'Speicher AC');

  const stale = model.statusOf(state({ no_current_values: true }));
  assert.equal(stale.tone, 'warn');
  assert.equal(stale.key, 'efficiency.status.no_values');

  const learning = model.statusOf(state({ learning: true }));
  assert.equal(learning.tone, 'muted');
  assert.equal(learning.key, 'efficiency.status.learning');
  assert.equal(learning.text, 'Lernt noch');
  assert.doesNotMatch(learning.text, /plausibel/i);

  const ok = model.statusOf(state());
  assert.equal(ok.tone, 'ok');
  assert.equal(ok.key, 'efficiency.status.plausible');
  assert.equal(ok.text, 'Plausibel');
});

test('curveSeries names directions by chain type', () => {
  const model = loadModel();
  const curve = {
    down: [{ power_w: 500, efficiency: 0.96 }, { power_w: 250, efficiency: 0.9612 }],
    up: [{ power_w: 300, efficiency: 0.92 }],
  };

  const auto = plain(model.curveSeries(curve, state({ role: 'battery', direction: 'auto' })));
  assert.deepEqual(auto.map(series => [series.name, series.dash]), [['Laden', 0], ['Entladen', 5]]);
  assert.deepEqual(auto[0].data, [[250, 96.1], [500, 96]]);
  assert.deepEqual(auto[1].data, [[300, 92]]);

  const reversed = plain(model.curveSeries(curve, state({ role: 'battery', direction: 'reversed' })));
  assert.deepEqual(reversed.map(series => [series.name, series.dash]), [['Laden', 0], ['Entladen', 5]]);
  assert.deepEqual(reversed[0].data, [[300, 92]]);
  assert.deepEqual(reversed[1].data, [[250, 96.1], [500, 96]]);

  const load = plain(model.curveSeries({ down: curve.down, up: [] }, state({ role: 'load', direction: 'auto' })));
  assert.equal(load.length, 1);
  assert.equal(load[0].name, 'Wirkungsgrad');
  assert.equal(load[0].dash, 0);

  assert.deepEqual(plain(model.curveSeries({ down: [], up: [] }, state())), []);
});

test('curveOptions marks the best range', () => {
  const model = loadModel();
  const series = [{ key: 'charge', name: 'Laden', dash: 0, data: [[250, 96], [500, 95]] }];

  const options = plain(model.curveOptions(series, { from_w: 250, to_w: 500 }, 600));
  assert.equal(options.annotations.xaxis.length, 1);
  assert.equal(options.annotations.xaxis[0].x, 250);
  assert.equal(options.annotations.xaxis[0].x2, 500);
  assert.equal(options.annotations.xaxis[0].label.text, '');
  assert.equal(options.legend.show, false);
  assert.equal(options.chart.animations.enabled, false);

  const none = plain(model.curveOptions(series, null, 600));
  assert.equal(none.annotations.xaxis.length, 0);

  const two = plain(model.curveOptions([...series, { key: 'discharge', name: 'Entladen', dash: 5, data: [] }], null, 0));
  assert.equal(two.legend.show, true);
});

test('curveOptions draws the operating line inside the plotted range and grows the axis to cover it', () => {
  const model = loadModel();
  const series = [{ key: 'charge', name: 'Laden', dash: 0, data: [[250, 96], [500, 95]] }];
  const best = { from_w: 250, to_w: 500 };
  const dashed = options => options.annotations.xaxis.filter(item => item.strokeDashArray);

  const inside = plain(model.curveOptions(series, best, 600, 320));
  assert.equal(dashed(inside).length, 1);
  assert.equal(dashed(inside)[0].x, 320);
  assert.equal(dashed(inside)[0].label.text, '');
  assert.equal(inside.xaxis.max, 500);

  const above = plain(model.curveOptions(series, best, 600, 800));
  assert.equal(dashed(above)[0].x, 800);
  assert.equal(above.xaxis.max, 800);

  const below = plain(model.curveOptions(series, best, 600, 100));
  assert.equal(dashed(below).length, 0);
  assert.equal(below.xaxis.max, 500);

  const none = plain(model.curveOptions(series, best, 600, null));
  assert.equal(dashed(none).length, 0);
});

test('curveMarks follows the same rule as curveOptions for the live update', () => {
  const model = loadModel();
  const series = [{ key: 'charge', name: 'Laden', dash: 0, data: [[250, 96], [500, 95]] }];
  const marks = plain(model.curveMarks(series, { from_w: 250, to_w: 500 }, 980));
  assert.equal(marks.xaxis.filter(item => item.strokeDashArray).length, 1);
  assert.equal(marks.max, 980);
  assert.equal(plain(model.curveMarks(series, null, null)).max, 500);
});

test('livePowerOf takes the absolute power of the counting point, nothing when idle or without values', () => {
  const model = loadModel();
  const live = (overrides = {}) => state({
    counting: 'dc',
    points: [
      { entity_id: 'ac', label: 'AC', value: 1000, has_value: true },
      { entity_id: 'dc', label: 'DC', value: -970, has_value: true },
    ],
    ...overrides,
  });
  assert.equal(model.livePowerOf(live()), 970);
  assert.equal(model.livePowerOf(live({ flow: 'idle' })), null);
  assert.equal(model.livePowerOf(live({ no_current_values: true })), null);
  assert.equal(model.livePowerOf(live({ points: [{ entity_id: 'dc', value: null, has_value: false }] })), null);
  assert.equal(model.livePowerOf(null), null);
});

test('curveLegend lists the best range and the current power only when they are drawn', () => {
  const model = loadModel();
  const series = [{ key: 'charge', name: 'Laden', dash: 0, data: [[250, 96], [500, 95]] }];
  const best = { from_w: 250, to_w: 500 };

  const both = plain(model.curveLegend(series, best, 320));
  assert.deepEqual(both.map(item => item.id), ['best', 'now']);
  assert.equal(both[0].text, 'Bester Bereich 250 bis 500 W');
  assert.equal(both[1].text, 'Aktuell 320 W');

  assert.deepEqual(plain(model.curveLegend(series, null, 320)).map(item => item.id), ['now']);
  assert.deepEqual(plain(model.curveLegend(series, best, null)).map(item => item.id), ['best']);
  assert.deepEqual(plain(model.curveLegend(series, best, 100)).map(item => item.id), ['best']);
  assert.deepEqual(plain(model.curveLegend([], null, null)), []);

  const english = plain(loadModel('en').curveLegend(series, null, 320));
  assert.equal(english[0].text, 'Now 320 W');
});

test('curveOptions reads its colours from the CSS variables and takes the theme mode of the page', () => {
  const { model, dom } = loadModelDom('de');
  const root = dom.window.document.documentElement;
  root.style.setProperty('--text-muted', '#123456');
  root.dataset.theme = 'light';
  const series = [{ key: 'charge', name: 'Laden', dash: 0, data: [[250, 96], [500, 95]] }];

  const light = plain(model.curveOptions(series, null, 600));
  assert.equal(light.theme.mode, 'light');
  assert.equal(light.legend.labels.colors, '#123456');
  assert.equal(light.yaxis.title.style.color, '#123456');

  root.dataset.theme = 'dark';
  assert.equal(plain(model.curveOptions(series, null, 600)).theme.mode, 'dark');
});

test('segmentRows', () => {
  const model = loadModel();
  const chain = state({
    points: [
      { entity_id: 'plug', label: 'Steckdose' },
      { entity_id: 'ac', label: 'Speicher AC' },
      { entity_id: 'dc', label: 'Speicher DC' },
    ],
    // The first segment runs against the point order: the flow is not the order of the rows.
    segments: [
      { from: 'ac', to: 'plug', available: true, loss_w: 20, efficiency: 0.96, normal: { low_w: 6, high_w: 14 } },
      { from: 'ac', to: 'dc', available: true, loss_w: 5, efficiency: 0.9 },
    ],
    tolerances: [{ a: 'dc', b: 'ac', w: 3 }],
  });

  const rows = plain(model.segmentRows(chain));
  assert.equal(rows.length, 2);
  assert.deepEqual([rows[0].a, rows[0].b], ['plug', 'ac']);
  assert.equal(rows[0].fromLabel, 'Steckdose');
  assert.equal(rows[0].toLabel, 'Speicher AC');
  assert.equal(rows[0].normalText, 'Normal 6 bis 14 W');
  assert.equal(rows[0].learned, true);
  assert.equal(rows[0].tolerance, null);

  assert.deepEqual([rows[1].a, rows[1].b], ['ac', 'dc']);
  assert.equal(rows[1].normalText, 'lernt noch');
  assert.equal(rows[1].learned, false);
  assert.deepEqual(rows[1].tolerance, { a: 'dc', b: 'ac', w: 3 });

  const idle = plain(model.segmentRows({ ...chain, flow: 'idle' }));
  assert.equal(idle[0].normalText, 'Normal 6 bis 14 W');
  assert.equal(idle[1].normalText, 'gerade kein Vergleich');
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
