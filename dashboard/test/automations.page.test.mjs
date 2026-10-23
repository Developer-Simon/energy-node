// Regression tests for the hand-written automation rule editor. Alpine
// templates drive the DOM here (unlike config.page.js's generic renderer),
// so these tests call plain component methods directly - the
// settings.page.js testing convention, not config.page.js's DOM-node one.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const configStatusSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'config-status.js'),
  'utf8',
);
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const viewSource = fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8');
const scenarioSource = fs.readFileSync(path.join(jsDir, 'automations-scenario.js'), 'utf8');
const chartSource = fs.readFileSync(path.join(jsDir, 'automations-chart.js'), 'utf8');
const devicesSource = fs.readFileSync(path.join(jsDir, 'automations-devices.js'), 'utf8');
const rollupSource = fs.readFileSync(path.join(jsDir, 'history-rollup.js'), 'utf8');
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'automations.page.js'),
  'utf8',
);

function createAutomationsPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  vm.runInContext(configStatusSource, context);
  vm.runInContext(viewSource, context);
  vm.runInContext(scenarioSource, context);
  vm.runInContext(chartSource, context);
  vm.runInContext(devicesSource, context);
  vm.runInContext(rollupSource, context);
  vm.runInContext(scriptSource, context);
  const component = factory();
  component.$refs = {};
  const stores = attachStores(component);
  return { component, window: dom.window, stores };
}

test('setpoint mode builds a publish action with the entity command_topic and bounds', () => {
  const { component } = createAutomationsPanel();
  const entity = { unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set', min_value: 1380, max_value: 11000, step: 10 };
  const action = component.buildSetpointAction(entity, { mode: 'balance', field: 'grid_export', scale: 1, offset: -200 });
  assert.equal(action.type, 'publish');
  assert.equal(action.topic, 'werkstatt/wallbox/set');
  assert.equal(action.entity_id, 'wallbox_power');
  assert.equal(action.min, 1380);
  assert.equal(action.max, 11000);
  assert.equal(action.step, 10);
  assert.equal(action.payload_source, 'balance');
  assert.equal(action.field, 'grid_export');
});

test('switch mode builds a publish action with payload_on/payload_off', () => {
  const { component } = createAutomationsPanel();
  const entity = { unique_id: 'wallbox_enable', command_topic: 'werkstatt/wallbox/enable', payload_on: 'ON', payload_off: 'OFF' };
  const action = component.buildSwitchAction(entity, 'on');
  assert.equal(action.topic, 'werkstatt/wallbox/enable');
  assert.equal(action.payload, 'ON');
  assert.equal(action.payload_source, 'constant');
});

test('buildToggleAction reads state from the entity and carries payload_on/payload_off', () => {
  const { component } = createAutomationsPanel();
  const entity = {
    unique_id: 'wallbox_enable', command_topic: 'werkstatt/wallbox/enable', state_topic: 'werkstatt/wallbox/state',
    value_template: '{{ value_json.state }}', payload_on: 'ON', payload_off: 'OFF',
  };
  const action = component.buildToggleAction(entity);
  assert.equal(action.type, 'publish');
  assert.equal(action.topic, 'werkstatt/wallbox/enable');
  assert.equal(action.payload_source, 'toggle');
  assert.equal(action.source_topic, 'werkstatt/wallbox/state');
  assert.equal(action.value_template, '{{ value_json.state }}');
  assert.equal(action.payload_on, 'ON');
  assert.equal(action.payload_off, 'OFF');
  assert.equal(action.entity_id, 'wallbox_enable');
});

test('flattenCommandableEntities tags each entity with its device name', () => {
  const { component } = createAutomationsPanel();
  const devices = [
    { name: 'Wallbox', entities: [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set' }] },
    { name: 'Ohne Topic', entities: [{ unique_id: 'no_command' }] },
  ];
  const entities = component.flattenCommandableEntities(devices);
  assert.equal(entities.length, 1);
  assert.equal(entities[0].deviceName, 'Wallbox');
});

test('commandableEntityGroups groups entities by device name', () => {
  const { component } = createAutomationsPanel();
  component.entities = [
    { unique_id: 'a', deviceName: 'Wallbox' },
    { unique_id: 'b', deviceName: 'Wallbox' },
    { unique_id: 'c', deviceName: 'Heizstab' },
    { unique_id: 'd' },
  ];
  const groups = component.commandableEntityGroups();
  assert.deepEqual(JSON.parse(JSON.stringify(groups.map(([name]) => name))), ['Wallbox', 'Heizstab', 'Ohne Gerät']);
  assert.equal(groups[0][1].length, 2);
});

test('drift warning fires when the stored topic no longer matches current discovery', () => {
  const { component } = createAutomationsPanel();
  const action = { type: 'publish', topic: 'werkstatt/wallbox/set_old', entity_id: 'wallbox_power' };
  const devices = [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set_new' }];
  const drift = component.computeDrift(action, devices);
  assert.equal(drift.drifted, true);
  assert.equal(drift.currentTopic, 'werkstatt/wallbox/set_new');
});

test('no drift warning when topics still match', () => {
  const { component } = createAutomationsPanel();
  const action = { type: 'publish', topic: 'werkstatt/wallbox/set', entity_id: 'wallbox_power' };
  const devices = [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set' }];
  assert.equal(component.computeDrift(action, devices).drifted, false);
});

test('no drift warning for actions without an entity_id (raw MQTT mode)', () => {
  const { component } = createAutomationsPanel();
  const action = { type: 'publish', topic: 'werkstatt/x/set' };
  assert.equal(component.computeDrift(action, []).drifted, false);
});

test('state document results render to German badge labels', () => {
  const { component } = createAutomationsPanel();
  assert.equal(component.badgeLabel('fired'), 'Ausgelöst');
  assert.equal(component.badgeLabel('conditions_not_met'), 'Bedingungen nicht erfüllt');
  assert.equal(component.badgeLabel('hold_pending'), 'Wartet auf Haltedauer');
  assert.equal(component.badgeLabel('cooldown'), 'Sperrzeit');
  assert.equal(component.badgeLabel('settling'), 'Beruhigungsphase');
  assert.equal(component.badgeLabel('balance_stale'), 'Bilanz veraltet');
  assert.equal(component.badgeLabel('blocked'), 'Blockiert');
  assert.equal(component.badgeLabel('disabled'), 'Deaktiviert');
  assert.equal(component.badgeLabel('error'), 'Fehler');
});

test('toggling enabled flips only the targeted rule but keeps the whole document intact', () => {
  const { component } = createAutomationsPanel();
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30, publish_allowed_prefixes: [] },
    rules: [
      { id: 'r1', name: 'A', enabled: false, cooldown_seconds: 30, conditions: [], actions: [] },
      { id: 'r2', name: 'B', enabled: true, cooldown_seconds: 30, conditions: [], actions: [] },
    ],
  };
  component.toggleEnabled('r1');
  assert.equal(component.document.rules[0].enabled, true);
  assert.equal(component.document.rules[1].enabled, true);
  assert.equal(component.document.rules.length, 2);
});

test('client-side validation blocks a publishing rule with hold_seconds under 30', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 300,
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 10 }],
    actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' }],
  };
  const errors = component.validateRuleBeforeSave(rule);
  assert.ok(errors.some((message) => message.includes('hold_seconds')));
});

test('client-side validation blocks a publishing rule with cooldown_seconds under 30', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 10,
    conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }],
    actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' }],
  };
  const errors = component.validateRuleBeforeSave(rule);
  assert.ok(errors.some((message) => message.includes('cooldown_seconds')));
});

test('client-side validation passes a valid publishing rule', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 300,
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 300 }],
    actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' }],
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.validateRuleBeforeSave(rule))), []);
});

test('client-side validation does not require hold_seconds >= 30 for notification-only rules', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 0,
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 0 }],
    actions: [{ type: 'notification', severity: 'info', title: 't', message: 'm' }],
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.validateRuleBeforeSave(rule))), []);
});

test('client-side validation uses the configured publish minimum', () => {
  const { component } = createAutomationsPanel();
  const publish = { type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' };
  component.document.settings.publish_min_interval_s = 10;
  const atMinimum = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 10,
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 10 }],
    actions: [publish],
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.validateRuleBeforeSave(atMinimum))), []);
  component.document.settings.publish_min_interval_s = 60;
  const belowMinimum = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 30,
    conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }],
    actions: [publish],
  };
  const errors = component.validateRuleBeforeSave(belowMinimum);
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('60'));
});

test('client-side validation blocks repeat with a toggle action', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 60, repeat_seconds: 60,
    conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }],
    actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'toggle' }],
  };
  const errors = component.validateRuleBeforeSave(rule);
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('Umschalten'));
  rule.repeat_seconds = 0;
  assert.deepEqual(JSON.parse(JSON.stringify(component.validateRuleBeforeSave(rule))), []);
});

test('client-side validation blocks repeat below the publish minimum on a publishing rule', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A', enabled: true, cooldown_seconds: 60, repeat_seconds: 20,
    conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }],
    actions: [{ type: 'publish', topic: 'werkstatt/x/set', retain: false, payload_source: 'constant', payload: '1' }],
  };
  const errors = component.validateRuleBeforeSave(rule);
  assert.equal(errors.length, 1);
  assert.ok(errors[0].includes('repeat_seconds'));
  rule.repeat_seconds = 30;
  assert.deepEqual(JSON.parse(JSON.stringify(component.validateRuleBeforeSave(rule))), []);
});

test('hasToggleAction detects a toggle publish action', () => {
  const { component } = createAutomationsPanel();
  assert.equal(component.hasToggleAction({ actions: [{ type: 'publish', payload_source: 'toggle' }] }), true);
  assert.equal(component.hasToggleAction({ actions: [{ type: 'publish', payload_source: 'constant' }] }), false);
  assert.equal(component.hasToggleAction({ actions: [{ type: 'notification' }] }), false);
  assert.equal(component.hasToggleAction({}), false);
});

test('the view helpers load on their own, without the page', () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), dom.getInternalVMContext());
  const view = dom.window.__automationsView;
  const described = view.describeCondition({ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 1500 }, null);
  assert.ok(described.title);
  assert.ok(described.summary.includes('1500'));
});

function view() {
  const { window } = createAutomationsPanel();
  return window.__automationsView;
}

test('conditionState maps the four live cases', () => {
  const v = view();
  assert.equal(v.conditionState({ value: null, raw_met: false, met: false }), 'novalue');
  assert.equal(v.conditionState({ value: 320, raw_met: false, met: false }), 'unmet');
  assert.equal(v.conditionState({ value: 620, raw_met: true, met: false }), 'pending');
  assert.equal(v.conditionState({ value: 620, raw_met: true, met: true }), 'met');
});

test('conditionState treats a missing report as novalue', () => {
  assert.equal(view().conditionState(undefined), 'novalue');
});

test('meterScale uses a percent scale for percentage conditions', () => {
  const v = view();
  const scale = v.meterScale({ type: 'balance_threshold', field: 'autarkie' }, { value: 62, target: 50 });
  assert.deepEqual({ min: scale.min, max: scale.max, kind: scale.kind }, { min: 0, max: 100, kind: 'percent' });
});

test('meterScale uses a day scale for time windows', () => {
  const scale = view().meterScale({ type: 'time_window' }, { value: '14:05', target: null });
  assert.equal(scale.kind, 'time');
  assert.equal(scale.max, 1440);
});

test('meterScale spans twice the threshold for a normal power condition', () => {
  const scale = view().meterScale(
    { type: 'balance_threshold', field: 'grid_export' }, { value: 620, target: 500 });
  assert.equal(scale.min, 0);
  assert.equal(scale.max, 1000);
  assert.equal(scale.kind, 'linear');
});

test('meterScale grows past twice the threshold when the value is larger', () => {
  const scale = view().meterScale(
    { type: 'balance_threshold', field: 'grid_export' }, { value: 2000, target: 500 });
  assert.equal(scale.max, 2200);
});

test('meterScale centres zero when value or threshold is negative', () => {
  const scale = view().meterScale(
    { type: 'balance_threshold', field: 'base' }, { value: -300, target: -100 });
  assert.equal(scale.kind, 'signed');
  assert.equal(scale.min, -330);
  assert.equal(scale.max, 330);
});

