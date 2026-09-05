// Regression tests for mqtt.page.js: role/HTTPS-gated controls, the
// password field never round-tripping a value back from the server, and the
// save/test/reconnect request shapes the MQTT settings endpoints expect.
// Run with `npm test` from dashboard/ (Node's built-in test runner plus
// jsdom - no browser or build step required), same approach as
// devicemap.page.test.mjs and config.page.test.mjs.
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
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'mqtt.page.js'),
  'utf8',
);

function createMqttPanel({ fetchImpl, url, basePath } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: url || 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  // base.html sets this from <html data-base-path> before any panel script
  // runs; leaving it undefined is the direct-access case every other test
  // here exercises.
  if (basePath !== undefined) dom.window.__DASHBOARD_BASE_PATH__ = basePath;
  vm.runInContext(scriptSource, context);
  const component = factories.mqttPanel();
  const stores = attachStores(component);
  return { component, window: dom.window, stores };
}

const configResponse = {
  enabled: true, host: 'broker.local', port: 1883, client_id: 'dashboard', username: 'user',
  tls: false, tls_insecure: false, keepalive_seconds: 30, clean_session: true,
  discovery_prefix: 'homeassistant', connect_timeout_seconds: 10,
  source: 'settings', password_configured: true,
};

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

test('load() populates the form and never surfaces a password value', async () => {
  const { component } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse(configResponse);
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true });
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.form.host, 'broker.local');
  assert.equal(component.form.port, 1883);
  assert.equal(component.source, 'settings');
  assert.equal(component.passwordConfigured, true);
  assert.equal(component.password, '');
  assert.equal(component.csrfToken, 'tok');
  assert.equal(component.canConfigure, true);
});

test('canConfigure is false without the mqtt_config role even over HTTPS', async () => {
  const { component } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse(configResponse);
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: false });
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.canConfigure, false);
});

test('canConfigure is false over plain HTTP even with the mqtt_config role', async () => {
  const { component } = createMqttPanel({
    url: 'http://dashboard.local/',
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse(configResponse);
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true });
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.canConfigure, false);
});

test('save() and test() are no-ops without canConfigure, even if called directly', async () => {
  let calls = 0;
  const { component } = createMqttPanel({
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.canConfigure = false;
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };
  await component.save(false);
  await component.test();
  assert.equal(calls, 0);
});

test('valid requires host, client_id, discovery_prefix and in-range numbers', () => {
  const { component } = createMqttPanel();
  component.form.host = '';
  assert.equal(component.valid, false);
  component.form.host = 'broker.local';
  component.form.client_id = '';
  assert.equal(component.valid, false);
  component.form.client_id = 'dashboard';
  component.form.port = 0;
  assert.equal(component.valid, false);
  component.form.port = 1883;
  assert.equal(component.valid, true);
});

test('save(false) PUTs the form with a CSRF header and does not reconnect', async () => {
  const calls = [];
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt') {
        assert.equal(options.method, 'PUT');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        return jsonResponse(JSON.parse(options.body));
      }
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.save(false);

  assert.deepEqual(calls, ['/api/v1/mqtt', '/api/v1/mqtt/status']);
  assert.equal(stores.toasts.last(), 'Gespeichert.');
  assert.deepEqual(stores.toasts.criticals, []);
});

test('save(true) also calls reconnect and reports its outcome', async () => {
  const calls = [];
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt') return jsonResponse(JSON.parse(options.body));
      if (url === '/api/v1/mqtt/reconnect') {
        assert.equal(options.method, 'POST');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        return jsonResponse({ ok: true, status: { connected: true, broker_address: 'broker.local:1883' } });
      }
      // save(true) always re-fetches status after reconnecting (to reflect
      // the canonical post-reconnect state), so this - not reconnect's own
      // echoed status - is what ends up in component.status.
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true, broker_address: 'broker.local:1883' });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.save(true);

  assert.deepEqual(calls, ['/api/v1/mqtt', '/api/v1/mqtt/reconnect', '/api/v1/mqtt/status']);
  assert.equal(stores.toasts.last(), 'Gespeichert und neu verbunden.');
  assert.equal(component.status.broker_address, 'broker.local:1883');
});

test('saveEnergyDevice() PUTs only the flag to its own endpoint, no full-config validity needed', async () => {
  const calls = [];
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/energy-device') {
        assert.equal(options.method, 'PUT');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        assert.deepEqual(JSON.parse(options.body), { publish_energy_device: false });
        return jsonResponse({ publish_energy_device: false });
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  // No host / client_id set: the standalone switch must not depend on `valid`.
  component.form = { ...component.form, host: '', client_id: '', publish_energy_device: false };

  await component.saveEnergyDevice();

  assert.deepEqual(calls, ['/api/v1/mqtt/energy-device']);
  assert.equal(stores.toasts.criticals.length, 0);
});

test('saveEnergyDevice() reverts the toggle when the request fails', async () => {
  const { component, stores } = createMqttPanel({
    fetchImpl: async () => { throw new Error('boom'); },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, publish_energy_device: true };

  await component.saveEnergyDevice();

  assert.equal(component.form.publish_energy_device, false);
  assert.equal(stores.toasts.criticals.at(-1), 'boom');
});

test('a non-empty password field is saved via /api/v1/mqtt/credentials before the PUT, then cleared', async () => {
  const calls = [];
  const { component } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/credentials') {
        assert.equal(JSON.parse(options.body).password, 's3cret');
        return jsonResponse({ password_configured: true });
      }
      if (url === '/api/v1/mqtt') return jsonResponse(JSON.parse(options.body));
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.password = 's3cret';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.save(false);

  assert.deepEqual(calls, ['/api/v1/mqtt/credentials', '/api/v1/mqtt', '/api/v1/mqtt/status']);
  assert.equal(component.password, '');
  assert.equal(component.passwordConfigured, true);
});

test('test() posts the candidate config plus the in-memory password and stores the result', async () => {
  const { component } = createMqttPanel({
    fetchImpl: async (url, options) => {
      if (url === '/api/v1/mqtt/test') {
        const body = JSON.parse(options.body);
        assert.equal(body.host, 'broker.local');
        assert.equal(body.password, 's3cret');
        return jsonResponse({ ok: false, error_code: 'connection_refused', message: 'refused' });
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.password = 's3cret';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.test();

  assert.equal(component.testResult.ok, false);
  assert.equal(component.testResult.error_code, 'connection_refused');
  // test() must not persist anything or touch the saved password.
  assert.equal(component.password, 's3cret');
});

test('sourceLabel translates the effective-config sources', () => {
  const { component } = createMqttPanel();
  assert.equal(component.sourceLabel('settings'), 'Dashboard-Einstellungen');
  assert.equal(component.sourceLabel('config'), 'Zentrale Konfigurationsdatei');
  assert.equal(component.sourceLabel(''), '-');
});

test('every request goes through the reverse-proxy base path when one is set', async () => {
  const seen = [];
  const { component } = createMqttPanel({
    basePath: '/node',
    fetchImpl: async (url) => {
      seen.push(url);
      if (url === '/node/api/v1/mqtt') return jsonResponse(configResponse);
      if (url === '/node/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true });
      if (url === '/node/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.deepEqual(
    [...seen].sort(),
    ['/node/api/v1/auth/session', '/node/api/v1/mqtt', '/node/api/v1/mqtt/status'],
  );
  assert.equal(component.form.host, 'broker.local');
});
