// The "Formatierung" card of settings.page.js: the language_switch_hidden
// round trip, and the UI language, which is this browser's cookie rather
// than a node setting and therefore only switches after a successful save.
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

const settingsResponse = {
  health_score_threshold: 3, sweep_interval_seconds: 300,
  show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
  show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
  live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
  language_switch_hidden: true,
};

function load({ saveOk = true } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const factories = {};
  const switched = [];
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.I18n = { lang: 'de', setLanguage: (lang) => { switched.push(lang); } };
  dom.window.fetch = async (url, options = {}) => {
    const target = String(url);
    if (target.includes('/api/v1/settings') && options.method === 'PUT') return jsonResponse({}, saveOk);
    if (target.includes('/api/v1/settings')) return jsonResponse(settingsResponse);
    return jsonResponse({});
  };
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const component = factories.settingsPanel();
  attachStores(component);
  component.$refs = { widePanelsSelect: dom.window.document.createElement('select'), statusBarItemsSelect: dom.window.document.createElement('select') };
  return { dom, component, switched };
}

test('load() takes language_switch_hidden and the current UI language', async () => {
  const { dom, component } = load();
  await component.load();
  assert.equal(component.languageSwitchHidden, true);
  assert.equal(component.uiLanguage, 'de');
  assert.equal(component.payload().language_switch_hidden, true);
  dom.window.close();
});

test('save() switches the language only after the settings were stored', async () => {
  const { dom, component, switched } = load();
  await component.load();
  component.uiLanguage = 'en';
  let visible;
  dom.window.document.addEventListener('language-switch-setting-changed', (event) => { visible = event.detail.visible; });
  await component.save();
  assert.deepEqual(switched, ['en']);
  assert.equal(visible, false);
  dom.window.close();
});

test('an unchanged language does not reload', async () => {
  const { dom, component, switched } = load();
  await component.load();
  await component.save();
  assert.deepEqual(switched, []);
  dom.window.close();
});

test('a failed save keeps the page and its language', async () => {
  const { dom, component, switched } = load({ saveOk: false });
  await component.load();
  component.uiLanguage = 'en';
  await component.save();
  assert.deepEqual(switched, []);
  dom.window.close();
});
