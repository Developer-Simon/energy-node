// Tests for the generic schema-form engine (schema-form.js) that both the
// Konfigurationen tab and the Systemkonfiguration on the Einstellungsseite
// build their forms from. Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'schema-form.js'),
  'utf8',
);

function loadSchemaForm() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  vm.runInContext(source, context);
  return { SchemaForm: dom.window.SchemaForm, window: dom.window, document: dom.window.document };
}

const ctx = { hooks: {} };

// Values built inside the jsdom VM carry that realm's prototypes and fail
// assert/strict's cross-realm identity check despite being structurally
// identical - a JSON round-trip rehomes them in this process.
const plain = (value) => JSON.parse(JSON.stringify(value));

// The central config.json schema, trimmed to the shapes that matter here.
const appSchema = {
  type: 'object',
  additionalProperties: false,
  required: ['schema_version', 'mqtt', 'paths', 'node'],
  properties: {
    schema_version: { type: 'integer', minimum: 1, maximum: 1, title: 'Schemaversion' },
    mqtt: {
      type: 'object',
      additionalProperties: false,
      required: ['host', 'port'],
      properties: {
        host: { type: 'string', minLength: 1, title: 'Broker-Host' },
        port: { type: 'integer', minimum: 1, maximum: 65535, title: 'Broker-Port' },
      },
    },
    paths: {
      type: 'object',
      additionalProperties: false,
      required: ['data_dir'],
      properties: {
        data_dir: { type: 'string', minLength: 1, title: 'Daten' },
        services_version_file: { type: 'string', title: 'Versionsdatei' },
      },
    },
    node: {
      type: 'object',
      additionalProperties: false,
      required: ['device_id', 'managed_bridges', 'poll_interval_s'],
      properties: {
        device_id: { type: 'string', minLength: 1, title: 'Node-ID' },
        managed_bridges: { type: 'array', items: { type: 'string', minLength: 1 }, title: 'Bridges' },
        poll_interval_s: { type: 'number', exclusiveMinimum: 0, title: 'Intervall' },
      },
    },
  },
};

const appValue = {
  schema_version: 1,
  mqtt: { host: 'localhost', port: 1883 },
  paths: { data_dir: '/var/lib/werkstatt' },
  node: { device_id: 'node', managed_bridges: ['bridge-a', 'bridge-b'], poll_interval_s: 5 },
};

test('renderNode + readNode round-trips a nested config document', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const result = JSON.parse(JSON.stringify(SchemaForm.readNode(node)));
  assert.deepEqual(result, appValue);
});

test('an optional string left untouched is omitted, not written as ""', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const result = SchemaForm.readNode(node);
  assert.equal('services_version_file' in result.paths, false);
});

test('editing an optional string makes readNode keep it', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const field = node.querySelector('[data-schema-key="services_version_file"] .schema-control');
  field.value = '/opt/versions/src.txt';
  field.dispatchEvent(new window.Event('input', { bubbles: true }));
  assert.equal(SchemaForm.readNode(node).paths.services_version_file, '/opt/versions/src.txt');
});

test('a single-value field (minimum === maximum) renders read-only but still reads back', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const versionNode = node.querySelector('[data-schema-key="schema_version"]');
  const control = versionNode.querySelector('.schema-control');
  assert.equal(control.readOnly, true);
  assert.equal(versionNode.dataset.schemaLocked, 'true');
  assert.equal(SchemaForm.readNode(node).schema_version, 1);
});

test('numeric bounds land on the control as min / max attributes', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const port = node.querySelector('[data-schema-key="port"] .schema-control');
  assert.equal(port.getAttribute('min'), '1');
  assert.equal(port.getAttribute('max'), '65535');
});

test('adding and removing array items is reflected in readNode', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const node = SchemaForm.renderNode(ctx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  const bridges = node.querySelector('[data-schema-key="managed_bridges"]');
  bridges.querySelector(':scope > button').click(); // "Eintrag hinzufügen"
  const controls = bridges.querySelectorAll('.array-items .schema-control');
  assert.equal(controls.length, 3);
  controls[2].value = 'bridge-c';
  assert.deepEqual(plain(SchemaForm.readNode(node).node.managed_bridges), ['bridge-a', 'bridge-b', 'bridge-c']);

  const firstRemove = bridges.querySelector('.array-item .array-item-header button');
  firstRemove.click();
  assert.deepEqual(plain(SchemaForm.readNode(node).node.managed_bridges), ['bridge-b', 'bridge-c']);
});

