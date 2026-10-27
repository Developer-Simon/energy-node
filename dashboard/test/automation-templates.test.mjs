// Vorlagen des Automations-Assistenten (Teil 1: Nulleinspeisung und Wallbox-Ueberschuss).
// Die Vorlagen laufen in einem jsdom-Kontext mit echtem i18n und dem Engine-Modul.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';
import { buildFixture } from './tools/automation-template-fixture.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load(lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const i18n = installI18n(dom.window, { lang });
  const context = dom.getInternalVMContext();
  for (const file of ['automation-wizard.js', 'automation-templates.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, file), 'utf8'), context);
  }
  return {
    wizard: dom.window.__automationWizard,
    templates: dom.window.__automationTemplates.TEMPLATES,
    chargeFit: dom.window.__automationTemplates.chargeFit,
    t: (key, params) => i18n.t(key, params),
  };
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich durch JSON laufen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const devices = [
  {
    id: 'pv1', name: 'Wechselrichter Dach',
    entities: [
      { unique_id: 'pv1_limit', name: 'Leistungsgrenze', command_topic: 'werkstatt/pv1/limit', min_value: 50, max_value: 800, step: 10, unit_of_measurement: 'W', has_value: true, value: '500' },
      { unique_id: 'pv1_power', name: 'Leistung', state_topic: 'werkstatt/pv1/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '742' },
    ],
  },
  {
    id: 'wb', name: 'Wallbox',
    entities: [
      { unique_id: 'wb_limit', name: 'Ladeleistung', command_topic: 'werkstatt/wb/limit', min_value: 1400, max_value: 11000, step: 100, unit_of_measurement: 'W', has_value: true, value: '5000' },
      { unique_id: 'wb_switch', name: 'Laden', command_topic: 'werkstatt/wb/switch', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'ON' },
      { unique_id: 'wb_plug', name: 'Stecker', state_topic: 'werkstatt/wb/plug', template_supported: true, device_class: 'plug', has_value: true, value: 'ON' },
      { unique_id: 'wb_power', name: 'Ladeleistung Ist', state_topic: 'werkstatt/wb/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '1400' },
    ],
  },
  {
    id: 'hp', name: 'Wärmepumpe Keller',
    entities: [
      { unique_id: 'hp_setpoint', name: 'Warmwasser-Soll', command_topic: 'werkstatt/hp/setpoint', min_value: 30, max_value: 60, step: 1, unit_of_measurement: '°C', has_value: true, value: '45' },
      { unique_id: 'hp_sg', name: 'SG Ready', command_topic: 'werkstatt/hp/sg', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'OFF' },
      { unique_id: 'hp_temp', name: 'Temperatur', state_topic: 'werkstatt/hp/temp', template_supported: true, device_class: 'temperature', unit_of_measurement: '°C', has_value: true, value: '44' },
    ],
  },
  {
    id: 'bat', name: 'Batteriespeicher',
    entities: [
      { unique_id: 'bat_limit', name: 'Ladeleistung', command_topic: 'werkstatt/bat/limit', min_value: 0, max_value: 5000, step: 250, unit_of_measurement: 'W', has_value: true, value: '0' },
      { unique_id: 'bat_soc', name: 'Ladestand', state_topic: 'werkstatt/bat/soc', template_supported: true, device_class: 'battery', unit_of_measurement: '%', has_value: true, value: '46' },
    ],
  },
  {
    id: 'tibber', name: 'Tibber Preis',
    entities: [
      { unique_id: 'tibber_price', name: 'Strompreis', state_topic: 'werkstatt/tibber/price', template_supported: true, unit_of_measurement: 'ct/kWh', has_value: true, value: '12.5' },
    ],
  },
  {
    id: 'rod', name: 'Heizstab AC THOR',
    entities: [
      { unique_id: 'rod_power', name: 'Leistung', command_topic: 'werkstatt/rod/power', min_value: 0, max_value: 3000, step: 100, unit_of_measurement: 'W', has_value: true, value: '0' },
      { unique_id: 'rod_temp', name: 'Temperatur', state_topic: 'werkstatt/rod/temp', template_supported: true, device_class: 'temperature', unit_of_measurement: '°C', has_value: true, value: '52' },
    ],
  },
];

const snapshot = {
  values: { pv: 862, grid_export: 0, grid_import: 0, battery_soc: 46, battery_capacity_kwh: 10 },
  entities: [
    { device_id: 'pv1', entity_id: 'pv1_power', role: { role: 'pv' } },
    { device_id: 'wb', entity_id: 'wb_power', role: { role: 'wallbox' } },
    { device_id: 'hp', entity_id: 'hp_temp', role: { role: 'heat_pump' } },
    { device_id: 'bat', entity_id: 'bat_soc', role: { role: 'battery_soc' } },
  ],
};

// Ohne Regler-Szenarien, damit der Wechselrichter wählbar bleibt.
const document = { rules: [], scenarios: [], settings: { latitude: 52.5, longitude: 13.4 } };

const contextFor = (wizard, overrides = {}) => wizard.buildContext({ devices, snapshot, document, ...overrides });

const templateById = (templates, id) => templates.find((item) => item.id === id);

// Antworten einer Vorlage: Standard plus Abweichungen.
function answersFor(wizard, template, ctx, overrides = {}) {
  return { ...wizard.defaultAnswers(template, ctx), ...overrides };
}

// Varianten je Vorlage, wie sie das Fixture abdeckt.
const VARIANTS = {
  zero_feed: [
    {},
    { period: 'always' },
    { period: 'fixed', window: { from: '08:00', to: '18:00', days: [0, 1, 2, 3, 4] } },
    { measure: 'last' },
    { measure: 'balance' },
  ],
  surplus_wallbox: [
    {},
    { mode: 'battery', soc: 40 },
    { mode: 'min' },
    { extras: { connected: false, history: true } },
  ],
  surplus_switch: [
    { off: 'runtime', runtime: 120 },
    { extras: { weekdays: false, weekdays_value: [0, 1, 2, 3, 4], repeat: false } },
    { off: 'sunset', extras: { weekdays: true, weekdays_value: [0, 1, 2, 3, 4], repeat: true } },
  ],
  battery_warn: [
    { below: 10, severity: 'critical' },
  ],
  time_switch: [
    { window: { from: '06:00', to: '08:00', days: [0, 1, 2, 3, 4] } },
  ],
  heat_pump: [
    { control: 'setpoint', lift: 3 },
    { contact: 'wb_switch' },
    { battery: 'parallel' },
    { period: 'fixed', window: { from: '10:00', to: '17:00', days: [0, 1, 2, 3, 4] } },
    { extras: { notify: true, history: false } },
  ],
  tariff: [
    { source: 'price', thr: 10 },
    { charge: { target: 60, power: 1000 } },
    { extras: { block: false, only: true, notify: true, history: false } },
  ],
  grid_cap: [
    { limit: 25000, speed: 'fast' },
    { extras: { notify: true, history: false } },
  ],
  grid_signal: [
    { input: 'steuer_dimm', targets: ['wb_limit'], power: 4.2, extras: { notify: false } },
    { input: 'steuer_dimm', targets: ['wb_limit'], power: 2, extras: { notify: true } },
  ],
  pv_missing: [
    { limit: 50, minutes: 60, severity: 'critical' },
  ],
  heating_rod: [
    { target: 10, max_temp: 55, battery: 'parallel' },
    { extras: { notify: true, history: false } },
  ],
};

// Zusaetzliche Mitteilungstexte, die eine Vorlage im gebauten Eintrag verwendet.
const NOTICE_KEYS = {
  zero_feed: ['automations.templates.zero_feed.notice.title', 'automations.templates.zero_feed.notice.message'],
  battery_warn: ['automations.templates.battery_warn.notice.title', 'automations.templates.battery_warn.notice.message'],
  heat_pump: ['automations.templates.heat_pump.notice.title', 'automations.templates.heat_pump.notice.message'],
  tariff: ['automations.templates.tariff.notice.title', 'automations.templates.tariff.notice.message'],
  pv_missing: ['automations.templates.pv_missing.notice.title', 'automations.templates.pv_missing.notice.message'],
  heating_rod: ['automations.templates.heating_rod.notice.title', 'automations.templates.heating_rod.notice.message'],
  inverter_silent: ['automations.templates.inverter_silent.notice.title', 'automations.templates.inverter_silent.notice.message'],
};

test('zero_feed builds the documented shape with the default answers', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'zero_feed');
  const ctx = plain(contextFor(wizard));
  const result = template.build(answersFor(wizard, template, ctx), ctx);
  const entry = plain(result.entry);
  assert.equal(result.kind, 'scenario');
  assert.equal(entry.template, 'zero_feed');
  assert.equal(entry.id, 'zero_feed');
  assert.equal(entry.enabled, true);
  assert.equal(entry.when.conditions[0].type, 'sun_window');
  assert.equal(entry.while.mode, 'regulate');
  assert.equal(entry.while.regulator.direction, 'producer');
  assert.equal(entry.while.regulator.target_w, 30);
  assert.equal(entry.while.regulator.deadband_w, 20);
  assert.equal(entry.while.regulator.output.decimals, 0);
  assert.equal(entry.while.regulator.output.topic, 'werkstatt/pv1/limit');
  assert.equal(entry.while.regulator.output.max, 800);
  assert.equal(entry.while.regulator.fallback.payload, '800');
  assert.equal(entry.while.regulator.base.source, 'entity');
  assert.equal(entry.while.regulator.base.entity_id, 'pv1_power');
  assert.equal('gain' in entry.while.regulator, false);
  assert.equal(entry.on_end.actions[0].payload, '800');
  assert.deepEqual(entry.on_start.actions, []);
  assert.equal(entry.history_enabled, true);
  assert.equal('until' in entry, false);
});

test('zero_feed uses the tolerance answer as deadband, 20 W without one', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'zero_feed');
  const ctx = plain(contextFor(wizard));
  const deadband = (overrides) => plain(template.build(answersFor(wizard, template, ctx, overrides), ctx).entry)
    .while.regulator.deadband_w;
  assert.equal(deadband({ tolerance: 35 }), 35);
  assert.equal(deadband({}), 20);
  assert.equal(answersFor(wizard, template, ctx).tolerance, 20);
});

