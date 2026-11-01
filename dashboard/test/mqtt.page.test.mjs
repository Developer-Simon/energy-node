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
import { installI18n, catalog } from './helpers/i18n.mjs';

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
  // Initialize i18n with German catalog so t() calls return translated strings
  installI18n(dom.window);
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

test('save(true) shows the broker error of a failed reconnect (HTTP 502) instead of a generic failure', async () => {
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse({});
      if (url === '/api/v1/mqtt/reconnect') {
        return jsonResponse({ ok: false, error: 'mqttclient: reconfigure failed: not Authorized', status: { connected: false, last_error: 'not Authorized' } }, false);
      }
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: false, last_error: 'not Authorized' });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.save(true);

  assert.equal(stores.toasts.criticals.length, 1);
  assert.match(stores.toasts.criticals[0], /not Authorized/);
  assert.equal(component.status.last_error, 'not Authorized');
});

test('save(true) names the switch in the failure toast while the dashboard settings are off', async () => {
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse({});
      if (url === '/api/v1/mqtt/reconnect') return jsonResponse({ ok: false, error: 'not Authorized' }, false);
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: false });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, enabled: false, host: 'broker.local', client_id: 'dashboard' };

  await component.save(true);

  assert.match(stores.toasts.criticals[0], /not Authorized/);
  assert.match(stores.toasts.criticals[0], /Schalter/);

  component.form.enabled = true;
  await component.save(true);
  assert.equal(stores.toasts.criticals.length, 2);
  assert.doesNotMatch(stores.toasts.criticals.at(-1), /Schalter/);
});

