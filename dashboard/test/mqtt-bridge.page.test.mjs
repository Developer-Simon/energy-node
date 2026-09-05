// Regression tests for the mqttBridgePanel() Alpine component in
// mqtt.page.js: role/HTTPS-gated controls, the password field never
// round-tripping a value back from the server, topic table
// add/remove/validation, and the save/apply/restart request shapes the
// bridge endpoints expect. Same approach as mqtt.page.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores, fakeModalStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'mqtt.page.js'),
  'utf8',
);

function createBridgePanel({ fetchImpl, url, confirmResult = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: url || 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(scriptSource, context);
  const component = factories.mqttBridgePanel();
  const stores = attachStores(component, { modal: fakeModalStore({ answer: confirmResult }) });
  return { component, window: dom.window, stores };
}

const bridgeConfigResponse = {
  configured: true, enabled: true, name: 'aussenstandort-zu-hauptsystem', address: '100.64.1.2',
  port: 1883, remote_client_id: 'pi-bridge', remote_username: 'ha',
  topics: [{ pattern: 'outstation/#', direction: 'both', qos: 0 }],
  try_private: true, start_type_auto: true, restart_timeout: 30, keepalive_seconds: 60,
  cleansession: false, password_configured: true, preview: 'connection aussenstandort-zu-hauptsystem\n',
};

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

test('load() populates the form and never surfaces a password value', async () => {
  const { component } = createBridgePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true, system_actions: true });
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({ service_state: 'active', bridge: { configured: true, connected: true }, drift: { known: true, matches: true } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.form.address, '100.64.1.2');
  assert.equal(component.form.topics.length, 1);
  assert.equal(component.passwordConfigured, true);
  assert.equal(component.password, '');
  assert.equal(component.csrfToken, 'tok');
  assert.equal(component.canConfigure, true);
  assert.equal(component.canApply, true);
  assert.equal(component.preview, bridgeConfigResponse.preview);
});

test('canConfigure/canApply are false without the required roles even over HTTPS', async () => {
  const { component } = createBridgePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: false, system_actions: false });
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.canConfigure, false);
  assert.equal(component.canApply, false);
  assert.equal(component.canRestart, false);
});

test('canApply requires system_actions even when mqtt_config is present', async () => {
  const { component } = createBridgePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true, system_actions: false });
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.canConfigure, true);
  assert.equal(component.canApply, false);
});

test('canConfigure is false over plain HTTP even with both roles', async () => {
  const { component } = createBridgePanel({
    url: 'http://dashboard.local/',
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true, system_actions: true });
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.load();
  assert.equal(component.canConfigure, false);
  assert.equal(component.canApply, false);
});

test('addTopic/removeTopic manage the topics array up to the 32-entry cap', () => {
  const { component } = createBridgePanel();
  assert.equal(component.form.topics.length, 0);
  component.addTopic();
  assert.equal(component.form.topics.length, 1);
  assert.equal(component.form.topics[0].direction, 'out');
  component.removeTopic(0);
  assert.equal(component.form.topics.length, 0);

  for (let i = 0; i < 40; i += 1) component.addTopic();
  assert.equal(component.form.topics.length, 32);
});

test('valid requires name, address, remote_client_id and at least one well-formed topic', () => {
  const { component } = createBridgePanel();
  assert.equal(component.valid, false);
  component.form.name = 'n';
  component.form.address = '100.64.1.2';
  component.form.remote_client_id = 'id';
  assert.equal(component.valid, false, 'still invalid without any topic');
  component.addTopic();
  assert.equal(component.valid, false, 'still invalid with an empty pattern');
  component.form.topics[0].pattern = 'outstation/#';
  assert.equal(component.valid, true);
  component.form.topics[0].qos = 5;
  assert.equal(component.valid, false);
});

test('save() PUTs the form with a CSRF header and never sends a password field the server did not ask for', async () => {
  const calls = [];
  const { component, stores } = createBridgePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/bridge') {
        assert.equal(options.method, 'PUT');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        const body = JSON.parse(options.body);
        assert.equal(body.password, undefined);
        return jsonResponse({ ...bridgeConfigResponse, configured: true });
      }
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, name: 'n', address: '100.64.1.2', remote_client_id: 'id', topics: [{ pattern: 'a/#', direction: 'out', qos: 0 }] };

  await component.save();

  assert.deepEqual(calls, ['/api/v1/mqtt/bridge', '/api/v1/mqtt/bridge/status']);
  assert.equal(stores.toasts.last(), 'Gespeichert.');
  assert.deepEqual(stores.toasts.criticals, []);
});