test('zero_feed always and fixed periods build their time windows', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'zero_feed');
  const ctx = plain(contextFor(wizard));
  const always = plain(template.build(answersFor(wizard, template, ctx, { period: 'always' }), ctx).entry);
  assert.deepEqual(always.when.conditions[0], { type: 'time_window', start: '00:00', end: '00:00', weekdays: [] });
  const fixed = plain(template.build(answersFor(wizard, template, ctx, {
    period: 'fixed', window: { from: '08:00', to: '18:00', days: [0, 1, 2, 3, 4] },
  }), ctx).entry);
  assert.deepEqual(fixed.when.conditions[0], { type: 'time_window', start: '08:00', end: '18:00', weekdays: [0, 1, 2, 3, 4] });
});

test('zero_feed measure choices pick the regulator base', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'zero_feed');
  const ctx = plain(contextFor(wizard));
  const base = (measure) => plain(template.build(answersFor(wizard, template, ctx, { measure }), ctx).entry)
    .while.regulator.base;
  assert.deepEqual(base('last'), { source: 'last' });
  assert.deepEqual(base('balance'), { source: 'balance', field: 'pv' });
});

test('zero_feed notify and reset add their actions', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'zero_feed');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    extras: { reset: false, notify: true, history: false },
  }), ctx).entry);
  assert.equal(entry.on_start.actions[0].type, 'notification');
  assert.equal(entry.on_start.actions[0].title, t('automations.templates.zero_feed.notice.title'));
  assert.deepEqual(entry.on_end.actions, []);
  assert.equal(entry.history_enabled, false);
});

test('surplus_wallbox builds the documented shape with the default answers', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_wallbox');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'surplus_wallbox');
  assert.deepEqual(entry.when.conditions[0], {
    type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 1400, hysteresis: 0, hold_seconds: 180,
  });
  assert.deepEqual(entry.when.conditions[1], {
    type: 'entity_value', topic: 'werkstatt/wb/plug', entity_id: 'wb_plug', value_template: '',
    comparison: 'equals', text: 'ON',
  });
  assert.deepEqual(entry.until.conditions[0], {
    type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 200, hysteresis: 0, hold_seconds: 300,
  });
  assert.equal(entry.while.regulator.direction, 'consumer');
  assert.equal(entry.while.regulator.target_w, 0);
  assert.equal(entry.while.regulator.deadband_w, 100);
  assert.equal(entry.while.regulator.base.entity_id, 'wb_power');
  assert.equal(entry.while.regulator.fallback.payload, '1400');
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/wb/switch', retain: false, payload_source: 'constant', payload: 'ON', entity_id: 'wb_switch',
  }]);
  assert.equal(entry.on_end.actions[0].payload, 'OFF');
  assert.equal(entry.history_enabled, true);
});

test('surplus_wallbox battery mode ends at the battery threshold', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_wallbox');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { mode: 'battery', soc: 40 }), ctx).entry);
  assert.deepEqual(entry.until.conditions[0], {
    type: 'balance_threshold', field: 'battery_soc', comparison: 'below', threshold: 40, hysteresis: 0, hold_seconds: 60,
  });
});

test('surplus_wallbox without a switch publishes the minimum on end', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_wallbox');
  const noSwitch = devices.map((device) => (device.id === 'wb'
    ? { ...device, entities: device.entities.filter((entity) => entity.unique_id !== 'wb_switch') }
    : device));
  const ctx = plain(contextFor(wizard, { devices: noSwitch }));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.deepEqual(entry.on_start.actions, []);
  assert.equal(entry.on_end.actions[0].type, 'publish');
  assert.equal(entry.on_end.actions[0].payload, '1400');
  assert.equal(entry.on_end.actions[0].topic, 'werkstatt/wb/limit');
});

test('surplus_wallbox always has an end condition', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_wallbox');
  const ctx = plain(contextFor(wizard));
  for (const mode of ['sun', 'min', 'battery']) {
    const entry = plain(template.build(answersFor(wizard, template, ctx, { mode, soc: 40 }), ctx).entry);
    assert.ok(Array.isArray(entry.until.conditions), mode);
    assert.ok(entry.until.conditions.length >= 1, mode);
  }
});

test('surplus_switch builds the documented shape', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_switch');
  const ctx = plain(contextFor(wizard));
  const result = template.build(answersFor(wizard, template, ctx), ctx);
  const entry = plain(result.entry);
  assert.equal(result.kind, 'scenario');
  assert.equal(entry.id, 'surplus_switch');
  assert.equal(entry.template, 'surplus_switch');
  assert.equal(entry.enabled, true);
  assert.deepEqual(entry.when.conditions, [{
    type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 1500, hysteresis: 0, hold_seconds: 120,
  }]);
  assert.deepEqual(entry.until.conditions[0], {
    type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 120,
  });
  assert.equal('max_duration_s' in entry, false);
  assert.equal(entry.while.mode, 'hold');
  assert.deepEqual(entry.on_start.actions, []);
  assert.equal(entry.on_end.actions[0].payload, 'OFF');
});

test('surplus_switch without repeat switches on once at the start', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_switch');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    extras: { weekdays: false, weekdays_value: [0, 1, 2, 3, 4], repeat: false },
  }), ctx).entry);
  assert.equal(entry.while.mode, 'none');
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/wb/switch', retain: false, payload_source: 'constant', payload: 'ON', entity_id: 'wb_switch',
  }]);
});

test('surplus_switch with a fixed runtime ends by time only', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_switch');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { off: 'runtime', runtime: 120 }), ctx).entry);
  assert.equal(entry.max_duration_s, 7200);
  assert.equal('until' in entry, false);
});

test('surplus_switch repeat holds the switch on while the condition stays met', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_switch');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    extras: { weekdays: false, weekdays_value: [0, 1, 2, 3, 4], repeat: true },
  }), ctx).entry);
  assert.equal(entry.while.mode, 'hold');
  assert.equal(entry.while.repeat_seconds, 60);
  assert.equal(entry.while.actions[0].payload, 'ON');
  assert.deepEqual(entry.on_start.actions, []);
});

