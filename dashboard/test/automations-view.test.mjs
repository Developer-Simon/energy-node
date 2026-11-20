// Pure helpers for the automation condition cards. automations-view.js has no
// DOM of its own, so these tests load it into a jsdom window with the real
// German catalog, the way automations-scenario.test.mjs does.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automation-learned.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), context);
  return dom.window.__automationsView;
}

const plain = (value) => JSON.parse(JSON.stringify(value));

const chains = [{
  id: 'balkon', label: 'Balkon',
  points: [
    { entity_id: 'sensor.dc', label: 'DC-Eingang', device_id: 'wr' },
    { entity_id: 'sensor.ac', label: 'Wechselrichter', device_id: 'wr' },
  ],
}];

test('describeCondition chain_finding', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'balkon', kind: 'frozen', points: ['sensor.dc'], min_age_s: 900, once: true },
    null, null, chains,
  );
  assert.equal(card.icon, 'ico-chain');
  assert.equal(card.title, 'Balkon');
  assert.match(card.summary, /Messwert eingefroren/);
  assert.match(card.summary, /15 min/);
  assert.match(card.summary, /einmal je Meldung/i);
  assert.equal(card.detail, 'DC-Eingang');
});

test('describeCondition chain_finding without age, once or points', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'balkon', kind: 'any', points: [], min_age_s: 0, once: false },
    null, null, chains,
  );
  assert.equal(card.summary, 'Jede Meldung');
  assert.equal(card.detail, '');
});

test('describeCondition chain_finding with a missing chain keeps the summary and does not throw', () => {
  const view = load();
  const card = view.describeCondition(
    { type: 'chain_finding', chain_id: 'keller', kind: 'drift', points: [], min_age_s: 0, once: false },
    null, null, [],
  );
  assert.equal(card.icon, 'ico-chain');
  assert.equal(card.title, 'Messkette fehlt');
  assert.match(card.summary, /Verlust außerhalb des Normalbereichs/);
});

test('describeCondition chain_value', () => {
  const view = load();
  const standby = view.describeCondition(
    { type: 'chain_value', chain_id: 'balkon', field: 'standby_w', comparison: 'above', value: 10, hold_seconds: 7200 },
    null, null, chains,
  );
  assert.equal(standby.icon, 'ico-chain');
  assert.equal(standby.title, 'Balkon');
  assert.match(standby.summary, /Standby/);
  assert.match(standby.summary, /10 W/);
  assert.match(standby.summary, /2 h/);

  const efficiency = view.describeCondition(
    { type: 'chain_value', chain_id: 'balkon', field: 'efficiency_pct', comparison: 'below', value: 80, hold_seconds: 0 },
    null, null, chains,
  );
  assert.match(efficiency.summary, /Wirkungsgrad/);
  assert.match(efficiency.summary, /80 %/);
  assert.equal(efficiency.summary.includes(' h'), false);
});

test('describeCondition chain_value with a missing chain shows the placeholder title', () => {
  const view = load();
  const card = plain(view.describeCondition(
    { type: 'chain_value', chain_id: 'keller', field: 'loss_w', comparison: 'above', value: 5, hold_seconds: 0 },
    null, null, undefined,
  ));
  assert.equal(card.title, 'Messkette fehlt');
  assert.match(card.summary, /Verlust/);
});

const NOW = 1794000000;
const learnedValues = [
  { key: 'site/power_avg', kind: 'number', value: 40, n: 3, at: NOW - 60, changed_at: NOW - 7200, prev: 35 },
  { key: 'site/house_state', kind: 'state', value: 'quiet', n: 9, at: NOW - 60, changed_at: NOW - 3 * 86400, prev: 'active' },
  { key: 'device/wp/usual_on', kind: 'time', value: '23:30', n: 5, at: NOW - 60, changed_at: NOW - 86400 },
  { key: 'device/x/usual_runtime_s', kind: 'duration', value: 1200, n: 4, at: NOW - 60, changed_at: NOW - 7200 },
];
const learnedCtx = (extra) => ({ values: learnedValues, now: NOW, ...extra });
const powerThreshold = { learned: 'site/power_avg', min: 20, max: 80, fallback: 50 };
const gridCondition = { type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: powerThreshold, hysteresis: 0, hold_seconds: 0 };
const FALLBACK_ORIGIN = 'Ersatzwert, weil noch nichts gelernt ist oder der Wert zu alt ist';

