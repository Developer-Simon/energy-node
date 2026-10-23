// Pure helpers for scenarios (Szenarien): the scenario shape, its status,
// the phase bar and the save-time validation. automations-scenario.js has
// no DOM of its own, so these tests load it next to automations-view.js the
// way automations.page.test.mjs loads the view helpers.
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
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-scenario.js'), 'utf8'), context);
  return dom.window.__automationScenario;
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich
// einmal durch JSON laufen lassen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const balanceCondition = (threshold, holdSeconds = 0) => ({
  type: 'balance_threshold', field: 'grid_export', comparison: 'above',
  threshold, hysteresis: 0, hold_seconds: holdSeconds,
});

const scenarioWith = (overrides = {}) => ({
  id: 'wallbox', name: 'Wallbox', enabled: true,
  when: { conditions: [balanceCondition(1400, 180)] },
  on_start: { actions: [] },
  while: { mode: 'none' },
  on_end: { actions: [] },
  ...overrides,
});

const describeCondition = (condition) => ({ icon: 'ico-flash', title: `T:${condition.type}`, summary: `S:${condition.type}` });
const describeAction = (action) => ({ icon: 'ico-bell', kind: 'x', title: `A:${action.type}` });

test('emptyScenario has no until and no null fields', () => {
  const s = load();
  const scenario = s.emptyScenario('wallbox', 'Wallbox');
  assert.deepEqual(plain(scenario), {
    id: 'wallbox', name: 'Wallbox', enabled: true, history_enabled: true,
    when: { conditions: [] }, on_start: { actions: [] }, while: { mode: 'none' }, on_end: { actions: [] },
  });
  assert.equal(JSON.stringify(scenario).includes('null'), false);
  assert.equal('until' in scenario, false);
});

test('setUntil off removes the key, on adds one condition', () => {
  const s = load();
  const scenario = scenarioWith();
  s.setUntil(scenario, true);
  assert.deepEqual(plain(scenario.until), {
    conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 }],
  });
  s.setUntil(scenario, false);
  assert.equal('until' in scenario, false);
});

test('setWhileMode regulate seeds a producer with fallback', () => {
  const s = load();
  const scenario = scenarioWith({ while: { mode: 'hold', actions: [{ type: 'notification' }], repeat_seconds: 30 } });
  s.setWhileMode(scenario, 'regulate');
  assert.deepEqual(plain(scenario.while), {
    mode: 'regulate',
    regulator: {
      output: { topic: '', min: 0, max: 800 }, direction: 'producer', target_w: 0,
      base: { source: 'last' }, fallback: { payload: '800' },
    },
  });
});

test('setWhileMode hold seeds empty actions and a minute, none keeps only the mode', () => {
  const s = load();
  const scenario = scenarioWith();
  s.setWhileMode(scenario, 'hold');
  assert.deepEqual(plain(scenario.while), { mode: 'hold', actions: [], repeat_seconds: 60 });
  s.setWhileMode(scenario, 'none');
  assert.deepEqual(plain(scenario.while), { mode: 'none' });
});

test('status is offline before anything else', () => {
  const s = load();
  const scenario = scenarioWith({ while: { mode: 'regulate' } });
  const state = { phase: 'active', since: 1000 };
  assert.equal(s.scenarioStatus(scenario, state, false, 2000).key, 'offline');
  assert.equal(s.scenarioStatus(scenario, state, false, 2000).label, 'Dienst offline');
});

test('status is off when the scenario is disabled or its phase says so', () => {
  const s = load();
  assert.equal(s.scenarioStatus(scenarioWith({ enabled: false }), { phase: 'idle' }, true, 0).key, 'off');
  assert.equal(s.scenarioStatus(scenarioWith(), { phase: 'disabled' }, true, 0).key, 'off');
});

test('status regulating carries the start time', () => {
  const s = load();
  const scenario = scenarioWith({ while: { mode: 'regulate' } });
  const status = s.scenarioStatus(scenario, { phase: 'active', since: 1000 }, true, 2000);
  assert.equal(status.key, 'regulating');
  assert.equal(status.tone, 'ok');
  assert.ok(status.label.includes('Regelt seit'));
  assert.match(status.label, /^Regelt seit \S/);
});

test('status running when active without regulation', () => {
  const s = load();
  const status = s.scenarioStatus(scenarioWith({ while: { mode: 'hold', actions: [], repeat_seconds: 60 } }), { phase: 'active', since: 1000 }, true, 2000);
  assert.equal(status.key, 'running');
  assert.equal(status.tone, 'ok');
  assert.ok(status.label.includes('Läuft seit'));
});

