// Coverage for the update-check pieces of settings.page.js:
// settingsPanel's update_check_disabled round trip (the persisted "run the
// nightly check" preference) and systemPanel's on-demand check button
// (internal/updatecheck on the Go side). Two separate Alpine components,
// same file, same reasons as settings.page.test.mjs for mocking fetch
// rather than hitting a real server.
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
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'settings.page.js'),
  'utf8',
);

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

function load({ fetchImpl } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  vm.runInContext(scriptSource, context);
  return { dom, factories };
}

test('settingsPanel payload() sendet update_check_disabled explizit', () => {
  const { dom, factories } = load();
  const component = factories.settingsPanel();
  attachStores(component);
  component.updateCheckDisabled = true;
  assert.equal(component.payload().update_check_disabled, true);
  dom.window.close();
});

test('settingsPanel load() uebernimmt update_check_disabled aus der Antwort', async () => {
  const settingsResponse = {
    health_score_threshold: 3, sweep_interval_seconds: 300,
    show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
    show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
    live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
    update_check_disabled: true,
  };
  const { dom, factories } = load({
    fetchImpl: async (url) => {
      if (String(url).includes('/api/v1/settings')) return jsonResponse(settingsResponse);
      if (String(url).includes('/api/v1/auth/session')) return jsonResponse({});
      return jsonResponse({});
    },
  });
  const component = factories.settingsPanel();
  attachStores(component);
  component.$refs = { widePanelsSelect: dom.window.document.createElement('select'), statusBarItemsSelect: dom.window.document.createElement('select') };
  await component.load();
  assert.equal(component.updateCheckDisabled, true);
  dom.window.close();
});

test('systemPanel load() liest den zwischengespeicherten Update-Status', async () => {
  const { dom, factories } = load({
    fetchImpl: async (url) => {
      const target = String(url);
      if (target.includes('/api/v1/auth/session')) return jsonResponse({ username: 'admin', system_actions: true, csrf_token: 'tok' });
      if (target.includes('/api/v1/health')) return jsonResponse({ version: '1.4.1' });
      if (target.includes('/api/v1/updates/status')) return jsonResponse({ available: true, latest: '1.4.2', notes_url: 'https://example.invalid' });
      throw new Error(`unexpected request: ${target}`);
    },
  });
  const component = factories.systemPanel();
  attachStores(component);
  await component.load();
  assert.equal(component.updateStatus.available, true);
  assert.equal(component.updateStatus.latest, '1.4.2');
  dom.window.close();
});

test('systemPanel load() behandelt "noch nie geprueft" als kein Ergebnis', async () => {
  const { dom, factories } = load({
    fetchImpl: async (url) => {
      const target = String(url);
      if (target.includes('/api/v1/auth/session')) return jsonResponse({});
      if (target.includes('/api/v1/health')) return jsonResponse({});
      if (target.includes('/api/v1/updates/status')) return jsonResponse({ checked: false });
      throw new Error(`unexpected request: ${target}`);
    },
  });
  const component = factories.systemPanel();
  attachStores(component);
  await component.load();
  assert.equal(component.updateStatus, null);
  dom.window.close();
});

test('systemPanel checkForUpdates() setzt updateStatus aus der Antwort und blockiert Doppelklicks', async () => {
  let calls = 0;
  const { dom, factories } = load({
    fetchImpl: async (url) => {
      const target = String(url);
      if (target.includes('/api/v1/updates/check')) {
        calls += 1;
        return jsonResponse({ available: false, latest: '1.4.1' });
      }
      throw new Error(`unexpected request: ${target}`);
    },
  });
  const component = factories.systemPanel();
  attachStores(component);
  component.checkingForUpdates = true;
  await component.checkForUpdates();
  assert.equal(calls, 0, 'ein laufender Check darf keinen zweiten ausloesen');

  component.checkingForUpdates = false;
  await component.checkForUpdates();
  assert.equal(calls, 1);
  assert.equal(component.updateStatus.available, false);
  assert.equal(component.checkingForUpdates, false);
  dom.window.close();
});

test('systemPanel checkForUpdates() meldet einen Fehler ueber den Toast-Store', async () => {
  const { dom, factories } = load({
    fetchImpl: async () => jsonResponse({ message: 'Prüfung auf GitHub fehlgeschlagen' }, false),
  });
  const component = factories.systemPanel();
  const stores = attachStores(component);
  await component.checkForUpdates();
  assert.equal(stores.toasts.items.length, 1);
  assert.match(stores.toasts.items[0].message, /fehlgeschlagen/);
  dom.window.close();
});
