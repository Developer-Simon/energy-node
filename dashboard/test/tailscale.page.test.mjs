// Regression tests for tailscale.page.js: role/HTTPS-gated controls, the
// AuthURL showing up from a status poll, and the login/logout/restart
// request shapes the Tailscale endpoints expect. Run with `npm test` from
// dashboard/ (Node's built-in test runner plus jsdom - no browser or build
// step required), same approach as mqtt.page.test.mjs.
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
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'tailscale.page.js'),
  'utf8',
);

function createTailscalePanel({ fetchImpl, url, confirmResult = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: url || 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(scriptSource, context);
  const component = factories.tailscalePanel();
  const stores = attachStores(component, { modal: fakeModalStore({ answer: confirmResult }) });
  return { component, window: dom.window, stores };
}

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

test('init() loads prereqs, status and the session role/HTTPS gate', async () => {
  const { component } = createTailscalePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', system_actions: true });
      if (url === '/api/v1/tailscale/prereqs') return jsonResponse({ installed: true, version: '1.62.0', service_active: 'active', service_enabled: 'enabled' });
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: { installed: true, backend_state: 'NeedsLogin' } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.init();
  assert.equal(component.csrfToken, 'tok');
  assert.equal(component.canSystemActions, true);
  assert.equal(component.prereqs.installed, true);
  assert.equal(component.prereqs.version, '1.62.0');
  assert.equal(component.status.backend_state, 'NeedsLogin');
});

test('canSystemActions is false without the system_actions role even over HTTPS', async () => {
  const { component } = createTailscalePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', system_actions: false });
      if (url === '/api/v1/tailscale/prereqs') return jsonResponse({ installed: true });
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: {} });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.init();
  assert.equal(component.canSystemActions, false);
});

test('canSystemActions is false over plain HTTP even with the system_actions role', async () => {
  const { component } = createTailscalePanel({
    url: 'http://dashboard.local/',
    fetchImpl: async (url) => {
      if (url === '/api/v1/auth/session') return jsonResponse({ csrf_token: 'tok', system_actions: true });
      if (url === '/api/v1/tailscale/prereqs') return jsonResponse({ installed: true });
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: {} });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  await component.init();
  assert.equal(component.canSystemActions, false);
});

test('startLogin() is a no-op without canSystemActions, even if called directly', async () => {
  let calls = 0;
  const { component } = createTailscalePanel({
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.canSystemActions = false;
  await component.startLogin();
  assert.equal(calls, 0);
});

test('startLogin() POSTs confirm:true with a CSRF header, then starts polling status', async () => {
  const calls = [];
  const { component } = createTailscalePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/tailscale/login') {
        assert.equal(options.method, 'POST');
        assert.equal(options.headers['X-CSRF-Token'], 'tok');
        assert.equal(JSON.parse(options.body).confirm, true);
        return jsonResponse({ ok: true });
      }
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: { backend_state: 'NeedsLogin', auth_url: 'https://login.tailscale.com/a/xxx' } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canSystemActions = true;
  component.csrfToken = 'tok';

  await component.startLogin();

  assert.deepEqual(calls, ['/api/v1/tailscale/login', '/api/v1/tailscale/status']);
  assert.equal(component.status.auth_url, 'https://login.tailscale.com/a/xxx');
  assert.ok(component.pollTimer, 'polling should have started');
  component.stopPolling();
});

test('loadStatus() advances to step 3 and stops polling once backend_state leaves NeedsLogin and the device is online', async () => {
  const { component } = createTailscalePanel({
    fetchImpl: async (url) => {
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: { backend_state: 'Running', online: true } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.currentStep = 2;
  component.startPolling();

  await component.loadStatus();

  assert.equal(component.currentStep, 3);
  assert.equal(component.pollTimer, null);
});

test('logout() does nothing if the user declines the confirmation dialog', async () => {
  let calls = 0;
  const { component } = createTailscalePanel({
    confirmResult: false,
    fetchImpl: async () => { calls += 1; throw new Error('should not be called'); },
  });
  component.canSystemActions = true;
  await component.logout();
  assert.equal(calls, 0);
});

test('logout() POSTs confirm:true only after the user confirms', async () => {
  const calls = [];
  const { component, stores } = createTailscalePanel({
    confirmResult: true,
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/tailscale/logout') {
        assert.equal(JSON.parse(options.body).confirm, true);
        return jsonResponse({ ok: true });
      }
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: { backend_state: 'NeedsLogin' } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canSystemActions = true;
  component.csrfToken = 'tok';

  await component.logout();

  assert.deepEqual(calls, ['/api/v1/tailscale/logout', '/api/v1/tailscale/status']);
  assert.equal(stores.toasts.last(), 'Abgemeldet.');
});

test('restart() POSTs to /restart without a confirm body', async () => {
  const calls = [];
  const { component, stores } = createTailscalePanel({
    fetchImpl: async (url, options) => {
      calls.push(url);
      if (url === '/api/v1/tailscale/restart') {
        assert.equal(options.body, undefined);
        return jsonResponse({ ok: true });
      }
      if (url === '/api/v1/tailscale/status') return jsonResponse({ status: { backend_state: 'Running' } });
      throw new Error(`unexpected fetch ${url}`);
    },
  });
  component.canSystemActions = true;
  component.csrfToken = 'tok';

  await component.restart();

  assert.deepEqual(calls, ['/api/v1/tailscale/restart', '/api/v1/tailscale/status']);
  assert.equal(stores.toasts.last(), 'Dienst wurde neu gestartet.');
});

test('lastActionLabel formats success and failure records', () => {
  const { component } = createTailscalePanel();
  assert.equal(component.lastActionLabel(), '-');
  component.lastAction = { action: 'tailscale-up', ok: true, user: 'admin', at: '2026-01-01T00:00:00Z' };
  assert.match(component.lastActionLabel(), /tailscale-up erfolgreich von admin/);
  component.lastAction = { action: 'tailscale-logout', ok: false, error: 'boom' };
  assert.match(component.lastActionLabel(), /tailscale-logout fehlgeschlagen.*boom/);
});