test('status fallback names the reason', () => {
  const s = load();
  const scenario = scenarioWith({ while: { mode: 'regulate' } });
  const stale = s.scenarioStatus(scenario, { phase: 'fallback', reason: 'balance_stale' }, true, 0);
  assert.equal(stale.key, 'fallback');
  assert.equal(stale.tone, 'bad');
  assert.equal(stale.label, 'Ersatzwert');
  assert.equal(stale.detail, 'Bilanz veraltet');
  const missing = s.scenarioStatus(scenario, { phase: 'fallback', reason: 'base_missing' }, true, 0);
  assert.equal(missing.detail, 'Messwert fehlt');
});

test('status ready reports the remaining hold time', () => {
  const s = load();
  const scenario = scenarioWith({ when: { conditions: [balanceCondition(1400, 180), balanceCondition(900, 60)] } });
  const state = {
    phase: 'idle',
    conditions: [
      { value: 1500, raw_met: true, met: false, hold_remaining: 120 },
      { value: 950, raw_met: true, met: false, hold_remaining: 45 },
    ],
  };
  const status = s.scenarioStatus(scenario, state, true, 0);
  assert.equal(status.key, 'ready');
  assert.equal(status.tone, 'wait');
  assert.equal(status.label, 'Bereit');
  assert.equal(status.detail, 'Start in 2 min');
});

test('status waiting when a start condition is not met', () => {
  const s = load();
  const scenario = scenarioWith({ when: { conditions: [balanceCondition(1400), balanceCondition(900)] } });
  const state = { phase: 'idle', conditions: [{ value: 1500, raw_met: true, met: false, hold_remaining: 5 }, { value: 10, raw_met: false, met: false }] };
  const status = s.scenarioStatus(scenario, state, true, 0);
  assert.equal(status.key, 'waiting');
  assert.equal(status.tone, 'info');
  assert.equal(status.label, 'Wartet');
});

test('status unknown without a state', () => {
  const s = load();
  const status = s.scenarioStatus(scenarioWith(), undefined, true, 0);
  assert.equal(status.key, 'unknown');
  assert.equal(status.tone, 'off');
  assert.equal(status.label, 'Zustand unbekannt');
});

test('activePhase marks the while block while active', () => {
  const s = load();
  assert.equal(s.activePhase({ phase: 'active', since: 0 }), 'while');
  assert.equal(s.activePhase({ phase: 'fallback', since: 0 }), 'while');
  assert.equal(s.activePhase({ phase: 'idle', since: 990 }, 1000), 'end');
  assert.equal(s.activePhase({ phase: 'idle', since: 0 }, 1000), null);
  assert.equal(s.activePhase(undefined, 1000), null);
});

test('startProgress reads the first balance condition', () => {
  const s = load();
  const scenario = scenarioWith();
  const progress = s.startProgress(scenario, { phase: 'idle', conditions: [{ value: 900, raw_met: false, met: false }] });
  assert.ok(Math.abs(progress.fraction - 900 / 1400) < 0.001);
  assert.equal(progress.label, '900 / 1400 W');
});

test('startProgress is null without a balance condition or a value', () => {
  const s = load();
  const scenario = scenarioWith({ when: { conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }] } });
  assert.equal(s.startProgress(scenario, { phase: 'idle', conditions: [{ value: 1 }] }), null);
  assert.equal(s.startProgress(scenarioWith(), { phase: 'idle', conditions: [{ value: null }] }), null);
  assert.equal(s.startProgress(scenarioWith(), undefined), null);
});

test('phaseSummary returns three rows for regulate with a producer target', () => {
  const s = load();
  const scenario = scenarioWith({
    when: { conditions: [balanceCondition(1400), balanceCondition(900)] },
    while: { mode: 'regulate', regulator: { output: { topic: 't', min: 0, max: 800 }, direction: 'producer', target_w: 30, base: { source: 'last' }, interval_s: 5, fallback: { payload: '800' } } },
    until: { conditions: [balanceCondition(300)] },
    on_end: { actions: [{ type: 'notification' }] },
  });
  const rows = s.phaseSummary(scenario, describeCondition, describeAction);
  assert.equal(rows.length, 3);
  assert.deepEqual(plain(rows.map((row) => row.phase)), ['start', 'while', 'end']);
  assert.equal(rows[0].icon, 'ico-flash');
  assert.equal(rows[0].title, 'T:balance_threshold');
  assert.equal(rows[0].text, '+ 1 weitere');
  assert.equal(rows[1].icon, 'ico-grid');
  assert.equal(rows[1].title, 'Netz auf 30 W regeln');
  assert.ok(rows[1].text.includes('5'));
  assert.equal(rows[2].title, 'T:balance_threshold');
  assert.equal(rows[2].text, 'A:notification');
});