test('meterFraction clamps to the 0..1 range', () => {
  const v = view();
  const scale = { min: 0, max: 1000, kind: 'linear' };
  assert.equal(v.meterFraction(500, scale), 0.5);
  assert.equal(v.meterFraction(-50, scale), 0);
  assert.equal(v.meterFraction(5000, scale), 1);
});

test('gateVerdict covers every result the service can report', () => {
  const v = view();
  const results = ['fired', 'hold_pending', 'cooldown', 'conditions_not_met', 'settling',
                   'balance_stale', 'blocked', 'error', 'disabled'];
  for (const result of results) {
    const verdict = v.gateVerdict({ result, conditions: [], cooldown_remaining: 0 }, true);
    assert.ok(verdict.label && verdict.label !== result, `kein Klartext für ${result}`);
    assert.ok(['ok', 'wait', 'info', 'off', 'bad'].includes(verdict.tone), `unbekannter Ton bei ${result}`);
  }
});

test('gateVerdict reports the remaining hold time', () => {
  const verdict = view().gateVerdict(
    { result: 'hold_pending', conditions: [{ hold_remaining: 12 }, { hold_remaining: 0 }] }, true);
  assert.equal(verdict.tone, 'wait');
  assert.match(verdict.detail, /12/);
});

test('gateVerdict reports the remaining cooldown', () => {
  const verdict = view().gateVerdict({ result: 'cooldown', cooldown_remaining: 45, conditions: [] }, true);
  assert.match(verdict.detail, /45/);
});

test('gateVerdict overrides everything when the service is offline', () => {
  const verdict = view().gateVerdict({ result: 'fired', conditions: [] }, false);
  assert.equal(verdict.tone, 'off');
  assert.match(verdict.label, /offline/i);
});

test('describeCondition names the balance field in plain German', () => {
  const described = view().describeCondition(
    { type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 500, hysteresis: 100 });
  assert.equal(described.title, 'Netzeinspeisung');
  assert.equal(described.unit, 'W');
  assert.match(described.summary, /über 500/);
});

test('describeCondition falls back to a neutral card for an unknown type', () => {
  const described = view().describeCondition({ type: 'entity_state', entity_id: 'x' });
  assert.ok(described.title, 'ein unbekannter Typ muss einen Titel bekommen statt zu werfen');
  assert.ok(described.icon, 'ein unbekannter Typ muss ein Icon bekommen');
});

test('describeAction distinguishes commands from notifications', () => {
  const v = view();
  assert.equal(v.describeAction({ type: 'publish', topic: 'werkstatt/x/set' }).kind, 'command');
  assert.equal(v.describeAction({ type: 'notification', severity: 'info', title: 'T' }).kind, 'notify');
});

test('describeAction löst publish-Aktionen mit und ohne bekannte Entität auf', () => {
  const action = { type: 'publish', entity_id: 'plug_1', topic: 'outstation/plug_1/relay/0/set' };
  const entity = { unique_id: 'plug_1', name: 'Werkbank-Steckdose', device_class: 'power' };

  const withEntity = view().describeAction(action, entity);
  assert.equal(withEntity.title, 'Werkbank-Steckdose');
  assert.equal(withEntity.icon, 'ico-flash');

  const without = view().describeAction(action, null);
  assert.equal(without.title, 'outstation/plug_1/relay/0/set');
  assert.equal(without.icon, 'ico-switch');
});

test('formatSeconds stays readable across magnitudes', () => {
  const v = view();
  assert.equal(v.formatSeconds(0), '0 s');
  assert.equal(v.formatSeconds(45), '45 s');
  assert.equal(v.formatSeconds(90), '2 min');
  assert.equal(v.formatSeconds(1800), '30 min');
  assert.equal(v.formatSeconds(3600), '1 h');
  assert.equal(v.formatSeconds(7200), '2 h');
  assert.equal(v.formatSeconds(86400), '1 d');
  assert.equal(v.formatSeconds(172800), '2 d');
});

test('canTest refuses while the service is offline', () => {
  const result = view().canTest({ online: false, dirty: false, hasRole: true });
  assert.equal(result.allowed, false);
  assert.match(result.reason, /offline/i);
});

test('canTest refuses while the rule has unsaved changes', () => {
  const result = view().canTest({ online: true, dirty: true, hasRole: true });
  assert.equal(result.allowed, false);
  assert.match(result.reason, /speichern/i);
});

test('canTest allows a saved rule on an online service', () => {
  assert.equal(view().canTest({ online: true, dirty: false, hasRole: true }).allowed, true);
});

function panelWithState(stateDocument, { online = true, hasRole = true } = {}) {
  const { component } = createAutomationsPanel();
  component.runtimeState = stateDocument;
  component.online = online;
  component.hasAutomationsRole = hasRole;
  return component;
}

const sampleRule = {
  id: 'r1', name: 'Warmwasser', enabled: true, cooldown_seconds: 60,
  conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 500, hysteresis: 100, hold_seconds: 50 }],
  actions: [{ type: 'publish', topic: 'werkstatt/heizstab/set', retain: false, payload_source: 'constant', payload: 'on' }],
};

const sampleState = {
  online: true,
  rules: {
    r1: {
      enabled: true, result: 'hold_pending', reason: null, fired_at: null, fire_count: 4,
      cooldown_remaining: 0,
      conditions: [{ met: false, raw_met: true, value: 620, target: 500, since: 1000, hold_remaining: 12 }],
      actions: [{ topic: 'werkstatt/heizstab/set', payload: 'on', blocked: false, reason: null }],
    },
  },
};

test('conditionState reads the live report of the addressed condition', () => {
  const component = panelWithState(sampleState);
  assert.equal(component.conditionState(sampleRule, 0), 'pending');
});

test('conditionStateText shows "erfüllt seit" in hours for a long-held condition', () => {
  const component = panelWithState({
    online: true,
    rules: { r1: { result: 'fired', conditions: [{ met: true, raw_met: true, value: 620, target: 500, since: 1000, hold_remaining: 0 }] } },
  });
  component.nowTick = (1000 + 2 * 3600) * 1000; // 2 Stunden nach "since"
  assert.equal(component.conditionStateText(sampleRule, 0), 'erfüllt seit 2 h');
});

// Regression: "erfüllt seit" muss ohne neue Server-Daten weiterlaufen - es
// reicht, den reaktiven Sekundentakt (nowTick) voranzustellen.
test('conditionStateText ticks with nowTick alone, without a fresh runtimeState', () => {
  const component = panelWithState({
    online: true,
    rules: { r1: { result: 'fired', conditions: [{ met: true, raw_met: true, value: 620, target: 500, since: 1000, hold_remaining: 0 }] } },
  });
  component.nowTick = 1000 * 1000;
  assert.equal(component.conditionStateText(sampleRule, 0), 'erfüllt seit 0 s');
  component.nowTick = (1000 + 90) * 1000;
  assert.equal(component.conditionStateText(sampleRule, 0), 'erfüllt seit 2 min');
});

test('conditionState is novalue when the service has no state for the rule', () => {
  const component = panelWithState({ online: true, rules: {} });
  assert.equal(component.conditionState(sampleRule, 0), 'novalue');
});

test('gateVerdict reports the waiting hold time of the rule', () => {
  const component = panelWithState(sampleState);
  const verdict = component.gateVerdict(sampleRule);
  assert.equal(verdict.tone, 'wait');
  assert.match(verdict.detail, /12/);
});

test('gateVerdict falls back to offline when the service is not available', () => {
  const component = panelWithState(sampleState, { online: false });
  assert.equal(component.gateVerdict(sampleRule).tone, 'off');
});

test('actionPreview shows the resolved topic and payload', () => {
  const component = panelWithState(sampleState);
  const preview = component.actionPreview(sampleRule, 0);
  assert.equal(preview.blocked, false);
  assert.match(preview.text, /werkstatt\/heizstab\/set/);
  assert.match(preview.text, /on/);
});

test('actionPreview surfaces the block reason instead of a payload', () => {
  const component = panelWithState({
    online: true,
    rules: { r1: { result: 'blocked', conditions: [], actions: [{ topic: 'fremd/x', payload: null, blocked: true, reason: 'topic does not match any allowed prefix' }] } },
  });
  const preview = component.actionPreview(sampleRule, 0);
  assert.equal(preview.blocked, true);
  assert.match(preview.text, /allowed prefix/);
});

test('meterStyle turns the live value into a bar width', () => {
  const component = panelWithState(sampleState);
  // Wert 620, Schwelle 500 -> Skala 0..1000 -> 62 %
  assert.match(component.meterStyle(sampleRule, 0).width, /^62(\.\d+)?%$/);
});

test('isDirty detects an edited but unsaved rule', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  assert.equal(component.isDirty(component.document.rules[0]), false);
  component.document.rules[0].conditions[0].threshold = 900;
  assert.equal(component.isDirty(component.document.rules[0]), true);
});

test('collapsing an edited rule closes its editor and keeps the unsaved name', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  const rule = component.document.rules[0];
  component.expanded[rule.id] = true;
  component.editing[rule.id] = true;
  rule.name = 'Renamed but unsaved';
  component.toggleExpanded(rule);
  assert.equal(component.expanded[rule.id], false);
  assert.equal(component.editing[rule.id], false);
  assert.equal(component.document.rules[0].name, 'Renamed but unsaved');
});

test('expanding a collapsed rule does not start editing', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  const rule = component.document.rules[0];
  component.expanded[rule.id] = false;
  component.toggleExpanded(rule);
  assert.equal(component.expanded[rule.id], true);
  assert.equal(Boolean(component.editing[rule.id]), false);
});

test('collapsing a rule closes its editor even when it was open', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  const rule = component.document.rules[0];
  component.expanded[rule.id] = true;
  component.editing[rule.id] = true;
  component.toggleExpanded(rule);
  assert.equal(component.expanded[rule.id], false);
  assert.equal(component.editing[rule.id], false);
});

test('offline scenarios show a dash for net and setpoint and highlight no phase', () => {
  const { component } = createAutomationsPanel();
  const scenario = regulatorEditScenario();
  component.runtimeState = { rules: {}, scenarios: { [scenario.id]: { phase: 'active', output: 300, net: 120 } } };
  component.online = true;
  assert.equal(component.regulatorFigures(scenario).output, 300);
  assert.notEqual(component.gridNowText(scenario), '—');
  assert.equal(component.activePhaseIndex(scenario), 1);
  component.online = false;
  assert.equal(component.regulatorFigures(scenario).output, '—');
  assert.equal(component.gridNowText(scenario), '—');
  assert.equal(component.activePhaseIndex(scenario), -1);
  assert.equal(component.scenarioProgress(scenario), null);
});

test('applyStateObject stores the state and its timestamp', () => {
  const { component } = createAutomationsPanel();
  component.applyStateObject(sampleState);
  assert.equal(component.runtimeState.rules.r1.result, 'hold_pending');
  assert.ok(component.lastStateAt > 0);
});

// The state document is fetched from /api/v1/automation/state: last_message is
// capped at 4096 bytes and shared with the availability topic, so a larger
// document arrived truncated and an availability "online" replaced it.
test('loadRuntimeState fetches the state document from its own route when the state entity changes', async () => {
  const { component, window } = createAutomationsPanel();
  const calls = [];
  window.fetch = async (url) => {
    calls.push(String(url).replace(/^.*(\/api\/)/, '$1'));
    return { ok: true, json: async () => sampleState };
  };
  const deviceWithStateAt = (at, payload) => [{
    id: 'automation',
    entities: [{
      object_id: 'state', available: true, value: '1',
      last_message: { topic: 'outstation/automation/status/online', payload, at },
    }],
  }];
  component.devices = deviceWithStateAt('2026-10-15T10:00:00Z', 'online');
  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(calls, ['/api/v1/automation/state']);
  assert.equal(component.online, true);
  assert.equal(component.runtimeState.rules.r1.result, 'hold_pending');

  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 1, 'an unchanged state entity must not refetch');

  component.devices = deviceWithStateAt('2026-10-15T10:00:05Z', '{"at":1,"rules":{"r1":');
  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 2);
});

