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

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'automations.page.js'),
  'utf8',
);

function createAutomationsPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
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

test('renderMiniChain yields one entry per condition plus the gate tone', () => {
  const component = panelWithState(sampleState);
  const chain = component.renderMiniChain(sampleRule);
  assert.equal(chain.conditions.length, 1);
  assert.equal(chain.conditions[0].state, 'pending');
  assert.equal(chain.actions.length, 1);
  assert.equal(chain.gate.tone, 'wait');
});

test('applyStateDocument stores the state and its timestamp', () => {
  const { component } = createAutomationsPanel();
  component.applyStateDocument(JSON.stringify(sampleState), true);
  assert.equal(component.runtimeState.rules.r1.result, 'hold_pending');
  assert.equal(component.online, true);
  assert.ok(component.lastStateAt > 0);
});

test('applyStateDocument survives an unparseable payload', () => {
  const { component } = createAutomationsPanel();
  component.applyStateDocument('{kaputt', true);
  assert.equal(component.runtimeState, null);
});

// Regression: loadRuntimeState() must read the full state document from the
// registry entity, not from the value_template-reduced "value" field. The
// Go registry has no json_attributes_topic support (that field is dropped
// during discovery-config parsing), so the "state" sensor's .value only ever
// holds the single templated number (rules_enabled) - the full document sits
// in .last_message.payload, the raw untemplated MQTT payload of the state
// topic.
test('loadRuntimeState reads the full document from last_message.payload, not the templated value', () => {
  const { component } = createAutomationsPanel();
  component.devices = [{
    id: 'automation',
    entities: [{
      object_id: 'state',
      available: true,
      value: '1', // value_template-reduced rules_enabled count, not the document
      last_message: { topic: 'outstation/automation/state', payload: JSON.stringify(sampleState) },
    }],
  }];
  component.loadRuntimeState();
  assert.equal(component.online, true);
  assert.equal(component.runtimeState.rules.r1.result, 'hold_pending');
});

