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

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.AutomationLearned;
}

// Results come from the vm realm, so they are copied into this realm before strict comparison.
const plain = (v) => (v === undefined ? v : JSON.parse(JSON.stringify(v)));
const raw = load();
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
