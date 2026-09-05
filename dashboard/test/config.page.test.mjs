// Regression tests for the schema-driven form editor in config.page.js,
// notably the shelly_devices preset selector and the generic optional
// property read/write path it depends on. Run with `npm test` from
// dashboard/ (uses Node's built-in test runner plus jsdom for DOM emulation
// - no browser or build step required).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const staticJS = (name) => fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', name),
  'utf8',
);
const schemaFormSource = staticJS('schema-form.js');
const scriptSource = staticJS('config.page.js');

// Evaluates config.page.js in a fresh jsdom window/VM context and returns the
// Alpine component instance it registers, without touching Node's real
// globals (each test gets its own isolated DOM).
function createConfigPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  vm.runInContext(schemaFormSource, context);
  vm.runInContext(scriptSource, context);
  const component = factory();
  component.$refs = {};
  const stores = attachStores(component);
  return { component, window: dom.window, document: dom.window.document, stores };
}

const shellyDeviceItemSchema = {
  type: 'object',
  required: ['id', 'name', 'host', 'generation'],
  properties: {
    id: { type: 'string', default: 'new_shelly_device' },
    name: { type: 'string', default: 'New Shelly Device' },
    host: { type: 'string', default: '192.168.1.100' },
    generation: { type: 'integer', default: 2 },
    switch_channels: { type: 'integer', default: 0 },
    has_power: { type: 'boolean', default: false },
    has_energy: { type: 'boolean', default: false },
    has_temperature: { type: 'boolean', default: false },
  },
};

const shellyDevicesArraySchema = { type: 'array', items: shellyDeviceItemSchema };

function setControl(node, window, key, value, isCheckbox) {
  const field = node.querySelector(`[data-schema-key="${key}"]`);
  const control = field.querySelector('.schema-control');
  if (isCheckbox) control.checked = value; else control.value = value;
  control.dispatchEvent(new window.Event('input', { bubbles: true }));
  control.dispatchEvent(new window.Event('change', { bubbles: true }));
}

test('readNode saves optional properties the user touched, in the top-level form', () => {
  const { component, window, document } = createConfigPanel();
  const node = component.renderNode(shellyDeviceItemSchema, undefined, 'Eintrag');
  document.body.append(node);

  setControl(node, window, 'id', 'device_1', false);
  setControl(node, window, 'name', 'Test Device', false);
  setControl(node, window, 'host', '192.168.1.50', false);
  setControl(node, window, 'switch_channels', '1', false);
  setControl(node, window, 'has_power', true, true);
  setControl(node, window, 'has_energy', true, true);
  // has_temperature is intentionally left untouched.

  // Round-trip through JSON: `result`'s plain object literals were created
  // inside the jsdom VM context, so they carry that realm's Object.prototype
  // and fail assert/strict's cross-realm identity check despite being
  // structurally identical to a same-process object literal.
  const result = JSON.parse(JSON.stringify(component.readNode(node)));
  assert.deepEqual(result, {
    id: 'device_1',
    name: 'Test Device',
    host: '192.168.1.50',
    generation: 2,
    switch_channels: 1,
    has_power: true,
    has_energy: true,
  });
});

test('readNode leaves untouched optional properties unset (grayed-out placeholder convention)', () => {
  const { component, document } = createConfigPanel();
  const node = component.renderNode(shellyDeviceItemSchema, undefined, 'Eintrag');
  document.body.append(node);

  const result = component.readNode(node);
  assert.equal('has_power' in result, false);
  assert.equal('has_temperature' in result, false);
  assert.equal('switch_channels' in result, false);
});

test('shelly_devices array editor preselects a preset matching an existing entry', () => {
  const { component, document } = createConfigPanel();
  component.selectedName = 'shelly_devices';
  component.shellyPresets = [
    { id: 'shelly_1_gen1', name: 'Shelly 1 (Gen1)', properties: { generation: 1, switch_channels: 1 } },
    {
      id: 'shelly_plug_s_plus_gen2',
      name: 'Shelly Plug S+ (Gen2)',
      properties: { generation: 2, switch_channels: 1, has_power: true, has_energy: true, has_temperature: true },
    },
  ];

  const matchingDevice = {
    id: 'netz_meanwell', name: 'MeanWell', host: '192.168.2.103',
    generation: 2, switch_channels: 1, has_power: true, has_energy: true, has_temperature: true,
  };
  const nonMatchingDevice = {
    id: 'custom', name: 'Custom', host: '192.168.2.200',
    generation: 1, switch_channels: 1, has_power: true,
  };

  const arrayNode = component.renderNode(shellyDevicesArraySchema, [matchingDevice, nonMatchingDevice], 'Geräte');
  document.body.append(arrayNode);
  const selects = [...arrayNode.querySelectorAll('.shelly-preset-select')];

  assert.equal(selects[0].value, 'shelly_plug_s_plus_gen2');
  assert.equal(selects[1].value, '');
});