test('applyStateObject ignores a document without rules', () => {
  const { component } = createAutomationsPanel();
  component.applyStateObject(sampleState);
  component.applyStateObject({});
  component.applyStateObject(null);
  assert.equal(component.runtimeState.rules.r1.result, 'hold_pending');
});

test('without an automation device the page is offline and fetches nothing', async () => {
  const { component, window } = createAutomationsPanel();
  let called = false;
  window.fetch = async () => { called = true; return { ok: true, json: async () => ({}) }; };
  component.devices = [];
  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(component.online, false);
  assert.equal(called, false);
});

// last_message is capped at 4096 bytes and shared with the availability topic,
// so a long history arrived truncated there. The page fetches the full
// document from /api/v1/automation/history instead and only when something new
// landed on the history entity.
test('loadRuntimeState fetches the live history from its own route when the history entity changes', async () => {
  const { component, window } = createAutomationsPanel();
  const document = { at: 1, rule_count: 1, rules: { r1: [{ at: 1, result: 'fired', test: false, actions: [] }] } };
  const calls = [];
  const history = () => calls.filter(url => url.includes('/automation/history'));
  window.fetch = async (url) => {
    calls.push(String(url));
    return { ok: true, json: async () => (String(url).includes('/automation/state') ? sampleState : document) };
  };
  const deviceWithHistoryAt = at => [{
    id: 'automation',
    entities: [
      {
        object_id: 'state', available: true, value: '1',
        last_message: { topic: 'outstation/automation/state', payload: JSON.stringify(sampleState) },
      },
      {
        object_id: 'history', available: true, value: '1',
        // truncated by the registry, the page must not rely on it
        last_message: { topic: 'outstation/automation/history', payload: '{"at":1,"rules":{"r1":[...', at },
      },
    ],
  }];
  component.devices = deviceWithHistoryAt('2026-10-10T10:00:00Z');
  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.deepEqual(history().map(url => url.replace(/^.*(\/api\/)/, '$1')), ['/api/v1/automation/history']);
  assert.equal(component.historyEntries({ id: 'r1', history_enabled: true }).length, 1);

  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(history().length, 1, 'an unchanged history entity must not refetch');

  component.devices = deviceWithHistoryAt('2026-10-10T10:01:00Z');
  component.loadRuntimeState();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(history().length, 2);
});

test('applyHistoryDocument keeps the shown history on an empty 204 body', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument({ at: 1, rule_count: 1, rules: { r1: [{ at: 1, result: 'fired', test: false, actions: [] }] } });
  component.applyHistoryDocument({});
  assert.equal(component.historyEntries({ id: 'r1', history_enabled: true }).length, 1);
});

test('testState refuses while the service is offline', () => {
  const component = panelWithState(sampleState, { online: false });
  component.savedDocument = { rules: [sampleRule] };
  const state = component.testState(sampleRule, 0);
  assert.equal(state.allowed, false);
  assert.match(state.reason, /offline/i);
});

test('testState refuses while the rule has unsaved changes', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  component.document.rules[0].conditions[0].threshold = 900;
  const state = component.testState(component.document.rules[0], 0);
  assert.equal(state.allowed, false);
  assert.match(state.reason, /speichern/i);
});

test('testState allows a saved rule on an online service', () => {
  const component = panelWithState(sampleState);
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  assert.equal(component.testState(component.document.rules[0], 0).allowed, true);
});

test('testState refuses without the automations role', () => {
  const component = panelWithState(sampleState, { hasRole: false });
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  const state = component.testState(component.document.rules[0], 0);
  assert.equal(state.allowed, false);
  assert.match(state.reason, /Berechtigung/i);
});

test('confirmTest asks before a switching action', () => {
  const component = panelWithState(sampleState);
  assert.equal(component.confirmTest(sampleRule, 0).required, true);
  assert.match(component.confirmTest(sampleRule, 0).question, /werkstatt\/heizstab\/set/);
});

test('confirmTest does not ask for a notification', () => {
  const notifyRule = { ...sampleRule, actions: [{ type: 'notification', severity: 'info', title: 'T', message: 'M' }] };
  const component = panelWithState({
    online: true,
    rules: { r1: { result: 'fired', conditions: [], actions: [{ severity: 'info', title: 'T', message: 'M', blocked: false }] } },
  });
  assert.equal(component.confirmTest(notifyRule, 0).required, false);
});

test('testAction posts rule_id, action_index and the CSRF token', async () => {
  const { component, window } = createAutomationsPanel();
  component.runtimeState = sampleState;
  component.online = true;
  component.hasAutomationsRole = true;
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  component.csrfToken = 'token-123';
  const calls = [];
  window.fetch = async (url, options) => {
    calls.push({ url, options });
    return { ok: true, json: async () => ({ status: 'requested' }) };
  };
  component.awaitTestResult = async () => ({ status: 'published', topic: 'werkstatt/heizstab/set', payload: 'on' });
  await component.testAction(component.document.rules[0], 0, { skipConfirm: true });
  assert.equal(calls.length, 1);
  assert.match(calls[0].url, /\/api\/v1\/automations\/test$/);
  assert.equal(calls[0].options.method, 'POST');
  assert.equal(calls[0].options.headers['X-CSRF-Token'], 'token-123');
  assert.deepEqual(JSON.parse(calls[0].options.body), { rule_id: 'r1', action_index: 0 });
});

test('testAction does nothing when testing is not allowed', async () => {
  const { component, window } = createAutomationsPanel();
  component.runtimeState = sampleState;
  component.online = false;
  component.hasAutomationsRole = true;
  component.document = { version: 1, settings: {}, rules: [JSON.parse(JSON.stringify(sampleRule))] };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  let called = false;
  window.fetch = async () => { called = true; return { ok: true, json: async () => ({}) }; };
  await component.testAction(component.document.rules[0], 0, { skipConfirm: true });
  assert.equal(called, false);
});

test('computeAutoPublishPrefixes collects and sorts unique command_topics from known entities', () => {
  const { component } = createAutomationsPanel();
  component.entities = [
    { command_topic: 'werkstatt/wallbox/set' },
    { command_topic: 'werkstatt/heizstab/set' },
    { command_topic: 'werkstatt/wallbox/set' },
    {},
  ];
  assert.deepEqual(JSON.parse(JSON.stringify(component.computeAutoPublishPrefixes())), ['werkstatt/heizstab/set', 'werkstatt/wallbox/set']);
});

test('save() fills an empty prefix field with all known command_topics and flags it auto, without changing the displayed field', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: [], publish_allowed_prefixes_auto: false },
    rules: [],
  };
  component.csrfToken = 'tok';
  component.pollRuntimeStatus = async () => {};
  let sentBody;
  window.fetch = async (url, options) => {
    if (options && options.method === 'PUT') { sentBody = JSON.parse(options.body); return { ok: true, json: async () => ({}) }; }
    // save() refreshes the device catalog (A6) before computing auto prefixes.
    if (url.endsWith('/api/v1/devices')) {
      return {
        ok: true,
        json: async () => [
          { id: 'wallbox', name: 'Wallbox', entities: [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set' }] },
          { id: 'heizstab', name: 'Heizstab', entities: [{ unique_id: 'heizstab_power', command_topic: 'werkstatt/heizstab/set' }] },
        ],
      };
    }
    return { ok: true, json: async () => ({}) };
  };
  await component.save();
  assert.deepEqual(sentBody.settings.publish_allowed_prefixes, ['werkstatt/heizstab/set', 'werkstatt/wallbox/set']);
  assert.equal(sentBody.settings.publish_allowed_prefixes_auto, true);
  // Die Anzeige bleibt leer - der Default sieht nicht wie eine manuelle Eingabe aus.
  assert.deepEqual(component.document.settings.publish_allowed_prefixes, []);
});

test('save() stores exactly the typed prefixes and clears the auto flag', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: ['werkstatt/'], publish_allowed_prefixes_auto: true },
    rules: [],
  };
  component.csrfToken = 'tok';
  component.pollRuntimeStatus = async () => {};
  let sentBody;
  window.fetch = async (url, options) => {
    if (options && options.method === 'PUT') { sentBody = JSON.parse(options.body); return { ok: true, json: async () => ({}) }; }
    // save() refreshes the device catalog (A6) before computing auto prefixes.
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({}) };
  };
  await component.save();
  assert.deepEqual(sentBody.settings.publish_allowed_prefixes, ['werkstatt/']);
  assert.equal(sentBody.settings.publish_allowed_prefixes_auto, false);
});

function validScenario(id) {
  return {
    id, name: 'Nulleinspeisung', enabled: false,
    when: { conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 }] },
    on_start: { actions: [] },
    while: { mode: 'none' },
    on_end: { actions: [] },
  };
}

test('save keeps scenarios in the document', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: ['werkstatt/'], publish_allowed_prefixes_auto: false },
    rules: [],
    scenarios: [validScenario('nulleinspeisung')],
  };
  component.csrfToken = 'tok';
  component.pollRuntimeStatus = async () => {};
  let sentBody;
  window.fetch = async (url, options) => {
    if (options && options.method === 'PUT') { sentBody = JSON.parse(options.body); return { ok: true, json: async () => ({}) }; }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({}) };
  };
  await component.save();
  assert.deepEqual(JSON.parse(JSON.stringify(sentBody.scenarios)), [validScenario('nulleinspeisung')]);
});

test('load() displays an empty prefix field even though the auto-filled list is stored', async () => {
  const { component, window } = createAutomationsPanel();
  window.fetch = async (url) => {
    if (url.endsWith('/api/v1/configurations/automation_rules')) {
      return {
        ok: true,
        json: async () => ({
          version: 1,
          settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                      publish_allowed_prefixes: ['werkstatt/wallbox/set'], publish_allowed_prefixes_auto: true },
          rules: [],
        }),
      };
    }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    if (url.endsWith('/api/v1/auth/session')) return { ok: true, json: async () => ({ csrf_token: 'tok', automations: true }) };
    throw new Error(`unexpected fetch ${url}`);
  };
  await component.load();
  assert.deepEqual(JSON.parse(JSON.stringify(component.document.settings.publish_allowed_prefixes)), []);
  assert.equal(component.document.settings.publish_allowed_prefixes_auto, true);
  // load() startet den Sekundentakt fuer "erfüllt seit" (setInterval); ohne
  // window.close() haelt jsdoms interner Timer den Testprozess am Leben.
  window.close();
});

test('load() fills in missing history settings with their Python defaults', async () => {
  // Ein auf der Platte gespeichertes Dokument von vor Spec C hat weder
  // history_limit noch history_persist gesetzt - ohne den Merge in load()
  // wuerde die Oberflaeche history_persist als "aus" anzeigen, obwohl der
  // Python-Dienst dafuer seinen eigenen Default (true) anwendet.
  const { component, window } = createAutomationsPanel();
  window.fetch = async (url) => {
    if (url.endsWith('/api/v1/configurations/automation_rules')) {
      return {
        ok: true,
        json: async () => ({
          version: 1,
          settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                      publish_allowed_prefixes: [], publish_allowed_prefixes_auto: false },
          rules: [],
        }),
      };
    }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    if (url.endsWith('/api/v1/auth/session')) return { ok: true, json: async () => ({ csrf_token: 'tok', automations: true }) };
    throw new Error(`unexpected fetch ${url}`);
  };
  await component.load();
  assert.equal(component.document.settings.history_limit, 10);
  assert.equal(component.document.settings.history_persist, true);
  window.close();
});

// Regression: awaitTestResult() must read the full result document from
// last_message.payload, not the templated "value" field. test_result has a
// value_template ({{ value_json.status }}) like "state" does further up -
// .value only ever holds the bare status string (e.g. "published"), which
// is not valid JSON and can never match rule_id/action_index/at.
test('awaitTestResult ignores a result that predates the request', async () => {
  const component = panelWithState(sampleState);
  component.devices = [{
    id: 'automation',
    // sentAt (ms) / 1000 = 5 Sekunden; ein "at" von 2 Sekunden liegt davor.
    entities: [{
      object_id: 'test_result',
      available: true,
      value: 'published', // value_template-reduzierter Status, nicht das Dokument
      last_message: { topic: 'outstation/automation/test/result', payload: JSON.stringify({ at: 2, rule_id: 'r1', action_index: 0, status: 'published' }) },
    }],
  }];
  const result = await component.awaitTestResult('r1', 0, 5000, { timeoutMs: 20, pollMs: 5 });
  assert.equal(result, null, 'ein aelteres Ergebnis darf nicht als Antwort auf die neue Anfrage gelten');
});

