// Die Seite Effizienz: Auswahl der Kette, Live-Werte aus dem Registry-Ereignis,
// Nachladen nur bei neuer Revision, Fehler und Kette ohne Aufzeichnung.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

const DAYS = Array.from({length: 7}, (_, i) => ({
  local: `2026-11-0${i + 1}T00:00`, loss_kwh: 1 + i, standby_kwh: 0.1, efficiency: 0.9, has_data: true,
}));

const AKKU = {
  id: 'akku', label: 'Speicher', round_trip_30d: 0.88, standby_w_30d: 3.6, loss_month_kwh: 12.5, month: '2026-11', days: DAYS,
};

const WALLBOX = {
  id: 'wallbox', label: 'Wallbox', round_trip_30d: null, standby_w_30d: null, loss_month_kwh: 0, month: '2026-11',
  days: DAYS.map(day => ({...day, has_data: false, loss_kwh: 0, efficiency: null})),
};

// The live state of the storage chain, as the registry event carries it.
const akkuState = (efficiency = 0.97) => ({
  id: 'akku', label: 'Speicher', role: 'battery', counting: 'dc', direction: 'auto', flow: 'down', no_current_values: false,
  points: [
    {entity_id: 'ac', label: 'AC', value: 1000, has_value: true, freshness: 'fresh', counting: false},
    {entity_id: 'dc', label: 'DC', value: 970, has_value: true, freshness: 'fresh', counting: true},
  ],
  segments: [{from: 'ac', to: 'dc', available: true, loss_w: 30, efficiency}],
});

const reply = body => ({ok: true, status: 200, json: async () => body});
const flush = () => new Promise(resolve => setTimeout(resolve, 0));

function setup({chains = [AKKU], revision = 1, storage = {}} = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  installI18n(dom.window);
  for (const [key, value] of Object.entries(storage)) dom.window.localStorage.setItem(key, value);
  const calls = [];
  const server = {chains, revision, pending: null, rolesPending: null};
  dom.window.fetch = url => {
    calls.push(url);
    if (url.endsWith('/api/v1/energy/roles')) return server.rolesPending ? server.rolesPending(url) : Promise.resolve(reply({categories: {}}));
    if (server.pending) return server.pending(url);
    return Promise.resolve(reply({revision: server.revision, chains: server.chains}));
  };
  for (const name of ['energy-model.js', 'energy-stats-model.js', 'devicemap-model.js', 'energy-chains.js', 'efficiency-model.js', 'efficiency.page.js']) {
    vm.runInContext(read(name), context);
  }
  const efficiencyCalls = () => calls.filter(url => url.includes('/api/v1/energy/efficiency')).length;
  const emit = detail => dom.window.dispatchEvent(new dom.window.CustomEvent('registry-updated', {detail}));
  return {dom, panel: factory(), calls, efficiencyCalls, emit, server};
}

test('Ohne Ketten zeigt die Seite den Leerzustand mit Knopf zum Energie-Tab', async () => {
  const {dom, panel, server} = setup({chains: []});
  await panel.init();
  assert.equal(panel.empty, true);
  assert.equal(panel.current, null);
  const opened = [];
  dom.window.addEventListener('dashboard-open-panel', event => opened.push(event.detail));
  panel.openEnergy();
  assert.deepEqual(JSON.parse(JSON.stringify(opened)), [{panel: 'energy-panel'}]);
  server.chains = [AKKU];
  await panel.load();
  assert.equal(panel.empty, false);
  dom.window.close();
});

test('Die gespeicherte Kette wird gewaehlt, eine unbekannte oder geloeschte ID faellt auf die erste', async () => {
  const {dom, panel} = setup({chains: [AKKU, WALLBOX], storage: {'efficiency.chain': 'wallbox'}});
  await panel.init();
  assert.equal(panel.currentId, 'wallbox');
  panel.selectChain('akku');
  assert.equal(dom.window.localStorage.getItem('efficiency.chain'), 'akku');
  const reopened = setup({chains: [AKKU, WALLBOX], storage: {'efficiency.chain': 'geloescht'}});
  await reopened.panel.init();
  assert.equal(reopened.panel.currentId, 'akku');
  dom.window.close();
  reopened.dom.window.close();
});