test('a freshly added blank shelly_devices entry does not spuriously preselect a preset', () => {
  const { component, document } = createConfigPanel();
  component.selectedName = 'shelly_devices';
  component.shellyPresets = [
    { id: 'shelly_1_gen1', name: 'Shelly 1 (Gen1)', properties: { generation: 1, switch_channels: 1 } },
  ];

  const arrayNode = component.renderNode(shellyDevicesArraySchema, [], 'Geräte');
  document.body.append(arrayNode);
  const addButton = [...arrayNode.querySelectorAll('button')].find(b => b.textContent === 'Eintrag hinzufügen');
  addButton.click();

  const select = arrayNode.querySelector('.shelly-preset-select');
  assert.equal(select.value, '');
});

test('applying a preset overwrites only technical fields and preserves identity fields', () => {
  const { component, document } = createConfigPanel();
  component.selectedName = 'shelly_devices';
  const preset = {
    id: 'shelly_plus_ht',
    name: 'Shelly Plus H&T',
    properties: { generation: 2, switch_channels: 0, has_temperature: true },
  };
  component.shellyPresets = [preset];

  const existingDevice = {
    id: 'kept_id', name: 'Kept Name', host: '192.168.2.55',
    generation: 1, switch_channels: 1, has_power: true,
  };
  const item = document.createElement('div');
  item.append(component.renderNode(shellyDeviceItemSchema, existingDevice, 'Eintrag'));
  document.body.append(item);

  component.applyShellyPreset(item, shellyDeviceItemSchema, preset);
  const result = component.readNode(item.querySelector('.schema-node'));

  assert.equal(result.id, 'kept_id');
  assert.equal(result.name, 'Kept Name');
  assert.equal(result.host, '192.168.2.55');
  assert.equal(result.generation, 2);
  assert.equal(result.has_temperature, true);
  assert.equal('has_power' in result, false, 'preset omits has_power, so it must revert to unset/default');
});

// --- numeric fields -------------------------------------------------------
// Regression for the reported bug: the SoC configuration is all decimals, but
// a `type: "number"` control kept the browser default step=1 and rejected
// every one of its own default values.

test('number fields accept decimals, integer fields still do not', () => {
  const { component, document } = createConfigPanel();
  const schema = {
    type: 'object',
    properties: {
      charge_efficiency: { type: 'number', default: 0.98 },
      bank_a_cell_count: { type: 'integer', default: 8 },
    },
  };
  const node = component.renderNode(schema, {}, 'Eintrag');
  document.body.append(node);

  const decimal = node.querySelector('[data-schema-key="charge_efficiency"] .schema-control');
  assert.equal(decimal.step, 'any');
  decimal.value = '0.9';
  assert.equal(decimal.checkValidity(), true, '0.9 must be a valid number');

  const integer = node.querySelector('[data-schema-key="bank_a_cell_count"] .schema-control');
  assert.equal(integer.step, '1');
  integer.value = '1.5';
  assert.equal(integer.validity.stepMismatch, true);
  integer.value = '2';
  assert.equal(integer.checkValidity(), true);
});

test('multipleOf becomes step only when it lines up with the lower bound', () => {
  const { component } = createConfigPanel();
  assert.equal(component.numericStep({ type: 'number', multipleOf: 0.5 }), '0.5');
  assert.equal(component.numericStep({ type: 'number', multipleOf: 0.5, minimum: 1 }), '0.5');
  // HTML anchors step at min, JSON Schema anchors multipleOf at zero.
  assert.equal(component.numericStep({ type: 'number', multipleOf: 0.5, minimum: 0.3 }), 'any');
});