test('onDirty hook fires when an array item is added', () => {
  const { SchemaForm, document } = loadSchemaForm();
  let dirty = 0;
  const dirtyCtx = { hooks: { onDirty: () => { dirty += 1; } } };
  const node = SchemaForm.renderNode(dirtyCtx, appSchema, appValue, 'Konfiguration');
  document.body.append(node);
  node.querySelector('[data-schema-key="managed_bridges"] > button').click();
  assert.equal(dirty, 1);
});

test('findUnknownKeys reports keys the schema does not declare', () => {
  const { SchemaForm } = loadSchemaForm();
  const drifted = JSON.parse(JSON.stringify(appValue));
  drifted.mqtt.tls_ca_file = '/etc/ca.pem';
  drifted.legacy_section = { foo: 1 };
  const unknown = plain(SchemaForm.findUnknownKeys(appSchema, drifted));
  assert.deepEqual(unknown.sort(), ['legacy_section', 'mqtt.tls_ca_file']);
});

test('findUnknownKeys is empty for a document that matches the schema', () => {
  const { SchemaForm } = loadSchemaForm();
  assert.equal(SchemaForm.findUnknownKeys(appSchema, appValue).length, 0);
});

test('control hook can replace the rendered input', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const hookCtx = {
    hooks: {
      control: ({ key }) => {
        if (key !== 'host') return null;
        const select = document.createElement('select');
        for (const host of ['localhost', 'broker.local']) {
          const option = document.createElement('option');
          option.value = host;
          option.textContent = host;
          select.append(option);
        }
        return select;
      },
    },
  };
  const node = SchemaForm.renderNode(hookCtx, appSchema.properties.mqtt, appValue.mqtt, 'MQTT');
  document.body.append(node);
  const host = node.querySelector('[data-schema-key="host"] .schema-control');
  assert.equal(host.tagName, 'SELECT');
  assert.equal(SchemaForm.readNode(node).host, 'localhost');
});

const conditionalItemSchema = {
  type: 'object',
  required: ['id'],
  unevaluatedProperties: false,
  properties: {
    id: { type: 'string', title: 'ID' },
    system_type: { type: 'string', enum: ['ac_coupled', 'dc_only'], default: 'ac_coupled' },
    dc_topic: { type: 'string' },
    bank_b_enabled: { type: 'boolean', default: true },
  },
  allOf: [
    { if: { properties: { system_type: { const: 'ac_coupled' } } },
      then: { properties: { ac_topic: { type: 'string' } } } },
    { if: { properties: { bank_b_enabled: { const: true } } },
      then: {
        properties: { topology: { type: 'string', enum: ['parallel', 'series'], default: 'parallel' } },
        allOf: [
          { if: { required: ['topology'], properties: { topology: { const: 'series' } } },
            then: { properties: { bank_b_topic: { type: 'string' } } } },
        ],
      } },
  ],
};

function field(root, key) {
  return root.querySelector(`[data-schema-key="${key}"]`);
}

function choose(window, root, key, value) {
  const control = field(root, key).querySelector('.schema-control');
  if (control.type === 'checkbox') control.checked = value; else control.value = value;
  control.dispatchEvent(new window.Event('change', { bubbles: true }));
}

test('conditional fields follow their branch', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const root = SchemaForm.renderNode(ctx, conditionalItemSchema, { id: 'b', ac_topic: 'shelly/p' }, 'Batterie');
  document.body.append(root);
  assert.equal(field(root, 'ac_topic').hidden, false, 'default ac_coupled shows AC');
  assert.equal(field(root, 'topology').hidden, false, 'default bank_b_enabled shows topology');
  assert.equal(field(root, 'bank_b_topic').hidden, true, 'default parallel hides series fields');

  choose(window, root, 'system_type', 'dc_only');
  assert.equal(field(root, 'ac_topic').hidden, true);
  choose(window, root, 'topology', 'series');
  assert.equal(field(root, 'bank_b_topic').hidden, false);
  choose(window, root, 'bank_b_enabled', false);
  assert.equal(field(root, 'topology').hidden, true);
  assert.equal(field(root, 'bank_b_topic').hidden, true, 'nested branch needs its parent');
});