test('surplus_switch weekdays add a time window to the start condition', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'surplus_switch');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    extras: { weekdays: true, weekdays_value: [0, 1, 2, 3, 4], repeat: false },
  }), ctx).entry);
  assert.deepEqual(entry.when.conditions[1], {
    type: 'time_window', start: '00:00', end: '00:00', weekdays: [0, 1, 2, 3, 4],
  });
});

test('battery_warn builds the documented shape', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'battery_warn');
  const ctx = plain(contextFor(wizard));
  const result = template.build(answersFor(wizard, template, ctx), ctx);
  const rule = plain(result.entry);
  assert.equal(result.kind, 'rule');
  assert.equal(rule.id, 'battery_warn');
  assert.equal(rule.enabled, true);
  assert.equal(rule.cooldown_seconds, 3600);
  assert.deepEqual(rule.conditions, [{
    type: 'balance_threshold', field: 'battery_soc', comparison: 'below', threshold: 20, hysteresis: 2, hold_seconds: 60,
  }]);
  assert.deepEqual(rule.actions, [{
    type: 'notification',
    severity: 'warning',
    title: t('automations.templates.battery_warn.notice.title'),
    message: t('automations.templates.battery_warn.notice.message', { value: '20' }),
  }]);
});

test('time_switch builds the documented shape', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'time_switch');
  const ctx = plain(contextFor(wizard));
  const result = template.build(answersFor(wizard, template, ctx), ctx);
  const entry = plain(result.entry);
  assert.equal(result.kind, 'scenario');
  assert.equal(entry.id, 'time_switch');
  assert.equal(entry.template, 'time_switch');
  assert.deepEqual(entry.when.conditions, [{ type: 'time_window', start: '11:00', end: '15:00', weekdays: [] }]);
  assert.equal(entry.while.mode, 'none');
  assert.equal(entry.on_start.actions[0].payload, 'ON');
  assert.equal(entry.on_end.actions[0].payload, 'OFF');
});

test('no built entry contains null', () => {
  const { wizard, templates } = load();
  const ctx = plain(contextFor(wizard));
  const hasNull = (value) => JSON.stringify(value).includes('null');
  for (const template of templates) {
    // Das Netzsignal braucht einen Eingang und ein gewähltes Ziel, also den Signal-Kontext ohne Standard.
    const isSignal = template.id === 'grid_signal';
    const base = isSignal ? plain(signalContext(wizard)) : ctx;
    const variants = isSignal ? VARIANTS.grid_signal : [{}, ...(VARIANTS[template.id] || [])];
    for (const overrides of variants) {
      const answers = answersFor(wizard, template, base, overrides);
      const result = template.build(answers, base);
      assert.equal(hasNull(result), false, `${template.id} ${JSON.stringify(overrides)}`);
    }
  }
});

test('every template has texts in both catalogs', () => {
  const { templates } = load();
  const de = catalog('de');
  const en = catalog('en');
  for (const template of templates) {
    const keys = [
      `automations.templates.${template.id}.title`,
      `automations.templates.${template.id}.blurb`,
      `automations.templates.${template.id}.keywords`,
    ];
    for (const step of template.steps) {
      keys.push(`automations.templates.${template.id}.steps.${step.id}.title`);
      keys.push(`automations.templates.${template.id}.steps.${step.id}.help`);
      keys.push(`automations.templates.${template.id}.steps.${step.id}.short`);
      for (const option of step.options || []) {
        keys.push(`automations.templates.${template.id}.steps.${step.id}.options.${option.id}.title`);
        if (step.type !== 'presets') {
          keys.push(`automations.templates.${template.id}.steps.${step.id}.options.${option.id}.text`);
        }
      }
    }
    keys.push(...(NOTICE_KEYS[template.id] || []));
    for (const key of keys) {
      for (const [lang, dict] of [['de', de], ['en', en]]) {
        assert.ok(typeof dict[key] === 'string' && dict[key] !== '', `${lang} missing ${key}`);
      }
    }
  }
});

test('the template texts resolve in the German and English catalogs', () => {
  for (const lang of ['de', 'en']) {
    const { templates, t } = load(lang);
    for (const template of templates) {
      const key = `automations.templates.${template.id}.title`;
      assert.notEqual(t(key), key, `${lang} ${key}`);
    }
  }
});

test('summary steps point at visible steps', () => {
  const { wizard, templates } = load();
  for (const template of templates) {
    const ctx = plain(contextFor(wizard));
    const variants = [{}, ...(VARIANTS[template.id] || [])];
    for (const overrides of variants) {
      const answers = answersFor(wizard, template, ctx, overrides);
      const visible = new Set(wizard.visibleSteps(template, answers, ctx).map((step) => step.id));
      const rows = plain(template.summary(answers, ctx));
      assert.ok(rows.length > 0, template.id);
      for (const row of rows) {
        assert.ok(visible.has(row.step), `${template.id} ${row.step} ${JSON.stringify(overrides)}`);
        assert.equal(typeof row.label, 'string');
        assert.equal(typeof row.text, 'string');
      }
    }
  }
});

test('the built fixture is current', () => {
  const file = path.join(here, 'fixtures', 'automation-templates-built.json');
  assert.deepEqual(plain(buildFixture()), JSON.parse(fs.readFileSync(file, 'utf8')));
});

// Wärmepumpe: Ansteuerung über SG Ready oder Sollwert, mit Verdichterschutz.
const heatPumpPlain = (device) => ({ ...device, entities: device.entities });
const heatPumpPreset = (devicesOverride, snapshotOverride) => ({
  devices: devicesOverride || devices,
  snapshot: snapshotOverride || snapshot,
  document,
});

test('heat_pump build with sg uses the chosen contact', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(wizard.buildContext(heatPumpPreset()));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { control: 'sg', contact: 'wb_switch' }), ctx).entry);
  assert.equal(entry.template, 'heat_pump');
  assert.deepEqual(entry.on_start.actions[0], {
    type: 'publish', topic: 'werkstatt/wb/switch', retain: false, payload_source: 'constant', payload: 'ON', entity_id: 'wb_switch',
  });
  assert.deepEqual(entry.on_end.actions[0], {
    type: 'publish', topic: 'werkstatt/wb/switch', retain: false, payload_source: 'constant', payload: 'OFF', entity_id: 'wb_switch',
  });
  assert.deepEqual(entry.when.conditions[0], {
    type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 2500, hysteresis: 0, hold_seconds: 300,
  });
  assert.deepEqual(entry.when.conditions[1], {
    type: 'balance_threshold', field: 'battery_soc', comparison: 'above', threshold: 80, hysteresis: 0, hold_seconds: 0,
  });
  assert.deepEqual(entry.until.conditions, [{
    type: 'balance_threshold', field: 'grid_export', comparison: 'below', threshold: 1000, hysteresis: 0, hold_seconds: 600,
  }]);
  assert.deepEqual(entry.while, { mode: 'none' });
  assert.equal(entry.min_run_s, 1800);
  assert.equal(entry.cooldown_s, 1200);
  assert.equal(entry.history_enabled, true);
});

test('heat_pump sg takes the own contact of the device by default', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(wizard.buildContext(heatPumpPreset()));
  assert.equal(answersFor(wizard, template, ctx).contact, 'hp_sg');
});

test('heat_pump build with setpoint restores the value it found', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(wizard.buildContext(heatPumpPreset()));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { control: 'setpoint', lift: 3 }), ctx).entry);
  assert.deepEqual(entry.on_start.actions[0], {
    type: 'publish', topic: 'werkstatt/hp/setpoint', retain: false, payload_source: 'constant', payload: '48', entity_id: 'hp_setpoint',
  });
  assert.deepEqual(entry.on_end.actions[0], {
    type: 'publish', topic: 'werkstatt/hp/setpoint', retain: false, payload_source: 'constant', payload: '45', entity_id: 'hp_setpoint',
  });
});

test('heat_pump skips the contact step for setpoint', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(wizard.buildContext(heatPumpPreset()));
  const ids = (control) => wizard.visibleSteps(template, answersFor(wizard, template, ctx, { control }), ctx).map((step) => step.id);
  assert.equal(ids('setpoint').includes('contact'), false);
  assert.equal(ids('setpoint').includes('lift'), true);
  assert.equal(ids('sg').includes('contact'), true);
  assert.equal(ids('sg').includes('lift'), false);
});

