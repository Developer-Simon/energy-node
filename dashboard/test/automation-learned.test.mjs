// Tests for the learned value helper: resolve mirrors services/automation/learned.py,
// the rest covers prefill, choices, validate and keyLabel for the editor.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(jsDir, 'automation-learned.js'), 'utf8');
const vectors = JSON.parse(fs.readFileSync(
  path.join(here, '..', '..', 'services', 'automation', 'tests', 'learned_vectors.json'), 'utf8'));

const viewSource = fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8');

const deCatalog = JSON.parse(fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'catalogs', 'de.json'), 'utf8'));
// Die Oberflaeche uebersetzt mit dem deutschen Katalog, so stehen die Saetze der Karte im Test wie im Editor.
function translate(key, params) {
  let text = Object.prototype.hasOwnProperty.call(deCatalog, key) ? deCatalog[key] : key;
  for (const [name, value] of Object.entries(params || {})) text = text.split(`{${name}}`).join(String(value));
  return text;
}

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  // Monatsnamen des Jahresverlaufs kommen aus der Sprache der Oberflaeche.
  dom.window.I18n = {
    locale: 'de-DE',
    formatDate: (value, options) => new Date(value).toLocaleDateString('de-DE', options),
    t: translate,
  };
  vm.runInContext(source, context);
  // origin formats the age with the view's formatSeconds, so the view loads into the same window.
  vm.runInContext(viewSource, context);
  return dom.window;
}

// Results come from the vm realm, so they are copied into this realm before strict comparison.
const plain = (v) => (v === undefined ? v : JSON.parse(JSON.stringify(v)));
const win = load();
const raw = win.AutomationLearned;
const L = Object.fromEntries(Object.entries(raw).map(([name, fn]) => [name, (...args) => plain(fn(...args))]));
const t = (key, params) => (params ? key + ' ' + JSON.stringify(params) : key);

for (const v of vectors) {
  test(`resolve vector ${v.name}`, () => {
    assert.deepEqual(L.resolve(v.ref, v.kind, v.payload, v.now), v.expect);
  });
}

test('resolve never throws on deeply nested or broken JSON', () => {
  const ref = { learned: 'site/day_start', min: '23:00', max: '01:00', fallback: '00:00' };
  const deep = '['.repeat(200000) + ']'.repeat(200000);
  const nestedObj = '{"value":' + '['.repeat(100000) + ']'.repeat(100000) + ',"at":1794000000}';
  for (const payload of [deep, nestedObj, '{"value":"23:30"', '']) {
    assert.deepEqual(L.resolve(ref, 'time', payload, 1794000060), { value: '00:00', source: 'fallback' });
  }
});

test('prefill time spans one hour each side, max wraps past midnight', () => {
  assert.deepEqual(L.prefill('time', '23:30'), { min: '22:30', max: '00:30', fallback: '23:30' });
  assert.equal(L.prefill('time', null), null);
});

test('prefill duration rounds to 60 s', () => {
  assert.deepEqual(L.prefill('duration', 600), { min: 300, max: 1800, fallback: 600 });
  assert.deepEqual(L.prefill('duration', 100), { min: 60, max: 360, fallback: 120 });
});

test('prefill number uses half and one and a half', () => {
  assert.deepEqual(L.prefill('number', 40), { min: 20, max: 60, fallback: 40 });
});

test('kindOf maps learnable fields to their value kind', () => {
  assert.equal(L.kindOf('balance_threshold', 'hold_seconds'), 'duration');
  assert.equal(L.kindOf('time_window', 'start'), 'time');
  assert.equal(L.kindOf('time_window', 'end'), 'time');
  assert.equal(L.kindOf('balance_threshold', 'threshold'), 'number');
  assert.equal(L.kindOf('topic_value', 'value'), 'number');
  assert.equal(L.kindOf('learned_state', 'learned'), 'state');
  assert.equal(L.kindOf('time_window', 'threshold'), null);
});

