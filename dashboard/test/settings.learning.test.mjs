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

test('load() liest den Energie-Schalter als Boolean, fehlend heisst an', async () => {
  const { dom, component } = createPanel(loadFetch({ automation_energy_disabled: true }));
  await component.load();
  assert.equal(component.automationEnergyDisabled, true);
  dom.window.close();
  const fresh = createPanel(loadFetch({}));
  await fresh.component.load();
  assert.equal(fresh.component.automationEnergyDisabled, false);
  fresh.dom.window.close();
});

test('payload() schickt den Energie-Schalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ automation_energy_disabled: true }));
  await component.load();
  const payload = JSON.parse(JSON.stringify(component.payload()));
  assert.equal(payload.automation_energy_disabled, true);
  dom.window.close();
});

test('Der Energie-Schalter liegt in der Feineinstellung nach Zuverlaessigkeit und ist an canEditSetup gebunden', () => {
  const doc = new JSDOM(templateSource).window.document;
  const input = doc.getElementById('learning-energy');
  assert.ok(input, 'learning-energy fehlt');
  assert.equal(input.getAttribute('type'), 'checkbox');
  assert.equal(input.getAttribute('role'), 'switch');
  assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
  const fine = doc.getElementById('learning-fine');
  const reliability = doc.getElementById('learning-reliability');
  const order = [...fine.querySelectorAll('input')].map(el => el.id);
  assert.ok(order.indexOf('learning-energy') > order.indexOf('learning-reliability'), 'Energie steht nach Zuverlaessigkeit');
});

test('load() liest den Saison-Schalter als Boolean, fehlend heisst ganzes Jahr', async () => {
  const { dom, component } = createPanel(loadFetch({ learning_season_year_disabled: true }));
  await component.load();
  assert.equal(component.learningSeasonYearDisabled, true);
  dom.window.close();
  const fresh = createPanel(loadFetch({}));
  await fresh.component.load();
  assert.equal(fresh.component.learningSeasonYearDisabled, false);
  fresh.dom.window.close();
});

test('payload() schickt den Saison-Schalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ learning_season_year_disabled: true }));
  await component.load();
  const payload = JSON.parse(JSON.stringify(component.payload()));
  assert.equal(payload.learning_season_year_disabled, true);
  dom.window.close();
});

test('Der Saison-Schalter liegt in der Aufzeichnung, ist ein Label mit for und an canEditSetup gebunden', () => {
  const doc = new JSDOM(templateSource).window.document;
  const input = doc.getElementById('learning-season-year');
  assert.ok(input, 'learning-season-year fehlt');
  assert.equal(input.getAttribute('role'), 'switch');
  assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
  const label = input.closest('label');
  assert.ok(label && label.getAttribute('for') === 'learning-season-year');
  assert.ok(input.closest('[data-tour="learning-record"]'), 'nicht in der Karte Aufzeichnung');
});

test('load() nimmt den Modus pause_with_companion und den Abgleich-Schalter aus den Einstellungen, fehlend heisst aus', async () => {
  const { dom, component } = createPanel(loadFetch({ learning_minutes_mode: 'pause_with_companion', learning_ha_reconcile_disabled: true }));
  await component.load();
  assert.equal(component.learningMinutesMode, 'pause_with_companion');
  assert.equal(component.learningHaReconcileDisabled, true);
  dom.window.close();
  const fresh = createPanel(loadFetch({}));
  await fresh.component.load();
  assert.equal(fresh.component.learningHaReconcileDisabled, false);
  fresh.dom.window.close();
});

test('payload() schickt pause_with_companion und den Abgleich-Schalter als Boolean', async () => {
  const { dom, component } = createPanel(loadFetch({ learning_minutes_mode: 'pause_with_companion', learning_ha_reconcile_disabled: true }));
  await component.load();
  const payload = JSON.parse(JSON.stringify(component.payload()));
  assert.equal(payload.learning_minutes_mode, 'pause_with_companion');
  assert.equal(payload.learning_ha_reconcile_disabled, true);
  dom.window.close();
});