test('Ein registry-updated mit energy.chains aendert die Live-Werte ohne Anfrage', async () => {
  const {dom, panel, efficiencyCalls, emit} = setup();
  await panel.init();
  assert.equal(efficiencyCalls(), 1);
  // Vor dem ersten Ereignis gibt es keinen Live-Wert.
  assert.equal(panel.kpis[0].value, '–');
  assert.equal(panel.status.text, 'Keine aktuellen Werte');
  emit({energy: {chains: [akkuState(0.97)]}});
  assert.equal(panel.kpis[0].value, '97 %');
  assert.equal(panel.status.text, 'Messwerte aktuell');
  assert.equal(panel.stateLabel(panel.state), 'lädt');
  emit({energy: {chains: [akkuState(0.92)]}});
  assert.equal(panel.kpis[0].value, '92 %');
  assert.equal(efficiencyCalls(), 1);
  dom.window.close();
});

test('Neue Revision laedt nach, gleiche nicht, und eine langsame alte Antwort ueberschreibt nichts', async () => {
  const {dom, panel, efficiencyCalls, emit, server} = setup();
  await panel.init();
  emit({energy_stats: {rev: 2}});
  await flush();
  assert.equal(efficiencyCalls(), 2);
  emit({energy_stats: {rev: 2}});
  await flush();
  assert.equal(efficiencyCalls(), 2);

  // Zwei Anfragen, die erste antwortet erst nach der zweiten.
  const slow = [];
  server.pending = () => new Promise(resolve => slow.push(resolve));
  const older = panel.load();
  server.pending = null;
  server.chains = [{...AKKU, label: 'Neu'}];
  server.revision = 4;
  await panel.load();
  slow[0](reply({revision: 3, chains: [{...AKKU, label: 'Alt'}]}));
  await older;
  assert.equal(panel.chains[0].label, 'Neu');
  assert.equal(panel.dataRevision, 4);
  dom.window.close();
});

test('Fehler beim Laden zeigt den Text, der Live-Teil bleibt stehen', async () => {
  const {dom, panel, emit, server} = setup();
  await panel.init();
  emit({energy: {chains: [akkuState(0.92)]}});
  server.pending = () => Promise.reject(new Error('Netz weg'));
  await panel.load();
  assert.equal(panel.error, 'Netz weg');
  assert.equal(panel.kpis[0].value, '92 %');
  assert.equal(panel.status.text, 'Messwerte aktuell');
  dom.window.close();
});

test('Kette ohne aufgezeichnete Stunde zeigt den Hinweis statt Nullen', async () => {
  const {dom, panel} = setup({chains: [WALLBOX]});
  await panel.init();
  assert.equal(panel.noHistory, true);
  const [, round, standby, loss] = panel.kpis;
  assert.equal(round.value, '–');
  assert.equal(standby.value, '–');
  assert.equal(loss.value, '–');
  assert.match(loss.label, /November/);
  dom.window.close();
});

test('Ein fehlgeschlagenes erstes Laden zeigt den Fehlertext ausserhalb des Kettenblocks', async () => {
  const {dom, panel, server} = setup();
  server.pending = () => Promise.reject(new Error('Netz weg'));
  await panel.init();
  assert.equal(panel.current, null);
  assert.equal(panel.error, 'Netz weg');
  assert.equal(panel.empty, false);
  // Der Fehlertext darf nicht im x-if="current"-Block liegen, sonst ist er ohne Kette unsichtbar.
  const markup = new JSDOM(fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'efficiency.html'), 'utf8')).window.document;
  const error = markup.querySelector('.efficiency-error');
  assert.ok(error, 'Fehlerabsatz fehlt');
  assert.equal(error.closest('template[x-if="current"]'), null);
  dom.window.close();
});

test('Eine Kette mit Verlust im Monat, aber ohne Tageswerte, gilt als aufgezeichnet', async () => {
  const earlier = {
    id: 'akku', label: 'Speicher', round_trip_30d: null, standby_w_30d: null, loss_month_kwh: 3, month: '2026-11',
    days: DAYS.map(day => ({...day, has_data: false, loss_kwh: 0, efficiency: null})),
  };
  const {dom, panel} = setup({chains: [earlier]});
  await panel.init();
  assert.equal(panel.noHistory, false);
  assert.notEqual(panel.kpis[3].value, '–');
  dom.window.close();
});

test('Ein veraltetes Rollen-Ergebnis gewinnt nicht gegen ein neues', async () => {
  const {dom, panel, server} = setup();
  const slow = [];
  server.rolesPending = () => new Promise(resolve => slow.push(resolve));
  const older = panel.loadCategories();
  server.rolesPending = null;
  await panel.loadCategories();
  assert.deepEqual(panel.categories, {});
  slow[0](reply({categories: {old: {base: 'load'}}}));
  await older;
  assert.deepEqual(panel.categories, {});
  dom.window.close();
});