test('choices for hold_seconds offers only durations', () => {
  const values = [
    { key: 'device/pump_1/usual_on', kind: 'time' },
    { key: 'device/pump_1/usual_runtime_s', kind: 'duration' },
    { key: 'site/house_state', kind: 'state' },
  ];
  assert.deepEqual(L.choices(values, L.kindOf('balance_threshold', 'hold_seconds')).map((v) => v.key),
    ['device/pump_1/usual_runtime_s']);
  assert.deepEqual(L.choices(values, 'time').map((v) => v.key), ['device/pump_1/usual_on']);
  assert.deepEqual(L.choices(values, 'number'), []);
});

test('isRef recognises a reference object only', () => {
  assert.equal(L.isRef({ learned: 'site/day_start', min: '23:00', max: '01:00', fallback: '00:00' }), true);
  assert.equal(L.isRef('23:00'), false);
  assert.equal(L.isRef(null), false);
  assert.equal(L.isRef([]), false);
  assert.equal(L.isRef(15), false);
});

test('validate accepts a complete time reference', () => {
  assert.deepEqual(L.validate({ learned: 'site/day_start', min: '23:00', max: '01:00', fallback: '00:00' }, 'time'), []);
});

test('validate flags min above max for numbers', () => {
  assert.deepEqual(L.validate({ learned: 'device/pump_1/usual_runtime_s', min: 100, max: 10, fallback: 50 }, 'duration'),
    ['learned.min_max']);
});

test('validate flags a fallback outside the range', () => {
  assert.deepEqual(L.validate({ learned: 'device/pump_1/usual_runtime_s', min: 0, max: 100, fallback: 150 }, 'duration'),
    ['learned.fallback_range']);
  assert.deepEqual(L.validate({ learned: 'site/day_start', min: '23:00', max: '01:00', fallback: '12:00' }, 'time'),
    ['learned.fallback_range']);
});

test('validate flags a missing learned key', () => {
  assert.deepEqual(L.validate({ learned: '', min: 0, max: 100, fallback: 50 }, 'duration'), ['learned.missing']);
  assert.deepEqual(L.validate({ min: 0, max: 100, fallback: 50 }, 'duration'), ['learned.missing']);
});

test('keyLabel names the device for device keys', () => {
  const devices = [{ id: 'pump_1', name: 'Umwaelzpumpe' }];
  assert.equal(L.keyLabel('device/pump_1/usual_on', devices, t),
    'automations.learned.key.usual_on {"device":"Umwaelzpumpe"}');
  assert.equal(L.keyLabel('device/pump_1/usual_runtime_s', { pump_1: { name: 'Umwaelzpumpe' } }, t),
    'automations.learned.key.usual_runtime_s {"device":"Umwaelzpumpe"}');
});

test('keyLabel falls back to the device id when the name is unknown', () => {
  assert.equal(L.keyLabel('device/pump_9/usual_off', [], t),
    'automations.learned.key.usual_off {"device":"pump_9"}');
});

test('keyLabel uses the site keys without a device', () => {
  assert.equal(L.keyLabel('site/day_start', [], t), 'automations.learned.key.day_start {}');
  assert.equal(L.keyLabel('site/house_state', [], t), 'automations.learned.key.house_state {}');
});

test('keyLabel uses the unknown text for other names', () => {
  assert.equal(L.keyLabel('device/pump_1/other', [], t), 'automations.learned.key.unknown {"key":"device/pump_1/other"}');
});

const NOW = 1794000000;
const DAY_S = 86400;

test('origin fallback names the fallback without an entry', () => {
  assert.equal(L.origin('fallback', null, NOW, t), 'automations.learned.origin.fallback');
});