test('a non-empty password field is saved via /api/v1/mqtt/bridge/credentials before the PUT, then cleared', async () => {
  const calls = [];
  const { component } = createBridgePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/bridge/credentials') {
        assert.equal(JSON.parse(options.body).password, 'remote-pw');
        return jsonResponse({ password_configured: true });
      }
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.password = 'remote-pw';
  component.form = { ...component.form, name: 'n', address: '100.64.1.2', remote_client_id: 'id', topics: [{ pattern: 'a/#', direction: 'out', qos: 0 }] };

  await component.save();

  assert.deepEqual(calls, ['/api/v1/mqtt/bridge/credentials', '/api/v1/mqtt/bridge', '/api/v1/mqtt/bridge/status']);
  assert.equal(component.password, '');
});

test('apply() saves first, then POSTs confirm:true to /apply, only after the user confirms', async () => {
  const calls = [];
  const { component, stores } = createBridgePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/bridge') return jsonResponse(bridgeConfigResponse);
      if (url === '/api/v1/mqtt/bridge/apply') {
        assert.equal(options.method, 'POST');
        assert.equal(JSON.parse(options.body).confirm, true);
        return jsonResponse({ ok: true });
      }
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canApply = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, name: 'n', address: '100.64.1.2', remote_client_id: 'id', topics: [{ pattern: 'a/#', direction: 'out', qos: 0 }] };

  await component.apply();

  assert.deepEqual(calls, ['/api/v1/mqtt/bridge', '/api/v1/mqtt/bridge/apply', '/api/v1/mqtt/bridge/status']);
  assert.equal(stores.toasts.last(), 'Bridge angewendet, Mosquitto wurde neu gestartet.');
});

test('apply() does nothing if the user declines the confirmation dialog', async () => {
  let calls = 0;
  const { component } = createBridgePanel({
    confirmResult: false,
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.canApply = true;
  component.form = { ...component.form, name: 'n', address: '100.64.1.2', remote_client_id: 'id', topics: [{ pattern: 'a/#', direction: 'out', qos: 0 }] };

  await component.apply();

  assert.equal(calls, 0);
});

test('restartOnly() POSTs to /restart only after confirmation and without touching bridge.json', async () => {
  const calls = [];
  const { component, stores } = createBridgePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/bridge/restart') {
        assert.equal(options.method, 'POST');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        return jsonResponse({ ok: true });
      }
      if (url === '/api/v1/mqtt/bridge/status') return jsonResponse({});
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canRestart = true;
  component.csrfToken = 'tok';

  await component.restartOnly();

  assert.deepEqual(calls, ['/api/v1/mqtt/bridge/restart', '/api/v1/mqtt/bridge/status']);
  assert.equal(stores.toasts.last(), 'Mosquitto wurde neu gestartet.');
});

test('save()/apply()/restartOnly() are no-ops without the right flag, even if called directly', async () => {
  let calls = 0;
  const { component } = createBridgePanel({
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.canConfigure = false;
  component.canApply = false;
  component.canRestart = false;
  component.form = { ...component.form, name: 'n', address: '100.64.1.2', remote_client_id: 'id', topics: [{ pattern: 'a/#', direction: 'out', qos: 0 }] };

  await component.save();
  await component.apply();
  await component.restartOnly();

  assert.equal(calls, 0);
});

test('driftLabel/bridgeConnectionLabel summarise the status endpoint response', () => {
  const { component } = createBridgePanel();
  component.status = null;
  assert.equal(component.bridgeConnectionLabel(), 'nicht konfiguriert');
  assert.equal(component.driftLabel(), 'unbekannt');

  component.status = { bridge: { configured: true, connected: false }, drift: { known: true, matches: false } };
  assert.equal(component.bridgeConnectionLabel(), 'getrennt');
  assert.equal(component.driftLabel(), 'nein, abweichend');

  component.status = { bridge: { configured: true, connected: true }, drift: { known: true, matches: true } };
  assert.equal(component.bridgeConnectionLabel(), 'verbunden');
  assert.equal(component.driftLabel(), 'ja');
});