test('describeCondition with a learned threshold names the learned value and never prints [object Object]', () => {
  const view = load();
  const card = view.describeCondition(gridCondition, null, null, null, learnedCtx());
  assert.doesNotMatch(card.summary, /\[object Object\]/);
  assert.match(card.summary, /40 W/);
  assert.equal(card.learned.length, 1);
  const [chip] = card.learned;
  assert.equal(chip.field, 'threshold');
  assert.match(chip.name, /site\/power_avg/);
  assert.equal(chip.text, '40 W');
  assert.match(chip.label, /site\/power_avg, aktuell 40 W/);
  assert.equal(chip.scale.min, '20 W');
  assert.equal(chip.scale.max, '80 W');
  assert.ok(Math.abs(chip.scale.marker - 1 / 3) < 0.001);
  assert.match(chip.origin, /Gelernt aus/);
});

test('describeCondition uses the report target and source for the learned threshold', () => {
  const view = load();
  const report = { value: 12, target: 50, raw_met: false, met: false, learned: { threshold: 'fallback' } };
  const card = view.describeCondition(gridCondition, null, null, null, learnedCtx({ report }));
  assert.match(card.summary, /50 W/);
  assert.equal(card.learned[0].text, '50 W');
  assert.equal(card.learned[0].origin, FALLBACK_ORIGIN);
});

test('describeCondition without an endpoint entry or report falls back to the ref fallback', () => {
  const view = load();
  const card = view.describeCondition(gridCondition, null, null, null, { values: [], now: NOW });
  assert.match(card.summary, /50 W/);
  assert.equal(card.learned[0].text, '50 W');
  assert.equal(card.learned[0].origin, FALLBACK_ORIGIN);
});

test('describeCondition clamped report names the learned value in the origin line', () => {
  const view = load();
  const report = { value: 12, target: 80, raw_met: false, met: false, learned: { threshold: 'clamped' } };
  const entry = { key: 'site/power_avg', kind: 'number', value: 95, n: 3, at: NOW - 60, changed_at: NOW - 60 };
  const card = view.describeCondition(gridCondition, null, null, null, { values: [entry], now: NOW, report });
  assert.match(card.learned[0].origin, /Auf die Grenze gesetzt, gelernt sind 95/);
});

test('describeCondition learned_state names the house state and its origin', () => {
  const view = load();
  const condition = { type: 'learned_state', learned: 'site/house_state', equals: 'quiet', fallback: 'active', max_age_s: 7200, hold_seconds: 0 };
  const card = view.describeCondition(condition, null, null, null, learnedCtx());
  assert.equal(card.learned.length, 1);
  const [chip] = card.learned;
  assert.equal(chip.name, 'Hauszustand');
  assert.equal(chip.text, 'Haus ist ruhig');
  assert.equal(chip.scale, null);
  assert.equal(chip.origin, 'Gelernt aus 9 Tagen, zuletzt geändert vor 3 d (vorher Haus ist aktiv)');
});

test('describeCondition time window with a learned start across midnight has two scale segments', () => {
  const view = load();
  const start = { learned: 'device/wp/usual_on', min: '23:00', max: '01:00', fallback: '23:30' };
  const card = view.describeCondition({ type: 'time_window', start, end: '06:00', weekdays: [] }, null, null, null, learnedCtx());
  assert.equal(card.summary, '23:30 – 06:00');
  assert.equal(card.learned.length, 1);
  assert.equal(card.learned[0].scale.segments.length, 2);
  assert.equal(card.learned[0].text, '23:30');
});

test('describeCondition hold_seconds learned value shows a duration', () => {
  const view = load();
  const condition = { ...gridCondition, threshold: 50, hold_seconds: { learned: 'device/x/usual_runtime_s', min: 600, max: 3600, fallback: 1800 } };
  const card = view.describeCondition(condition, null, null, null, learnedCtx());
  assert.equal(card.learned.length, 1);
  assert.equal(card.learned[0].field, 'hold_seconds');
  assert.equal(card.learned[0].text, '20 min');
});

test('describeCondition with a fixed threshold has no learned descriptors', () => {
  const view = load();
  const card = view.describeCondition({ ...gridCondition, threshold: 300 }, null, null, null, learnedCtx());
  assert.equal(card.learned.length, 0);
  assert.match(card.summary, /300 W/);
});

// Ohne Lernwert-Helfer (Geraeteplan laedt ihn nicht) zeigt die Zusammenfassung den Ersatzwert, nie das Objekt.
test('describeCondition without the learned helper shows the fallback of a reference', () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), dom.getInternalVMContext());
  const view = dom.window.__automationsView;
  const card = view.describeCondition({
    type: 'balance_threshold', field: 'grid_export', comparison: 'above', hysteresis: 0, hold_seconds: 0,
    threshold: { learned: 'device/pump_1/usual_runtime_s', min: 100, max: 900, fallback: 800 },
  }, null, {});
  assert.ok(!card.summary.includes('[object Object]'), card.summary);
  assert.ok(card.summary.includes('800'), card.summary);
});