test('exclusive bounds map onto min/max, exactly for integers', () => {
  const { component, document } = createConfigPanel();
  const schema = {
    type: 'object',
    properties: {
      cells: { type: 'integer', exclusiveMinimum: 0 },
      scale: { type: 'number', exclusiveMinimum: 0 },
      ratio: { type: 'number', exclusiveMaximum: 1 },
    },
  };
  const node = component.renderNode(schema, {}, 'Eintrag');
  document.body.append(node);

  const cells = node.querySelector('[data-schema-key="cells"] .schema-control');
  assert.equal(cells.min, '1');
  cells.value = '0';
  assert.equal(cells.checkValidity(), false, 'an integer exclusiveMinimum is exact');

  // For decimals the bound itself stays as a coarse hint; the Go validator
  // rejects the boundary value.
  assert.equal(node.querySelector('[data-schema-key="scale"] .schema-control').min, '0');
  assert.equal(node.querySelector('[data-schema-key="ratio"] .schema-control').max, '1');
});

test('readNode returns decimals unchanged and omits untouched optional fields', () => {
  const { component, window, document } = createConfigPanel();
  const schema = {
    type: 'object',
    properties: {
      charge_efficiency: { type: 'number', default: 0.98 },
      // No default at all: this is how a property stays optional-and-empty,
      // the mechanism internal_resistance_mohm_per_cell relies on.
      internal_resistance_mohm_per_cell: { type: 'number' },
    },
  };
  const node = component.renderNode(schema, {}, 'Eintrag');
  document.body.append(node);

  const resistance = node.querySelector('[data-schema-key="internal_resistance_mohm_per_cell"] .schema-control');
  assert.equal(resistance.value, '', 'a property without default must render empty');

  setControl(node, window, 'charge_efficiency', '0.9');
  const result = component.readNode(node);
  assert.equal(result.charge_efficiency, 0.9);
  assert.equal('internal_resistance_mohm_per_cell' in result, false);
});

test('saveForm reports an invalid optional field instead of doing nothing', async () => {
  const { component, window, document, stores } = createConfigPanel();
  let requests = 0;
  window.fetch = () => { requests += 1; return Promise.resolve({ ok: true, json: async () => ({}) }); };
  component.selectedName = 'battery_soc_devices';
  component.schema = { type: 'object', properties: { charge_efficiency: { type: 'number', maximum: 1 } } };
  component.value = {};

  const form = document.createElement('form');
  document.body.append(form);
  component.$refs = { schemaForm: form };
  component.renderForm();

  setControl(form, window, 'charge_efficiency', '1.5');
  await component.saveForm();

  assert.equal(requests, 0, 'an invalid value must not be sent');
  assert.notEqual(stores.toasts.last('critical'), '', 'the user must see why nothing was saved');
  const details = form.querySelector('details.schema-optional-group');
  assert.equal(details.hasAttribute('open'), true, 'the collapsed group must open, or the message points at a hidden field');
});

// --- JSON key suggestions -------------------------------------------------

const topicKeySchema = {
  type: 'object',
  properties: {
    x_topic: { type: 'string', format: 'mqtt-topic' },
    x_json_key: { type: 'string', format: 'mqtt-json-key' },
  },
};

function renderTopicKeyForm(component, document, { topics, samples }) {
  component.topics = topics;
  component.topicSamples = new Map(samples.map(sample => [sample.topic, sample]));
  const node = component.renderNode(topicKeySchema, {}, 'Eintrag');
  document.body.append(node);
  return node;
}

test('a JSON object payload fills the datalist, numeric keys first', () => {
  const { component, window, document } = createConfigPanel();
  const node = renderTopicKeyForm(component, document, {
    topics: ['shelly/power'],
    samples: [{ topic: 'shelly/power', payload: '{"id":0,"apower":12.5,"source":"init"}' }],
  });

  setControl(node, window, 'x_topic', 'shelly/power');

  const keyNode = node.querySelector('[data-schema-key="x_json_key"]');
  const options = [...keyNode.querySelectorAll('datalist option')].map(option => option.value);
  assert.deepEqual(options, ['id', 'apower', 'source']);
  assert.equal(keyNode.querySelector('.schema-control').placeholder, 'id');
});

test('a payload that is not a JSON object warns instead of suggesting', () => {
  const { component, window, document } = createConfigPanel();
  const node = renderTopicKeyForm(component, document, {
    topics: ['bms/voltage'],
    samples: [{ topic: 'bms/voltage', payload: '26.8' }],
  });

  setControl(node, window, 'x_topic', 'bms/voltage');

  const keyNode = node.querySelector('[data-schema-key="x_json_key"]');
  assert.equal(keyNode.querySelectorAll('datalist option').length, 0);
  const hint = keyNode.querySelector('.schema-hint');
  assert.ok(hint, 'expected a hint');
  assert.equal(hint.classList.contains('warn'), true);
  assert.ok(hint.textContent.includes('26.8'), 'the hint must show the actual payload');
});