test('loadRuntimeState also reads live history from the history entity\'s last_message.payload', () => {
  const { component } = createAutomationsPanel();
  component.devices = [{
    id: 'automation',
    entities: [
      {
        object_id: 'state', available: true, value: '1',
        last_message: { topic: 'outstation/automation/state', payload: JSON.stringify(sampleState) },
      },
      {
        object_id: 'history', available: true, value: '1',
        last_message: {
          topic: 'outstation/automation/history',
          payload: JSON.stringify({ at: 1, rule_count: 1, rules: { r1: [{ at: 1, result: 'fired', test: false, actions: [] }] } }),
        },
      },
    ],
  }];
  component.loadRuntimeState();
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
  assert.deepEqual(calls, ['/api/v1/devices/automation']);
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

test('refreshFromRegistry uebernimmt den Laufzeitzustand aus dem Einzelgeraet', async () => {
  const { component, window } = createAutomationsPanel();
  stubFetch(window, () => automationDeviceView({ rulesEnabled: false, available: true }));
  await component.refreshFromRegistry();
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

test('pollRuntimeStatus fragt die Einzelgeraete-Route', async () => {
  const { component, window, stores } = createAutomationsPanel();
  const calls = stubFetch(window, () => ({
    id: 'automation',
    entities: [{ object_id: 'status', value: JSON.stringify({ runtime_status: 'ok' }) }],
  }));
  await component.pollRuntimeStatus();
  assert.deepEqual(calls, ['/api/v1/devices/automation']);
  assert.equal(stores.toasts.items[0].message, 'übernommen');
});

// save() ruft am Ende pollRuntimeStatus(), das bis zu 10 s lang pollt.
// Deshalb liefert der Stub fuer /api/v1/devices/automation hier sofort ein
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
  let saved = null;
  stubFetch(window, (url, options) => {
    if (url === '/api/v1/devices') {
      return [{ id: 'wallbox', name: 'Wallbox', entities: [{ unique_id: 'wallbox_power', command_topic: 'werkstatt/neu/set' }] }];
    }
    if (url === '/api/v1/devices/automation') {
      return { id: 'automation', entities: [{ object_id: 'status', value: JSON.stringify({ runtime_status: 'ok' }) }] };
    }
    saved = JSON.parse(options.body);
    return {};
  });
  await component.save();
  assert.deepEqual(saved.settings.publish_allowed_prefixes, ['werkstatt/neu/set']);
  assert.equal(saved.settings.publish_allowed_prefixes_auto, true);
});

// --- Typ-Kacheln und Hilfetexte -------------------------------------------

test('CONDITION_TYPES liefert genau die drei anbietbaren Bedingungstypen', () => {
  const types = view().CONDITION_TYPES;
  // Spread statt direktem .map()-Vergleich: das Array kommt aus dem vm-Sandbox-
  // Realm der Komponente, deepEqual vergleicht sonst auch das Array-Prototyp.
  assert.deepEqual([...types.map((entry) => entry.key)], ['balance', 'entity', 'time']);
  assert.equal(types[0].label, 'Energiewert');
  for (const entry of types) {
    assert.ok(entry.icon.startsWith('ico-'), `${entry.key} hat kein Sprite-Icon`);
    assert.ok(entry.hint.length > 10, `${entry.key} hat keinen Erklaertext`);
  }
});

test('ACTION_TYPES markiert den rohen MQTT-Befehl als fortgeschritten', () => {
  const types = view().ACTION_TYPES;
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

// --- Assistent fuer neue Regeln --------------------------------------------

test('startRuleWizard legt eine Regel an und beginnt bei Schritt 1', () => {
  const { component } = createAutomationsPanel();
  component.startRuleWizard();
  assert.equal(component.document.rules.length, 1);
  assert.equal(component.wizard.active, true);
  assert.equal(component.wizard.step, 1);
  assert.equal(component.wizard.ruleId, component.document.rules[0].id);
});

test('der Assistent laesst Schritt 1 erst verlassen, wenn eine Bedingung steht', () => {
  const { component } = createAutomationsPanel();
  component.startRuleWizard();
  const rule = component.document.rules[0];

  assert.equal(component.wizardCanAdvance(), false);
  component.wizardNext();
  assert.equal(component.wizard.step, 1);

  component.addCondition(rule, 'balance');
  assert.equal(component.wizardCanAdvance(), true);
  component.wizardNext();
  assert.equal(component.wizard.step, 2);
});

test('der Assistent laesst Schritt 2 erst verlassen, wenn eine Aktion steht', () => {
  const { component } = createAutomationsPanel();
  component.startRuleWizard();
  const rule = component.document.rules[0];
  component.addCondition(rule, 'balance');
  component.wizardNext();

  assert.equal(component.wizardCanAdvance(), false);
  component.addAction(rule, 'notify');
  assert.equal(component.wizardCanAdvance(), true);
  component.wizardNext();
  assert.equal(component.wizard.step, 3);
});

test('wizardBack geht zurueck, aber nie vor Schritt 1', () => {
  const { component } = createAutomationsPanel();
  component.startRuleWizard();
  component.wizardBack();
  assert.equal(component.wizard.step, 1);
});

test('wizardFinish beendet den Assistenten und laesst die Regel im Bearbeiten-Modus stehen', () => {
  const { component } = createAutomationsPanel();
  component.startRuleWizard();
  const ruleId = component.wizard.ruleId;
  component.wizardFinish();
  assert.equal(component.wizard.active, false);
  assert.equal(component.editing[ruleId], true);
  assert.equal(component.expanded[ruleId], true);
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
  component.applyHistoryDocument(JSON.stringify({
    at: 1, rule_count: 1,
    rules: { r1: [{ at: 1, result: 'fired', test: false, actions: [] }] },
  }));
  const rule = { id: 'r1', history_enabled: true };
  assert.equal(component.historyEntries(rule).length, 1);
  assert.equal(component.historyEntries(rule)[0].result, 'fired');
});

test('historyEntries is empty for a rule missing from the live history document', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument(JSON.stringify({ at: 1, rule_count: 0, rules: {} }));
  const rule = { id: 'r1', history_enabled: true };
  assert.deepEqual(JSON.parse(JSON.stringify(component.historyEntries(rule))), []);
});

test('applyHistoryDocument survives an unparseable payload', () => {
  const { component } = createAutomationsPanel();
  component.applyHistoryDocument('{kaputt');
  assert.deepEqual(JSON.parse(JSON.stringify(component.liveHistory)), {});
});
