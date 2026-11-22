// Tests for the monitoring page model (part A): groups, order, since text, steps, shade facts, week ranges.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(jsDir, 'monitoring-model.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  // Zahlen kommen aus I18n.formatNumber, hier mit deutschem Tausenderpunkt.
  dom.window.I18n = { formatNumber: (value) => Number(value).toLocaleString('de-DE') };
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.MonitoringModel;
}

// Results come from the vm realm, so they are copied into this realm before strict comparison.
const plain = (v) => (v === undefined ? v : JSON.parse(JSON.stringify(v)));
const raw = load();
const M = Object.fromEntries(Object.entries(raw).map(([name, fn]) => [name, (...args) => plain(fn(...args))]));
const t = (key, params) => (params ? key + JSON.stringify(params) : key);
const tWeeks = (key, p) => {
  if (key === 'monitoring.weeks.range') return p.from + ' bis ' + p.to;
  if (key === 'monitoring.weeks.and') return p.a + ' und ' + p.b;
  return key;
};

test('groups puts new before known and drops empty groups', () => {
  const onlyNew = [{ id: 'a', ack: null }, { id: 'b', ack: null }];
  assert.deepEqual(M.groups(onlyNew), [{ id: 'new', items: onlyNew }]);
  const mixed = [{ id: 'k', ack: { at: 1, user: 'x' } }, { id: 'n', ack: null }];
  assert.deepEqual(M.groups(mixed).map((g) => g.id), ['new', 'known']);
  const onlyKnown = [{ id: 'k', ack: { at: 1, user: 'x' } }];
  assert.deepEqual(M.groups(onlyKnown).map((g) => g.id), ['known']);
});

test('sortNotices orders warn before info inside new', () => {
  const notices = [
    { id: 'info-new', severity: 'info', ack: null, first_seen: 300 },
    { id: 'warn-old', severity: 'warn', ack: null, first_seen: 100 },
    { id: 'warn-new', severity: 'warn', ack: null, first_seen: 200 },
    { id: 'acked', severity: 'warn', ack: { at: 1, user: 'x' }, first_seen: 999 },
  ];
  assert.deepEqual(M.sortNotices(notices).map((n) => n.id), ['warn-new', 'warn-old', 'info-new', 'acked']);
});

test('sinceText says heute, gestern and vor n Tagen', () => {
  const now = 1794000000;
  assert.equal(M.sinceText(now - 3600, now, t), 'monitoring.since.today');
  assert.equal(M.sinceText(now - 90000, now, t), 'monitoring.since.yesterday');
  assert.equal(M.sinceText(now - 5 * 86400, now, t), 'monitoring.since.days{"n":5}');
});

test('steps drop targets without the right but keep the text', () => {
  const notice = {
    code: 'unassigned',
    source: 'energy',
    steps: [{ code: 'assign_roles', target: 'energy.focus', ids: ['sensor.a'] }],
  };
  const withRight = M.steps(notice, { view_energy: true }, t)[0];
  assert.equal(withRight.target, 'energy.focus');
  assert.equal(withRight.label, 'monitoring.step.assign_roles.button');

  const without = M.steps(notice, { view_energy: false }, t)[0];
  assert.equal(without.text, 'monitoring.step.assign_roles');
  assert.equal(without.target, '');
  assert.equal(without.label, '');
  assert.deepEqual(without.ids, ['sensor.a']);
});

test('steps hide role_keep without edit_energy', () => {
  const notice = {
    code: 'role_needed',
    source: 'energy',
    steps: [{ code: 'role_keep', target: 'action.role_keep', ids: ['sensor.a'] }],
  };
  const off = M.steps(notice, { view_energy: true, edit_energy: false }, t)[0];
  assert.equal(off.text, 'monitoring.step.role_keep');
  assert.equal(off.target, '');
  assert.equal(off.label, '');

  const on = M.steps(notice, { edit_energy: true }, t)[0];
  assert.equal(on.target, 'action.role_keep');
  assert.equal(on.label, 'monitoring.step.role_keep.button');
});

test('shade facts read direction and height from the id', () => {
  const notice = {
    id: 'learn:shade:225:15',
    source: 'learn',
    code: 'shade',
    params: { kind: 'shade', from: '14:35', to: '15:49', kwh_lost: 5.7, weeks: [4, 5, 6, 7, 44, 45, 46, 47] },
  };
  const facts = M.facts(notice, t);
  assert.deepEqual(facts.map(([label]) => label), [
    'monitoring.fact.time',
    'monitoring.fact.weeks',
    'monitoring.fact.direction',
    'monitoring.fact.loss',
  ]);
  assert.deepEqual(facts[2], [
    'monitoring.fact.direction',
    'monitoring.fact.direction_value{"az":225,"compass":"monitoring.compass.sw","el":15}',
  ]);
  assert.deepEqual(facts[0], ['monitoring.fact.time', 'monitoring.fact.time_value{"from":"14:35","to":"15:49"}']);
});