test('saveEnergyDevice() PUTs only the flag to its own endpoint, no full-config validity needed', async () => {
  const calls = [];
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/energy-device') {
        assert.equal(options.method, 'PUT');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        assert.deepEqual(JSON.parse(options.body), { publish_energy_device: false, energy_sensors: {} });
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

test('test() posts the candidate config plus the in-memory password and shows a failed result as a critical toast', async () => {
  const { component, stores } = createMqttPanel({
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

  assert.equal(stores.toasts.criticals.length, 1);
  assert.equal(stores.toasts.criticals[0], catalog('de')['mqtt.test.error.connection_refused']);
  // test() must not persist anything or touch the saved password.
  assert.equal(component.password, 's3cret');
});

test('test() shows a successful result as a normal toast', async () => {
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt/test') return jsonResponse({ ok: true, duration_ms: 42, discovery_messages_seen: 3 });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.form = { ...component.form, host: 'broker.local', client_id: 'dashboard' };

  await component.test();

  assert.equal(stores.toasts.last(), 'Verbindung erfolgreich (42 ms, 3 Discovery-Nachrichten gesehen)');
  assert.deepEqual(stores.toasts.criticals, []);
});

test('sourceLabel translates the effective-config sources', () => {
  const { component } = createMqttPanel();
  assert.equal(component.sourceLabel('settings'), 'Dashboard-Einstellungen');
  assert.equal(component.sourceLabel('config'), 'Zentrale Konfigurationsdatei');
  assert.equal(component.sourceLabel(''), '-');
});

// Die Stepper im Verbindungs-Formular schreiben form.port/keepalive_seconds/
// connect_timeout_seconds ueber stepField() mit fester Schrittweite und
// hartem Clamp an den Grenzen, die auch valid() prueft.
test('stepField erhoeht den Port um 1 und clampt bei 65535', () => {
  const { component } = createMqttPanel();
  component.form.port = 1883;
  component.stepField('port', 1);
  assert.equal(component.form.port, 1884);
  component.form.port = 65535;
  component.stepField('port', 1);
  assert.equal(component.form.port, 65535);
});

test('stepField clampt Keepalive und Verbindungs-Timeout nach unten an ihre Feldgrenze', () => {
  const { component } = createMqttPanel();
  component.form.keepalive_seconds = 5;
  component.stepField('keepalive_seconds', -1);
  assert.equal(component.form.keepalive_seconds, 5);
  component.form.connect_timeout_seconds = 1;
  component.stepField('connect_timeout_seconds', -1);
  assert.equal(component.form.connect_timeout_seconds, 1);
});

test('stepField ignoriert ein unbekanntes Feld, statt NaN in form zu schreiben', () => {
  const { component } = createMqttPanel();
  component.stepField('nichtVorhanden', 1);
  assert.equal(component.form.nichtVorhanden, undefined);
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

test('testErrorText() formats error with detail using error.with_detail', () => {
  const { component, window } = createMqttPanel();
  const catalogDe = catalog('de');
  const result = component.testErrorText({ok: false, error_code: 'auth_failed', message: 'x', detail: 'not authorized'});
  const expectedMessage = catalogDe['mqtt.test.error.auth_failed'];
  const expectedFormat = catalogDe['error.with_detail'];
  const expected = expectedFormat.replace('{message}', expectedMessage).replace('{detail}', 'not authorized');
  assert.equal(result, expected);
});

test('testErrorText() formats error without detail', () => {
  const { component, window } = createMqttPanel();
  const catalogDe = catalog('de');
  const result = component.testErrorText({ok: false, error_code: 'timeout', message: 'Verbindungsaufbau hat das Zeitlimit überschritten'});
  const expected = catalogDe['mqtt.test.error.timeout'];
  assert.equal(result, expected);
});

test('testErrorText() falls back to error code and message for unknown error codes', () => {
  const { component, window } = createMqttPanel();
  const result = component.testErrorText({ok: false, error_code: 'weird', message: 'boom'});
  assert.equal(result, 'weird: boom');
});

test('testErrorText() returns empty string without a result', () => {
  const { component, window } = createMqttPanel();
  const result = component.testErrorText(null);
  assert.equal(result, '');
});

test('testErrorText() returns empty string when the result is ok', () => {
  const { component, window } = createMqttPanel();
  const result = component.testErrorText({ok: true});
  assert.equal(result, '');
});

test('selectedMetrics() treats missing keys as enabled', () => {
  const { component } = createMqttPanel();
  component.form.metrics = { ram: false, tailscale: true };
  const selected = component.selectedMetrics();
  assert.equal(selected.includes('ram'), false);
  assert.equal(selected.includes('tailscale'), true);
  assert.equal(selected.includes('cpu_temp'), true);
  assert.deepEqual(selected, component.metricKeys.filter((k) => k !== 'ram'));
});

test('metricsFromSelection() writes every known key explicitly and keeps unknown stored keys', () => {
  const { component } = createMqttPanel();
  component.form.metrics = { future_metric: false };
  const map = component.metricsFromSelection(['cpu_temp', 'ram']);
  assert.equal(map.cpu_temp, true);
  assert.equal(map.ram, true);
  assert.equal(map.disk, false);
  assert.equal(map.future_metric, false);
  for (const key of component.metricKeys) assert.equal(typeof map[key], 'boolean');
});

test('onMetricsChange() posts the full map to node-settings', async () => {
  const calls = [];
  const { component } = createMqttPanel({
    fetchImpl: async (url, options) => { calls.push({ url, options }); return jsonResponse({}); },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.metricsChoices = { getValue: () => ['cpu_temp'] };
  await component.onMetricsChange();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/mqtt/node-settings');
  const body = JSON.parse(calls[0].options.body);
  assert.equal(body.metrics.cpu_temp, true);
  assert.equal(body.metrics.ram, false);
  assert.equal(component.form.metrics.ram, false);
});

test('onMetricsChange() does nothing without canConfigure (guest view)', async () => {
  const { component } = createMqttPanel();
  component.canConfigure = false;
  component.metricsChoices = { getValue: () => [] };
  await component.onMetricsChange();
  assert.equal(Object.keys(component.form.metrics).length, 0);
});

test('onMetricsChange() rolls the selection back when saving fails', async () => {
  const { component } = createMqttPanel({
    fetchImpl: async () => jsonResponse({ message: 'kaputt' }, false),
  });
  component.canConfigure = true;
  component.form.metrics = { ram: false };
  const restored = [];
  component.metricsChoices = {
    getValue: () => ['ram'],
    removeActiveItems: () => {},
    setChoiceByValue: (values) => restored.push(...values),
  };
  await component.onMetricsChange();
  assert.deepEqual(component.form.metrics, { ram: false });
  assert.equal(restored.includes('ram'), false);
  assert.equal(restored.includes('cpu_temp'), true);
});

test('initMetricsChoices() disables the select for guests', () => {
  const { component, window } = createMqttPanel();
  let disabled = false;
  window.Choices = class { constructor() {} disable() { disabled = true; } removeActiveItems() {} setChoiceByValue() {} };
  component.$refs = { metricsSelect: window.document.createElement('select') };
  component.canConfigure = false;
  component.initMetricsChoices();
  assert.equal(disabled, true);
});

const energySensorOptions = [
  {key: 'wallbox', role: 'wallbox'},
  {key: 'custom_sauna', label: 'Sauna'},
];

test('selectedEnergySensors() treats missing keys as on, like the node config', () => {
  const { component } = createMqttPanel();
  component.energySensorOptions = energySensorOptions;
  component.form.energy_sensors = { wallbox: false };
  assert.deepEqual(component.selectedEnergySensors(), ['custom_sauna']);
});

test('energySensorLabel() uses the role label, or the custom label for categories', () => {
  const { component } = createMqttPanel();
  assert.equal(component.energySensorLabel(energySensorOptions[0]), 'Wallbox');
  assert.equal(component.energySensorLabel(energySensorOptions[1]), 'Sauna');
});

test('onEnergySensorsChange() PUTs the map with the publish flag to energy-device', async () => {
  const calls = [];
  const { component, stores } = createMqttPanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/mqtt/energy-device') {
        assert.equal(options.method, 'PUT');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        assert.deepEqual(JSON.parse(options.body), {
          publish_energy_device: true,
          energy_sensors: { wallbox: true, custom_sauna: false },
        });
        return jsonResponse({ publish_energy_device: true, energy_sensors: { wallbox: true, custom_sauna: false } });
      }
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.energySensorOptions = energySensorOptions;
  component.form.energy_sensors = { wallbox: false };
  component.energySensorsChoices = { getValue: () => ['wallbox'] };

  await component.onEnergySensorsChange();

  assert.deepEqual(calls, ['/api/v1/mqtt/energy-device']);
  assert.deepEqual(JSON.parse(JSON.stringify(component.form.energy_sensors)), { wallbox: true, custom_sauna: false });
  assert.equal(stores.toasts.last(), 'Gespeichert.');
});

test('onEnergySensorsChange() restores the previous map when the PUT fails and keeps the publish flag', async () => {
  const { component, stores } = createMqttPanel({
    fetchImpl: async () => { throw new Error('boom'); },
  });
  component.canConfigure = true;
  component.csrfToken = 'tok';
  component.energySensorOptions = energySensorOptions;
  component.form = { ...component.form, publish_energy_device: true, energy_sensors: { wallbox: false } };
  const restored = [];
  component.energySensorsChoices = {
    getValue: () => ['wallbox'],
    removeActiveItems: () => {},
    setChoiceByValue: (values) => restored.push(...values),
  };

  await component.onEnergySensorsChange();

  assert.deepEqual(JSON.parse(JSON.stringify(component.form.energy_sensors)), { wallbox: false });
  assert.equal(component.form.publish_energy_device, true);
  assert.deepEqual(restored, ['custom_sauna']);
  assert.equal(stores.toasts.criticals.at(-1), 'boom');
});

test('initEnergySensorChoices() disables the select for guests', () => {
  const { component, window } = createMqttPanel();
  let disabled = false;
  window.Choices = class { constructor() {} disable() { disabled = true; } removeActiveItems() {} setChoiceByValue() {} };
  component.$refs = { energySensorsSelect: window.document.createElement('select') };
  component.canConfigure = false;
  component.initEnergySensorChoices();
  assert.equal(disabled, true);
});

// Fake Choices: records the select it was built on, the values it was told
// to pre-select and whether destroy() ran.
function fakeChoicesClass(instances) {
  return class {
    constructor(element) { this.element = element; this.selected = []; this.destroyed = false; instances.push(this); }
    removeActiveItems() { this.selected = []; }
    setChoiceByValue(values) { this.selected.push(...values); }
    getValue() { return []; }
    disable() { this.disabled = true; }
    destroy() { this.destroyed = true; }
  };
}

test('energySensorsFromSelection() keeps stale custom keys that are no longer offered', () => {
  const { component } = createMqttPanel();
  component.energySensorOptions = energySensorOptions;
  component.form.energy_sensors = { custom_old: false };
  const map = component.energySensorsFromSelection(['wallbox']);
  assert.deepEqual(JSON.parse(JSON.stringify(map)), { custom_old: false, wallbox: true, custom_sauna: false });
});

test('initEnergySensorChoices() builds Choices on the select and pre-selects the enabled sensors', () => {
  const { component, window } = createMqttPanel();
  const instances = [];
  window.Choices = fakeChoicesClass(instances);
  const select = window.document.createElement('select');
  component.$refs = { energySensorsSelect: select };
  component.canConfigure = true;
  component.energySensorOptions = energySensorOptions;
  component.form.energy_sensors = { wallbox: false };
  component.initEnergySensorChoices();
  assert.equal(instances.length, 1);
  assert.equal(instances[0].element, select);
  assert.deepEqual([...instances[0].selected], ['custom_sauna']);
});

test('energySensorLabel() names heat pumps by role and falls back to the ID of a label-less custom option', () => {
  const { component } = createMqttPanel();
  const catalogDe = catalog('de');
  assert.equal(component.energySensorLabel({key: 'heat_pump', role: 'heat_pump'}), catalogDe['energy.role_label.heat_pump']);
  assert.equal(component.energySensorLabel({key: 'custom_garage'}), 'garage');
  assert.equal(component.energySensorLabel({key: 'custom_sauna', label: 'Sauna'}), 'Sauna');
});

test('a repeated load() with new energy sensor options destroys and rebuilds Choices', async () => {
  const instances = [];
  let options = [{key: 'wallbox', role: 'wallbox'}];
  const { component, window } = createMqttPanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/mqtt') return jsonResponse({ ...configResponse, energy_sensors: {}, energy_sensor_options: options });
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', mqtt_config: true });
      if (url === '/api/v1/mqtt/status') return jsonResponse({ connected: true });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  window.Choices = fakeChoicesClass(instances);
  component.$refs = { energySensorsSelect: window.document.createElement('select') };
  component.$nextTick = (fn) => fn();

  await component.load();
  assert.equal(instances.length, 1);
  assert.deepEqual([...instances[0].selected], ['wallbox']);

  options = [{key: 'wallbox', role: 'wallbox'}, {key: 'custom_sauna', label: 'Sauna'}];
  await component.load();
  assert.equal(instances.length, 2);
  assert.equal(instances[0].destroyed, true);
  assert.equal(instances[1].destroyed, false);
  assert.deepEqual([...instances[1].selected], ['wallbox', 'custom_sauna']);
});