test('awaitTestResult accepts a matching, newer result', async () => {
  const component = panelWithState(sampleState);
  component.devices = [{
    id: 'automation',
    entities: [{
      object_id: 'test_result',
      available: true,
      value: 'blocked',
      last_message: { topic: 'outstation/automation/test/result', payload: JSON.stringify({ at: 9000, rule_id: 'r1', action_index: 0, status: 'blocked', reason: 'topic does not match any allowed prefix' }) },
    }],
  }];
  const result = await component.awaitTestResult('r1', 0, 5000, { timeoutMs: 50, pollMs: 5 });
  assert.equal(result.status, 'blocked');
});

test('battery_soc als Bilanzfeld bekommt eine Prozent-Skala, die kWh-Felder nicht', () => {
  const automations = view();
  assert.equal(automations.isPercentCondition({ type: 'balance_threshold', field: 'battery_soc' }), true);
  assert.equal(automations.isPercentCondition({ type: 'balance_threshold', field: 'battery_energy_kwh' }), false);

  const described = automations.describeCondition({
    type: 'balance_threshold', field: 'battery_soc', comparison: 'below', threshold: 20,
  });
  assert.equal(described.unit, '%');
  assert.match(described.title, /Füllstand/);
});

const readableDevices = [{
  id: 'bms', name: 'BMS',
  entities: [
    { unique_id: 'bank_a_soc', name: 'Bank A', state_topic: 'outstation/bms/state',
      value_template: '{{ value_json.soc_a }}', template_supported: true,
      unit_of_measurement: '%', device_class: 'battery' },
    { unique_id: 'exotic', name: 'Exotisch', state_topic: 'outstation/bms/state',
      value_template: '{{ value_json.a.b.c }}', template_supported: false },
    { unique_id: 'no_state', name: 'Ohne State-Topic', command_topic: 'outstation/x/set',
      template_supported: true },
  ],
}];

test('flattenReadableEntities filtert auf state_topic und template_supported', () => {
  const { component } = createAutomationsPanel();
  const readable = component.flattenReadableEntities(readableDevices);
  assert.deepEqual(JSON.parse(JSON.stringify(readable.map((entity) => entity.unique_id))), ['bank_a_soc']);
});

test('computeConditionDrift meldet Gleichstand, Abweichung und fehlende Entität', () => {
  const { component } = createAutomationsPanel();
  const entities = component.flattenReadableEntities(readableDevices);

  const same = component.computeConditionDrift(
    { entity_id: 'bank_a_soc', topic: 'outstation/bms/state', value_template: '{{ value_json.soc_a }}' },
    entities);
  assert.equal(same.drifted, false);
  assert.equal(same.missing, false);

  const moved = component.computeConditionDrift(
    { entity_id: 'bank_a_soc', topic: 'outstation/alt/state', value_template: '{{ value_json.soc_a }}' },
    entities);
  assert.equal(moved.drifted, true);
  assert.equal(moved.currentTopic, 'outstation/bms/state');

  const retemplated = component.computeConditionDrift(
    { entity_id: 'bank_a_soc', topic: 'outstation/bms/state', value_template: '{{ value_json.old }}' },
    entities);
  assert.equal(retemplated.drifted, true);
  assert.equal(retemplated.currentTemplate, '{{ value_json.soc_a }}');

  const gone = component.computeConditionDrift(
    { entity_id: 'weg', topic: 'outstation/bms/state', value_template: '' }, entities);
  assert.equal(gone.missing, true);
  assert.equal(gone.drifted, false);
});

test('Panel-describeAction löst entity_id über die steuerbaren Entitäten auf', () => {
  const { component } = createAutomationsPanel();
  component.entities = component.flattenCommandableEntities([{
    id: 'plug', name: 'Plug',
    entities: [{ unique_id: 'plug_1', name: 'Werkbank-Steckdose', command_topic: 'outstation/plug_1/relay/0/set', device_class: 'power' }],
  }]);

  const resolved = component.describeAction({ type: 'publish', entity_id: 'plug_1', topic: 'outstation/plug_1/relay/0/set' });
  assert.equal(resolved.title, 'Werkbank-Steckdose');

  const missing = component.describeAction({ type: 'publish', entity_id: 'weg', topic: 'outstation/weg/set' });
  assert.equal(missing.title, 'outstation/weg/set');
});

test('describeCondition beschreibt entity_value mit und ohne aufgelöste Entität', () => {
  const condition = { type: 'entity_value', entity_id: 'bank_a_soc',
                      topic: 'outstation/bms/state', comparison: 'below', value: 20 };
  const entity = { unique_id: 'bank_a_soc', name: 'Bank A',
                   unit_of_measurement: '%', device_class: 'battery' };

  const withEntity = view().describeCondition(condition, entity);
  assert.equal(withEntity.title, 'Bank A');
  assert.equal(withEntity.unit, '%');
  assert.equal(withEntity.icon, 'ico-battery');

  const without = view().describeCondition(condition, null);
  assert.equal(without.title, 'outstation/bms/state');
  assert.equal(without.unit, '');
});

test('isPercentCondition zieht die Einheit für entity_value aus der Entität', () => {
  const condition = { type: 'entity_value', entity_id: 'bank_a_soc' };
  assert.equal(view().isPercentCondition(condition, { unit_of_measurement: '%' }), true);
  assert.equal(view().isPercentCondition(condition, { unit_of_measurement: 'W' }), false);
  assert.equal(view().isPercentCondition(condition, null), false);

  const scale = view().meterScale(condition, { value: 55, target: 20 }, { unit_of_measurement: '%' });
  assert.equal(scale.min, 0);
  assert.equal(scale.max, 100);
});

test('describeCondition bleibt für die drei Alt-Typen unverändert', () => {
  assert.equal(view().describeCondition(
    { type: 'time_window', start: '08:00', end: '18:00' }).title, 'Zeitfenster');
  assert.equal(view().describeCondition(
    { type: 'topic_value', topic: 'werkstatt/x', comparison: 'equals', text: 'ON' }).icon, 'ico-topic');
  assert.equal(view().describeCondition(
    { type: 'balance_threshold', field: 'pv', comparison: 'above', threshold: 800 }).title, 'PV-Leistung');
});

test('describeCondition renders a balance_threshold summary with hysteresis via i18n in German', () => {
  const { window } = createAutomationsPanel();
  const deCatalog = JSON.parse(fs.readFileSync(
    path.join(here, '..', 'internal', 'webui', 'catalogs', 'de.json'), 'utf8'));
  const described = view().describeCondition(
    { type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 500, hysteresis: 100 });
  // German summary: "{comparison} {threshold} {unit}, Hysterese {hysteresis}"
  // Should be: "über 500 W, Hysterese 100"
  assert.equal(described.summary, 'über 500 W, Hysterese 100');
});

test('describeCondition renders a balance_threshold summary with hysteresis via i18n in English', () => {
  const { window } = createAutomationsPanel();
  installI18n(window, { lang: 'en' });
  const view_en = window.__automationsView;
  const described = view_en.describeCondition(
    { type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 500, hysteresis: 100 });
  assert.equal(described.summary, 'above 500 W, hysteresis 100');
});

// --- A6: der SSE-Tick zieht nicht mehr den ganzen Geraetepark ---------------

function stubFetch(window, handler) {
  const calls = [];
  window.fetch = async (url, options) => {
    calls.push(url);
    const body = handler(url, options);
    if (body === undefined) return { ok: false, status: 404, json: async () => ({ message: 'Gerät wurde nicht gefunden' }) };
    return { ok: true, status: 200, json: async () => body };
  };
  return calls;
}

function automationDeviceView({ rulesEnabled = true, available = true } = {}) {
  return {
    id: 'automation',
    entities: [{
      object_id: 'state',
      available,
      value: String(rulesEnabled),
      last_message: { payload: JSON.stringify({ rules_enabled: rulesEnabled, matches: [] }) },
    }],
  };
}

test('refreshFromRegistry holt nur das Automations-Geraet, nicht die Liste', async () => {
  const { component, window } = createAutomationsPanel();
  const calls = stubFetch(window, () => automationDeviceView({ rulesEnabled: true }));
  await component.refreshFromRegistry();
  // Der Zustand selbst kommt seit der eigenen Route ueber /api/v1/automation/state.
  assert.deepEqual(calls.filter(url => url.startsWith('/api/v1/devices')), ['/api/v1/devices/automation']);
});

test('refreshFromRegistry laesst die Auswahllisten unangetastet', async () => {
  const { component, window } = createAutomationsPanel();
  component.devices = [{ id: 'wallbox', name: 'Wallbox', entities: [] }];
  component.entities = [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/wallbox/set' }];
  component.readableEntities = [{ unique_id: 'wallbox_state', state_topic: 'werkstatt/wallbox/state' }];
  stubFetch(window, () => automationDeviceView());
  await component.refreshFromRegistry();
  assert.equal(component.entities.length, 1);
  assert.equal(component.readableEntities.length, 1);
  assert.equal(component.devices.length, 1);
});

test('refreshFromRegistry uebernimmt Verfuegbarkeit aus dem Einzelgeraet und den Zustand aus der eigenen Route', async () => {
  const { component, window } = createAutomationsPanel();
  stubFetch(window, (url) => (String(url).includes('/api/v1/automation/state')
    ? { rules: {}, rules_enabled: false }
    : automationDeviceView({ rulesEnabled: false, available: true })));
  await component.refreshFromRegistry();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(component.online, true);
  assert.equal(component.runtimeState.rules_enabled, false);
});

test('eine Instanz ohne Automations-Dienst wird offline, nicht fehlerhaft', async () => {
  const { component, window, stores } = createAutomationsPanel();
  component.online = true;
  stubFetch(window, () => undefined); // 404
  await component.refreshFromRegistry();
  assert.equal(component.online, false);
  assert.equal(stores.toasts.items.length, 0);
});

test('pollRuntimeStatus fragt die Konfigurationsstatus-Route', async () => {
  const { component, window, stores } = createAutomationsPanel();
  const calls = stubFetch(window, () => ({
    received: true, runtime_status: 'ok', config_revision: 'test-checksum'
  }));
  component.statusPollMs = 0;
  await component.pollRuntimeStatus('test-checksum');
  assert.deepEqual(calls, ['/api/v1/configurations/automation_rules/status']);
  assert.equal(stores.toasts.items[0].message, 'übernommen');
});

// save() ruft am Ende pollRuntimeStatus(), das bis zu 15 s lang pollt.
// Deshalb liefert der Stub fuer /api/v1/configurations/automation_rules/status hier sofort ein
// runtime_status: 'ok' - sonst laeuft der Test in den Deadline.
test('save frischt den Geraetekatalog auf, bevor es die Auto-Praefixe bildet', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = { rules: [], settings: { publish_allowed_prefixes: [] } };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  // Der Katalog im Speicher ist veraltet: das Geraet hat inzwischen ein
  // anderes command_topic. Ohne Auffrischung wuerde ein fail-closed falscher
  // Praefix-Satz gespeichert - ein fachlicher Fehler, kein Darstellungsproblem.
  component.devices = [];
  component.entities = [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/alt/set' }];
  component.statusPollMs = 0;
  let saved = null;
  stubFetch(window, (url, options) => {
    if (url === '/api/v1/devices') {
      return [{ id: 'wallbox', name: 'Wallbox', entities: [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/neu/set' }] }];
    }
    if (url === '/api/v1/configurations/automation_rules/status') {
      return { received: true, runtime_status: 'ok', config_revision: 'test-checksum' };
    }
    saved = JSON.parse(options.body);
    return { checksum: 'test-checksum' };
  });
  await component.save();
  assert.deepEqual(saved.settings.publish_allowed_prefixes, ['werkstatt/neu/set']);
  assert.equal(saved.settings.publish_allowed_prefixes_auto, true);
});

// --- Typ-Kacheln und Hilfetexte -------------------------------------------

test('CONDITION_TYPES liefert genau die vier anbietbaren Bedingungstypen', () => {
  const { component } = createAutomationsPanel();
  const types = component.conditionTypes();
  // Spread statt direktem .map()-Vergleich: das Array kommt aus dem vm-Sandbox-
  // Realm der Komponente, deepEqual vergleicht sonst auch das Array-Prototyp.
  assert.deepEqual([...types.map((entry) => entry.key)], ['balance', 'entity', 'time', 'sun']);
  assert.equal(types[0].label, 'Energiewert');
  for (const entry of types) {
    assert.ok(entry.icon.startsWith('ico-'), `${entry.key} hat kein Sprite-Icon`);
    assert.ok(entry.hint.length > 10, `${entry.key} hat keinen Erklaertext`);
  }
});

test('ACTION_TYPES markiert den rohen MQTT-Befehl als fortgeschritten', () => {
  const { component } = createAutomationsPanel();
  const types = component.actionTypes();
  assert.deepEqual([...types.map((entry) => entry.key)], ['entity', 'notify', 'mqtt']);
  assert.equal(types.find((entry) => entry.key === 'mqtt').advanced, true);
  assert.equal(types.find((entry) => entry.key === 'entity').advanced, false);
});

test('fieldHelp liefert fuer jeden Fachbegriff einen Text und sonst einen leeren String', () => {
  const { component } = createAutomationsPanel();
  for (const key of ['hysteresis', 'hold_seconds', 'cooldown_seconds', 'tick_interval_s',
                     'settling_seconds', 'balance_max_age_s', 'publish_allowed_prefixes',
                     'history_limit', 'history_persist', 'history_enabled',
                     'retain', 'scale', 'offset', 'json_key', 'topic', 'payload']) {
    assert.ok(component.fieldHelp(key).length > 10, `kein Hilfetext fuer ${key}`);
  }
  assert.equal(component.fieldHelp('gibtesnicht'), '');
});

// --- Hinzufuegen ueber die Typ-Kacheln --------------------------------------

test('addCondition legt je Schluessel die richtige Bedingung an', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [], actions: [] };

  component.addCondition(rule, 'balance');
  assert.equal(rule.conditions[0].type, 'balance_threshold');
  assert.equal(rule.conditions[0].field, 'grid_export');
  assert.equal(rule.conditions[0].comparison, 'above');
  assert.equal(rule.conditions[0].threshold, 800);
  assert.equal(rule.conditions[0].hold_seconds, 300);

  component.addCondition(rule, 'entity');
  assert.equal(rule.conditions[1].type, 'entity_value');

  component.addCondition(rule, 'time');
  assert.equal(rule.conditions[2].type, 'time_window');
  assert.equal(rule.conditions[2].start, '08:00');
});

test('addCondition ignoriert einen unbekannten Schluessel, statt Muell anzulegen', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [], actions: [] };
  component.addCondition(rule, 'gibtesnicht');
  assert.equal(rule.conditions.length, 0);
});