test('phaseSummary falls back to the plain labels for hold, none and missing end condition', () => {
  const s = load();
  const hold = scenarioWith({ while: { mode: 'hold', actions: [{ type: 'publish' }], repeat_seconds: 90 } });
  const rowsHold = s.phaseSummary(hold, describeCondition, describeAction);
  assert.equal(rowsHold[1].title, 'A:publish');
  assert.equal(rowsHold[1].text, 'alle 90 s erneut');
  const none = s.phaseSummary(scenarioWith(), describeCondition, describeAction);
  assert.equal(none[1].icon, 'ico-check');
  assert.equal(none[1].title, 'nichts weiter');
  assert.equal(none[2].icon, 'ico-power-off');
  assert.equal(none[2].title, 'Wenn eine Startbedingung nicht mehr erfüllt ist');
  assert.equal(none[2].text, 'nichts weiter');
});

test('validation rejects a producer without fallback', () => {
  const s = load();
  const scenario = validRegulated();
  delete scenario.while.regulator.fallback;
  const errors = s.validateScenarioBeforeSave(scenario, { minInterval: 10, otherOutputs: [] });
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Ersatzwert'));
});

test('validation rejects a second regulator on the same topic', () => {
  const s = load();
  const errors = s.validateScenarioBeforeSave(validRegulated(), { minInterval: 10, otherOutputs: ['werkstatt/bkw/limit/set'] });
  assert.equal(errors.length, 1);
});

test('validation rejects hold with a toggle action', () => {
  const s = load();
  const scenario = scenarioWith({
    while: { mode: 'hold', repeat_seconds: 60, actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'toggle' }] },
  });
  const errors = s.validateScenarioBeforeSave(scenario, { minInterval: 10, otherOutputs: [] });
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Umschalten'));
});

test('validation rejects hold repeat below the publish minimum', () => {
  const s = load();
  const scenario = scenarioWith({
    while: { mode: 'hold', repeat_seconds: 5, actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' }] },
  });
  const errors = s.validateScenarioBeforeSave(scenario, { minInterval: 10, otherOutputs: [] });
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('10'));
});

test('validation requires a valid id and at least one start condition', () => {
  const s = load();
  const scenario = scenarioWith({ id: 'Wallbox!', when: { conditions: [] } });
  const errors = s.validateScenarioBeforeSave(scenario, { minInterval: 10, otherOutputs: [] });
  assert.equal(errors.length, 2);
});

test('validation accepts the spec example', () => {
  const s = load();
  const scenario = {
    id: 'nulleinspeisung', name: 'Nulleinspeisung', enabled: true, template: 'zero_feed', history_enabled: true,
    when: { conditions: [{ type: 'sun_window', from: 'sunrise', to: 'sunset', from_offset_min: 0, to_offset_min: 0 }] },
    on_start: { actions: [{ type: 'notification', severity: 'info', title: 'Nulleinspeisung', message: 'startet' }] },
    while: {
      mode: 'regulate',
      regulator: {
        output: { topic: 'werkstatt/bkw/limit/set', entity_id: 'bkw_limit', min: 50, max: 800, step: 10, decimals: 0 },
        direction: 'producer', target_w: 30, deadband_w: 20, gain: 0.8,
        base: { source: 'entity', entity_id: 'bkw_power', topic: 'werkstatt/bkw/state', value_template: '{{ value_json.power }}' },
        fallback: { payload: '800' },
      },
    },
    on_end: { actions: [{ type: 'publish', topic: 'werkstatt/bkw/limit/set', retain: false, payload_source: 'constant', payload: '800' }] },
  };
  assert.deepEqual(plain(s.validateScenarioBeforeSave(scenario, { minInterval: 10, otherOutputs: [] })), []);
});

test('stripRegulatorDefaults drops default gain and deadband and empty optionals', () => {
  const s = load();
  const stripped = plain(s.stripRegulatorDefaults({
    output: { topic: 't', min: 0, max: 800, step: '', decimals: null },
    direction: 'consumer', target_w: 0, deadband_w: 20, gain: 0.8,
    max_change_w: null, interval_s: '', base: { source: 'last' }, fallback: { payload: '' },
  }));
  assert.deepEqual(stripped, {
    output: { topic: 't', min: 0, max: 800 },
    direction: 'consumer', target_w: 0, base: { source: 'last' },
  });
  const kept = plain(s.stripRegulatorDefaults({
    output: { topic: 't', min: 0, max: 800, step: 10 }, direction: 'producer', target_w: 30,
    deadband_w: 0, gain: 0.5, max_change_w: 100, interval_s: 5, base: { source: 'last' }, fallback: { payload: '800' },
  }));
  assert.equal(kept.gain, 0.5);
  assert.equal(kept.deadband_w, 0);
  assert.equal(kept.max_change_w, 100);
  assert.equal(kept.interval_s, 5);
  assert.equal(kept.output.step, 10);
  assert.deepEqual(kept.fallback, { payload: '800' });
});

function validRegulated() {
  return scenarioWith({
    while: {
      mode: 'regulate',
      regulator: {
        output: { topic: 'werkstatt/bkw/limit/set', min: 0, max: 800 }, direction: 'producer', target_w: 30,
        base: { source: 'last' }, fallback: { payload: '800' },
      },
    },
  });
}