test('heat_pump is available with only a foreign switch', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'heat_pump');
  const bare = { id: 'hp', name: 'Wärmepumpe Keller', entities: [
    { unique_id: 'hp_temp', name: 'Temperatur', state_topic: 'werkstatt/hp/temp', template_supported: true, device_class: 'temperature', has_value: true, value: '44' },
  ] };
  const bareSnapshot = { ...snapshot, entities: [{ device_id: 'hp', entity_id: 'hp_temp', role: { role: 'heat_pump' } }] };
  const withSwitch = plain(wizard.buildContext(heatPumpPreset([devices[1], bare], bareSnapshot)));
  const availability = wizard.templateAvailability(template, withSwitch);
  assert.equal(availability.available, true);
  const answers = answersFor(wizard, template, withSwitch);
  assert.equal(answers.control, 'sg');
  assert.equal(answers.contact, 'wb_switch');
  const entry = plain(template.build(answers, withSwitch).entry);
  assert.equal(entry.on_start.actions[0].entity_id, 'wb_switch');

  const bareOnly = plain(wizard.buildContext(heatPumpPreset([bare], bareSnapshot)));
  const blocked = wizard.templateAvailability(template, bareOnly);
  assert.equal(blocked.available, false);
  assert.equal(blocked.reason, t('automations.assistant.unavailable.heat_pump'));
});

test('surplus effect reports the first quarter hour above the threshold', async () => {
  const { wizard, t } = load();
  const series = new Array(96).fill(null);
  series[40] = 800;
  series[41] = 900;
  series[42] = 100;
  assert.deepEqual(plain(wizard.surplusStats(series, 500)), { first: 40, count: 2, hours: 0, minutes: 30 });
  assert.deepEqual(plain(wizard.surplusStats(series, 5000)), { first: -1, count: 0, hours: 0, minutes: 0 });

  // Gestern: Export 800 W von 10:00 bis 10:29 und 1200 W von 16:00 bis 17:59, zusammen 2 h 30 min.
  // Zeitstempel nach Wanduhr gebaut, damit ein Umstellungstag (Sommerzeit) nicht verschiebt.
  const [from] = wizard.yesterdayRange();
  const yesterday = new Date(from);
  const rows = [];
  for (let hour = 0; hour < 24; hour += 1) {
    for (let minute = 0; minute < 60; minute += 1) {
      const clock = hour * 60 + minute;
      let avg = 50;
      if (clock >= 600 && clock < 630) avg = -800;
      if (clock >= 960 && clock < 1080) avg = -1200;
      const ts = new Date(yesterday.getFullYear(), yesterday.getMonth(), yesterday.getDate(), hour, minute).getTime();
      rows.push({ ts, avg, min: avg, max: avg });
    }
  }
  const store = { readRange: async () => rows };
  const step = { effect: 'surplus' };
  const text = await wizard.presetEffect(step, 500, {}, store);
  assert.equal(text, t('automations.assistant.effect.surplus_reached', { time: '10:00', hours: 2, minutes: 30 }));
  const none = await wizard.presetEffect(step, 5000, {}, store);
  assert.equal(none, t('automations.assistant.effect.surplus_missed'));
  assert.equal(await wizard.presetEffect(step, 500, {}, { readRange: async () => [] }), '');
});

test('heat_pump contact step lists the own switch first and preselects it', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(contextFor(wizard));
  const contact = template.steps.find((step) => step.id === 'contact');
  const ids = wizard.devicesForStep(contact, ctx, { device: 'hp_setpoint' }).map((item) => item.id);
  assert.equal(ids[0], 'hp_sg');
  assert.deepEqual([...ids].sort(), ['hp_sg', 'wb_switch'].sort());
  assert.equal(answersFor(wizard, template, ctx).contact, 'hp_sg');
});

test('heat_pump sg option shows the badge and the own-contact text only with an own switch', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'heat_pump');
  const control = template.steps.find((step) => step.id === 'control');
  const sg = control.options.find((option) => option.id === 'sg');
  const ctx = plain(contextFor(wizard));
  // Die Geräte-ID der Wärmepumpe ist die ihres Sollwerts, sonst die ihres Schalters.
  const own = { device: 'hp_setpoint' };
  assert.equal(wizard.optionBadge(sg, ctx, own), t('automations.templates.heat_pump.steps.control.options.sg.badge'));
  assert.equal(wizard.optionTextField(sg, ctx, own), 'text_own');

  const bare = wizard.buildContext(heatPumpPreset([devices[1], {
    id: 'hp', name: 'Wärmepumpe Keller', entities: [devices[2].entities[2]],
  }], { ...snapshot, values: snapshot.values }));
  const plainBare = plain(bare);
  assert.equal(wizard.optionBadge(sg, plainBare, own), '');
  assert.equal(wizard.optionTextField(sg, plainBare, own), 'text');
});

test('heat_pump setpoint option names a missing value', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'heat_pump');
  const control = template.steps.find((step) => step.id === 'control');
  const setpoint = control.options.find((option) => option.id === 'setpoint');
  const withoutValue = { unique_id: 'hp_setpoint', name: 'Warmwasser-Soll', command_topic: 'werkstatt/hp/setpoint', min_value: 30, max_value: 60, step: 1, unit_of_measurement: '°C', has_value: false };
  const ctx = plain(wizard.buildContext(heatPumpPreset([{ ...devices[2], entities: [withoutValue, devices[2].entities[1], devices[2].entities[2]] }])));
  assert.equal(setpoint.unavailable(ctx, { device: 'hp_setpoint' }), t('automations.assistant.unavailable.heat_pump_setpoint_value'));

  const noSetpoint = plain(wizard.buildContext(heatPumpPreset([{ ...devices[2], entities: [devices[2].entities[1], devices[2].entities[2]] }])));
  assert.equal(setpoint.unavailable(noSetpoint, { device: 'hp_sg' }), t('automations.assistant.unavailable.heat_pump_setpoint'));
});

test('heat_pump without a soc signal drops the battery step and its condition', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heat_pump');
  const noSoc = { ...snapshot, values: { pv: 862, grid_export: 0, grid_import: 0 } };
  const ctx = plain(wizard.buildContext(heatPumpPreset(devices, noSoc)));
  const answers = answersFor(wizard, template, ctx);
  assert.equal(wizard.visibleSteps(template, answers, ctx).some((step) => step.id === 'battery'), false);
  const entry = plain(template.build(answers, ctx).entry);
  assert.equal(entry.when.conditions.length, 2);
  assert.equal(entry.when.conditions.some((condition) => condition.field === 'battery_soc'), false);
  assert.equal(template.summary(answers, ctx).some((row) => row.text.includes('Akku')), false);
});

test('surplus markup draws the threshold, the quarter hours above it and the text, empty without data', () => {
  const { wizard } = load();
  const series = new Array(96).fill(null);
  series[40] = 800;
  series[41] = 900;
  series[42] = 100;
  const labels = { start: 'Start ab 500 W', aria: 'Überschuss', text: 'Satz' };
  const markup = wizard.surplusMarkup(series, 500, labels);
  assert.ok(markup.includes('class="auto-surplus-svg"'));
  assert.ok(markup.includes('Start ab 500 W'));
  assert.ok(markup.includes('Satz'));
  assert.equal(markup.split('<rect').length - 1, 2);
  assert.equal(wizard.surplusMarkup([], 500, labels), '');
  assert.equal(wizard.surplusMarkup(new Array(96).fill(null), 500, labels), '');
});

// Akku laden bei Niedrigtarif: feste Zeiten oder Preisschwelle, Ziel und Entladesperre.
const batteryWithLimit = {
  id: 'bat', name: 'Batteriespeicher',
  entities: [
    ...devices.find((device) => device.id === 'bat').entities,
    { unique_id: 'bat_discharge', name: 'Discharge limit', command_topic: 'werkstatt/bat/discharge', min_value: 0, max_value: 100, step: 1, unit_of_measurement: '%', has_value: true, value: '30' },
  ],
};