test('addAction legt Benachrichtigung und rohen MQTT-Befehl an', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [], actions: [] };

  component.addAction(rule, 'notify');
  assert.equal(rule.actions[0].type, 'notification');
  assert.equal(rule.actions[0].severity, 'info');

  component.addAction(rule, 'mqtt');
  assert.equal(rule.actions[1].type, 'publish');
  assert.equal(rule.actions[1].payload_source, 'constant');
  assert.equal(rule.actions[1].topic, '');
});

test('addAction mit "entity" legt keine Aktion an, sondern oeffnet den Geraete-Assistenten', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [], actions: [] };
  const scope = { showEntityWizard: false };
  component.addAction(rule, 'entity', scope);
  assert.equal(rule.actions.length, 0);
  assert.equal(scope.showEntityWizard, true);
});

// --- Loeschen mit Rueckfrage ------------------------------------------------

test('removeRule fragt vor dem Loeschen nach und entfernt erst nach Bestaetigung', async () => {
  const { component, stores } = createAutomationsPanel();
  component.document.rules = [{ id: 'r1', name: 'Warmwasser' }, { id: 'r2', name: 'Wallbox' }];

  await component.removeRule('r1');

  assert.equal(stores.modal.calls.length, 1);
  assert.match(stores.modal.calls[0].title, /Warmwasser/);
  assert.equal(stores.modal.calls[0].danger, true);
  assert.deepEqual(component.document.rules.map((rule) => rule.id), ['r2']);
});

test('removeRule laesst die Regel stehen, wenn die Rueckfrage verneint wird', async () => {
  const { component } = createAutomationsPanel();
  const modal = { calls: [], async confirm(options) { this.calls.push(options); return false; } };
  component.$store.modal = modal;
  component.document.rules = [{ id: 'r1', name: 'Warmwasser' }];

  await component.removeRule('r1');

  assert.equal(modal.calls.length, 1);
  assert.deepEqual(component.document.rules.map((rule) => rule.id), ['r1']);
});

test('historyResultInfo maps result codes to tone and German label', () => {
  const { component } = createAutomationsPanel();
  assert.deepEqual(JSON.parse(JSON.stringify(component.historyResultInfo('fired'))), { tone: 'ok', label: 'Ausgelöst' });
  assert.deepEqual(JSON.parse(JSON.stringify(component.historyResultInfo('blocked'))), { tone: 'bad', label: 'Blockiert' });
  assert.deepEqual(JSON.parse(JSON.stringify(component.historyResultInfo('error'))), { tone: 'bad', label: 'Fehler' });
});

test('historyActionText renders publish and notification entries', () => {
  const { component } = createAutomationsPanel();
  assert.equal(
    component.historyActionText({ type: 'publish', topic: 'werkstatt/x/set', payload: '1', blocked: false }),
    'werkstatt/x/set → 1',
  );
  assert.equal(
    component.historyActionText({ type: 'publish', topic: 'werkstatt/x/set', blocked: true, reason: 'kein Präfix erlaubt' }),
    'kein Präfix erlaubt',
  );
  assert.equal(
    component.historyActionText({ type: 'notification', severity: 'info', title: 'T', message: 'M' }),
    '„T" — M',
  );
});

test('toggleHistory only flips visibility, it never fetches', () => {
  const { component, window } = createAutomationsPanel();
  let calls = 0;
  window.fetch = async () => { calls += 1; throw new Error('toggleHistory should not fetch anymore'); };
  const rule = { id: 'r1', history_enabled: true };
  component.toggleHistory(rule);
  assert.equal(component.historyExpanded.r1, true);
  assert.equal(calls, 0);
  component.toggleHistory(rule);
  assert.equal(component.historyExpanded.r1, false);
  assert.equal(calls, 0);
});

test('historyEntries reads the live per-rule history set by applyHistoryDocument', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument({
    at: 1, rule_count: 1,
    rules: { r1: [{ at: 1, result: 'fired', test: false, actions: [] }] },
  });
  const rule = { id: 'r1', history_enabled: true };
  assert.equal(component.historyEntries(rule).length, 1);
  assert.equal(component.historyEntries(rule)[0].result, 'fired');
});

test('historyEntries is empty for a rule missing from the live history document', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument({ at: 1, rule_count: 0, rules: {} });
  const rule = { id: 'r1', history_enabled: true };
  assert.deepEqual(JSON.parse(JSON.stringify(component.historyEntries(rule))), []);
});

test('applyHistoryDocument ignores a document without a rules object', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument(1);
  assert.deepEqual(JSON.parse(JSON.stringify(component.liveHistory)), {});
});

test('pollRuntimeStatus reports the result of its own revision', async () => {
  const { component, window, stores } = createAutomationsPanel();
  const statuses = [
    { received: true, runtime_status: 'ok', config_revision: 'old' },
    { received: true, runtime_status: 'ok', config_revision: 'new' },
  ];
  window.fetch = async url => {
    assert.match(String(url), /\/api\/v1\/configurations\/automation_rules\/status$/);
    return { ok: true, json: async () => statuses.shift() };
  };
  component.statusPollMs = 0;
  await component.pollRuntimeStatus('new');
  assert.equal(stores.toasts.last('info'), 'übernommen');
});

// --- Wochentage, Sonnenzeit, Standort -------------------------------------

test('formatWeekdays fasst zusammenhaengende Tage zu Spannen zusammen', () => {
  assert.equal(view().formatWeekdays([]), 'täglich');
  assert.equal(view().formatWeekdays([0, 1, 2, 3, 4, 5, 6]), 'täglich');
  assert.equal(view().formatWeekdays([0, 1, 2, 3, 4]), 'Mo–Fr');
  assert.equal(view().formatWeekdays([6, 5]), 'Sa, So');
  assert.equal(view().formatWeekdays([0, 2, 3, 4]), 'Mo, Mi–Fr');
  assert.equal(view().formatWeekdays([4]), 'Fr');
});

test('describeCondition nennt die gewaehlten Wochentage im Zeitfenster', () => {
  const plain = view().describeCondition({ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] });
  assert.equal(plain.summary, '08:00 – 18:00');
  const workdays = view().describeCondition({ type: 'time_window', start: '08:00', end: '18:00', weekdays: [0, 1, 2, 3, 4] });
  assert.equal(workdays.summary, '08:00 – 18:00, Mo–Fr');
});

test('describeCondition beschreibt ein Sonnenfenster mit Versatz', () => {
  const described = view().describeCondition({
    type: 'sun_window', from: 'sunset', from_offset_min: -30, to: 'sunrise', to_offset_min: 15, weekdays: [5, 6] });
  assert.equal(described.title, 'Sonnenzeit');
  assert.equal(described.icon, 'ico-sun');
  assert.equal(described.summary, 'Sonnenuntergang −30 min – Sonnenaufgang +15 min, Sa, So');
  const plain = view().describeCondition({
    type: 'sun_window', from: 'sunrise', from_offset_min: 0, to: 'sunset', to_offset_min: 0, weekdays: [] });
  assert.equal(plain.summary, 'Sonnenaufgang – Sonnenuntergang');
});

test('addCondition legt fuer "sun" ein Tagfenster ohne Versatz an', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [], actions: [] };
  component.addCondition(rule, 'sun');
  assert.deepEqual(JSON.parse(JSON.stringify(rule.conditions[0])), {
    type: 'sun_window', from: 'sunrise', from_offset_min: 0, to: 'sunset', to_offset_min: 0, weekdays: [] });
});

test('toggleWeekday schaltet einen Tag um und haelt die Liste sortiert', () => {
  const { component } = createAutomationsPanel();
  const condition = { type: 'time_window', start: '08:00', end: '18:00', weekdays: [4] };
  component.toggleWeekday(condition, 0);
  assert.deepEqual([...condition.weekdays], [0, 4]);
  component.toggleWeekday(condition, 4);
  assert.deepEqual([...condition.weekdays], [0]);
  const legacy = { type: 'time_window', start: '08:00', end: '18:00' };
  component.toggleWeekday(legacy, 2);
  assert.deepEqual([...legacy.weekdays], [2]);
});

test('conditionStateText erklaert ein Sonnenfenster ohne Standort', () => {
  const component = panelWithState({ online: true, rules: { r1: { result: 'conditions_not_met',
    conditions: [{ met: false, raw_met: false, value: null, target: null, since: null, hold_remaining: 0 }], actions: [] } } });
  const rule = { id: 'r1', conditions: [{ type: 'sun_window', from: 'sunrise', to: 'sunset' }], actions: [] };
  component.document = { version: 1, settings: {}, rules: [rule] };
  assert.match(component.conditionStateText(rule, 0), /Standort/);
});

test('conditionStateText sagt bei Zeitfenstern "außerhalb" statt "Schwelle"', () => {
  const component = panelWithState({ online: true, rules: { r1: { result: 'conditions_not_met',
    conditions: [{ met: false, raw_met: false, value: '07:00', target: null, since: null, hold_remaining: 0 }], actions: [] } } });
  const rule = { id: 'r1', conditions: [{ type: 'time_window', start: '08:00', end: '18:00' }], actions: [] };
  assert.equal(component.conditionStateText(rule, 0), 'außerhalb des Zeitfensters');
});