test('a topic without any message says so, in the hint and in the option label', () => {
  const { component, window, document } = createConfigPanel();
  const node = renderTopicKeyForm(component, document, {
    topics: ['shelly/silent'],
    samples: [{ topic: 'shelly/silent' }],
  });

  const option = [...node.querySelectorAll('[data-schema-key="x_topic"] option')]
    .find(candidate => candidate.value === 'shelly/silent');
  assert.ok(option.textContent.includes('(noch keine Daten)'));

  setControl(node, window, 'x_topic', 'shelly/silent');
  const hint = node.querySelector('[data-schema-key="x_json_key"] .schema-hint');
  assert.ok(hint.textContent.includes('noch keine Nachricht'));
});

test('a key missing from the sample survives - the datalist suggests, it does not restrict', () => {
  const { component, window, document } = createConfigPanel();
  component.topics = ['shelly/power'];
  component.topicSamples = new Map([['shelly/power', { topic: 'shelly/power', payload: '{"apower":12.5}' }]]);
  const node = component.renderNode(topicKeySchema, { x_topic: 'shelly/power', x_json_key: 'total_act_power' }, 'Eintrag');
  document.body.append(node);

  setControl(node, window, 'x_topic', 'shelly/power');

  assert.equal(component.readNode(node).x_json_key, 'total_act_power');
});

test('an empty key against a JSON payload warns about the raw-text fallback', () => {
  const { component, window, document } = createConfigPanel();
  const node = renderTopicKeyForm(component, document, {
    topics: ['shelly/power'],
    samples: [{ topic: 'shelly/power', payload: '{"id":0,"apower":12.5}' }],
  });

  setControl(node, window, 'x_topic', 'shelly/power');

  const hint = node.querySelector('[data-schema-key="x_json_key"] .schema-hint');
  assert.equal(hint.classList.contains('warn'), true);
  assert.ok(hint.textContent.includes('erste Zahl'));
});

test('a failing /api/v1/topics/samples does not block the form', async () => {
  const { component, window, document, stores } = createConfigPanel();
  window.fetch = url => {
    if (String(url).includes('/topics/samples')) return Promise.reject(new Error('not found'));
    if (String(url).endsWith('/api/v1/topics')) return Promise.resolve({ ok: true, json: async () => ['shelly/power'] });
    return Promise.resolve({ ok: true, json: async () => [] });
  };
  const form = document.createElement('form');
  const diff = document.createElement('pre');
  document.body.append(form, diff);
  component.$refs = { schemaForm: form, revisionDiff: diff };

  await component.load();

  assert.deepEqual(stores.toasts.criticals, [], `load() failed: ${stores.toasts.last('critical')}`);
  assert.deepEqual(component.topics, ['shelly/power']);
  assert.equal(component.topicSamples.size, 0);
});

function topicField(component, document, displayedValue = '') {
  const node = component.renderNode(
    { type: 'string', format: 'mqtt-topic' }, displayedValue, 'Topic', false, 'charger_power_topic');
  document.body.append(node);
  return node;
}

test('topic selector groups options by owning device', () => {
  const { component, document } = createConfigPanel();
  component.topics = ['bms/bank_a/voltage', 'fremd/topic', 'shelly/netz_meanwell/status'];
  component.topicSamples = new Map([
    ['bms/bank_a/voltage', { topic: 'bms/bank_a/voltage', device: 'Shelly Uni Bank A', payload: '26.8' }],
    ['shelly/netz_meanwell/status', { topic: 'shelly/netz_meanwell/status', device: 'MeanWell-Ladegeraet', payload: '{"apower":12.5}' }],
    ['fremd/topic', { topic: 'fremd/topic' }],
  ]);

  const node = topicField(component, document);
  const groups = [...node.querySelectorAll('optgroup')].map(group => group.label);
  assert.deepEqual(groups, ['MeanWell-Ladegeraet', 'Shelly Uni Bank A', 'Sonstige']);

  const values = [...node.querySelectorAll('option')].map(option => option.value);
  assert.ok(values.includes('bms/bank_a/voltage'));
  assert.ok(values.includes('fremd/topic'));
});

test('topic selector stays a flat list when no device names are known', () => {
  const { component, document } = createConfigPanel();
  component.topics = ['a/one', 'b/two'];
  component.topicSamples = new Map();

  const node = topicField(component, document);

  assert.equal(node.querySelectorAll('optgroup').length, 0);
  assert.deepEqual([...node.querySelectorAll('option')].map(option => option.value), ['', 'a/one', 'b/two']);
});