test('origin learned from days names n, age and the previous value', () => {
  const entry = { key: 'device/wp/usual_on', kind: 'time', value: '07:05', n: 9, at: NOW, changed_at: NOW - 3 * DAY_S, prev: '06:50' };
  assert.equal(L.origin('learned', entry, NOW, t), 'automations.learned.origin.days {"n":9,"age":"3 d","prev":"06:50"}');
});

test('origin learned from runs without a previous value uses the runs_new key', () => {
  const entry = { key: 'device/x/usual_runtime_s', kind: 'duration', value: 2700, n: 4, at: NOW, changed_at: NOW - 7200 };
  assert.equal(L.origin('learned', entry, NOW, t), 'automations.learned.origin.runs_new {"n":4,"age":"2 h"}');
});

test('origin learned from runs with a previous value shows minutes', () => {
  const entry = { key: 'device/x/usual_runtime_s', kind: 'duration', value: 2700, n: 4, at: NOW, changed_at: NOW - 7200, prev: 1800 };
  assert.equal(L.origin('learned', entry, NOW, t), 'automations.learned.origin.runs {"n":4,"age":"2 h","prev":"30 min"}');
});

test('origin clamped shows the learned value', () => {
  const entry = { key: 'device/x/usual_runtime_s', kind: 'duration', value: 2700, n: 4, at: NOW, changed_at: NOW - 60 };
  assert.equal(L.origin('clamped', entry, NOW, t), 'automations.learned.origin.clamped {"value":"45 min"}');
});

test('origin learned without an entry has no line', () => {
  assert.equal(L.origin('learned', null, NOW, t), '');
});

test('scale of a time across midnight runs the arc from min to max as one beam', () => {
  assert.equal(L.scale('time', '23:00', '01:00', '23:30').marker, 0.25);
  assert.equal(L.scale('time', '23:00', '01:00', '00:30').marker, 0.75);
  // Ausserhalb des Bogens auf die naehere Grenze: 02:00 liegt naeher an 01:00, 21:00 naeher an 23:00.
  assert.equal(L.scale('time', '23:00', '01:00', '02:00').marker, 1);
  assert.equal(L.scale('time', '23:00', '01:00', '21:00').marker, 0);
});

test('scale of a time inside the day', () => {
  assert.equal(L.scale('time', '06:00', '10:00', '08:00').marker, 0.5);
  assert.deepEqual(L.scale('time', '06:00', '10:00', null), { marker: null });
});

test('scale of a number clamps the marker to the bounds', () => {
  assert.deepEqual(L.scale('number', 20, 80, 90), { marker: 1 });
  assert.deepEqual(L.scale('number', 20, 80, 50), { marker: 0.5 });
});

test('scale of a duration and of a state', () => {
  assert.ok(Math.abs(L.scale('duration', 900, 3600, 1800).marker - 0.3333333) < 1e-6);
  assert.equal(L.scale('state', null, null, 'quiet'), null);
  assert.equal(L.scale('time', 'x', '01:00', '08:00'), null);
});

test('validate treats empty or non finite bounds as missing, also for times', () => {
  assert.deepEqual(L.validate({ learned: 'site/day_start', min: '', max: '01:00', fallback: '00:00' }, 'time'), ['learned.missing']);
  assert.deepEqual(L.validate({ learned: 'device/pump_1/usual_runtime_s', min: null, max: 100, fallback: 50 }, 'duration'), ['learned.missing']);
  assert.deepEqual(L.validate({ learned: 'device/pump_1/usual_runtime_s', min: 0, max: NaN, fallback: 50 }, 'duration'), ['learned.missing']);
});

test('prefill sorts the bounds of a negative number so min does not pass max', () => {
  assert.deepEqual(L.prefill('number', -40), { min: -60, max: -20, fallback: -40 });
  assert.deepEqual(L.validate({ learned: 'site/x', ...L.prefill('number', -40) }, 'number'), []);
});

