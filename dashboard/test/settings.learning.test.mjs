// Die Karte "Messketten" auf der Seite Lernfunktionen. Geprueft werden Laden
// und Speichern der beiden Schalter, das Ereignis nach dem Speichern und die
// Markup-Regeln: die Messketten-Schalter sind an die Rechte gebunden.
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
  vm.runInContext(read('internal', 'webui', 'static', 'js', 'settings-field.js'), context);
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

test('Beide Messketten-Schalter sind an canEditSetup gebunden', () => {
  const doc = new JSDOM(templateSource).window.document;
  for (const id of ['learning-chain-suggestions', 'learning-chain-learning']) {
    const input = doc.getElementById(id);
    assert.ok(input, `${id} fehlt`);
    assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
  }
});

test('Die beiden Zeilen haben den Hilfe-Anker am Ende wie die Versions-Karte, ohne field-help-align-start', () => {
  const doc = new JSDOM(templateSource).window.document;
  const ids = ['learning-chain-suggestions', 'learning-chain-learning'];
  for (const id of ids) {
    const label = doc.querySelector(`label[for="${id}"]`);
    assert.ok(label, `Label fuer ${id} fehlt`);
    const anchor = label.querySelector('.version-help-anchor');
    assert.ok(anchor, `${id}: Anker fehlt`);
    const help = anchor.querySelector('.field-help.version-help');
    assert.ok(help, `${id}: Hilfe fehlt im Anker`);
    assert.equal(label.querySelector('.field-help-align-start'), null, `${id}: field-help-align-start noch da`);
  }
});

test('load() liest die beiden Gruppenschalter als Boolean, fehlend heisst an', async () => {
  const { dom, component } = createPanel(loadFetch({ automation_habits_disabled: true }));
  await component.load();
  assert.equal(component.automationHabitsDisabled, true);
  assert.equal(component.automationReliabilityDisabled, false);
  dom.window.close();
});

test('payload() schickt die beiden Gruppenschalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ automation_reliability_disabled: true }));
  await component.load();
  const payload = JSON.parse(JSON.stringify(component.payload()));
  assert.equal(payload.automation_habits_disabled, false);
  assert.equal(payload.automation_reliability_disabled, true);
  dom.window.close();
});

test('Die beiden Gruppenschalter liegen in der Feineinstellung vor "Geräte ausnehmen" und sind an canEditSetup gebunden', () => {
  const doc = new JSDOM(templateSource).window.document;
  const fine = doc.getElementById('learning-fine');
  assert.ok(fine, 'Feineinstellung fehlt');
  const excluded = [...fine.querySelectorAll('button')].find(button => button.textContent.includes('settings.learning.auto.excluded') || button.querySelector('span')?.textContent.includes('settings.learning.auto.excluded'));
  assert.ok(excluded, 'Zeile Geräte ausnehmen fehlt');
  for (const id of ['learning-habits', 'learning-reliability']) {
    const input = doc.getElementById(id);
    assert.ok(input, `${id} fehlt`);
    assert.equal(input.getAttribute('type'), 'checkbox');
    assert.equal(input.getAttribute('role'), 'switch');
    assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
    const label = input.closest('label');
    assert.ok(label && label.getAttribute('for') === id, `${id}: Schalter ist nicht in einem Label mit for`);
    assert.ok(fine.contains(label), `${id}: nicht in der Feineinstellung`);
    assert.ok(label.compareDocumentPosition(excluded) & doc.defaultView.Node.DOCUMENT_POSITION_FOLLOWING, `${id}: steht nicht vor Geräte ausnehmen`);
  }
});