test('weekRanges joins runs', () => {
  assert.equal(M.weekRanges([4, 5, 6, 7, 44, 45], tWeeks), '4 bis 7, 44 und 45');
  assert.equal(M.weekRanges([45, 4], tWeeks), '4 und 45');
  assert.equal(M.weekRanges([12], tWeeks), '12');
  assert.equal(M.weekRanges([], tWeeks), '');
});

const NOW = 1794000000;

test('learnedGroups lists all eight site keys and marks missing ones', () => {
  const values = [{ key: 'site/day_start', kind: 'time', value: '06:10', n: 4, at: NOW - 60, changed_at: NOW - 300 }];
  const groups = M.learnedGroups(values, NOW, t);
  assert.equal(groups[0].id, 'site');
  assert.equal(groups[0].total, 8);
  assert.equal(groups[0].have, 1);
  assert.deepEqual(groups[0].rows.map((r) => r.key), [
    'site/day_start', 'site/night_rest', 'site/house_state', 'site/pv_reserve_w', 'site/surplus_left_wh',
    'site/base_w', 'site/battery_full_at', 'site/night_wh',
  ]);
  const dayStart = groups[0].rows[0];
  assert.equal(dayStart.have, true);
  assert.equal(dayStart.value, 'monitoring.value.time{"time":"06:10"}');
  const missing = groups[0].rows[1];
  assert.equal(missing.have, false);
  assert.equal(missing.value, 'monitoring.learned.missing');
  assert.equal(missing.meta, 'monitoring.learned.missing_days');
  assert.equal(groups[0].rows[3].meta, 'monitoring.learned.missing_pv');
});

test('learnedGroups shows the night energy with its unit and the battery time', () => {
  const values = [
    { key: 'site/night_wh', kind: 'number', unit: 'Wh', value: 13200, n: 12, at: NOW - 60 },
    { key: 'site/battery_full_at', kind: 'time', value: '11:15', n: 10, at: NOW - 60 },
  ];
  const rows = M.learnedGroups(values, NOW, t)[0].rows;
  assert.equal(rows[7].key, 'site/night_wh');
  assert.equal(rows[7].have, true);
  assert.equal(rows[7].value, '13.200 Wh');
  assert.equal(rows[6].value, 'monitoring.value.time{"time":"11:15"}');
});

test('learnedGroups orders device values on, off, runtime, power, cycle', () => {
  const values = [
    { key: 'device/pump/usual_runtime_s', kind: 'duration', value: 600, device_name: 'Pumpe', device_icon: 'ico-pump' },
    { key: 'device/pump/cycle_wh', kind: 'number', value: 90, unit: 'Wh', device_name: 'Pumpe', device_icon: 'ico-pump' },
    { key: 'device/pump/usual_on', kind: 'time', value: '06:00', device_name: 'Pumpe', device_icon: 'ico-pump' },
    { key: 'device/pump/op_power_w', kind: 'number', value: 1930, unit: 'W', device_name: 'Pumpe', device_icon: 'ico-pump' },
    { key: 'device/pump/usual_off', kind: 'time', value: '07:00', device_name: 'Pumpe', device_icon: 'ico-pump' },
  ];
  const device = M.learnedGroups(values, NOW, t)[1];
  assert.equal(device.id, 'device/pump');
  assert.equal(device.label, 'Pumpe');
  assert.equal(device.icon, 'ico-pump');
  assert.deepEqual(device.rows.map((r) => r.key.split('/')[2]), ['usual_on', 'usual_off', 'usual_runtime_s', 'op_power_w', 'cycle_wh']);
});

test('origin uses singular for one day', () => {
  const one = M.learnedGroups([{ key: 'site/day_start', kind: 'time', value: '06:10', n: 1, at: NOW - 60, changed_at: NOW - 300 }], NOW, t)[0].rows[0];
  assert.equal(one.meta.split(' · ')[0], 'monitoring.learned.origin.days_one');
  assert.equal(one.meta.split(' · ')[1], 'monitoring.learned.origin.changed{"age":"5 min"}');
  const many = M.learnedGroups([{ key: 'site/day_start', kind: 'time', value: '06:10', n: 3, at: NOW - 60 }], NOW, t)[0].rows[0];
  assert.equal(many.meta.split(' · ')[0], 'monitoring.learned.origin.days{"n":3}');
});

test('valueText formats time, duration, number and state', () => {
  assert.equal(M.valueText({ kind: 'time', value: '22:30' }, t), 'monitoring.value.time{"time":"22:30"}');
  assert.equal(M.valueText({ kind: 'duration', value: 20520 }, t), '5 h 42 min');
  assert.equal(M.valueText({ kind: 'number', value: 1930, unit: 'W' }, t), '1.930 W');
  assert.equal(M.valueText({ kind: 'state', value: 'active' }, t), 'automations.learned.state.active');
  assert.equal(M.valueText({ kind: 'state', value: 'quiet' }, t), 'automations.learned.state.quiet');
});