// Der Geraeteplan laedt den Lernwert-Helfer auch; die Zusammenfassung braucht ihn in beiden Plaenen.
test('the device map panel loads the learned helper before the automation view', () => {
  const base = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'base.html'), 'utf8');
  const line = base.split('\n').find((entry) => entry.includes('devicemap.page.js?v='));
  assert.ok(line, 'devicemap panel script list not found');
  assert.match(line, /automation-learned\.js\?v=\d+/, 'helper missing in device map panel');
  assert.ok(line.indexOf('automation-learned.js') < line.indexOf('automations-view.js'), 'helper must come before the view');
});

// Jahresverlauf: 30 Wochen ab 2026-W01, Tiefstwert in Januar (W03), Hoechstwert im Juli (W30).
const yearWeeks = (count, valueOf) => Array.from({ length: count }, (_, i) => {
  const week = `2026-W${String(i + 1).padStart(2, '0')}`;
  return { week, value: valueOf(i + 1) };
});

test('yearLine names the month of the highest and the lowest week', () => {
  const year = yearWeeks(30, (w) => (w === 3 ? '05:10' : w === 30 ? '22:30' : '12:00'));
  assert.equal(L.yearLine({ kind: 'time', year }, t),
    'automations.learned.year.line {"high_month":"Juli","high":"22:30","low_month":"Januar","low":"05:10"}');
});

test('yearLine is empty under 8 weeks', () => {
  const year = yearWeeks(5, (w) => `0${w}:00`);
  assert.equal(L.yearLine({ kind: 'time', year }, t), '');
  assert.equal(L.yearLine({ kind: 'time' }, t), '');
});

test('yearLine formats number values without the clock', () => {
  const year = yearWeeks(10, (w) => (w === 2 ? 900 : w === 7 ? 1700 : 1200));
  assert.equal(L.yearLine({ kind: 'number', year }, t),
    'automations.learned.year.line {"high_month":"Februar","high":"1700","low_month":"Januar","low":"900"}');
});

test('yearPlot draws one point per week and needs 8 weeks', () => {
  const year = yearWeeks(30, (w) => 10 + w);
  const plot = L.yearPlot('number', 0, 100, { year });
  assert.equal(plot.split(' ').length, 30);
  assert.equal(L.yearPlot('number', 0, 100, { year: yearWeeks(5, () => 50) }), null);
  assert.equal(L.yearPlot('number', 0, 100, null), null);
});

test('choices filter number values by unit, so thresholds offer W only', () => {
  const values = [
    { key: 'site/pv_reserve_w', kind: 'number', unit: 'W' },
    { key: 'device/wb/op_power_w', kind: 'number', unit: 'W' },
    { key: 'device/wb/cycle_wh', kind: 'number', unit: 'Wh' },
    { key: 'site/surplus_left_wh', kind: 'number', unit: 'Wh' },
  ];
  const kind = L.kindOf('balance_threshold', 'threshold');
  assert.deepEqual(L.choices(values, kind, 'W').map((v) => v.key),
    ['site/pv_reserve_w', 'device/wb/op_power_w']);
  assert.deepEqual(L.choices(values, kind).length, 4);
});

test('keyLabel names the new energy and power keys', () => {
  assert.equal(L.keyLabel('device/wb/op_power_w', [{ id: 'wb', name: 'Wallbox' }], t),
    'automations.learned.key.op_power_w {"device":"Wallbox"}');
  assert.equal(L.keyLabel('device/wb/cycle_wh', [], t), 'automations.learned.key.cycle_wh {"device":"wb"}');
  assert.equal(L.keyLabel('site/pv_reserve_w', [], t), 'automations.learned.key.pv_reserve_w {}');
  assert.equal(L.keyLabel('site/surplus_left_wh', [], t), 'automations.learned.key.surplus_left_wh {}');
});