test('topic selector keeps a configured topic that discovery does not know', () => {
  const { component, document } = createConfigPanel();
  component.topics = ['a/one'];
  component.topicSamples = new Map([['a/one', { topic: 'a/one', device: 'Geraet A', payload: '1' }]]);

  const node = topicField(component, document, 'nur/in/der/konfiguration');
  const control = node.querySelector('.schema-control');

  assert.equal(control.value, 'nur/in/der/konfiguration');
});

const batteryItemSchema = {
  type: 'object',
  required: ['id', 'name'],
  properties: {
    id: { type: 'string', default: 'new_battery_soc' },
    name: { type: 'string', default: 'New Battery SoC' },
    topology: { type: 'string', enum: ['parallel', 'series'], default: 'parallel' },
    bank_a_cell_count: { type: 'integer', default: 8 },
    bank_b_cell_count: { type: 'integer', default: 8 },
    empty_v_per_cell: { type: 'number', default: 2.5 },
    full_v_per_cell: { type: 'number', default: 3.55 },
  },
};

function batteryPanel(payload) {
  const created = createConfigPanel();
  created.component.selectedName = 'battery_soc_devices';
  created.component.topics = ['outstation/battery_soc/state'];
  created.component.topicSamples = new Map([[
    'outstation/battery_soc/state',
    { topic: 'outstation/battery_soc/state', device: 'Batterie-Ladezustand', payload: JSON.stringify(payload), at: new Date().toISOString() },
  ]]);
  return created;
}

const liveBatteryStateSeries = {
  bank_a_voltage_v: 27.36,
  bank_b_voltage_v: 27.28,
  bank_a_corrected_v_per_cell: 3.398,
  bank_b_corrected_v_per_cell: 3.388,
};

const liveBatteryStatePack = {
  pack_voltage_v: 27.36,
  pack_corrected_v_per_cell: 3.398,
};

test('battery form offers the measured cell voltage for both threshold fields (series topology)', () => {
  const { component, document } = batteryPanel(liveBatteryStateSeries);
  const node = component.renderNode(batteryItemSchema, { id: 'battery_soc', name: 'Batterie', topology: 'series' }, 'Eintrag');
  document.body.append(node);

  ['empty_v_per_cell', 'full_v_per_cell'].forEach(key => {
    const block = node.querySelector(`[data-schema-key="${key}"] .battery-measure`);
    assert.ok(block, `Messblock fehlt bei ${key}`);
    const labels = [...block.querySelectorAll('.battery-measure-apply')].map(button => button.textContent);
    // 27.36 / 8 = 3.420 roh, 3.398 lastkorrigiert
    assert.ok(labels.some(text => text.includes('roh 3.420')), labels.join(' | '));
    assert.ok(labels.some(text => text.includes('lastkorrigiert 3.398')), labels.join(' | '));
  });
});

// topology="parallel" ist der Python-Default (battery_soc_mqtt.py) und fuehrt
// nur EINEN Bus statt zweier Baenke - der State-Payload traegt dann
// pack_voltage_v statt bank_a_voltage_v/bank_b_voltage_v.
test('battery form offers the measured cell voltage for the default parallel topology', () => {
  const { component, document } = batteryPanel(liveBatteryStatePack);
  const node = component.renderNode(batteryItemSchema, { id: 'battery_soc', name: 'Batterie' }, 'Eintrag');
  document.body.append(node);

  const block = node.querySelector('[data-schema-key="empty_v_per_cell"] .battery-measure');
  assert.ok(block, 'Messblock fehlt');
  const labels = [...block.querySelectorAll('.battery-measure-apply')].map(button => button.textContent);
  // 27.36 / 8 = 3.420 roh, 3.398 lastkorrigiert
  assert.ok(labels.some(text => text.includes('roh 3.420')), labels.join(' | '));
  assert.ok(labels.some(text => text.includes('lastkorrigiert 3.398')), labels.join(' | '));
});

