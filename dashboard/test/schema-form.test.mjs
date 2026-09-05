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
