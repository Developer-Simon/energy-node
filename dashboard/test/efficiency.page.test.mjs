// Die Seite Effizienz: Auswahl der Kette, Live-Werte aus dem Registry-Ereignis,
// Nachladen nur bei neuer Revision, Fehler und Kette ohne Aufzeichnung.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';

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

function setup({chains = [AKKU], revision = 1, storage = {}, session = {csrf_token: 'tok', edit_energy: true}} = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  let factory;
  dom.window.Alpine = {data: (_name, fn) => { factory = fn; }};
  installI18n(dom.window);
  for (const [key, value] of Object.entries(storage)) dom.window.localStorage.setItem(key, value);
  const calls = [];
  const requests = [];
  const server = {chains, revision, session, pending: null, rolesPending: null, write: null};
  dom.window.fetch = (url, options = {}) => {
    calls.push(url);
    requests.push({url, options});
    if (url.endsWith('/api/v1/auth/session')) return Promise.resolve(reply(server.session));
    if (options.method && options.method !== 'GET') return server.write ? server.write(url, options) : Promise.resolve(reply({}));
    if (url.endsWith('/api/v1/energy/roles')) return server.rolesPending ? server.rolesPending(url) : Promise.resolve(reply({categories: {}}));
    if (server.pending) return server.pending(url);
    return Promise.resolve(reply({revision: server.revision, chains: server.chains}));
  };
  for (const name of ['energy-model.js', 'energy-stats-model.js', 'devicemap-model.js', 'energy-chains.js', 'efficiency-model.js', 'efficiency.page.js']) {
    vm.runInContext(read(name), context);
  }
  const efficiencyCalls = () => calls.filter(url => url.includes('/api/v1/energy/efficiency')).length;
  const emit = detail => dom.window.dispatchEvent(new dom.window.CustomEvent('registry-updated', {detail}));
  return {dom, panel: factory(), calls, requests, efficiencyCalls, emit, server};
}

// A fake ApexCharts: records every chart the page creates, render and destroy.
function fakeCharts(window) {
  const made = [];
  window.ApexCharts = class {
    constructor(el, options) { this.el = el; this.options = options; this.rendered = false; this.destroyed = false; this.updates = []; made.push(this); }
    render() { this.rendered = true; }
    updateOptions(options) { this.updates.push(options); }
    destroy() { this.destroyed = true; }
  };
  return {made};
}

// The store the page talks to: confirm answers yes, toasts are recorded.
function fakeStore(panel, {confirmAnswer = true} = {}) {
  const store = {toasts: [], asked: []};
  panel.$store = {
    toasts: {push: (message, tone) => store.toasts.push([message, tone])},
    modal: {confirm: async options => { store.asked.push(options); return confirmAnswer; }},
  };
  return store;
}

const CURVE = {
  down: [{power_w: 250, efficiency: 0.96}, {power_w: 500, efficiency: 0.95}],
  up: [],
  best_down: {from_w: 250, to_w: 500},
};

const WITH_CURVE = {...AKKU, curve: CURVE};

// Walks the markup, template contents included, and records each element with its ancestors.
function markupOf() {
  const doc = new JSDOM(fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'efficiency.html'), 'utf8')).window.document;
  const out = [];
  const visit = (node, ancestors) => {
    for (const child of Array.from(node.children || [])) {
      const chain = [...ancestors, child];
      out.push({el: child, chain});
      visit(child.tagName === 'TEMPLATE' ? child.content : child, chain);
    }
  };
  visit(doc, []);
  return out;
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
  assert.equal(panel.status.text, 'Plausibel');
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
  assert.equal(panel.status.text, 'Plausibel');
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

const plain = value => JSON.parse(JSON.stringify(value));
const catalogDe = catalog('de');

const FINDING_STATE = () => ({
  ...akkuState(),
  findings: [{
    kind: 'drift', chain_id: 'akku', chain_label: 'Speicher', since: '2026-11-06T10:00:00Z',
    from: 'ac', to: 'dc', suggestion: {entity_id: 'dc', invert: true},
  }],
});