test('tariff fixed builds a time window over midnight', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'tariff');
  assert.deepEqual(entry.when.conditions[0], { type: 'time_window', start: '22:00', end: '06:00', weekdays: [] });
  assert.deepEqual(entry.when.conditions[1], {
    type: 'balance_threshold', field: 'battery_soc', comparison: 'below', threshold: 80, hysteresis: 0, hold_seconds: 0,
  });
  assert.equal('until' in entry, false);
  assert.deepEqual(entry.while, { mode: 'none' });
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/bat/limit', retain: false, payload_source: 'constant', payload: '2000', entity_id: 'bat_limit',
  }]);
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/bat/limit', retain: false, payload_source: 'constant', payload: '0', entity_id: 'bat_limit',
  }]);
});

test('tariff price builds an entity_value below the threshold', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const ctx = plain(contextFor(wizard));
  const answers = answersFor(wizard, template, ctx, { source: 'price', thr: 15 });
  const entry = plain(template.build(answers, ctx).entry);
  assert.deepEqual(entry.when.conditions[0], {
    type: 'entity_value', topic: 'werkstatt/tibber/price', entity_id: 'tibber_price', value_template: '', comparison: 'below', value: 15, hold_seconds: 0,
  });
});

test('tariff price is unavailable without price entities', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const ctx = plain(contextFor(wizard, { devices: devices.filter((device) => device.id !== 'tibber') }));
  const price = template.steps.find((step) => step.id === 'source').options.find((option) => option.id === 'price');
  assert.equal(price.unavailable(ctx, {}), 'Kein Messwert in ct/kWh gefunden');
  assert.equal(wizard.defaultAnswers(template, ctx).source, 'fixed');
  const withPrice = plain(contextFor(wizard));
  assert.equal(price.unavailable(withPrice, {}), '');
});

test('tariff block option needs a discharge limit', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const block = template.steps.find((step) => step.id === 'extras').options.find((option) => option.id === 'block');
  const without = plain(contextFor(wizard));
  assert.equal(block.unavailable(without, { battery: 'bat_limit' }), 'Akku hat keine Entladegrenze');
  assert.equal(wizard.defaultAnswers(template, without).extras.block, false);

  const withLimit = plain(wizard.buildContext({
    devices: devices.map((device) => (device.id === 'bat' ? batteryWithLimit : device)), snapshot, document,
  }));
  assert.equal(block.unavailable(withLimit, { battery: 'bat_limit' }), '');
  const answers = answersFor(wizard, template, withLimit, { extras: { block: true, only: false, notify: false, history: true } });
  const entry = plain(template.build(answers, withLimit).entry);
  assert.deepEqual(entry.on_start.actions[1], {
    type: 'publish', topic: 'werkstatt/bat/discharge', retain: false, payload_source: 'constant', payload: '0', entity_id: 'bat_discharge',
  });
  assert.deepEqual(entry.on_end.actions[1], {
    type: 'publish', topic: 'werkstatt/bat/discharge', retain: false, payload_source: 'constant', payload: '30', entity_id: 'bat_discharge',
  });
});

test('tariff fit text for 46 to 80 percent at 2000 W on 10 kWh', () => {
  const { chargeFit } = load();
  assert.equal(
    chargeFit({ soc: 46, target: 80, power: 2000, capacity: 10 }),
    'Von 46 % auf 80 % sind 3,4 kWh, bei 2000 W also etwa 1 h 42 min.',
  );
  assert.equal(chargeFit({ soc: 46, target: 80, power: 2000, capacity: 0 }), '');
  assert.equal(chargeFit({ soc: 46, target: 80, power: 2000, capacity: undefined }), '');
});

test('tariff price text says whether the current price loads the battery', () => {
  const { wizard } = load();
  assert.equal(wizard.priceNowText(12.5, 15), 'Jetzt 12,5 ct, damit würde jetzt geladen.');
  assert.equal(wizard.priceNowText(20, 15), 'Jetzt 20,0 ct, damit würde jetzt nicht geladen.');
  assert.equal(wizard.priceNowText(null, 15), '');
});

test('tariff price summary names the chosen price device', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const ctx = plain(contextFor(wizard));
  const answers = answersFor(wizard, template, ctx, { source: 'price', thr: 15 });
  assert.ok(template.summary(answers, ctx)[0].text.startsWith('Tibber Preis unter 15 ct/kWh'));
});

test('tariff charge step returns the fit hint and nothing without a capacity', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const ctx = plain(contextFor(wizard));
  const charge = template.steps.find((step) => step.id === 'charge');
  const answers = answersFor(wizard, template, ctx, { charge: { target: 80, power: 2000 } });
  assert.equal(charge.hint(answers, ctx), 'Von 46 % auf 80 % sind 3,4 kWh, bei 2000 W also etwa 1 h 42 min.');
  const noCapacity = plain(contextFor(wizard, { snapshot: { ...snapshot, values: { ...snapshot.values, battery_capacity_kwh: undefined } } }));
  assert.equal(charge.hint(answers, noCapacity), '');
});

test('price units convert between euro and cent per kWh', () => {
  const { wizard } = load();
  assert.equal(wizard.priceToCt(0.285, '€/kWh'), 28.5);
  assert.equal(wizard.priceToCt(0.2, 'EUR/kWh'), 20);
  assert.equal(wizard.priceToCt(12.5, 'ct/kWh'), 12.5);
  assert.equal(wizard.ctToPriceUnit(15, '€/kWh'), 0.15);
  assert.equal(wizard.ctToPriceUnit(15, 'EUR/kWh'), 0.15);
  assert.equal(wizard.ctToPriceUnit(15, 'ct/kWh'), 15);
});

test('tariff price in euro per kWh compares the threshold in euro', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const euroDevices = devices.map((device) => (device.id === 'tibber'
    ? { ...device, entities: [{ ...device.entities[0], unit_of_measurement: '€/kWh', value: '0.285' }] }
    : device));
  const ctx = plain(wizard.buildContext({ devices: euroDevices, snapshot, document }));
  const answers = answersFor(wizard, template, ctx, { source: 'price', thr: 15 });
  const entry = plain(template.build(answers, ctx).entry);
  assert.equal(entry.when.conditions[0].value, 0.15);
  assert.ok(template.summary(answers, ctx)[0].text.includes('15 ct/kWh'));
});

// Netzbezug deckeln und Steuersignal nach §14a. Das Signal braucht einen lesbaren Eingang,
// eine Wallbox in A und die Kontext-Rolle wallbox im Snapshot.
const controlBox = {
  id: 'steuer', name: 'Steuerbox',
  entities: [
    { unique_id: 'steuer_dimm', name: 'Dimmsignal', state_topic: 'werkstatt/steuer/dimm', template_supported: true, payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'OFF' },
  ],
};

const ampereWallbox = {
  id: 'wba', name: 'Wallbox Garage',
  entities: [
    { unique_id: 'wba_limit', name: 'Ladestrom', command_topic: 'werkstatt/wba/limit', min_value: 6, max_value: 16, step: 1, unit_of_measurement: 'A', has_value: true, value: '16' },
    { unique_id: 'wba_power', name: 'Ladestrom Ist', state_topic: 'werkstatt/wba/power', template_supported: true, unit_of_measurement: 'A', has_value: true, value: '10' },
  ],
};

const signalSnapshot = {
  ...snapshot,
  entities: [...snapshot.entities, { device_id: 'wba', entity_id: 'wba_power', role: { role: 'wallbox' } }],
};

const signalContext = (wizard, overrides = {}) => plain(wizard.buildContext({
  devices: [...devices, controlBox, ampereWallbox], snapshot: signalSnapshot, document, ...overrides,
}));

test('grid_cap regulates as consumer just below the limit', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_cap');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'grid_cap');
  assert.equal(entry.while.mode, 'regulate');
  assert.equal(entry.while.regulator.direction, 'consumer');
  assert.equal(entry.while.regulator.target_w, 10500);
  assert.equal(entry.while.regulator.deadband_w, 200);
  assert.equal(entry.while.regulator.max_change_w, 1000);
  assert.equal(entry.while.regulator.output.topic, 'werkstatt/wb/limit');
  assert.equal(entry.while.regulator.output.max, 11000);
  assert.equal(entry.while.regulator.base.entity_id, 'wb_power');
  assert.deepEqual(entry.when.conditions, [{
    type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 10500, hysteresis: 0, hold_seconds: 10,
  }]);
  assert.deepEqual(entry.until, { conditions: [{
    type: 'balance_threshold', field: 'grid_import', comparison: 'below', threshold: 9500, hysteresis: 0, hold_seconds: 600,
  }] });
  assert.deepEqual(entry.on_start.actions, []);
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/wb/limit', retain: false, payload_source: 'constant', payload: '11000', entity_id: 'wb_limit',
  }]);
  assert.equal(entry.history_enabled, true);
});

