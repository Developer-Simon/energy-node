// The "Kopfzeile" card of settings.page.js: title and subtitle with their
// visibility switches round trip through load()/payload(), and a save shows
// the new masthead without a reload, falling back to the defaults when empty.
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

const settingsResponse = {
  health_score_threshold: 180, sweep_interval_seconds: 300,
  show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
  show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
  live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
  dashboard_title: 'Werkstatt', dashboard_subtitle: '',
  dashboard_title_hidden: false, dashboard_subtitle_hidden: true,
};

const masthead = '<header class="masthead"><h1 data-masthead-title>Werkstatt</h1><small data-masthead-subtitle hidden>x</small></header>';

function load() {
  const dom = new JSDOM(`<!doctype html><html><head><title>Werkstatt</title></head><body>${masthead}</body></html>`, {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const factories = {};
  const saved = [];
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  installI18n(dom.window);
  dom.window.fetch = async (url, options = {}) => {
    const target = String(url);
    if (target.includes('/api/v1/settings') && options.method === 'PUT') {
      saved.push(JSON.parse(options.body));
      return jsonResponse({});
    }
    if (target.includes('/api/v1/settings')) return jsonResponse(settingsResponse);
    return jsonResponse({});
  };
  vm.runInContext(scriptSource, dom.getInternalVMContext());
  const component = factories.settingsPanel();
  attachStores(component);
  component.$refs = { widePanelsSelect: dom.window.document.createElement('select'), statusBarItemsSelect: dom.window.document.createElement('select') };
  return { dom, component, saved };
}

test('load() takes the masthead settings and payload() sends them trimmed', async () => {
  const { dom, component } = load();
  await component.load();
  assert.equal(component.dashboardTitle, 'Werkstatt');
  assert.equal(component.dashboardSubtitleHidden, true);
  component.dashboardSubtitle = '  Halle 2  ';
  const payload = component.payload();
  assert.equal(payload.dashboard_title, 'Werkstatt');
  assert.equal(payload.dashboard_subtitle, 'Halle 2');
  assert.equal(payload.dashboard_title_hidden, false);
  assert.equal(payload.dashboard_subtitle_hidden, true);
  dom.window.close();
});

test('save() updates the masthead and the tab title without a reload', async () => {
  const { dom, component, saved } = load();
  await component.load();
  component.dashboardTitle = 'Energiezentrale';
  component.dashboardSubtitle = 'Halle 2';
  component.dashboardTitleHidden = true;
  component.dashboardSubtitleHidden = false;
  await component.save();
  const doc = dom.window.document;
  assert.equal(saved.length, 1);
  assert.equal(doc.title, 'Energiezentrale');
  assert.equal(doc.querySelector('[data-masthead-title]').textContent, 'Energiezentrale');
  assert.equal(doc.querySelector('[data-masthead-title]').hidden, true);
  assert.equal(doc.querySelector('[data-masthead-subtitle]').textContent, 'Halle 2');
  assert.equal(doc.querySelector('[data-masthead-subtitle]').hidden, false);
  dom.window.close();
});

test('empty fields fall back to the default title and the catalog subtitle', async () => {
  const { dom, component } = load();
  await component.load();
  component.dashboardTitle = '   ';
  component.dashboardSubtitle = '';
  component.dashboardSubtitleHidden = false;
  await component.save();
  const doc = dom.window.document;
  assert.equal(doc.title, 'Energy Node Dashboard');
  assert.equal(doc.querySelector('[data-masthead-title]').textContent, 'Energy Node Dashboard');
  assert.equal(doc.querySelector('[data-masthead-subtitle]').textContent, dom.window.I18n.t('masthead.subtitle'));
  assert.notEqual(doc.querySelector('[data-masthead-subtitle]').textContent, 'masthead.subtitle');
  dom.window.close();
});