test('applying a measured value writes it into the field and marks it as touched', () => {
  const { component, document } = batteryPanel(liveBatteryStateSeries);
  const node = component.renderNode(batteryItemSchema, { id: 'battery_soc', name: 'Batterie', topology: 'series' }, 'Eintrag');
  document.body.append(node);

  const fullField = node.querySelector('[data-schema-key="full_v_per_cell"]');
  const applyCorrected = [...fullField.querySelectorAll('.battery-measure-apply')]
    .find(button => button.textContent.includes('lastkorrigiert 3.398'));
  applyCorrected.click();

  assert.equal(fullField.querySelector('.schema-control').value, '3.398');
  // Ohne den Wechsel auf "nicht mehr Default" wuerde readNode() den Wert
  // verwerfen und das Speichern liefe ins Leere.
  assert.equal(fullField.dataset.schemaDefault, 'false');
  const stored = JSON.parse(JSON.stringify(component.readNode(node)));
  assert.equal(stored.full_v_per_cell, 3.398);
});

test('measured value uses the cell count currently in the form, not the saved one', () => {
  const { component, window, document } = batteryPanel(liveBatteryStateSeries);
  const node = component.renderNode(batteryItemSchema, { id: 'battery_soc', name: 'Batterie', topology: 'series', bank_a_cell_count: 8 }, 'Eintrag');
  document.body.append(node);

  setControl(node, window, 'bank_a_cell_count', '16', false);
  const block = node.querySelector('[data-schema-key="empty_v_per_cell"] .battery-measure');
  block.querySelector('.battery-measure-refresh').click();

  const labels = [...block.querySelectorAll('.battery-measure-apply')].map(button => button.textContent);
  // 27.36 / 16 = 1.710
  assert.ok(labels.some(text => text.includes('roh 1.710')), labels.join(' | '));
});

test('battery form says so when no live values are available', () => {
  const { component, document } = createConfigPanel();
  component.selectedName = 'battery_soc_devices';
  component.topics = [];
  component.topicSamples = new Map();
  const node = component.renderNode(batteryItemSchema, { id: 'battery_soc', name: 'Batterie' }, 'Eintrag');
  document.body.append(node);

  const block = node.querySelector('[data-schema-key="empty_v_per_cell"] .battery-measure');
  assert.match(block.textContent, /Keine Live-Daten/);
  assert.equal(block.querySelectorAll('.battery-measure-apply').length, 0);
});

test('other configurations get no measurement block', () => {
  const { component, document } = createConfigPanel();
  component.selectedName = 'shelly_devices';
  const node = component.renderNode(shellyDeviceItemSchema, undefined, 'Eintrag');
  document.body.append(node);

  assert.equal(node.querySelectorAll('.battery-measure').length, 0);
});

// --- Aktionsleiste: Änderungsstand und Reduzieren ---------------------------
//
// Die Leiste behauptet einen Speicherstand - dieser Teil muss stimmen, sonst
// ist sie schlimmer als keine Leiste. Die reine Anzeige (Material, Pill,
// Einklappen) prueft die Sichtpruefung im Smoke-Test, hier steht nur, was der
// Zustand sagt.

const soloSchema = { type: 'object', properties: { host: { type: 'string' } } };

function mountForm(component, document, { schema = soloSchema, value = {}, name = 'demo' } = {}) {
  component.selectedName = name;
  component.schema = schema;
  component.value = value;
  component.editorText = JSON.stringify(value, null, 2);
  const form = document.createElement('form');
  document.body.append(form);
  component.$refs = { schemaForm: form };
  component.renderForm();
  return form;
}

test('renderForm clears the touched marker, editing an array sets it again', () => {
  const { component, document } = createConfigPanel();
  const arraySchema = { type: 'array', items: { type: 'object', properties: { id: { type: 'string' } } } };
  const form = mountForm(component, document, { schema: arraySchema, value: [] });

  // Rendering an existing config is not an edit.
  assert.equal(component.formDirty, false);

  form.querySelector('.schema-array > button').click();
  assert.equal(component.formDirty, true, 'ein neuer Eintrag ist eine ungespeicherte Aenderung');

  component.renderForm();
  assert.equal(component.formDirty, false, 'Formular zuruecksetzen loescht die Marke');
});

test('removing an array entry counts as an unsaved change', () => {
  const { component, document } = createConfigPanel();
  const arraySchema = { type: 'array', items: { type: 'object', properties: { id: { type: 'string' } } } };
  const form = mountForm(component, document, { schema: arraySchema, value: [{ id: 'a' }, { id: 'b' }] });
  assert.equal(component.formDirty, false);

  form.querySelector('.array-item .array-item-header button:last-child').click();
  assert.equal(component.formDirty, true);
  assert.equal(form.querySelectorAll('.array-item').length, 1);
});