test('grid_cap fast speed and the 16 kW limit change the regulator', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_cap');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { limit: 16000, speed: 'fast' }), ctx).entry);
  assert.equal(entry.while.regulator.target_w, 15500);
  assert.equal(entry.while.regulator.max_change_w, 3000);
  assert.equal(entry.when.conditions[0].threshold, 15500);
  assert.equal(entry.until.conditions[0].threshold, 14500);
});

test('grid_cap ends only after ten minutes below the limit minus 1500 W', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_cap');
  const ctx = plain(contextFor(wizard));
  const until = plain(template.build(answersFor(wizard, template, ctx, { limit: 11000 }), ctx).entry).until;
  assert.equal(until.conditions[0].field, 'grid_import');
  assert.equal(until.conditions[0].comparison, 'below');
  assert.equal(until.conditions[0].threshold, 9500);
  assert.equal(until.conditions[0].hold_seconds, 600);
});

test('grid_cap notify adds the notice on start', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'grid_cap');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    extras: { notify: true, history: false },
  }), ctx).entry);
  assert.equal(entry.on_start.actions[0].type, 'notification');
  assert.equal(entry.on_start.actions[0].title, t('automations.templates.grid_cap.notice.title'));
  assert.equal(entry.history_enabled, false);
});

test('grid_cap is unavailable without a wallbox', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_cap');
  const ctx = plain(contextFor(wizard, { devices: devices.filter((device) => device.id !== 'wb') }));
  const availability = wizard.templateAvailability(template, ctx);
  assert.equal(availability.available, false);
  assert.equal(availability.reason, 'Keine Wallbox mit einstellbarer Leistung gefunden');
});

test('grid_signal converts kW to amps for a current setpoint', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = signalContext(wizard);
  const answers = answersFor(wizard, template, ctx, {
    input: 'steuer_dimm', targets: ['wba_limit'], power: 4.2, extras: { notify: false },
  });
  const entry = plain(template.build(answers, ctx).entry);
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/wba/limit', retain: false, payload_source: 'constant', payload: '6', entity_id: 'wba_limit',
  }]);
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/wba/limit', retain: false, payload_source: 'constant', payload: '16', entity_id: 'wba_limit',
  }]);
});

test('grid_signal sets watts for a watt setpoint and triggers on the input value', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = signalContext(wizard);
  const answers = answersFor(wizard, template, ctx, { input: 'steuer_dimm', targets: ['wb_limit'], power: 4.2 });
  const entry = plain(template.build(answers, ctx).entry);
  assert.deepEqual(entry.when.conditions, [{
    type: 'entity_value', topic: 'werkstatt/steuer/dimm', entity_id: 'steuer_dimm', value_template: '', comparison: 'equals', text: 'ON',
  }]);
  assert.equal(entry.on_start.actions[0].payload, '4200');
  assert.equal(entry.on_start.actions[1].type, 'notification');
  assert.equal(entry.on_start.actions[1].title, t('automations.templates.grid_signal.notice.title'));
  assert.equal(entry.on_end.actions[0].payload, '11000');
  assert.deepEqual(entry.while, { mode: 'none' });
  assert.equal('until' in entry, false);
});

test('grid_signal locks targets with another unit and names the reason', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = signalContext(wizard);
  const heatPump = ctx.gridTargets.find((item) => item.id === 'hp_setpoint');
  assert.equal(heatPump.disabled, 'Stellglied in °C, nur W oder A möglich');
  assert.equal(ctx.gridTargets.find((item) => item.id === 'wb_limit').disabled, '');
  const answers = answersFor(wizard, template, ctx, { input: 'steuer_dimm', targets: ['hp_setpoint'] });
  assert.throws(() => template.build(answers, ctx), /target not usable/);
});

test('grid_signal starts without a chosen target and needs one to continue', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = signalContext(wizard);
  const answers = plain(wizard.defaultAnswers(template, ctx));
  assert.deepEqual(answers.targets, []);
  const targets = template.steps.find((step) => step.id === 'targets');
  assert.equal(wizard.stepComplete(targets, answers.targets), false);
  assert.equal(wizard.stepComplete(targets, ['wb_limit']), true);
});

test('grid_signal is unavailable without a heat pump setpoint or wallbox control value', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = plain(wizard.buildContext({ devices: [controlBox], snapshot, document }));
  const availability = wizard.templateAvailability(template, ctx);
  assert.equal(availability.available, false);
  assert.equal(availability.reason, 'Keine Wärmepumpe mit Sollwert und keine Wallbox mit Stellglied gefunden');
  const withTargets = wizard.templateAvailability(template, signalContext(wizard));
  assert.equal(withTargets.available, true);
});

test('grid_signal summary names the input and every chosen target', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'grid_signal');
  const ctx = signalContext(wizard);
  const answers = answersFor(wizard, template, ctx, { input: 'steuer_dimm', targets: ['wba_limit'], power: 4.2 });
  const rows = plain(template.summary(answers, ctx));
  assert.equal(rows[0].text, 'Wenn Steuerbox das Dimmen meldet');
  assert.equal(rows[1].text, 'Wallbox Garage geht auf 6 A');
  assert.equal(rows[1].step, 'targets');
});

// PV-Ertrag fehlt: Regel mit Sonnenfenster, Ertragsgrenze über Haltedauer und Mitteilung.
test('pv_missing builds a rule with a sun window and a hold on the yield', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'pv_missing');
  const ctx = plain(contextFor(wizard));
  const result = plain(template.build(answersFor(wizard, template, ctx), ctx));
  assert.equal(result.kind, 'rule');
  const rule = result.entry;
  assert.equal(rule.enabled, true);
  assert.equal(rule.cooldown_seconds, 21600);
  assert.deepEqual(rule.conditions[0], {
    type: 'sun_window', from: 'sunrise', to: 'sunset', from_offset_min: 60, to_offset_min: -60,
  });
  assert.deepEqual(rule.conditions[1], {
    type: 'entity_value', topic: 'werkstatt/pv1/power', entity_id: 'pv1_power', value_template: '',
    comparison: 'below', value: 20, hold_seconds: 1800,
  });
  assert.deepEqual(rule.actions, [{
    type: 'notification', severity: 'warning',
    title: t('automations.templates.pv_missing.notice.title'),
    message: 'Der Wechselrichter liefert seit 30 Minuten weniger als 20 W, er hängt vermutlich.',
  }]);
});

test('pv_missing takes the chosen limit, minutes and severity', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'pv_missing');
  const ctx = plain(contextFor(wizard));
  const rule = plain(template.build(answersFor(wizard, template, ctx, {
    limit: 50, minutes: 60, severity: 'critical',
  }), ctx).entry);
  assert.equal(rule.conditions[1].value, 50);
  assert.equal(rule.conditions[1].hold_seconds, 3600);
  assert.equal(rule.actions[0].severity, 'critical');
});

test('pv_missing needs an inverter with its own reading', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'pv_missing');
  const noReading = devices.map((device) => (device.id === 'pv1'
    ? { ...device, entities: device.entities.filter((entity) => entity.unique_id !== 'pv1_power') }
    : device));
  const ctx = plain(contextFor(wizard, { devices: noReading }));
  const availability = wizard.templateAvailability(template, ctx);
  assert.equal(availability.available, false);
  assert.equal(availability.reason, 'Der Wechselrichter meldet keinen eigenen Messwert');
  assert.equal(wizard.devicesFor('inverter_measured', ctx).length, 0);
  assert.equal(wizard.templateAvailability(template, plain(contextFor(wizard))).available, true);
});

