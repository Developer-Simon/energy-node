// Coverage for settingsPanel's update_check_disabled round trip (the
// persisted "run the nightly check" preference). Same reasons as
// settings.page.test.mjs for mocking fetch rather than hitting a real server.
// The on-demand check button moved to versions.page.js; see versions.page.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

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
  installI18n(dom.window);
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
