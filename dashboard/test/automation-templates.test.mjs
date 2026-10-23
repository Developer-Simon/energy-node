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
];

const snapshot = {
  values: { pv: 862, grid_export: 0, grid_import: 0, battery_soc: 64 },
  entities: [
    { device_id: 'pv1', entity_id: 'pv1_power', role: { role: 'pv' } },
    { device_id: 'wb', entity_id: 'wb_power', role: { role: 'wallbox' } },
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
};

// Zusaetzliche Mitteilungstexte, die eine Vorlage im gebauten Eintrag verwendet.
const NOTICE_KEYS = {
  zero_feed: ['automations.templates.zero_feed.notice.title', 'automations.templates.zero_feed.notice.message'],
  battery_warn: ['automations.templates.battery_warn.notice.title', 'automations.templates.battery_warn.notice.message'],
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
    const variants = [{}, ...(VARIANTS[template.id] || [])];
    for (const overrides of variants) {
      const answers = answersFor(wizard, template, ctx, overrides);
      const result = template.build(answers, ctx);
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
      const visible = new Set(wizard.visibleSteps(template, answers).map((step) => step.id));
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