test('needsLocation meldet eine Sonnenregel ohne Standort', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', conditions: [{ type: 'sun_window' }], actions: [] };
  component.document = { version: 1, settings: {}, rules: [rule] };
  assert.equal(component.needsLocation(rule), true);
  component.document.settings = { latitude: 52.52, longitude: 13.4 };
  assert.equal(component.needsLocation(rule), false);
  assert.equal(component.needsLocation({ conditions: [{ type: 'time_window' }] }), false);
});

function saveCapturingBody(component, window) {
  component.csrfToken = 'tok';
  component.pollRuntimeStatus = async () => {};
  const captured = {};
  window.fetch = async (url, options) => {
    if (options && options.method === 'PUT') { captured.body = JSON.parse(options.body); return { ok: true, json: async () => ({}) }; }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({}) };
  };
  return captured;
}

test('save() laesst leere Standortfelder weg', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = { version: 1, settings: { publish_allowed_prefixes: ['x/'], latitude: '', longitude: '' }, rules: [] };
  const captured = saveCapturingBody(component, window);
  await component.save();
  assert.equal('latitude' in captured.body.settings, false);
  assert.equal('longitude' in captured.body.settings, false);
});

test('save() speichert einen vollstaendigen Standort', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = { version: 1, settings: { publish_allowed_prefixes: ['x/'], latitude: 52.52, longitude: 13.405 }, rules: [] };
  const captured = saveCapturingBody(component, window);
  await component.save();
  assert.equal(captured.body.settings.latitude, 52.52);
  assert.equal(captured.body.settings.longitude, 13.405);
});

test('save() lehnt einen halben Standort ab', async () => {
  const { component, window, stores } = createAutomationsPanel();
  component.document = { version: 1, settings: { publish_allowed_prefixes: ['x/'], latitude: 52.52, longitude: '' }, rules: [] };
  const captured = saveCapturingBody(component, window);
  await component.save();
  assert.equal(captured.body, undefined);
  assert.match(stores.toasts.last("critical"), /Breiten- und Längengrad/);
});

test('geolocationAvailable verlangt einen sicheren Kontext', () => {
  const { component, window } = createAutomationsPanel();
  Object.defineProperty(window.navigator, 'geolocation', { value: { getCurrentPosition() {} }, configurable: true });
  Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true });
  assert.equal(component.geolocationAvailable(), false);
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
  assert.equal(component.geolocationAvailable(), true);
});

test('useCurrentLocation uebernimmt die Position auf drei Nachkommastellen', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = { version: 1, settings: {}, rules: [] };
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
  Object.defineProperty(window.navigator, 'geolocation', { configurable: true, value: {
    getCurrentPosition(ok) { ok({ coords: { latitude: 52.520008, longitude: 13.404954 } }); } } });
  await component.useCurrentLocation();
  assert.equal(component.document.settings.latitude, 52.52);
  assert.equal(component.document.settings.longitude, 13.405);
  assert.equal(component.locating, false);
});

test('useCurrentLocation meldet eine verweigerte Freigabe', async () => {
  const { component, window, stores } = createAutomationsPanel();
  component.document = { version: 1, settings: {}, rules: [] };
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
  Object.defineProperty(window.navigator, 'geolocation', { configurable: true, value: {
    getCurrentPosition(_ok, fail) { fail({ code: 1, message: 'denied' }); } } });
  await component.useCurrentLocation();
  assert.equal(component.document.settings.latitude, undefined);
  assert.match(stores.toasts.last("critical"), /Standort/);
});

test('client-side validation verlangt ganze Minuten im Sonnenversatz', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', name: 'Nacht', cooldown_seconds: 0, actions: [{ type: 'notification' }],
    conditions: [{ type: 'sun_window', from: 'sunset', from_offset_min: '', to: 'sunrise', to_offset_min: 0 }] };
  assert.equal(component.validateRuleBeforeSave(rule).length, 1);
  rule.conditions[0].from_offset_min = 300;
  assert.equal(component.validateRuleBeforeSave(rule).length, 1);
  rule.conditions[0].from_offset_min = -30;
  assert.equal(component.validateRuleBeforeSave(rule).length, 0);
});

test('openLocationSettings opens the advanced settings and tolerates a missing latitude field', () => {
  const { component, window } = createAutomationsPanel();
  let scheduled = 0;
  component.$nextTick = callback => { scheduled += 1; callback(); };
  assert.equal(component.settingsOpen, false);
  component.openLocationSettings();
  assert.equal(component.settingsOpen, true);
  assert.equal(scheduled, 1);
  window.close();
});

test('focusRule expands the rule and marks it for the flash', () => {
  const { component, window } = createAutomationsPanel();
  // jsdom kennt CSS.escape nicht; focusRule braucht es nur fuers Suchen des Knotens.
  window.CSS = { escape: value => value };
  component.document = { version: 1, settings: {}, rules: [{ id: 'r 1', name: 'A', enabled: false, cooldown_seconds: 30, conditions: [], actions: [] }] };
  component.focusRule('r 1');
  assert.equal(component.expanded['r 1'], true);
  assert.equal(component.flashRule, 'r 1');
});

test('customBalanceFields lists categories by label and keeps a removed one used in a rule', () => {
  const { component } = createAutomationsPanel();
  component.categories = {
    wp: { label: 'Waermepumpe', base: 'consumer' },
    sauna: { label: 'Sauna', base: 'consumer' },
  };
  component.document = { version: 1, settings: {}, rules: [
    { id: 'r1', name: 'A', enabled: false, cooldown_seconds: 60, conditions: [
      { type: 'balance_threshold', field: 'custom:gone', comparison: 'above', threshold: 100 },
    ], actions: [] },
    { id: 'r2', name: 'B', enabled: false, cooldown_seconds: 60, conditions: [
      { type: 'balance_threshold', field: 'custom:gone', comparison: 'above', threshold: 100 },
    ], actions: [
      { type: 'publish', topic: 'x', payload_source: 'balance', field: 'custom:wp' },
    ] },
  ] };
  // JSON-Umweg: das Getter-Array stammt aus dem vm-Kontext (andere Realm).
  const fields = JSON.parse(JSON.stringify(component.customBalanceFields));
  assert.deepEqual(fields.map(([field]) => field), ['custom:sauna', 'custom:wp', 'custom:gone']);
  assert.equal(fields[0][1], 'Sauna');
  assert.equal(fields[1][1], 'Waermepumpe');
  assert.match(fields[2][1], /gone/);
});

test('describeCondition shows custom categories with their label, unit and base icon', () => {
  const categories = { sauna: { label: 'Sauna', base: 'consumer' }, gone: undefined };
  const sauna = view().describeCondition(
    { type: 'balance_threshold', field: 'custom:sauna', comparison: 'above', threshold: 500 }, null, categories);
  assert.equal(sauna.title, 'Sauna');
  assert.equal(sauna.unit, 'W');
  assert.equal(sauna.icon, 'ico-load');
  const gone = view().describeCondition(
    { type: 'balance_threshold', field: 'custom:gone', comparison: 'above', threshold: 500 }, null, categories);
  assert.match(gone.title, /gone/);
  assert.equal(gone.unit, 'W');
  assert.equal(gone.icon, 'ico-warning');
});

test('load() stores the custom categories from /api/v1/energy/roles', async () => {
  const { component, window } = createAutomationsPanel();
  window.fetch = async (url) => {
    if (url.endsWith('/api/v1/configurations/automation_rules')) {
      return { ok: true, json: async () => ({ version: 1, settings: {}, rules: [] }) };
    }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    if (url.endsWith('/api/v1/auth/session')) return { ok: true, json: async () => ({ csrf_token: 'tok', automations: true }) };
    if (url.endsWith('/api/v1/energy/roles')) {
      return { ok: true, json: async () => ({ categories: { sauna: { label: 'Sauna', base: 'consumer', color: '#f00', icon: 'ico-load' } } }) };
    }
    throw new Error(`unexpected fetch ${url}`);
  };
  await component.load();
  assert.equal(component.categories.sauna.label, 'Sauna');
  assert.equal(component.categories.sauna.base, 'consumer');
  window.close();
});

test('the old wizard is gone from the page component', () => {
  for (const name of ['startRuleWizard', 'wizardRule', 'wizardCanAdvance', 'wizardNext', 'wizardBack', 'wizardFinish', 'isWizardRule', 'wizardShows', 'this.wizard']) {
    assert.equal(scriptSource.includes(name), false, name);
  }
});

test('the old wizard is gone from the templates and the stylesheet', () => {
  const webui = path.join(here, '..', 'internal', 'webui');
  const templateDir = path.join(webui, 'templates');
  const templates = fs.readdirSync(templateDir).filter((name) => /^automations.*\.html$/.test(name));
  assert.ok(templates.includes('automations-rows.html'));
  for (const name of templates) {
    const source = fs.readFileSync(path.join(templateDir, name), 'utf8');
    for (const marker of ['wizard-bar', 'startRuleWizard', 'wizardShows', 'isWizardRule', 'wizardCanAdvance', 'wizardNext', 'wizardBack', 'wizard.step']) {
      assert.equal(source.includes(marker), false, `${name} still contains ${marker}`);
    }
  }
  const css = fs.readFileSync(path.join(webui, 'static', 'css', 'automations.css'), 'utf8');
  assert.equal(css.includes('.wizard-'), false);
});

test('unsavedCount counts new, changed and deleted entries and settings', () => {
  const { component } = createAutomationsPanel();
  component.document = {
    version: 1, settings: { history_limit: 10 },
    rules: [JSON.parse(JSON.stringify(sampleRule))],
    scenarios: [{ id: 's1', name: 'Pumpe', enabled: false }],
  };
  component.savedDocument = JSON.parse(JSON.stringify(component.document));
  assert.equal(component.unsavedCount(), 0);
  component.document.rules[0].name = 'Geaendert';
  component.document.rules.push({ id: 'r9', name: 'Neu', conditions: [], actions: [] });
  component.document.scenarios = [];
  component.document.settings.history_limit = 20;
  assert.equal(component.unsavedCount(), 4);
});

test('addScenario marks the entry new, opens it and offers undo', () => {
  const { component } = createAutomationsPanel();
  component.addScenario();
  const scenario = component.document.scenarios[0];
  assert.match(scenario.id, /^szenario_\d+$/);
  assert.equal(component.newIds[scenario.id], true);
  assert.equal(component.scenarioExpanded[scenario.id], true);
  assert.equal(component.scenarioEditing[scenario.id], true);
  assert.equal(component.view, 'scenarios');
  assert.equal(component.undo.kind, 'scenario');
  assert.equal(component.undo.id, scenario.id);
  component.undoInsert();
});

test('undoInsert removes the new entry even after it was edited', () => {
  const { component } = createAutomationsPanel();
  component.addRule();
  const rule = component.document.rules[0];
  component.addCondition(rule, 'balance');
  rule.name = 'Geaendert';
  component.undoInsert();
  assert.equal(component.document.rules.length, 0);
  assert.equal(component.newIds[rule.id], undefined);
  assert.equal(component.undo, null);
});

test('adding is blocked at 16 rules and 8 scenarios', () => {
  const { component } = createAutomationsPanel();
  for (let i = 0; i < 16; i += 1) component.document.rules.push({ id: `r${i}`, name: 'x', conditions: [], actions: [] });
  component.addRule();
  assert.equal(component.document.rules.length, 16);
  assert.equal(component.canAddRule(), false);
  for (let i = 0; i < 8; i += 1) component.document.scenarios.push({ id: `s${i}`, name: 'x' });
  component.addScenario();
  assert.equal(component.document.scenarios.length, 8);
  assert.equal(component.canAddScenario(), false);
});

function savePanelWithFetch(component, window, sent) {
  component.csrfToken = 'tok';
  component.pollRuntimeStatus = async () => {};
  window.fetch = async (url, options) => {
    if (options && options.method === 'PUT') { sent.body = options.body; return { ok: true, json: async () => ({}) }; }
    if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({}) };
  };
}