test('editorDirty compares against the loaded config, resetEditor restores it', () => {
  const { component, document } = createConfigPanel();
  mountForm(component, document, { value: { host: '192.168.1.5' } });
  assert.equal(component.editorDirty, false, 'frisch geladen ist nichts geaendert');

  component.editorText = '{"host": "10.0.0.1"}';
  assert.equal(component.editorDirty, true);

  component.resetEditor();
  assert.equal(component.editorDirty, false);
  assert.equal(component.editorText, JSON.stringify({ host: '192.168.1.5' }, null, 2));
});

test('editorDirty stays false before any configuration is loaded', () => {
  const { component } = createConfigPanel();
  assert.equal(component.value, null);
  assert.equal(component.editorDirty, false, 'ohne geladene Datei darf die Leiste nichts behaupten');
});

test('actionStatusText names the form change over the JSON change', () => {
  const { component, document } = createConfigPanel();
  mountForm(component, document, { value: { host: 'a' } });
  assert.equal(component.actionStatusText, 'Alles gespeichert');

  component.editorText = '{"host": "b"}';
  assert.equal(component.actionStatusText, 'JSON-Text geändert');

  component.formDirty = true;
  assert.equal(component.actionStatusText, 'Ungespeicherte Änderungen', 'die Leiste speichert das Formular, das steht vorn');

  component.saving = true;
  assert.equal(component.actionStatusText, 'Speichert ...');
});

test('saveForm clears the touched marker and says that it replaced the JSON text', async () => {
  const { component, window, document, stores } = createConfigPanel();
  window.fetch = () => Promise.resolve({ ok: true, json: async () => ({}) });
  const form = mountForm(component, document, { value: { host: 'a' } });

  setControl(form.querySelector('.schema-node'), window, 'host', 'b');
  component.formDirty = true;
  component.editorText = '{"host": "aus dem Rohtext"}';

  await component.saveForm();

  assert.equal(component.formDirty, false, 'nach dem Speichern gibt es nichts Ungespeichertes mehr');
  assert.equal(component.editorDirty, false);
  assert.equal(component.editorText, JSON.stringify({ host: 'b' }, null, 2));
  assert.match(stores.toasts.last('warning'), /JSON-Text/, 'der verworfene Rohtext darf nicht stumm verschwinden');
});

test('saveForm stays quiet about the JSON text when it was untouched', async () => {
  const { component, window, document, stores } = createConfigPanel();
  window.fetch = () => Promise.resolve({ ok: true, json: async () => ({}) });
  const form = mountForm(component, document, { value: { host: 'a' } });
  setControl(form.querySelector('.schema-node'), window, 'host', 'b');

  await component.saveForm();

  assert.equal(stores.toasts.last('warning'), '', 'ohne eigenen Rohtext-Stand gibt es nichts zu warnen');
});

test('expandActions reopens the collapsed bar', () => {
  const { component } = createConfigPanel();
  component.actionsCompact = true;
  component.expandActions();
  assert.equal(component.actionsCompact, false);
});

test('initActionBar collapses on scroll only while the bar floats', async () => {
  const { component, window } = createConfigPanel();
  // jsdom laeuft ohne pretendToBeVisual und hat deshalb kein
  // requestAnimationFrame; der Scroll-Handler braucht eines.
  const frames = [];
  window.requestAnimationFrame = callback => frames.push(callback);
  const nextFrame = async () => { frames.splice(0).forEach(callback => callback()); };

  component.$refs = { actionSentinel: window.document.createElement('div') };
  component.$nextTick = fn => fn();
  component.initActionBar();

  // Ohne IntersectionObserver (jsdom) faellt initActionBar auf "geloest"
  // zurueck - andernfalls waere die Leiste dort nie reduzierbar.
  assert.equal(component.actionsFloating, true);

  // Der Scroll-Handler arbeitet auf dem naechsten Frame, sonst rechnet er
  // pro Scroll-Ereignis statt pro Ereignis.
  component.actionsFloating = false;
  window.dispatchEvent(new window.Event('scroll'));
  await nextFrame();
  assert.equal(component.actionsCompact, false, 'angedockt bleibt die Leiste vollstaendig');

  component.actionsFloating = true;
  window.dispatchEvent(new window.Event('scroll'));
  await nextFrame();
  assert.equal(component.actionsCompact, true, 'geloest schrumpft sie beim Scrollen');
});

// --- Speichern fragt vorher --------------------------------------------------
//
// Save() im Go-Teil schreibt die Datei UND ruft direkt danach reload() - der
// Dienst-Neustart haengt also am Speichern-Button. Die Frage muss deshalb vor
// dem PUT stehen; danach waere sie eine Frage nach etwas Geschehenem.