test('Die Kurve wird mit dem Laden gezeichnet, nur beim ersten Live-Zustand und bei neuer Revision neu', async () => {
  const {dom, panel, emit, server} = setup({chains: [WITH_CURVE]});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.equal(charts.made.length, 1);
  assert.equal(charts.made[0].el, panel.$refs.curve);
  assert.equal(charts.made[0].rendered, true);
  assert.equal(charts.made[0].options.series.length, 1);
  assert.equal(charts.made[0].options.annotations.xaxis.length, 1);

  // Der erste Live-Zustand morpht den Chart in place, er wird nicht neu erzeugt, und danach nicht mehr im Registry-Takt.
  emit({energy: {chains: [akkuState(0.97)]}});
  assert.equal(charts.made.length, 1);
  assert.equal(charts.made[0].destroyed, false);
  assert.equal(charts.made[0].updates.length, 1);
  emit({energy: {chains: [akkuState(0.92)]}});
  assert.equal(charts.made.length, 1);
  assert.equal(charts.made[0].updates.length, 1);

  // Eine neue Revision derselben Kette morpht ebenfalls, ohne Neuaufbau.
  server.revision = 2;
  emit({energy_stats: {rev: 2}});
  await flush();
  assert.equal(charts.made.length, 1);
  assert.equal(charts.made[0].updates.length, 2);

  panel.destroy();
  assert.equal(charts.made[0].destroyed, true);
  dom.window.close();
});

// A registry event for the storage chain whose counting point (DC) shows the given power.
const liveAt = (dc, overrides = {}) => ({energy: {chains: [{
  ...akkuState(0.97),
  points: [
    {entity_id: 'ac', label: 'AC', value: 1000, has_value: true, freshness: 'fresh', counting: false},
    {entity_id: 'dc', label: 'DC', value: dc, has_value: true, freshness: 'fresh', counting: true},
  ],
  ...overrides,
}]}});

const dashedLines = options => options.annotations.xaxis.filter(item => item.strokeDashArray);

test('Der Betriebspunkt wandert als Linie mit, unter 1 W Aenderung bleibt der Chart unveraendert', async () => {
  const {dom, panel, emit} = setup({chains: [WITH_CURVE]});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  emit(liveAt(970));
  // The first live state morphs the chart: the axis covers the power, no annotation carries the line.
  assert.equal(charts.made.length, 1);
  assert.equal(charts.made[0].updates.length, 1);
  assert.equal(charts.made[0].updates[0].xaxis.max, 970);
  assert.equal(dashedLines(charts.made[0].updates[0]).length, 0);

  emit(liveAt(970.4));
  assert.equal(charts.made[0].updates.length, 1);

  emit(liveAt(980));
  assert.equal(charts.made[0].updates.length, 2);
  assert.equal(charts.made[0].updates[1].xaxis.max, 980);

  emit(liveAt(980, {flow: 'idle'}));
  assert.equal(charts.made[0].updates.length, 3);
  assert.equal(charts.made[0].updates[2].xaxis.max, 500);
  dom.window.close();
});

test('Die Betriebspunkt-Linie liegt ueber dem Wert im Plot, gleitet per transform und verschwindet ohne Leistung', async () => {
  const {dom, panel, emit} = setup({chains: [WITH_CURVE]});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div'), now: dom.window.document.createElement('span')};
  await panel.init();
  charts.made[0].w = {globals: {minX: 0, maxX: 1000, gridWidth: 500, translateX: 40, translateY: 10, gridHeight: 200}};
  const now = panel.$refs.now;

  emit(liveAt(500));
  await flush();
  assert.equal(now.style.transform, 'translateX(290px)');
  assert.equal(now.style.top, '10px');
  assert.equal(now.style.height, '200px');
  assert.equal(now.style.opacity, '1');

  emit(liveAt(250));
  await flush();
  assert.equal(now.style.transform, 'translateX(165px)');

  emit(liveAt(1200));
  await flush();
  assert.equal(now.style.opacity, '0');

  emit(liveAt(980, {flow: 'idle'}));
  await flush();
  assert.equal(now.style.opacity, '0');
  dom.window.close();
});