test('save strips regulator defaults and never sends null', async () => {
  const { component, window } = createAutomationsPanel();
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: ['werkstatt/'], publish_allowed_prefixes_auto: false },
    rules: [],
    scenarios: [{
      ...validScenario('regler'),
      when: { conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: null, hold_seconds: 300 }] },
      while: { mode: 'regulate', regulator: {
        output: { topic: 'werkstatt/heizstab/set', min: 0, max: 3000, step: 1, decimals: 0 },
        gain: 0.8, deadband_w: 20, direction: 'consumer',
      } },
    }],
  };
  const sent = {};
  savePanelWithFetch(component, window, sent);
  await component.save();
  assert.ok(sent.body, 'the save must reach the server');
  assert.equal(sent.body.includes('"gain":0.8'), false);
  assert.equal(sent.body.includes('null'), false);
  assert.equal(component.document.scenarios[0].while.regulator.gain, 0.8, 'the document keeps its values');
});

test('save clears the new markers and the undo bar', async () => {
  const { component, window } = createAutomationsPanel();
  component.addScenario();
  const scenario = component.document.scenarios[0];
  scenario.when.conditions.push({ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 });
  const sent = {};
  savePanelWithFetch(component, window, sent);
  await component.save();
  assert.ok(sent.body, 'the save must reach the server');
  assert.deepEqual(Object.keys(component.newIds), []);
  assert.equal(component.undo, null);
});

// --- Szenario-Karte: Daten, Live-Punkte, Tagesexport ---------------------

const regulatorScenario = (id) => ({
  id, name: id, enabled: true,
  when: { conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 0 }] },
  on_start: { actions: [] },
  while: { mode: 'regulate', regulator: { output: { topic: 't', min: 0, max: 800 }, direction: 'producer', target_w: 0 } },
  on_end: { actions: [] },
});

const holdScenario = (id) => ({
  id, name: id, enabled: true,
  when: { conditions: [] }, on_start: { actions: [] }, while: { mode: 'none' }, on_end: { actions: [] },
});

test('regulating scenarios start expanded, others collapsed', async () => {
  const { component, window } = createAutomationsPanel();
  try {
    const doc = { version: 1, settings: {}, rules: [], scenarios: [regulatorScenario('reg'), holdScenario('plain')] };
    window.fetch = async (url) => {
      if (url.endsWith('/api/v1/configurations/automation_rules')) return { ok: true, json: async () => doc };
      if (url.endsWith('/api/v1/devices')) return { ok: true, json: async () => [] };
      if (url.endsWith('/api/v1/auth/session')) return { ok: true, json: async () => ({ csrf_token: 'tok', automations: true }) };
      if (url.endsWith('/api/v1/energy/roles')) return { ok: true, json: async () => ({ categories: {} }) };
      if (url.includes('/api/v1/energy/stats')) return { ok: true, json: async () => ({ total: { kwh: { grid_export: 1 } } }) };
      throw new Error(`unexpected fetch ${url}`);
    };
    await component.load();
    assert.equal(component.scenarioExpanded.reg, true);
    assert.notEqual(component.scenarioExpanded.plain, true);
  } finally {
    window.close();
  }
});

test('loadScenarioChart survives a missing HistoryStore', async () => {
  const { component, window } = createAutomationsPanel();
  try {
    delete window.HistoryStore;
    await component.loadScenarioChart(regulatorScenario('a'));
    assert.deepEqual(JSON.parse(JSON.stringify(component.chartPoints.a)), { grid: [], output: [] });
  } finally {
    window.close();
  }
});

test('loadScenarioChart fills raw gaps with minute rows', async () => {
  const { component, window } = createAutomationsPanel();
  try {
    const now = Date.now();
    const minute = 60000;
    // Rohwerte bis 21 min vor jetzt, 1m-Werte danach: die Luecke fuellt die Minutenreihe.
    const raw = Array.from({ length: 10 }, (_, i) => ({ series: 'role:grid', ts: now - 30 * minute + i * minute, v: 100, u: 'W' }));
    const rollup = Array.from({ length: 20 }, (_, i) => ({ series: 'role:grid', ts: now - 20 * minute + i * minute, min: 50, max: 50, avg: 50, n: 1, u: 'W' }));
    window.HistoryStore = {
      readRange: async (tier, series) => {
        if (series !== 'role:grid') return [];
        return tier === 'raw' ? raw : rollup;
      },
    };
    await component.loadScenarioChart(regulatorScenario('a'));
    const points = JSON.parse(JSON.stringify(component.chartPoints.a.grid));
    assert.equal(points.length, 30);
    assert.ok(points.every((point, index) => index === 0 || point.ts > points[index - 1].ts), 'points are ordered by time');
    assert.ok(points[points.length - 1].ts >= now / 1000 - 120, 'the chart reaches the present');
    assert.deepEqual(JSON.parse(JSON.stringify(component.chartPoints.a.output)), []);
  } finally {
    window.close();
  }
});

test('state updates append live points and drop those older than 30 minutes', () => {
  const { component } = createAutomationsPanel();
  const nowSec = Date.now() / 1000;
  component.livePoints = { a: [{ ts: nowSec - 1900, grid: 5 }, { ts: nowSec - 60, grid: 6 }] };
  component.applyStateObject({ rules: {}, scenarios: { a: { phase: 'active', net: 120, output: 300 } } });
  const points = JSON.parse(JSON.stringify(component.livePoints.a));
  assert.equal(points.length, 2);
  assert.ok(Math.abs(points[0].ts - (nowSec - 60)) < 1, 'the 30 minute old point is gone');
  assert.equal(points[0].grid, 6);
  assert.equal(points[1].grid, 120);
  assert.equal(points[1].output, 300);
  assert.ok(Math.abs(points[1].ts - nowSec) < 5, 'live points carry seconds');
});

test('gridNowText falls back to the last chart point and then to a dash', () => {
  const { component } = createAutomationsPanel();
  component.online = true;
  const scenario = regulatorScenario('a');
  const nowSec = Date.now() / 1000;
  component.nowTick = nowSec * 1000;
  component.runtimeState = { rules: {}, scenarios: { a: { phase: 'active' } } };
  component.chartPoints = { a: { grid: [{ ts: nowSec - 30, value: 250 }], output: [] } };
  assert.equal(component.gridNowText(scenario), '+250');
  component.chartPoints.a.grid[0].ts = nowSec - 300;
  assert.equal(component.gridNowText(scenario), '—');
  component.runtimeState.scenarios.a.net = -120;
  assert.equal(component.gridNowText(scenario), '−120');
});

test('today export reads grid_export from the day statistics', async () => {
  const { component, window } = createAutomationsPanel();
  try {
    let requested = '';
    window.fetch = async (url) => {
      requested = String(url);
      return { ok: true, json: async () => ({ total: { kwh: { grid_export: 12.34 } } }) };
    };
    await component.refreshTodayExport();
    assert.match(requested, /\/api\/v1\/energy\/stats\?period=day$/);
    assert.equal(component.todayExportKWh, 12.34);
    // Ein Fehler laesst den alten Wert stehen.
    window.fetch = async () => { throw new Error('offline'); };
    await component.refreshTodayExport();
    assert.equal(component.todayExportKWh, 12.34);
  } finally {
    window.close();
  }
});

test('an offline service shows no running time', () => {
  const { component } = createAutomationsPanel();
  component.online = false;
  component.runtimeState = { rules: {}, scenarios: { a: { phase: 'active', since: Date.now() / 1000 - 100 } } };
  const status = component.scenarioStatus(regulatorScenario('a'));
  assert.equal(status.key, 'offline');
  assert.equal(status.label, 'Dienst offline');
  assert.equal(status.detail, '');
});

test('regulator figures read target, band, limits and the live output', () => {
  const { component } = createAutomationsPanel();
  component.online = true;
  const scenario = regulatorScenario('a');
  component.runtimeState = { rules: {}, scenarios: { a: { phase: 'active', output: 412.4 } } };
  assert.deepEqual(JSON.parse(JSON.stringify(component.regulatorFigures(scenario))), { target: 0, band: 20, min: 0, max: 800, output: 412, direction: 'producer' });
  component.runtimeState.scenarios.a = { phase: 'active' };
  assert.equal(component.regulatorFigures(scenario).output, '—');
});

test('the tile icon is the inverter for producers and the first start action otherwise', () => {
  const { component } = createAutomationsPanel();
  assert.equal(component.scenarioIcon(regulatorScenario('a')), 'ico-inverter');
  const consumer = regulatorScenario('b');
  consumer.while.regulator.direction = 'consumer';
  assert.equal(component.scenarioIcon(consumer), 'ico-flash');
  assert.equal(component.scenarioIcon(holdScenario('c')), 'ico-power-on');
});

test('today export text uses two decimals with a comma and a dash before the first value', () => {
  const { component } = createAutomationsPanel();
  assert.equal(component.todayExportText(), '—');
  component.todayExportKWh = 12.3;
  assert.equal(component.todayExportText(), '12,30');
});

test('the phase bar marks the while phase while running and the end phase just after it ended', () => {
  const { component } = createAutomationsPanel();
  component.online = true;
  const scenario = regulatorScenario('a');
  const nowSec = Date.now() / 1000;
  component.nowTick = nowSec * 1000;
  component.runtimeState = { rules: {}, scenarios: { a: { phase: 'active', since: nowSec - 100 } } };
  assert.equal(component.activePhaseIndex(scenario), 1);
  component.runtimeState.scenarios.a = { phase: 'idle', since: nowSec - 5, conditions: [] };
  assert.equal(component.activePhaseIndex(scenario), 2);
  component.runtimeState.scenarios.a = { phase: 'idle', since: nowSec - 600, conditions: [] };
  assert.equal(component.activePhaseIndex(scenario), -1);
  const phases = JSON.parse(JSON.stringify(component.scenarioPhases(scenario)));
  assert.deepEqual(phases.map((row) => row.phase), ['start', 'while', 'end']);
  assert.ok(phases.every((row) => typeof row.label === 'string' && row.label.length > 0));
});

test('the start progress bar shows only while waiting for the start', () => {
  const { component } = createAutomationsPanel();
  const scenario = regulatorScenario('a');
  component.online = true;
  component.runtimeState = { rules: {}, scenarios: { a: { phase: 'idle', conditions: [{ value: 50, raw_met: false, met: false }] } } };
  const progress = JSON.parse(JSON.stringify(component.scenarioProgress(scenario)));
  assert.equal(progress.percent, Math.round(50 / 100 * 100));
  assert.equal(progress.label, '50 / 100 W');
  component.runtimeState.scenarios.a.phase = 'active';
  assert.equal(component.scenarioProgress(scenario), null);
});

test('ruleIcon follows the first action', () => {
  const { component } = createAutomationsPanel();
  assert.equal(component.ruleIcon({ actions: [{ type: 'notification', title: 'Hallo' }] }), 'ico-bell');
  assert.equal(component.ruleIcon({ actions: [{ type: 'publish', topic: 'werkstatt/x/set', payload_source: 'constant', payload: '1' }] }), 'ico-switch');
  assert.equal(component.ruleIcon({ actions: [] }), 'ico-flash');
  assert.equal(component.ruleIcon({}), 'ico-flash');
});

test('rulePreview lists condition states and action icons', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A',
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 0, hold_seconds: 0 }],
    actions: [{ type: 'notification', title: 'Hallo' }],
  };
  component.runtimeState = { rules: { r1: { conditions: [{ value: 900, target: 800, raw_met: true, met: true }] } } };
  const preview = JSON.parse(JSON.stringify(component.rulePreview(rule)));
  assert.equal(preview.conditions.length, 1);
  assert.equal(preview.conditions[0].state, 'met');
  assert.deepEqual(preview.actions, [{ icon: 'ico-bell' }]);
  assert.deepEqual(JSON.parse(JSON.stringify(component.rulePreview({}))), { conditions: [], actions: [] });
});