test('hidden fields are not saved but keep their value until then', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const root = SchemaForm.renderNode(ctx, conditionalItemSchema, { id: 'b', ac_topic: 'shelly/p' }, 'Batterie');
  document.body.append(root);
  choose(window, root, 'system_type', 'dc_only');
  assert.equal(plain(SchemaForm.readNode(root)).ac_topic, undefined);
  choose(window, root, 'system_type', 'ac_coupled');
  assert.equal(plain(SchemaForm.readNode(root)).ac_topic, 'shelly/p');
});

test('conditional fields render right behind the field they depend on', () => {
  const { SchemaForm, document } = loadSchemaForm();
  const root = SchemaForm.renderNode(ctx, conditionalItemSchema, { id: 'b' }, 'Batterie');
  document.body.append(root);
  const keys = [...root.querySelectorAll('.schema-node[data-schema-key]')].map(node => node.dataset.schemaKey);
  assert.equal(keys.indexOf('ac_topic'), keys.indexOf('system_type') + 1);
  assert.equal(keys.indexOf('topology'), keys.indexOf('bank_b_enabled') + 1);
  assert.equal(keys.indexOf('bank_b_topic'), keys.indexOf('topology') + 1);
});

test('resetting an optional boolean re-evaluates the branches', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const root = SchemaForm.renderNode(ctx, conditionalItemSchema, { id: 'b', bank_b_enabled: false }, 'Batterie');
  document.body.append(root);
  assert.equal(field(root, 'topology').hidden, true);
  field(root, 'bank_b_enabled').querySelector('.boolean-reset').click();
  assert.equal(field(root, 'topology').hidden, false, 'unset means the default true');
});

test('conditionHolds treats an absent value like JSON Schema', () => {
  const { SchemaForm } = loadSchemaForm();
  const condition = { properties: { kind: { const: 'a' } } };
  assert.equal(SchemaForm.conditionHolds(condition, {}), true);
  assert.equal(SchemaForm.conditionHolds(condition, { kind: 'b' }), false);
  assert.equal(SchemaForm.conditionHolds({ required: ['kind'], ...condition }, {}), false);
});

test('findUnknownKeys knows the fields of every branch', () => {
  const { SchemaForm } = loadSchemaForm();
  const found = SchemaForm.findUnknownKeys(conditionalItemSchema, { id: 'b', ac_topic: 'x', bank_b_topic: 'y', stray: 1 });
  assert.deepEqual(plain(found), ['stray']);
});

test('the battery schema form hides AC fields for a DC-only system', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const batterySchema = JSON.parse(fs.readFileSync(
    path.join(here, '..', '..', 'services', 'battery_soc', 'battery_soc_devices.schema.json'), 'utf8'));
  const root = SchemaForm.renderNode(ctx, batterySchema.items, { id: 'b', name: 'B', charger_power_topic: 'shelly/p' }, 'Batterie');
  document.body.append(root);
  assert.equal(field(root, 'charger_power_topic').hidden, false);
  assert.equal(field(root, 'bank_b_voltage_topic').hidden, true);
  choose(window, root, 'system_type', 'dc_only');
  const saved = plain(SchemaForm.readNode(root));
  assert.equal(saved.charger_power_topic, undefined);
  assert.equal(field(root, 'charger_dc_power_unit').hidden, false);
  choose(window, root, 'topology', 'series');
  assert.equal(field(root, 'bank_a_voltage_measures').hidden, false);
});

test('the imbalance warning only shows for two banks in series', () => {
  const { SchemaForm, window, document } = loadSchemaForm();
  const batterySchema = JSON.parse(fs.readFileSync(
    path.join(here, '..', '..', 'services', 'battery_soc', 'battery_soc_devices.schema.json'), 'utf8'));
  const root = SchemaForm.renderNode(ctx, batterySchema.items, { id: 'b', name: 'B' }, 'Batterie');
  document.body.append(root);
  assert.equal(field(root, 'imbalance_warn_v').hidden, true, 'default parallel');
  choose(window, root, 'topology', 'series');
  assert.equal(field(root, 'imbalance_warn_v').hidden, false);
  choose(window, root, 'bank_b_enabled', false);
  assert.equal(field(root, 'imbalance_warn_v').hidden, true, 'single bank');
});