test('Mit prefers-reduced-motion zeichnet die Kurve ohne Animation, die Linie hat keine Bewegung', async () => {
  const {dom, panel} = setup({chains: [WITH_CURVE]});
  dom.window.matchMedia = query => ({matches: query.includes('reduce'), media: query});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div'), now: dom.window.document.createElement('span')};
  await panel.init();
  assert.equal(charts.made[0].options.chart.animations.enabled, false);
  dom.window.close();
});

test('Ohne reduzierte Bewegung zeichnet der erste Draw die Kurve ein, mit Kettenwechsel blendet der Host ein', async () => {
  const {dom, panel} = setup({chains: [WITH_CURVE, {...WALLBOX, curve: CURVE}]});
  dom.window.matchMedia = () => ({matches: false});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  let faded = null;
  panel.$refs.curve.animate = (frames, options) => { faded = {frames, options}; };
  await panel.init();
  assert.equal(charts.made[0].options.chart.animations.enabled, true);
  assert.equal(charts.made[0].options.chart.animations.easing, 'easeout');
  assert.equal(faded, null);
  panel.selectChain('wallbox');
  assert.equal(faded.options.duration, 160);
  assert.equal(charts.made[1].options.chart.animations.enabled, true);
  dom.window.close();
});

test('Die Legende zeigt die Kette nur mit dem Eintrag, den die Kurve auch zeigt', async () => {
  const {dom, panel, emit} = setup({chains: [WITH_CURVE]});
  fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.deepEqual(plain(panel.curveLegend.map(item => item.id)), ['best']);
  assert.equal(panel.curveLegend[0].text, catalogDe['efficiency.curve.best'].replace('{from}', '250').replace('{to}', '500'));

  emit(liveAt(980));
  assert.deepEqual(plain(panel.curveLegend.map(item => item.id)), ['best', 'now']);
  assert.equal(panel.curveLegend[1].text, catalogDe['efficiency.curve.now'].replace('{power}', '980'));
  dom.window.close();
});

test('Ein Themenwechsel zeichnet die Kurve neu, auch ohne neue Daten, und meldet sich beim Schliessen ab', async () => {
  const {dom, panel} = setup({chains: [WITH_CURVE]});
  const charts = fakeCharts(dom.window);
  let handler = null;
  dom.window.DashboardTheme = {onChange: fn => { handler = fn; return () => { handler = null; }; }};
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.equal(charts.made.length, 1);
  handler('light');
  assert.equal(charts.made.length, 2);
  assert.equal(charts.made[0].destroyed, true);
  panel.destroy();
  assert.equal(handler, null);
  dom.window.close();
});

test('Ein Kettenwechsel zeichnet die Kurve neu und raeumt den alten Chart weg', async () => {
  const {dom, panel} = setup({chains: [WITH_CURVE, {...WALLBOX, curve: CURVE}]});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.equal(charts.made.length, 1);
  panel.selectChain('wallbox');
  assert.equal(charts.made.length, 2);
  assert.equal(charts.made[0].destroyed, true);
  assert.equal(charts.made[1].destroyed, false);
  dom.window.close();
});

test('Ohne window.ApexCharts zeigt die Kurve einen Text statt abzustuerzen', async () => {
  const {dom, panel} = setup({chains: [WITH_CURVE]});
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.equal(panel.curveNote, catalogDe['efficiency.curve.unavailable']);
  dom.window.close();
});

test('Ohne Kurvenpunkte zeigt die Kurve efficiency.curve.learning statt eines leeren Diagramms', async () => {
  const {dom, panel} = setup({chains: [AKKU]});
  const charts = fakeCharts(dom.window);
  panel.$refs = {curve: dom.window.document.createElement('div')};
  await panel.init();
  assert.equal(charts.made.length, 0);
  assert.equal(panel.curveNote, catalogDe['efficiency.curve.learning']);
  dom.window.close();
});