test('Der Modus ist eine Radiogruppe mit Immer, Pausieren und Aus, an canEditSetup gebunden, der alte Schalter ist weg', () => {
  const doc = new JSDOM(templateSource).window.document;
  assert.equal(doc.getElementById('learning-minutes-toggle'), null, 'alter Schalter noch da');
  const group = doc.querySelector('[role="radiogroup"][aria-labelledby="lbl-learning-minutes-mode"]');
  assert.ok(group, 'Radiogruppe Modus fehlt');
  assert.ok(doc.getElementById('lbl-learning-minutes-mode'), 'Gruppen-Label fehlt');
  const inputs = [...group.querySelectorAll('input[type="radio"][name="learning-minutes-mode"]')];
  assert.deepEqual(inputs.map(input => input.value), ['always', 'pause_with_companion', 'off']);
  for (const input of inputs) {
    assert.ok(input.getAttribute('x-bind:disabled').includes('!canEditSetup'), `${input.value}: nicht an canEditSetup gebunden`);
  }
  const offInput = inputs.find(input => input.value === 'off');
  assert.ok(offInput.getAttribute('x-on:change').includes("switchedOff('minutes_drive', false)"), 'Aus löst switchedOff nicht aus');
});

test('Die Zeile Pausieren ist ohne Companion gesperrt, zeigt den Hinweis und führt zur Seite Benutzer', () => {
  const doc = new JSDOM(templateSource).window.document;
  const input = doc.querySelector('input[name="learning-minutes-mode"][value="pause_with_companion"]');
  assert.ok(input, 'Radio Pausieren fehlt');
  assert.ok(input.getAttribute('x-bind:disabled').includes('!minutesMeta.companion'), 'Radio nicht ohne Companion gesperrt');
  const row = input.closest('.learn-radio-row');
  assert.ok(row.getAttribute('x-bind:class').includes("'is-unavailable': !minutesMeta.companion"), 'Zeile nicht ausgegraut');
  assert.ok(row.textContent.includes('settings.learning.minutes.mode.pause_unavailable'));
  const connect = [...row.querySelectorAll('button')].find(button => button.textContent.includes('settings.learning.minutes.mode.pause_connect'));
  assert.ok(connect, 'Verbinden-Knopf fehlt');
  assert.equal(connect.getAttribute('x-on:click'), "activeSettingsPage = 'users'");
});

test('Der Abgleich-Schalter ist nur mit Companion und nicht bei Aus sichtbar, an canEditSetup gebunden, mit Hinweis', () => {
  const doc = new JSDOM(templateSource).window.document;
  const input = doc.getElementById('learning-ha-reconcile');
  assert.ok(input, 'learning-ha-reconcile fehlt');
  assert.equal(input.getAttribute('role'), 'switch');
  assert.equal(input.getAttribute('x-bind:disabled'), '!canEditSetup');
  const label = input.closest('label');
  assert.ok(label && label.getAttribute('for') === 'learning-ha-reconcile');
  const row = label.parentElement;
  const visible = row.getAttribute('x-show') || '';
  assert.ok(visible.includes('minutesMeta.companion'), 'nicht an Companion gebunden');
  assert.ok(visible.includes("learningMinutesMode !== 'off'"), 'nicht an Modus Aus gebunden');
  assert.ok(row.textContent.includes('settings.learning.minutes.reconcile_note') || row.nextElementSibling?.textContent.includes('settings.learning.minutes.reconcile_note'), 'Hinweis fehlt');
  assert.ok(input.getAttribute('x-bind:checked').includes('!learningHaReconcileDisabled'), 'Schalter nicht invertiert');
  assert.ok(input.getAttribute('x-on:change').includes('learningHaReconcileDisabled'), 'Schalter setzt das Feld nicht');
});

test('Bei pausierter Aufzeichnung steht der Hinweis paused_now in der Karte', () => {
  const doc = new JSDOM(templateSource).window.document;
  const note = [...doc.querySelectorAll('[data-tour="learning-record"] .learn-row-note')].find(el => el.textContent.includes('settings.learning.minutes.paused_now'));
  assert.ok(note, 'Hinweis paused_now fehlt');
  assert.equal(note.getAttribute('x-show'), 'minutesMeta.paused');
});