// Heizstab mit Überschuss: Start über Netzexport und Temperatur, Ende über Netzbezug.
test('heating_rod builds a consumer regulator, a start with two conditions and an end on grid import', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heating_rod');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'heating_rod');
  assert.deepEqual(entry.when.conditions[0], {
    type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 60,
  });
  assert.deepEqual(entry.when.conditions[1], {
    type: 'entity_value', topic: 'werkstatt/rod/temp', entity_id: 'rod_temp', value_template: '',
    comparison: 'below', value: 65, hold_seconds: 0,
  });
  assert.deepEqual(entry.when.conditions[2], {
    type: 'balance_threshold', field: 'battery_soc', comparison: 'above', threshold: 80, hysteresis: 0, hold_seconds: 0,
  });
  assert.deepEqual(entry.until, { conditions: [{
    type: 'balance_threshold', field: 'grid_import', comparison: 'above', threshold: 100, hysteresis: 0, hold_seconds: 600,
  }] });
  assert.equal(entry.while.regulator.direction, 'consumer');
  assert.equal(entry.while.regulator.target_w, 30);
  assert.equal(entry.while.regulator.deadband_w, 20);
  assert.equal(entry.while.regulator.output.topic, 'werkstatt/rod/power');
  assert.equal(entry.while.regulator.output.max, 3000);
  assert.deepEqual(entry.while.regulator.base, { source: 'last' });
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/rod/power', retain: false, payload_source: 'constant', payload: '0', entity_id: 'rod_power',
  }]);
});

test('heating_rod reads its base from the actuator when that is readable', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heating_rod');
  const readableRod = devices.map((device) => (device.id === 'rod'
    ? { ...device, entities: [{ ...device.entities[0], state_topic: 'werkstatt/rod/power', template_supported: true }, device.entities[1]] }
    : device));
  const ctx = plain(contextFor(wizard, { devices: readableRod }));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.deepEqual(entry.while.regulator.base, {
    source: 'entity', entity_id: 'rod_power', topic: 'werkstatt/rod/power', value_template: '',
  });
});

test('heating_rod drops the battery condition without a state of charge', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heating_rod');
  const noSoc = plain(contextFor(wizard, { snapshot: { ...snapshot, values: { pv: 862, grid_export: 0, grid_import: 0 } } }));
  const answers = answersFor(wizard, template, noSoc);
  assert.equal(template.steps.find((step) => step.id === 'battery').when(answers, noSoc), false);
  const entry = plain(template.build(answers, noSoc).entry);
  assert.equal(entry.when.conditions.length, 2);
});

test('heating_rod says that the thermostat limits the run, not the start check', () => {
  const { t } = load();
  assert.equal(
    t('automations.templates.heating_rod.steps.max_temp.help'),
    'Der Start prüft diese Temperatur. Während des Laufs begrenzt der Thermostat des Heizstabs.',
  );
  const { wizard, templates } = load();
  const template = templateById(templates, 'heating_rod');
  const ctx = plain(contextFor(wizard));
  const rows = plain(template.summary(answersFor(wizard, template, ctx), ctx));
  assert.ok(rows.some((row) => row.text === 'Der Start prüft die Temperatur. Während des Laufs begrenzt der Thermostat des Heizstabs.'));
});

test('heating_rod is unavailable without a rod with power control and thermometer', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'heating_rod');
  const ctx = plain(contextFor(wizard, { devices: devices.filter((device) => device.id !== 'rod') }));
  const availability = wizard.templateAvailability(template, ctx);
  assert.equal(availability.available, false);
  assert.equal(availability.reason, 'Kein Heizstab mit Leistungsstellglied und Temperaturmesswert gefunden');
});

// Symbole und Platzhalter des Assistenten: Icons existieren im Sprite, Einheiten und Reveal-Texte
// sind im Katalog, und kein Katalogtext behält einen nicht ersetzten Platzhalter.
const spriteIds = () => {
  const base = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'base.html'), 'utf8');
  const ids = new Set([...base.matchAll(/symbol id="(ico-[a-z-]+)"/g)].map((match) => match[1]));
  for (const match of base.matchAll(/deviceIconSymbol "(ico-[a-z-]+)"/g)) ids.add(match[1]);
  return ids;
};

test('every icon the assistant scripts name exists in the sprite', () => {
  const ids = spriteIds();
  const files = ['automation-templates.js', 'automation-wizard.js', 'automations.page.js'];
  for (const file of files) {
    const source = fs.readFileSync(path.join(jsDir, file), 'utf8');
    for (const match of source.matchAll(/'(ico-[a-z-]+)'/g)) {
      assert.ok(ids.has(match[1]), `${file} uses ${match[1]}, not in the sprite`);
    }
  }
  const { templates } = load();
  for (const template of templates) {
    assert.ok(ids.has(`ico-${template.icon.replace(/^ico-/, '')}`) || ids.has(template.icon), `${template.id} icon ${template.icon}`);
    for (const step of template.steps) {
      for (const field of step.fields || []) {
        if (field.icon) assert.ok(ids.has(field.icon), `${template.id} ${field.id} icon ${field.icon}`);
      }
    }
  }
});

test('every numbers field has an icon and a unit in the catalog', () => {
  const { templates } = load();
  const de = catalog('de');
  const en = catalog('en');
  const ids = spriteIds();
  let checked = 0;
  for (const template of templates) {
    for (const step of template.steps.filter((item) => item.type === 'numbers')) {
      for (const field of step.fields) {
        checked += 1;
        assert.ok(ids.has(field.icon || 'ico-flash'), `${template.id} ${field.id} icon`);
        for (const dict of [de, en]) {
          const unitKey = `automations.templates.${template.id}.steps.${step.id}.fields.${field.id}.unit`;
          assert.ok(typeof dict[unitKey] === 'string' && dict[unitKey] !== '', `missing ${unitKey}`);
        }
      }
    }
  }
  assert.ok(checked >= 4, 'the numbers steps of heat pump and tariff are covered');
});

test('the fallback icon keeps a numbers row filled', () => {
  const { wizard } = load();
  assert.equal(wizard.fieldIcon({ id: 'run' }), 'ico-flash');
  assert.equal(wizard.fieldIcon({ id: 'run', icon: '' }), 'ico-flash');
  assert.equal(wizard.fieldIcon({ id: 'run', icon: 'ico-history' }), 'ico-history');
});

test('the reveal of the battery shows its unit and the current state of charge', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'heat_pump');
  const ctx = plain(contextFor(wizard));
  const answers = answersFor(wizard, template, ctx);
  const params = wizard.textParams(ctx, answers);
  assert.equal(params.now, 46);
  assert.equal(wizard.revealTextKey(params), 'reveal.text');
  assert.equal(t('automations.templates.heat_pump.steps.battery.options.first.reveal.text', params), 'Heimspeicher, jetzt 46 %');
  assert.equal(t('automations.templates.heat_pump.steps.battery.options.first.reveal.unit'), '%');
  assert.equal(t('automations.templates.heat_pump.steps.battery.options.first.reveal.title'), 'Akku mindestens');
});

test('the reveal drops the current state of charge without a reading', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'heat_pump');
  const noSoc = plain(contextFor(wizard, { snapshot: { ...snapshot, values: { pv: 862, grid_export: 0, grid_import: 0 } } }));
  const params = wizard.textParams(noSoc, answersFor(wizard, template, noSoc));
  assert.equal(params.now, undefined);
  assert.equal(wizard.revealTextKey(params), 'reveal.text_fallback');
  assert.equal(t('automations.templates.heat_pump.steps.battery.options.first.reveal.text_fallback', params), 'Heimspeicher');
});

test('no template text keeps a placeholder, with and without a state of charge', () => {
  const { wizard, templates, t } = load();
  const de = catalog('de');
  const contexts = [
    plain(contextFor(wizard)),
    plain(contextFor(wizard, { snapshot: { ...snapshot, values: { pv: 862, grid_export: 0, grid_import: 0 } } })),
  ];
  const keysOf = (template, step) => {
    const base = `automations.templates.${template.id}.steps.${step.id}`;
    const keys = [`${base}.title`, `${base}.help`, `${base}.short`];
    for (const option of step.options || []) {
      const optionKey = `${base}.options.${option.id}`;
      for (const field of ['title', 'text', 'badge', 'reveal.title', 'reveal.text', 'reveal.text_fallback']) {
        keys.push(`${optionKey}.${field}`);
      }
    }
    return keys.filter((key) => typeof de[key] === 'string');
  };
  for (const ctx of contexts) {
    for (const template of templates) {
      const answers = answersFor(wizard, template, ctx);
      const params = wizard.textParams(ctx, answers);
      // Der Reveal-Untertext wird nur in der Fassung gerendert, die der Kontext erlaubt.
      const unused = wizard.revealTextKey(params) === 'reveal.text' ? 'reveal.text_fallback' : 'reveal.text';
      for (const step of template.steps) {
        for (const key of keysOf(template, step).filter((item) => !item.endsWith(`.${unused}`))) {
          const text = t(key, params);
          assert.equal(text.includes('{'), false, `${key} renders ${text}`);
        }
      }
    }
  }
});