test('Diagnose ohne Meldung zeigt die Bestaetigungszeile, mit Meldung Text, Seit-Zeit und Vorschlag', async () => {
  const {dom, panel, emit} = setup({chains: [WITH_CURVE]});
  await panel.init();
  emit({energy: {chains: [akkuState()]}});
  assert.equal(panel.diagnosis.quiet, true);
  assert.equal(panel.diagnosisLine, catalogDe['efficiency.diagnosis.ok']);

  emit({energy: {chains: [{...akkuState(), learning: true}]}});
  assert.equal(panel.diagnosisLine, catalogDe['efficiency.diagnosis.learning']);

  emit({energy: {chains: [FINDING_STATE()]}});
  const diagnosis = plain(panel.diagnosis);
  assert.equal(diagnosis.quiet, false);
  assert.equal(diagnosis.findings.length, 1);
  const [item] = diagnosis.findings;
  assert.match(item.text, /AC/);
  assert.match(item.sinceText, /^seit /);
  assert.equal(item.suggestion, catalogDe['chains.suggestion.invert'].replace('{point}', 'DC'));
  assert.equal(item.canApply, true);
  dom.window.close();
});

test('Vorschlag uebernehmen oeffnet den Energie-Tab mit dem Entwurf, ohne Recht fehlt der Knopf', async () => {
  const {dom, panel, emit} = setup({chains: [WITH_CURVE]});
  const opened = [];
  dom.window.addEventListener('dashboard-open-panel', event => opened.push(event.detail));
  await panel.init();
  emit({energy: {chains: [FINDING_STATE()]}});
  panel.applySuggestion(panel.diagnosis.findings[0]);
  assert.equal(opened.length, 1);
  assert.equal(opened[0].panel, 'energy-panel');
  assert.deepEqual(plain(opened[0].energyDraft), {entity_id: 'dc', invert: true});
  dom.window.close();

  const denied = setup({chains: [WITH_CURVE], session: {csrf_token: 'tok', edit_energy: false}});
  const deniedOpened = [];
  denied.dom.window.addEventListener('dashboard-open-panel', event => deniedOpened.push(event.detail));
  await denied.panel.init();
  denied.emit({energy: {chains: [FINDING_STATE()]}});
  assert.equal(denied.panel.diagnosis.findings[0].canApply, false);
  denied.panel.applySuggestion(denied.panel.diagnosis.findings[0]);
  assert.equal(deniedOpened.length, 0);
  denied.dom.window.close();
});

test('Toleranz speichern schickt PUT mit CSRF, leere Felder als null, Fehler zeigt Toast und laesst die Eingabe', async () => {
  const {dom, panel, emit, requests, server} = setup({chains: [WITH_CURVE]});
  const store = fakeStore(panel);
  await panel.init();
  emit({energy: {chains: [akkuState()]}});
  const [row] = plain(panel.diagnosis.segments);
  panel.openEdit(row);
  assert.equal(panel.editing, 'ac|dc');

  panel.draftW = '5';
  panel.draftPct = '';
  await panel.saveTolerance(row);
  const put = requests.filter(item => item.options.method === 'PUT');
  assert.equal(put.length, 1);
  assert.equal(put[0].url, '/api/v1/energy/chains/akku/tolerance');
  assert.equal(put[0].options.headers['X-CSRF-Token'], 'tok');
  assert.deepEqual(JSON.parse(put[0].options.body), {a: 'ac', b: 'dc', w: 5, pct: null});
  assert.equal(panel.editing, '');

  panel.openEdit(row);
  panel.draftW = '';
  panel.draftPct = '';
  await panel.saveTolerance(row);
  assert.deepEqual(JSON.parse(requests.filter(item => item.options.method === 'PUT')[1].options.body), {a: 'ac', b: 'dc', w: null, pct: null});

  server.write = () => Promise.resolve({ok: false, status: 400, json: async () => ({error: 'abgelehnt'})});
  panel.openEdit(row);
  panel.draftW = '7';
  await panel.saveTolerance(row);
  assert.equal(store.toasts.length, 1);
  assert.equal(store.toasts[0][1], 'critical');
  assert.equal(panel.editing, 'ac|dc');
  assert.equal(panel.draftW, '7');
  dom.window.close();
});