test('valueSuffix adds the unit and the estimate text, nothing for a measured duration', () => {
  assert.equal(L.valueSuffix({ unit: 'W', estimate: true }, 'number', t), ' W (automations.learned.estimate)');
  assert.equal(L.valueSuffix({ unit: 'W' }, 'number', t), ' W');
  assert.equal(L.valueSuffix({ estimate: true }, 'duration', t), ' (automations.learned.estimate)');
  assert.equal(L.valueSuffix(null, 'number', t), '');
});

test('origin adds the other programs of a cycle energy value', () => {
  const entry = { kind: 'number', value: 900, n: 5, at: 1000, changed_at: 1000, programs: [1200, 800.4] };
  const line = L.origin('learned', entry, 1000 + 3600, t);
  assert.match(line, /automations\.learned\.origin\.days_new /);
  assert.match(line, / automations\.learned\.origin\.programs \{"programs":"1200, 800"\}$/);
  assert.equal(L.origin('learned', { ...entry, programs: undefined }, 1000 + 3600, t).endsWith('}'), true);
  assert.equal(L.origin('fallback', null, 0, t), 'automations.learned.origin.fallback');
});

test('kindOf maps the learned_value fields to their kind', () => {
  assert.equal(L.kindOf('learned_value', 'learned'), 'number');
  assert.equal(L.kindOf('learned_value', 'threshold'), 'number');
  assert.equal(L.kindOf('learned_value', 'hold_seconds'), 'duration');
  assert.equal(L.kindOf('learned_value', 'fallback'), null);
  assert.equal(L.kindOf('learned_value', 'comparison'), null);
});

test('unitOfKey reads the unit from the key suffix', () => {
  assert.equal(L.unitOfKey('site/pv_reserve_w'), 'W');
  assert.equal(L.unitOfKey('device/wb/cycle_wh'), 'Wh');
  assert.equal(L.unitOfKey('site/house_state'), '');
});

test('a learned_value threshold offers only values in the unit of the learned key', () => {
  const values = [
    { key: 'site/pv_reserve_w', kind: 'number', unit: 'W' },
    { key: 'device/wb/cycle_wh', kind: 'number', unit: 'Wh' },
  ];
  const kind = L.kindOf('learned_value', 'threshold');
  assert.deepEqual(L.choices(values, kind, L.unitOfKey('site/pv_reserve_w')).map((v) => v.key), ['site/pv_reserve_w']);
  assert.deepEqual(L.choices(values, L.kindOf('learned_value', 'learned')).map((v) => v.key),
    ['site/pv_reserve_w', 'device/wb/cycle_wh']);
});

test('a learned_value condition reads as PV-Reserve über 1500 W', () => {
  const condition = { type: 'learned_value', learned: 'site/pv_reserve_w', comparison: 'above', threshold: 1500,
    hysteresis: 0, fallback: 0, max_age_s: 1800, hold_seconds: 0 };
  const card = win.__automationsView.describeCondition(condition, null, [], [], null);
  assert.equal(card.icon, 'ico-learning');
  assert.equal(card.title, 'Lernwert');
  assert.equal(card.summary, 'PV-Reserve über 1500 W');
});

test('a learned_value condition with hysteresis names the hysteresis', () => {
  const condition = { type: 'learned_value', learned: 'site/pv_reserve_w', comparison: 'above', threshold: 1500,
    hysteresis: 100, fallback: 0, max_age_s: 1800, hold_seconds: 0 };
  const card = win.__automationsView.describeCondition(condition, null, [], [], null);
  assert.equal(card.summary, 'PV-Reserve über 1500 W, Hysterese 100');
});

test('a learned_value threshold given as a reference shows the resolved value in its own chip', () => {
  const condition = { type: 'learned_value', learned: 'site/pv_reserve_w', comparison: 'above',
    threshold: { learned: 'device/wb/cycle_wh', min: 800, max: 1200, fallback: 1000 }, fallback: 0, max_age_s: 1800, hold_seconds: 0 };
  const card = win.__automationsView.describeCondition(condition, null, [], [], null);
  assert.equal(card.summary, 'PV-Reserve über 1000 Wh');
});