test('saveForm asks before writing and names the configuration', async () => {
  const { component, window, document, stores } = createConfigPanel();
  let requests = 0;
  window.fetch = () => { requests += 1; return Promise.resolve({ ok: true, json: async () => ({}) }); };
  component.configs = [{ name: 'battery_soc_devices', label: 'Batterie-Ladezustand (SoC)' }];
  mountForm(component, document, { value: { host: 'a' }, name: 'battery_soc_devices' });

  await component.saveForm();

  assert.equal(stores.modal.calls.length, 1, 'ohne Rueckfrage darf nichts geschrieben werden');
  assert.match(stores.modal.calls[0].body, /Batterie-Ladezustand \(SoC\)/, 'die Frage muss sagen, welche Datei betroffen ist');
  assert.match(stores.modal.calls[0].body, /Dienst/, 'und dass der Dienst dabei neu laedt');
  assert.equal(requests, 1);
});

test('declining the confirmation writes nothing and keeps the change pending', async () => {
  const { component, window, document, stores } = createConfigPanel();
  let requests = 0;
  window.fetch = () => { requests += 1; return Promise.resolve({ ok: true, json: async () => ({}) }); };
  stores.modal.answer = false;
  const form = mountForm(component, document, { value: { host: 'a' } });
  setControl(form.querySelector('.schema-node'), window, 'host', 'b');
  component.formDirty = true;

  await component.saveForm();

  assert.equal(requests, 0, 'Abbrechen darf nicht speichern');
  assert.equal(component.formDirty, true, 'die Aenderung bleibt ungespeichert und muss das auch sagen');
  assert.equal(stores.toasts.last(), '', 'kein "gespeichert" fuer etwas, das nicht gespeichert wurde');
});

test('an invalid field is reported before the confirmation is asked for', async () => {
  const { component, window, document, stores } = createConfigPanel();
  window.fetch = () => Promise.resolve({ ok: true, json: async () => ({}) });
  component.selectedName = 'battery_soc_devices';
  component.schema = { type: 'object', properties: { charge_efficiency: { type: 'number', maximum: 1 } } };
  component.value = {};
  const form = document.createElement('form');
  document.body.append(form);
  component.$refs = { schemaForm: form };
  component.renderForm();
  setControl(form, window, 'charge_efficiency', '1.5');

  await component.saveForm();

  assert.equal(stores.modal.calls.length, 0, 'erst pruefen, dann fragen');
});

test('saveEditor rejects broken JSON before asking, and asks for valid JSON', async () => {
  const { component, window, document, stores } = createConfigPanel();
  // Nach einem erfolgreichen PUT laedt saveEditor die Datei neu (zwei weitere
  // GETs) - gezaehlt wird deshalb nur der Schreibzugriff.
  let writes = 0;
  window.fetch = (_url, options) => {
    if (options && options.method === 'PUT') writes += 1;
    return Promise.resolve({ ok: true, json: async () => ({}) });
  };
  mountForm(component, document, { value: { host: 'a' } });

  component.editorText = '{kaputt';
  await component.saveEditor();
  assert.equal(stores.modal.calls.length, 0, 'kaputtes JSON wird nicht zur Rueckfrage');
  assert.equal(writes, 0);
  assert.notEqual(stores.toasts.last('critical'), '');

  component.editorText = '{"host": "b"}';
  await component.saveEditor();
  assert.equal(stores.modal.calls.length, 1);
  assert.equal(writes, 1);
});

test('the success message says the service reloaded, unless it did not', async () => {
  const { component, window, document, stores } = createConfigPanel();
  window.fetch = () => Promise.resolve({ ok: true, json: async () => ({}) });
  mountForm(component, document, { value: { host: 'a' } });
  await component.saveForm();
  assert.match(stores.toasts.last('info'), /Dienst neu geladen/);

  const failing = createConfigPanel();
  failing.window.fetch = () => Promise.resolve({ ok: true, json: async () => ({ reload_failed: true, reload_error: 'kein Socket' }) });
  mountForm(failing.component, failing.document, { value: { host: 'a' } });
  await failing.component.saveForm();
  assert.equal(failing.component.reloadFailed, true);
  assert.doesNotMatch(failing.stores.toasts.last('info'), /Dienst neu geladen/, 'kein Neuladen behaupten, das fehlgeschlagen ist');
});