test('Neu lernen fragt nach, schickt POST relearn, Fehler als Toast', async () => {
  const {dom, panel, emit, requests, server} = setup({chains: [WITH_CURVE]});
  const store = fakeStore(panel);
  await panel.init();
  emit({energy: {chains: [akkuState()]}});
  const [row] = plain(panel.diagnosis.segments);
  await panel.relearn(row);
  assert.equal(store.asked.length, 1);
  assert.equal(store.asked[0].body, catalogDe['efficiency.segment.relearn_confirm']);
  const post = requests.filter(item => item.options.method === 'POST');
  assert.equal(post.length, 1);
  assert.equal(post[0].url, '/api/v1/energy/chains/akku/relearn');
  assert.deepEqual(JSON.parse(post[0].options.body), {a: 'ac', b: 'dc'});

  server.write = () => Promise.reject(new Error('Netz weg'));
  await panel.relearn(row);
  assert.deepEqual(store.toasts.at(-1), ['Netz weg', 'critical']);
  dom.window.close();

  const declined = setup({chains: [WITH_CURVE]});
  const refused = fakeStore(declined.panel, {confirmAnswer: false});
  await declined.panel.init();
  declined.emit({energy: {chains: [akkuState()]}});
  await declined.panel.relearn(plain(declined.panel.diagnosis.segments)[0]);
  assert.equal(refused.asked.length, 1);
  assert.equal(declined.requests.filter(item => item.options.method === 'POST').length, 0);
  declined.dom.window.close();
});

test('Ohne edit_energy gibt es keine Bearbeiten-Schreibwege, Normalbereiche bleiben lesbar', async () => {
  const {dom, panel, emit, requests} = setup({chains: [WITH_CURVE], session: {csrf_token: 'tok', edit_energy: false}});
  const store = fakeStore(panel);
  await panel.init();
  assert.equal(panel.canEdit, false);
  emit({energy: {chains: [akkuState()]}});
  const [row] = plain(panel.diagnosis.segments);
  assert.equal(typeof row.normalText, 'string');
  assert.notEqual(row.normalText, '');
  panel.openEdit(row);
  assert.equal(panel.editing, '');
  await panel.relearn(row);
  await panel.saveTolerance(row);
  assert.equal(store.asked.length, 0);
  assert.equal(requests.filter(item => item.options.method).length, 0);
  dom.window.close();
});

test('Markup: Tour-Marken, Bearbeiten nur unter x-if canEdit, Normalbereich ausserhalb', () => {
  const nodes = markupOf();
  const find = selector => nodes.filter(item => item.el.matches(selector));
  assert.equal(find('[data-tour="efficiency-curve"]').length, 1);
  assert.equal(find('[data-tour="efficiency-diagnosis"]').length, 1);
  const guarded = item => item.chain.some(el => el.tagName === 'TEMPLATE' && el.getAttribute('x-if') === 'canEdit');
  const edits = find('.efficiency-edit');
  assert.ok(edits.length >= 1);
  for (const item of edits) assert.equal(guarded(item), true);
  const normals = find('.efficiency-segment-normal');
  assert.ok(normals.length >= 1);
  for (const item of normals) assert.equal(guarded(item), false);
  for (const item of nodes) {
    if (item.el.tagName === 'TEMPLATE' && item.el.hasAttribute('x-if')) {
      assert.equal(item.el.content.children.length, 1, `x-if ${item.el.getAttribute('x-if')} braucht genau eine Wurzel`);
    }
  }
});