test('tariff needs a battery level, a battery without soc reports why', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'tariff');
  const noSoc = { ...snapshot, values: { pv: 862, grid_export: 0, grid_import: 0 } };
  const ctx = plain(wizard.buildContext({ devices, snapshot: noSoc, document }));
  assert.equal(ctx.batteries.length, 1);
  const availability = wizard.templateAvailability(template, ctx);
  assert.equal(availability.available, false);
  assert.equal(availability.reason, 'Kein Akku mit Ladestand gefunden');
  assert.equal(templateById(templates, 'tariff').requires.soc, true);
  const withSoc = plain(contextFor(wizard));
  assert.equal(wizard.templateAvailability(template, withSoc).available, true);
});

test('surplus scale text says whether the current surplus would start the heat pump', () => {
  const { wizard } = load();
  assert.equal(wizard.surplusNowText(1800, 1500), 'Jetzt 1800 W Überschuss, damit würde sie jetzt starten.');
  assert.equal(wizard.surplusNowText(1200, 1500), 'Jetzt 1200 W Überschuss, damit würde sie jetzt nicht starten.');
  assert.equal(wizard.surplusNowText(null, 1500), '');
});

test('surplus now reads the grid export from the context snapshot', () => {
  const { wizard } = load();
  assert.equal(wizard.surplusNow({ snapshot: { values: { grid_export: 800 } } }), 800);
  assert.equal(wizard.surplusNow({ snapshot: { values: { grid: -450 } } }), 450);
  assert.equal(wizard.surplusNow({ snapshot: { values: { grid_import: 300 } } }), 0);
  assert.equal(wizard.surplusNow({ snapshot: { values: {} } }), null);
  assert.equal(wizard.surplusNow(null), null);
});

// Jeder presets-Schritt ausser band hat eine Skala mit den Werten der Vorlage. Der Schritt
// "Eigener Wert" entfaellt dort, der Schieber stellt den Wert ein.
test('every presets step except the band carries its threshold scale', () => {
  const { templates } = load();
  const expected = {
    'surplus_wallbox:start': { min: 0, max: 6000, step: 100, ticks: 1000, grow: true, fill: 'above', unit: 'W' },
    'surplus_switch:on': { min: 0, max: 6000, step: 100, ticks: 1000, grow: true, fill: 'above', unit: 'W' },
    'battery_warn:below': { min: 0, max: 100, step: 5, ticks: 20, fill: 'below', unit: '%' },
    'heat_pump:start': { min: 0, max: 6000, step: 100, ticks: 1000, grow: true, fill: 'above', unit: 'W' },
    'tariff:thr': { min: 0, max: 40, step: 0.5, ticks: 10, fill: 'below', unit: 'ct' },
    'wallbox_low_tariff:thr': { min: 0, max: 40, step: 0.5, ticks: 10, fill: 'below', unit: 'ct' },
    'grid_cap:limit': { min: 0, max: 30000, step: 100, ticks: 5000, grow: true, fill: 'above', unit: 'W' },
  };
  const found = {};
  for (const template of templates) {
    for (const step of template.steps) {
      if (step.type !== 'presets' || step.effect === 'band') continue;
      found[`${template.id}:${step.id}`] = plain(step.scale);
    }
  }
  assert.deepEqual(Object.keys(found).sort(), Object.keys(expected).sort());
  for (const key of Object.keys(expected)) assert.deepEqual(found[key], expected[key], key);
});

test('battery_full_monthly with surplus charges on one day of the month up to full', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'battery_full_monthly');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'battery_full_monthly');
  assert.equal(entry.id, 'battery_full_monthly');
  assert.deepEqual(entry.when.conditions, [
    { type: 'time_window', start: '00:00', end: '00:00', weekdays: [], month_days: [1] },
    { type: 'balance_threshold', field: 'battery_soc', comparison: 'below', threshold: 100, hysteresis: 0, hold_seconds: 0 },
    { type: 'balance_threshold', field: 'grid_export', comparison: 'above', threshold: 200, hysteresis: 0, hold_seconds: 60 },
  ]);
  assert.equal('until' in entry, false);
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/bat/limit', retain: false, payload_source: 'constant', payload: '5000', entity_id: 'bat_limit',
  }]);
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/bat/limit', retain: false, payload_source: 'constant', payload: '0', entity_id: 'bat_limit',
  }]);
});

test('battery_full_monthly with grid source drops the surplus condition and takes the chosen day', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'battery_full_monthly');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, { source: 'grid', day: 28 }), ctx).entry);
  assert.equal(entry.when.conditions.length, 2);
  assert.deepEqual(entry.when.conditions[0].month_days, [28]);
  assert.equal(entry.when.conditions.some((condition) => condition.field === 'grid_export'), false);
});

test('inverter_silent builds a stale entity rule without a time window', () => {
  const { wizard, templates, t } = load();
  const template = templateById(templates, 'inverter_silent');
  const ctx = plain(contextFor(wizard));
  const rule = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(rule.id, 'inverter_silent');
  assert.equal(rule.enabled, true);
  assert.equal(rule.cooldown_seconds, 21600);
  assert.deepEqual(rule.conditions, [{
    type: 'entity_value', topic: 'werkstatt/pv1/power', entity_id: 'pv1_power', value_template: '', comparison: 'stale', max_age_s: 900,
  }]);
  assert.deepEqual(rule.actions, [{
    type: 'notification',
    severity: 'warning',
    title: t('automations.templates.inverter_silent.notice.title'),
    message: t('automations.templates.inverter_silent.notice.message', { minutes: '15' }),
  }]);
});

test('wallbox_low_tariff shares the tariff steps', () => {
  const { templates } = load();
  const tariff = templateById(templates, 'tariff');
  const wallbox = templateById(templates, 'wallbox_low_tariff');
  for (const id of ['source', 'window', 'price', 'thr']) {
    const shared = tariff.steps.find((step) => step.id === id);
    assert.ok(shared, id);
    assert.equal(wallbox.steps.find((step) => step.id === id), shared, id);
  }
});

test('wallbox_low_tariff builds a fixed window, the plug condition and the wallbox limits', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'wallbox_low_tariff');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx), ctx).entry);
  assert.equal(entry.template, 'wallbox_low_tariff');
  assert.deepEqual(entry.when.conditions, [
    { type: 'time_window', start: '22:00', end: '06:00', weekdays: [] },
    { type: 'entity_value', topic: 'werkstatt/wb/plug', entity_id: 'wb_plug', value_template: '', comparison: 'equals', text: 'ON' },
  ]);
  assert.deepEqual(entry.on_start.actions, [{
    type: 'publish', topic: 'werkstatt/wb/limit', retain: false, payload_source: 'constant', payload: '11000', entity_id: 'wb_limit',
  }]);
  assert.deepEqual(entry.on_end.actions, [{
    type: 'publish', topic: 'werkstatt/wb/limit', retain: false, payload_source: 'constant', payload: '1400', entity_id: 'wb_limit',
  }]);
  assert.deepEqual(entry.while, { mode: 'none' });
});

test('wallbox_low_tariff without the connected option and with a price drops the plug condition', () => {
  const { wizard, templates } = load();
  const template = templateById(templates, 'wallbox_low_tariff');
  const ctx = plain(contextFor(wizard));
  const entry = plain(template.build(answersFor(wizard, template, ctx, {
    source: 'price', thr: 15, connected: { connected: false },
  }), ctx).entry);
  assert.deepEqual(entry.when.conditions, [{
    type: 'entity_value', topic: 'werkstatt/tibber/price', entity_id: 'tibber_price', value_template: '', comparison: 'below', value: 15, hold_seconds: 0,
  }]);
});