test('liveBar puts the hysteresis band below an above-threshold', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A',
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 100, hold_seconds: 300 }],
    actions: [],
  };
  component.runtimeState = { rules: { r1: { conditions: [{ value: 650, target: 800, raw_met: false, met: false }] } } };
  const bar = JSON.parse(JSON.stringify(component.liveBar(rule)));
  assert.equal(bar.fraction, 650 / 1600);
  assert.equal(bar.mark, 800 / 1600);
  assert.equal(bar.band.left, 700 / 1600);
  assert.equal(bar.band.width, 100 / 1600);
  assert.equal(bar.valueText, '650 W');
});

test('liveBar puts the hysteresis band above a below-threshold', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A',
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'below', threshold: 800, hysteresis: 100, hold_seconds: 0 }],
    actions: [],
  };
  component.runtimeState = { rules: { r1: { conditions: [{ value: 900, target: 800, raw_met: false, met: false }] } } };
  const bar = JSON.parse(JSON.stringify(component.liveBar(rule)));
  assert.equal(bar.band.left, 800 / 1600);
  assert.equal(bar.band.width, 100 / 1600);
});

test('liveBar is null without a balance condition and has no band without hysteresis', () => {
  const { component } = createAutomationsPanel();
  const timeOnly = { id: 'r1', conditions: [{ type: 'time_window', start: '08:00', end: '18:00', weekdays: [] }], actions: [] };
  assert.equal(component.liveBar(timeOnly), null);
  const plain = { id: 'r2', conditions: [{ type: 'balance_threshold', field: 'pv', comparison: 'above', threshold: 500, hysteresis: 0, hold_seconds: 0 }], actions: [] };
  assert.equal(component.liveBar(plain).band, null);
});

test('setRepeatMode once removes repeat_seconds', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', repeat_seconds: 120, actions: [] };
  component.setRepeatMode(rule, 'once');
  assert.equal('repeat_seconds' in rule, false);
});

test('setRepeatMode repeat uses 60 s or the publish minimum when that is larger', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', actions: [] };
  component.setRepeatMode(rule, 'repeat');
  assert.equal(rule.repeat_seconds, 60);
  component.document.settings.publish_min_interval_s = 90;
  component.setRepeatMode(rule, 'repeat');
  assert.equal(rule.repeat_seconds, 90);
});

test('repeat stays locked while a toggle action exists', () => {
  const { component } = createAutomationsPanel();
  const rule = { id: 'r1', actions: [{ type: 'publish', topic: 'werkstatt/x/set', payload_source: 'toggle' }] };
  assert.equal(component.setRepeatMode(rule, 'repeat'), false);
  assert.equal('repeat_seconds' in rule, false);
  component.setRepeatMode(rule, 'once');
  assert.equal('repeat_seconds' in rule, false);
});

test('row editors write into the model', () => {
  const { component } = createAutomationsPanel();
  const rule = {
    id: 'r1', name: 'A',
    conditions: [{ type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 800, hysteresis: 100, hold_seconds: 300 }],
    actions: [],
  };
  component.setComparison(rule.conditions[0], 'below');
  assert.equal(rule.conditions[0].comparison, 'below');
  component.setComparison(rule.conditions[0], 'sideways');
  assert.equal(rule.conditions[0].comparison, 'below');
  rule.conditions[0].threshold = 950;
  assert.equal(rule.conditions[0].threshold, 950);
});

test('ruleTimeline keeps only the last six hours', () => {
  const { component } = createAutomationsPanel();
  const nowSec = 1_000_000;
  component.nowTick = nowSec * 1000;
  component.liveHistory = {
    r1: [
      { at: nowSec - 7 * 3600, result: 'fired' },
      { at: nowSec - 5 * 3600, result: 'blocked' },
      { at: nowSec - 60, result: 'fired' },
      { at: nowSec + 10, result: 'fired' },
    ],
  };
  const timeline = JSON.parse(JSON.stringify(component.ruleTimeline({ id: 'r1' })));
  assert.equal(timeline.length, 2);
  assert.equal(timeline[0].x, ((nowSec - 5 * 3600) - (nowSec - 6 * 3600)) / (6 * 3600) * 600);
  assert.equal(timeline[0].tone, 'bad');
  assert.equal(timeline[1].tone, 'ok');
  assert.deepEqual(JSON.parse(JSON.stringify(component.ruleTimeline({ id: 'unknown' }))), []);
});

test('a consumer regulator reports its direction so the card takes the consumer colour', () => {
  const { component } = createAutomationsPanel();
  const consumer = regulatorScenario('a');
  consumer.while.regulator.direction = 'consumer';
  assert.equal(component.regulatorFigures(consumer).direction, 'consumer');
  assert.equal(component.regulatorFigures(regulatorScenario('b')).direction, 'producer');
});

// --- Szenario-Regler und Phasen bearbeiten (Task 8a) ---------------------

const regulatorEditScenario = () => ({
  id: 's1', name: 'Regler', enabled: false,
  when: { conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 }] },
  on_start: { actions: [] },
  while: { mode: 'regulate', regulator: {
    output: { topic: 'werkstatt/x/set', min: 0, max: 800 },
    direction: 'producer', target_w: 0, base: { source: 'last' }, fallback: { payload: '800' },
  } },
  on_end: { actions: [] },
});

test('setRegulatorOutput takes topic and bounds from the entity and seeds the fallback', () => {
  const { component } = createAutomationsPanel();
  component.entities = [{ unique_id: 'heizstab', command_topic: 'werkstatt/heizstab/set', min_value: 0, max_value: 3000, step: 1 }];
  const scenario = regulatorEditScenario();
  component.setRegulatorOutput(scenario, 'heizstab');
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.regulator.output)),
    { topic: 'werkstatt/heizstab/set', entity_id: 'heizstab', min: 0, max: 3000, step: 1 });
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.regulator.fallback)), { payload: '3000' });
});

test('regulatorOutputEntities keeps only entities with numeric bounds', () => {
  const { component } = createAutomationsPanel();
  component.entities = [
    { unique_id: 'heizstab', min_value: 0, max_value: 3000 },
    { unique_id: 'schalter', payload_on: 'ON', payload_off: 'OFF' },
    { unique_id: 'halb', min_value: 0 },
  ];
  assert.deepEqual(component.regulatorOutputEntities().map((entity) => entity.unique_id), ['heizstab']);
});

test('setRegulatorBase entity copies topic and template', () => {
  const { component } = createAutomationsPanel();
  component.readableEntities = [{ unique_id: 'pv_inv', state_topic: 'werkstatt/pv/state', value_template: '{{ value_json.power }}' }];
  const scenario = regulatorEditScenario();
  component.setRegulatorBase(scenario, 'entity', 'pv_inv');
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.regulator.base)),
    { source: 'entity', entity_id: 'pv_inv', topic: 'werkstatt/pv/state', value_template: '{{ value_json.power }}' });
  component.setRegulatorBase(scenario, 'balance');
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.regulator.base)), { source: 'balance', field: 'pv' });
  component.setRegulatorBase(scenario, 'last');
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.regulator.base)), { source: 'last' });
});

test('setRegulatorDirection keeps the fallback for consumers and seeds it for producers', () => {
  const { component } = createAutomationsPanel();
  const scenario = regulatorEditScenario();
  component.setRegulatorDirection(scenario, 'consumer');
  assert.equal(scenario.while.regulator.direction, 'consumer');
  assert.equal(scenario.while.regulator.fallback.payload, '800');
  scenario.while.regulator.fallback = undefined;
  component.setRegulatorDirection(scenario, 'producer');
  assert.equal(scenario.while.regulator.fallback.payload, '800');
});

test('turning the end condition off leaves no until key', async () => {
  const { component, window } = createAutomationsPanel();
  const scenario = { ...regulatorEditScenario(), while: { mode: 'none' },
    until: { conditions: [{ type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 300 }] } };
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: ['werkstatt/'], publish_allowed_prefixes_auto: false },
    rules: [],
    scenarios: [scenario],
  };
  component.toggleUntil(scenario, false);
  const sent = {};
  savePanelWithFetch(component, window, sent);
  await component.save();
  assert.ok(sent.body, 'the save must reach the server');
  assert.equal('until' in JSON.parse(sent.body).scenarios[0], false);
});

test('switching while mode to hold starts with a 60 s repeat', () => {
  const { component } = createAutomationsPanel();
  const scenario = { ...regulatorEditScenario() };
  component.changeWhileMode(scenario, 'hold');
  assert.equal(scenario.while.mode, 'hold');
  assert.equal(scenario.while.repeat_seconds, 60);
  assert.deepEqual(JSON.parse(JSON.stringify(scenario.while.actions)), []);
});

test('scenario condition rows read reports from the scenario state', () => {
  const { component } = createAutomationsPanel();
  const scenario = regulatorEditScenario();
  const report = { met: true, raw_met: true, value: 120, target: 100, since: 1, hold_remaining: 0 };
  component.runtimeState = { rules: {}, scenarios: { s1: { when: [report], until: [] } } };
  assert.equal(component.scenarioConditionReport(scenario, 'when', 0), report);
  assert.equal(component.scenarioConditionReport(scenario, 'until', 0), null);
  assert.equal(component.scenarioConditionReport(scenario, 'when', 3), null);
});

test('the owner hooks derive condition and action text from a report, not from a rule', () => {
  const { component } = createAutomationsPanel();
  const report = { met: true, raw_met: true, value: 120, target: 100, since: 1, hold_remaining: 0 };
  assert.equal(component.conditionStateOf(null), 'novalue');
  assert.equal(component.conditionStateOf(report), 'met');
  assert.equal(component.conditionStateOf({ value: 120, raw_met: true, met: false }), 'pending');
  assert.equal(component.conditionTextOf(null, { type: 'balance_threshold' }), component.conditionStateText({ conditions: [{ type: 'balance_threshold' }] }, 0));
  assert.equal(component.actionPreviewOf({ topic: 'a/set', payload: 'ON' }, { type: 'publish' }).text, 'a/set → ON');
  assert.equal(component.actionPreviewOf({ blocked: true, reason: 'nope' }, { type: 'publish' }).blocked, true);
});

test('the shared editors add and remove rows on scenario owners', () => {
  const { component } = createAutomationsPanel();
  const scenario = { ...regulatorEditScenario(), when: { conditions: [] }, on_start: { actions: [] } };
  component.addCondition(scenario.when, 'balance');
  component.addCondition(scenario.when, 'time');
  component.removeCondition(scenario.when, 0);
  assert.equal(scenario.when.conditions.length, 1);
  assert.equal(scenario.when.conditions[0].type, 'time_window');
  component.addAction(scenario.on_start, 'notify');
  component.addAction(scenario.on_start, 'mqtt');
  assert.equal(scenario.on_start.actions.length, 2);
  component.removeAction(scenario.on_start, 0);
  assert.equal(scenario.on_start.actions.length, 1);
  assert.equal(scenario.on_start.actions[0].type, 'publish');
});

test('scenarioRows follows the owner when the while mode changes', () => {
  const { component } = createAutomationsPanel();
  const scenario = regulatorEditScenario();
  const scope = component.scenarioRows(scenario, 'while', null);
  assert.equal(scope.rowsOwner, scenario.while);
  component.changeWhileMode(scenario, 'hold');
  assert.equal(scope.rowsOwner.mode, 'hold');
  assert.equal(scope.rowsOwner.actions.length, 0);
  assert.equal(scope.showActTiles, false);
  assert.equal(component.scenarioRows(scenario, 'until', 'until').rowsReport(0), null);
});

test('the editor keeps default fine tuning out of the saved document', async () => {
  const { component, window } = createAutomationsPanel();
  const scenario = regulatorEditScenario();
  scenario.while.regulator.gain = 0.8;
  component.document = {
    version: 1,
    settings: { tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
                publish_allowed_prefixes: ['werkstatt/'], publish_allowed_prefixes_auto: false },
    rules: [],
    scenarios: [scenario],
  };
  let sent = {};
  savePanelWithFetch(component, window, sent);
  await component.save();
  assert.equal(JSON.parse(sent.body).scenarios[0].while.regulator.gain, undefined);
  scenario.while.regulator.gain = 0.5;
  sent = {};
  savePanelWithFetch(component, window, sent);
  await component.save();
  assert.equal(JSON.parse(sent.body).scenarios[0].while.regulator.gain, 0.5);
});
