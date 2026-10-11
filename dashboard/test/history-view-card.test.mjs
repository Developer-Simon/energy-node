// dashboard/test/history-view-card.test.mjs
// Die Verlaufskachel der Uebersicht. ApexCharts wird wie in
// history.page.test.mjs durch ein Double ersetzt.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { IDBFactory as FDBFactory, IDBKeyRange as FDBKeyRange } from 'fake-indexeddb';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');
const HOUR = 3600 * 1000;

const card = (id, config, {missing = false} = {}) =>
  `<section id="${id}" class="history-view-card" hx-preserve x-ignore data-history-view='${JSON.stringify(config)}'${missing ? ' data-history-view-missing' : ''}>`
  + `<header><h3>T</h3><span data-history-view-range></span></header>`
  + `<p data-history-view-status${missing ? '' : ' hidden'}>${missing ? 'weg' : ''}</p>`
  + `<div data-history-view-plot></div></section>`;

const OWN = {series: [], range_mode: 'relative', range_hours: 6, aggregate: 'avg'};

function load(bodyHTML, {withStore = true, beforeController} = {}) {
  const dom = new JSDOM(`<!doctype html><html><body><div id="overview-live">${bodyHTML}</div></body></html>`, {runScripts: 'outside-only', url: 'http://localhost/', pretendToBeVisual: true});
  installI18n(dom.window);
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  const charts = [];
  dom.window.ApexCharts = class {
    constructor(element, options) { this.element = element; this.options = options; charts.push(this); }
    render() { return Promise.resolve(); }
    updateOptions(options) { this.options = options; this.updated = (this.updated || 0) + 1; }
    destroy() { this.destroyed = true; }
  };
  const context = dom.getInternalVMContext();
  const sources = ['theme.js', 'history-rollup.js', ...(withStore ? ['history-store.js'] : []), 'history-chart.js'];
  sources.forEach(name => vm.runInContext(read(name), context));
  return beforeController ? beforeController(dom).then(() => {
    vm.runInContext(read('history-view-card.js'), context);
    return {dom, charts, api: dom.window.HistoryViewCard};
  }) : (() => {
    vm.runInContext(read('history-view-card.js'), context);
    return {dom, charts, api: dom.window.HistoryViewCard};
  })();
}

async function seed(dom) {
  const ts = Date.now() - HOUR;
  await dom.window.HistoryStore.writeRaw([
    {series: 'role:pv', ts, v: 640, u: 'W'},
    {series: 'role:grid', ts, v: -120, u: 'W'},
  ]);
}

test('scan baut je Kachel genau ein Diagramm und beim zweiten Mal keins dazu', async () => {
  const {dom, charts, api} = await load(card('hv-a', OWN), {beforeController: seed});
  await api.scan();
  assert.equal(charts.length, 1);
  assert.equal(charts[0].element, dom.window.document.querySelector('#hv-a [data-history-view-plot]'));
  assert.equal(charts[0].options.chart.toolbar.show, false, 'Kachel ist nur lesend');
  assert.equal(dom.window.document.querySelector('#hv-a [data-history-view-range]').textContent, '6 h');
  dom.window.close();
});

test('eine verschwundene Kachel wird beim naechsten scan abgebaut', async () => {
  const {dom, charts, api} = await load(card('hv-a', OWN), {beforeController: seed});
  await api.scan();
  dom.window.document.getElementById('hv-a').remove();
  await api.scan();
  assert.equal(charts[0].destroyed, true);
  assert.equal(api.entries.size, 0);
  dom.window.close();
});

test('missing view mounts no chart and keeps the server text', async () => {
  const {dom, charts, api} = await load(card('hv-m', {}, {missing: true}));
  await api.scan();
  assert.equal(charts.length, 0);
  const status = dom.window.document.querySelector('#hv-m [data-history-view-status]');
  assert.equal(status.textContent, 'weg');
  assert.equal(status.hidden, false);
  dom.window.close();
});

test('empty range shows the empty text of the history page', async () => {
  const {dom, charts, api} = await load(card('hv-a', OWN));
  await api.scan();
  assert.equal(charts.length, 0);
  const status = dom.window.document.querySelector('#hv-a [data-history-view-status]');
  assert.equal(status.textContent, dom.window.I18n.t('history.status.empty'));
  assert.equal(status.hidden, false);
  dom.window.close();
});

