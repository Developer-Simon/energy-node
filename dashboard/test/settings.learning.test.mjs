// Die Karte "Lernfunktionen" in den allgemeinen Einstellungen. Geprueft werden
// Laden und Speichern der beiden Schalter, das Ereignis nach dem Speichern und
// die Markup-Regeln: der Automations-Schalter ist nur angezeigt, die beiden
// Messketten-Schalter sind an die Rechte gebunden.
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
const read = (...parts) => fs.readFileSync(path.join(here, '..', ...parts), 'utf8');
const settingsSource = read('internal', 'webui', 'static', 'js', 'settings.page.js');
const templateSource = read('internal', 'webui', 'templates', 'settings.html');

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

const settingsResponse = (overrides = {}) => ({
  health_score_threshold: 180, sweep_interval_seconds: 300,
  show_runtime_status: true, device_view_mode: 'compact', theme: 'mint',
  show_config_entities_on_tile: false, show_diagnostic_entities_on_tile: false,
  live_update_interval_seconds: 3, wide_panels: [], status_bar_items: [],
  number_format: 'auto', number_grouping: 'match',
  ...overrides,
});

function createPanel(fetchImpl) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl;
  installI18n(dom.window);
  vm.runInContext(settingsSource, context);
  const component = factories.settingsPanel();
  attachStores(component);
  component.$refs = {
    widePanelsSelect: dom.window.document.createElement('select'),
    statusBarItemsSelect: dom.window.document.createElement('select'),
  };
  return { dom, component };
}

const loadFetch = (overrides) => async (url) => {
  if (url === '/api/v1/settings') return jsonResponse(settingsResponse(overrides));
  if (url === '/api/v1/auth/session') return jsonResponse({});
  if (url === '/api/v1/health/storage') return jsonResponse({ available: false });
  return jsonResponse({});
};

test('load() liest beide Messketten-Schalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ chain_suggestions_disabled: true, chain_learning_disabled: true }));
  await component.load();
  assert.equal(component.chainSuggestionsDisabled, true);
  assert.equal(component.chainLearningDisabled, true);
  dom.window.close();
});

test('load() setzt fehlende Messketten-Schalter auf false', async () => {
  const { dom, component } = createPanel(loadFetch({}));
  await component.load();
  assert.equal(component.chainSuggestionsDisabled, false);
  assert.equal(component.chainLearningDisabled, false);
  dom.window.close();
});

test('payload() schickt beide Schalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ chain_learning_disabled: true }));
  await component.load();
  const payload = JSON.parse(JSON.stringify(component.payload()));
  assert.equal(payload.chain_suggestions_disabled, false);
  assert.equal(payload.chain_learning_disabled, true);
  dom.window.close();
});

test('save() meldet das Ende der Messketten-Aenderung auf window', async () => {
  const { dom, component } = createPanel(async () => jsonResponse({}));
  let fired = 0;
  dom.window.addEventListener('energy-chain-suggestions-changed', () => { fired += 1; });
  component.reloadPage = () => {};
  await component.save();
  assert.equal(fired, 1);
  dom.window.close();
});

test('Der Automations-Schalter ist nur angezeigt und hat keine Bindung', () => {
  const doc = new JSDOM(templateSource).window.document;
  const input = doc.getElementById('learning-automation-suggestions');
  assert.ok(input, 'Automations-Schalter fehlt');
  assert.equal(input.hasAttribute('disabled'), true);
  assert.equal(input.hasAttribute('x-model'), false);
  assert.equal(input.hasAttribute('x-on:change'), false);
  assert.equal(input.hasAttribute('x-bind:checked'), false);
  assert.match(templateSource, /settings\.learning\.automation_suggestions\.planned/);
});

test('Beide Messketten-Schalter sind an canEditSetup gebunden', () => {
  const doc = new JSDOM(templateSource).window.document;
  for (const id of ['learning-chain-suggestions', 'learning-chain-learning']) {
    const input = doc.getElementById(id);
    assert.ok(input, `${id} fehlt`);
    assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
  }
});
