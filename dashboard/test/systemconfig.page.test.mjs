// Tests for systemconfig.page.js: it now renders /etc/energy-node/config.json
// as the shared schema-form (schema-form.js) and only drops to a raw textarea
// when the file drifts from the schema. Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const staticJS = (name) => fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', name),
  'utf8',
);
const schemaFormSource = staticJS('schema-form.js');
const scriptSource = staticJS('systemconfig.page.js');

const schema = {
  type: 'object',
  additionalProperties: false,
  required: ['schema_version', 'mqtt', 'paths'],
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
      properties: { data_dir: { type: 'string', minLength: 1, title: 'Daten' } },
    },
  },
};

const configValue = {
  schema_version: 1,
  mqtt: { host: 'localhost', port: 1883 },
  paths: { data_dir: '/var/lib/werkstatt' },
};

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

function createPanel({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error(`unexpected fetch`); });
  vm.runInContext(schemaFormSource, context);
  vm.runInContext(scriptSource, context);

  const component = factories.systemConfigPanel();
  const form = dom.window.document.createElement('form');
  dom.window.document.body.append(form);
  component.$refs = { form };
  component.$nextTick = (fn) => { fn(); };
  return { component, window: dom.window, document: dom.window.document, form };
}

const defaultFetch = (overrides = {}) => async (url) => {
  if (url.endsWith('/api/v1/system/config/schema')) return jsonResponse(overrides.schema ?? schema);
  if (url.endsWith('/api/v1/system/config')) return jsonResponse({ config: overrides.config ?? configValue });
  if (url.endsWith('/api/v1/auth/session')) return jsonResponse({ csrf_token: 'tok-1' });
  throw new Error(`unexpected fetch ${url}`);
};

test('load() builds the schema form from the file and schema', async () => {
  const { component, form } = createPanel({ fetchImpl: defaultFetch() });
  await component.load();

  assert.equal(component.recoverMode, false);
  assert.equal(component.error, '');
  assert.equal(form.querySelector('[data-schema-key="host"] .schema-control').value, 'localhost');
  assert.equal(form.querySelector('[data-schema-key="port"] .schema-control').value, '1883');
  // schema_version is pinned (min === max) and must render locked.
  assert.equal(form.querySelector('[data-schema-key="schema_version"] .schema-control').readOnly, true);
});

test('save() reads the form, sends the CSRF token, and applies the response', async () => {
  let putBody = null;
  let putHeaders = null;
  const fetchImpl = async (url, options) => {
    if (url.endsWith('/api/v1/system/config') && options && options.method === 'PUT') {
      putBody = JSON.parse(options.body);
      putHeaders = options.headers;
      return jsonResponse({ config: putBody, restart_required: ['mqtt'], reloaded: {} });
    }
    return defaultFetch()(url, options);
  };
  const { component, form } = createPanel({ fetchImpl });
  await component.load();

  const host = form.querySelector('[data-schema-key="host"] .schema-control');
  host.value = 'broker.local';
  host.dispatchEvent(new (form.ownerDocument.defaultView.Event)('input', { bubbles: true }));

  await component.save();

  assert.equal(putHeaders['X-CSRF-Token'], 'tok-1');
  assert.equal(putBody.mqtt.host, 'broker.local');
  assert.deepEqual(JSON.parse(JSON.stringify(component.restartRequired)), ['mqtt']);
  assert.equal(component.value.mqtt.host, 'broker.local');
  assert.equal(component.error, '');
});

test('dirty flips once a field is edited', async () => {
  const { component, form } = createPanel({ fetchImpl: defaultFetch() });
  await component.load();
  assert.equal(component.dirty, false);

  const host = form.querySelector('[data-schema-key="host"] .schema-control');
  host.value = 'other.host';
  component.dirtyTick += 1; // the template binds x-on:input="dirtyTick++"
  assert.equal(component.dirty, true);
});

test('a file with keys the schema does not know drops to recover mode', async () => {
  const drifted = JSON.parse(JSON.stringify(configValue));
  drifted.mqtt.legacy_ca = '/etc/ca.pem';
  const { component, form } = createPanel({ fetchImpl: defaultFetch({ config: drifted }) });
  await component.load();

  assert.equal(component.recoverMode, true);
  assert.deepEqual(JSON.parse(JSON.stringify(component.unknownKeys)), ['mqtt.legacy_ca']);
  assert.equal(form.querySelector('.schema-node'), null);
  assert.equal(component.text.includes('legacy_ca'), true);
});

test('an unreachable schema endpoint drops to recover mode without an error banner', async () => {
  const fetchImpl = async (url, options) => {
    if (url.endsWith('/api/v1/system/config/schema')) return jsonResponse({ message: 'nope' }, false);
    return defaultFetch()(url, options);
  };
  const { component } = createPanel({ fetchImpl });
  await component.load();

  assert.equal(component.recoverMode, true);
  assert.equal(component.schema, null);
  assert.equal(component.error, '');
});