test('ohne HistoryStore sagt die Kachel, dass dieser Browser keinen Verlauf speichert', async () => {
  const {dom, api} = await load(card('hv-a', OWN), {withStore: false});
  await api.scan();
  assert.equal(dom.window.document.querySelector('#hv-a [data-history-view-status]').textContent, dom.window.I18n.t('overview.history_view.no_store'));
  dom.window.close();
});

test('die Kachel zeigt nur ihre eigenen Serien', async () => {
  const {dom, charts, api} = await load(card('hv-a', {...OWN, series: ['role:grid']}), {beforeController: seed});
  await api.scan();
  const names = charts[0].options.series.map(item => item.name);
  assert.ok(names.includes('role:grid'));
  assert.ok(!names.includes('role:pv'));
  dom.window.close();
});

test('ein Lesefehler steht in der Kachel, der naechste Takt versucht es erneut', async () => {
  const {dom, charts, api} = await load(card('hv-a', OWN));
  const realRead = dom.window.HistoryStore.readRange;
  dom.window.HistoryStore.readRange = () => Promise.reject(new Error('kaputt'));
  await api.scan();
  const status = dom.window.document.querySelector('#hv-a [data-history-view-status]');
  assert.equal(status.textContent, dom.window.I18n.t('overview.history_view.read_failed', {message: 'kaputt'}));
  dom.window.HistoryStore.readRange = realRead;
  await seed(dom);
  await api.tick();
  assert.equal(charts.length, 1);
  assert.equal(status.hidden, true);
  dom.window.close();
});

test('tick aktualisiert ein bestehendes Diagramm statt ein neues zu bauen', async () => {
  const {dom, charts, api} = await load(card('hv-a', OWN), {beforeController: seed});
  await api.scan();
  await api.tick();
  assert.equal(charts.length, 1);
  assert.equal(charts[0].updated, 1);
  dom.window.close();
});

// Haengt einen Recorder-Ersatz ein, der An- und Abmeldungen festhaelt.
async function seedWithWatches(dom, calls) {
  await seed(dom);
  dom.window.dashboardHistorizer = {
    config: () => ({rawWindowHours: 24, minuteWindowDays: 7}),
    watchRange: (view, range) => calls.watched.push([view, range]),
    unwatchRange: view => calls.unwatched.push(view),
  };
}

test('eine Kachel meldet ihren Zeitraum beim Austausch an', async () => {
  const calls = {watched: [], unwatched: []};
  const {dom, api} = await load(card('hv-a', OWN), {beforeController: d => seedWithWatches(d, calls)});
  await api.scan();
  assert.equal(calls.watched.length, 1);
  const [view, range] = calls.watched[0];
  assert.equal(view, 'hv-a');
  assert.equal(range.tier, 'raw');
  assert.equal(range.to - range.from, 6 * HOUR);
  dom.window.close();
});

test('eine abgebaute Kachel meldet sich ab', async () => {
  const calls = {watched: [], unwatched: []};
  const {dom, api} = await load(card('hv-a', OWN), {beforeController: d => seedWithWatches(d, calls)});
  await api.scan();
  dom.window.document.getElementById('hv-a').remove();
  await api.scan();
  assert.deepEqual(calls.unwatched, ['hv-a']);
  dom.window.close();
});

test('ein verborgener Tab meldet seine Kacheln ab', async () => {
  const calls = {watched: [], unwatched: []};
  const {dom, api} = await load(card('hv-a', OWN), {beforeController: d => seedWithWatches(d, calls)});
  await api.scan();
  Object.defineProperty(dom.window.document, 'visibilityState', {value: 'hidden', configurable: true});
  dom.window.document.dispatchEvent(new dom.window.Event('visibilitychange'));
  assert.deepEqual(calls.unwatched, ['hv-a']);
  dom.window.close();
});

test('eine Lieferung laedt die Kachel nach einer Sekunde Ruhe einmal neu', async () => {
  const calls = {watched: [], unwatched: []};
  const {dom, charts, api} = await load(card('hv-a', OWN), {beforeController: d => seedWithWatches(d, calls)});
  await api.scan();
  dom.window.dispatchEvent(new dom.window.CustomEvent('dashboard-history-exchanged', {detail: {rows: 500}}));
  dom.window.dispatchEvent(new dom.window.CustomEvent('dashboard-history-exchanged', {detail: {rows: 200}}));
  await new Promise(resolve => setTimeout(resolve, 1200));
  assert.equal(charts[0].updated, 1);
  dom.window.close();
});
