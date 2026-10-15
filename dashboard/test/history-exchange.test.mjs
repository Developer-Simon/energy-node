// dashboard/test/history-exchange.test.mjs
// Der Zustandsteil des Verlauf-Austauschs gegen eingespeiste Doubles fuer
// fetch und EventSource. Das echte Netz und der echte SSE-Strom sind hier
// nicht beobachtbar - die Doubles machen genau die Uebergaenge pruefbar,
// auf die es ankommt: Ankuendigung lesen, Angebot senden, fremdes Angebot
// beantworten, Lieferung schreiben.
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

const HOUR = 3600000;
const NOW = 1000 * HOUR;

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  vm.runInContext(read('history-rollup.js'), context);
  vm.runInContext(read('history-store.js'), context);
  vm.runInContext(read('history-coverage.js'), context);
  vm.runInContext(read('history-exchange.js'), context);
  return dom;
}

const ANNOUNCEMENT = {
  protocol: 1,
  tiers: ['1m', '5m'],
  max_rows_per_deliver: 500,
  max_rows_per_request: 20000,
  max_body_bytes: 1048576,
  request_timeout_seconds: 30,
  buffer: {tier: '1m', retention_hours: 24, rows: 0},
};

// Sammelt alle POSTs und beantwortet die Ankuendigung.
function fakeFetch(announcement = ANNOUNCEMENT) {
  const calls = [];
  const impl = async (url, options = {}) => {
    if (!options.method || options.method === 'GET') {
      if (announcement === null) return {ok: false, status: 404, json: async () => ({})};
      return {ok: true, status: 200, json: async () => announcement};
    }
    calls.push({url, body: JSON.parse(options.body)});
    return {ok: true, status: 204, json: async () => ({})};
  };
  impl.calls = calls;
  return impl;
}

// Ein EventSource-Double, in das der Test Ereignisse einspeisen kann.
function fakeEventSource() {
  const instances = [];
  class FakeEventSource {
    constructor(url) {
      this.url = url;
      this.listeners = {};
      this.closed = false;
      instances.push(this);
    }
    addEventListener(name, handler) { this.listeners[name] = handler; }
    close() { this.closed = true; }
    emit(name, data) {
      const handler = this.listeners[name];
      if (handler) handler({data: JSON.stringify(data)});
    }
  }
  FakeEventSource.instances = instances;
  return FakeEventSource;
}

// Die bestehenden Tests pruefen den vollen Abgleich, also den Modus
// "always". mode: null laesst localStorage leer (Standard on-demand).
async function startExchange(dom, {fetchImpl, eventSourceImpl, mode = 'always', now = () => NOW} = {}) {
  if (mode) dom.window.localStorage.setItem('history-exchange-mode', mode);
  const fetcher = fetchImpl || fakeFetch();
  const source = eventSourceImpl || fakeEventSource();
  const started = await dom.window.HistoryExchange.start({
    basePath: '', fetchImpl: fetcher, eventSourceImpl: source, now,
  });
  return {started, fetcher, source};
}

const settle = () => new Promise(resolve => setTimeout(resolve, 60));
// Wartet auf eine Bedingung statt auf eine feste Zeit: unter Last braucht
// die IndexedDB laenger als eine feste Wartezeit.
const until = async (condition, timeoutMs = 2000) => {
  const deadline = Date.now() + timeoutMs;
  while (!condition() && Date.now() < deadline) {
    // eslint-disable-next-line no-await-in-loop
    await new Promise(resolve => setTimeout(resolve, 10));
  }
};
const posts = (fetcher, suffix) => fetcher.calls.filter(call => call.url.endsWith(suffix));

// Ein Angebot fuer role:pv in der 1m-Stufe mit Anzahlen an den genannten
// Bucket-Indizes.
function pvOffer(dom, counts) {
  const span = dom.window.HistoryCoverage.rasterWindow('1m', NOW);
  const n = new Array(span.buckets).fill(0);
  Object.entries(counts).forEach(([index, value]) => { n[Number(index)] = value; });
  return {from: span.from, coverage: {'1m': {'role:pv': {from: span.from, step: HOUR, n}}}};
}

test('start bricht ab, wenn der Server die Faehigkeit nicht ankuendigt', async () => {
  const dom = load();
  const {started} = await startExchange(dom, {fetchImpl: fakeFetch(null)});
  assert.equal(started, false);
  assert.equal(dom.window.HistoryExchange.status().connected, false);
  dom.window.close();
});

test('start bricht ab bei fremder Protokollversion', async () => {
  const dom = load();
  const {started} = await startExchange(dom, {fetchImpl: fakeFetch({...ANNOUNCEMENT, protocol: 99})});
  assert.equal(started, false);
  dom.window.close();
});

test('nach hello wird das eigene Deckungsraster angeboten', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {started, fetcher, source} = await startExchange(dom);
  assert.equal(started, true);

  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: ['server']});
  await until(() => fetcher.calls.some(call => call.url.endsWith('/offer')));

  const offer = fetcher.calls.find(call => call.url.endsWith('/offer'));
  assert.ok(offer, 'kein Angebot gesendet');
  assert.equal(offer.body.peer, 'p-selbst');
  assert.ok(offer.body.coverage['1m']['role:pv'], 'role:pv fehlt im Angebot');
  dom.window.close();
});

test('ein fremdes Angebot mit mehr Deckung loest genau eine Nachfrage aus', async () => {
  const dom = load();
  const {fetcher, source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  const {from} = dom.window.HistoryCoverage.rasterWindow('1m', NOW);
  const buckets = dom.window.HistoryCoverage.rasterWindow('1m', NOW).buckets;
  const n = new Array(buckets).fill(0);
  n[0] = 60;
  stream.emit('offer', {peer: 'p-fremd', coverage: {'1m': {'role:pv': {from, step: HOUR, n}}}});
  await new Promise(resolve => setTimeout(resolve, 20));

  const requests = fetcher.calls.filter(call => call.url.endsWith('/request'));
  assert.equal(requests.length, 1);
  assert.equal(requests[0].body.to, 'p-fremd');
  assert.equal(requests[0].body.series, 'role:pv');
  assert.deepEqual(requests[0].body.ranges, [[from, from + HOUR]]);
  dom.window.close();
});

test('eine eingehende Nachfrage wird aus der eigenen Datenbank beliefert', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 3, avg: 2, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  stream.emit('request', {
    peer: 'p-fremd', req_id: 'r1', tier: '1m', series: 'role:pv',
    ranges: [[NOW - 3 * HOUR, NOW - HOUR]],
  });
  await new Promise(resolve => setTimeout(resolve, 20));

  const delivers = fetcher.calls.filter(call => call.url.endsWith('/deliver'));
  assert.equal(delivers.length, 1);
  assert.equal(delivers[0].body.to, 'p-fremd');
  assert.equal(delivers[0].body.final, true);
  assert.equal(delivers[0].body.rows.length, 1);
  assert.equal(delivers[0].body.rows[0].avg, 2);
  dom.window.close();
});

test('eine eingehende Lieferung wird geschrieben, ohne Eigenes zu ersetzen', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 5, max: 5, avg: 5, n: 60, u: 'W'},
  ]);
  const {source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  stream.emit('deliver', {
    peer: 'p-fremd', req_id: 'r1', seq: 0, final: true, tier: '1m',
    rows: [
      {series: 'role:pv', ts: NOW - 2 * HOUR, min: 9, max: 9, avg: 9, n: 60, u: 'W'},
      {series: 'role:pv', ts: NOW - 3 * HOUR, min: 7, max: 7, avg: 7, n: 60, u: 'W'},
    ],
  });
  await new Promise(resolve => setTimeout(resolve, 30));

  const rows = await dom.window.HistoryStore.readRange('1m', 'role:pv', 0, NOW);
  assert.equal(rows.length, 2);
  const own = rows.find(row => row.ts === NOW - 2 * HOUR);
  assert.equal(own.avg, 5, 'eigenes Messgut wurde ersetzt');
  assert.equal(dom.window.HistoryExchange.status().addedRows, 1);
  dom.window.close();
});

test('eine Lieferung mit der Rohstufe wird verworfen', async () => {
  const dom = load();
  const {source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  stream.emit('deliver', {
    peer: 'p-fremd', req_id: 'r1', seq: 0, final: true, tier: 'raw',
    rows: [{series: 'role:pv', ts: NOW - 2 * HOUR, min: 9, max: 9, avg: 9, n: 60, u: 'W'}],
  });
  await new Promise(resolve => setTimeout(resolve, 30));

  assert.equal(dom.window.HistoryExchange.status().addedRows, 0);
  dom.window.close();
});

test('eine Lieferung meldet sich mit einem Ereignis', async () => {
  const dom = load();
  const {source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  const seen = [];
  dom.window.addEventListener('dashboard-history-exchanged', event => seen.push(event.detail));
  stream.emit('deliver', {
    peer: 'p-fremd', req_id: 'r1', seq: 0, final: true, tier: '1m',
    rows: [{series: 'role:pv', ts: NOW - 5 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'}],
  });
  await new Promise(resolve => setTimeout(resolve, 30));
  assert.equal(seen.length, 1);
  assert.equal(seen[0].peer, 'p-fremd');
  assert.equal(seen[0].rows, 1);
  dom.window.close();
});

test('die Rohstufe wird niemals angeboten', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRaw([{series: 'role:pv', ts: NOW - 60000, v: 100, u: 'W'}]);
  const {fetcher, source} = await startExchange(dom);
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));
  const offer = fetcher.calls.find(call => call.url.endsWith('/offer'));
  assert.deepEqual(Object.keys(offer.body.coverage).sort(), ['1m', '5m']);
  dom.window.close();
});

test('pushBuffer schickt Saetze an den Server-Ringpuffer', async () => {
  const dom = load();
  const {fetcher, source} = await startExchange(dom);
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  await dom.window.HistoryExchange.pushBuffer([
    {series: 'role:pv', ts: NOW - 60000, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const buffered = fetcher.calls.filter(call => call.url.endsWith('/buffer'));
  assert.equal(buffered.length, 1);
  assert.equal(buffered[0].body.rows.length, 1);
  dom.window.close();
});

test('stop schliesst den Strom und meldet getrennt', async () => {
  const dom = load();
  const {source} = await startExchange(dom);
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));
  dom.window.HistoryExchange.stop();
  assert.equal(source.instances[0].closed, true);
  assert.equal(dom.window.HistoryExchange.status().connected, false);
  dom.window.close();
});

test('eine Lieferung merkt sich die Bezeichnung des liefernden Peers', async () => {
  const dom = load();
  const {source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  stream.emit('offer', {peer: 'p-ha', label: 'Home Assistant', coverage: {}});
  await new Promise(resolve => setTimeout(resolve, 20));
  stream.emit('deliver', {
    peer: 'p-ha', req_id: 'r1', seq: 0, final: true, tier: '1m',
    rows: [{series: 'role:pv', ts: NOW - 3 * HOUR, min: 7, max: 7, avg: 7, n: 1, u: 'W'}],
  });
  await new Promise(resolve => setTimeout(resolve, 30));

  assert.equal(dom.window.HistoryExchange.status().lastSource, 'Home Assistant');
  dom.window.close();
});

test('ohne Bezeichnung bleibt die Quelle leer', async () => {
  const dom = load();
  const {source} = await startExchange(dom);
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await new Promise(resolve => setTimeout(resolve, 20));

  stream.emit('deliver', {
    peer: 'p-fremd', req_id: 'r1', seq: 0, final: true, tier: '1m',
    rows: [{series: 'role:pv', ts: NOW - 3 * HOUR, min: 7, max: 7, avg: 7, n: 1, u: 'W'}],
  });
  await new Promise(resolve => setTimeout(resolve, 30));

  assert.equal(dom.window.HistoryExchange.status().lastSource, '');
  dom.window.close();
});

test('ohne gespeicherten Modus gilt on-demand und es wird nichts angeboten', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom, {mode: null});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  assert.equal(posts(fetcher, '/offer').length, 0);
  assert.equal(dom.window.HistoryExchange.mode(), 'on-demand');
  assert.equal(dom.window.HistoryExchange.status().mode, 'on-demand');
  dom.window.close();
});

test('im Modus on-demand loest ein fremdes Angebot keine Nachfrage aus', async () => {
  const dom = load();
  const {fetcher, source} = await startExchange(dom, {mode: null});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const {coverage} = pvOffer(dom, {0: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage});
  await settle();
  assert.equal(posts(fetcher, '/request').length, 0);
  dom.window.close();
});

test('ein unbekannter gespeicherter Modus gilt als on-demand', async () => {
  const dom = load();
  await startExchange(dom, {mode: 'manchmal'});
  assert.equal(dom.window.HistoryExchange.mode(), 'on-demand');
  dom.window.close();
});

test('ein unlesbarer localStorage gilt als on-demand und setMode wirft nicht', async () => {
  const dom = load();
  Object.defineProperty(dom.window, 'localStorage', {
    configurable: true,
    get() { throw new Error('SecurityError'); },
  });
  const {fetcher, source} = await startExchange(dom, {mode: null});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  assert.equal(dom.window.HistoryExchange.mode(), 'on-demand');
  await dom.window.HistoryExchange.setMode('always');
  assert.equal(dom.window.HistoryExchange.mode(), 'always');
  dom.window.close();
});

test('setMode always merkt sich die Wahl, bietet sofort an und plant gegen bekannte Angebote', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom, {mode: null});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const {coverage} = pvOffer(dom, {0: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage});
  await settle();
  await dom.window.HistoryExchange.setMode('always');
  assert.equal(dom.window.localStorage.getItem('history-exchange-mode'), 'always');
  assert.equal(posts(fetcher, '/offer').length, 1);
  assert.equal(posts(fetcher, '/request').length, 1);
  dom.window.close();
});

test('ein storage-Ereignis aus einem anderen Tab schaltet den Modus um', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom, {mode: null});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  dom.window.localStorage.setItem('history-exchange-mode', 'always');
  dom.window.dispatchEvent(new dom.window.StorageEvent('storage', {key: 'history-exchange-mode', newValue: 'always'}));
  await settle();
  assert.equal(dom.window.HistoryExchange.mode(), 'always');
  assert.equal(posts(fetcher, '/offer').length, 1);
  dom.window.close();
});

// Bringt einen Austausch im Modus on-demand in den Zustand "verbunden, ein
// fremdes Angebot fuer die ersten drei Stunden des Rasters ist bekannt".
async function onDemandWithOffer(dom, options = {}) {
  const started = await startExchange(dom, {mode: null, ...options});
  const stream = started.source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const offer = pvOffer(dom, {0: 60, 1: 60, 2: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  return {...started, stream, from: offer.from, offer};
}

test('watchRange fragt nur den angezeigten Zeitraum an', async () => {
  const dom = load();
  const {fetcher, from} = await onDemandWithOffer(dom);
  assert.equal(posts(fetcher, '/request').length, 0);
  await dom.window.HistoryExchange.watchRange('t1:page', {tier: '1m', from: from + HOUR, to: from + 2 * HOUR});
  const requests = posts(fetcher, '/request');
  assert.equal(requests.length, 1);
  assert.equal(requests[0].body.to, 'p-fremd');
  assert.equal(requests[0].body.tier, '1m');
  assert.deepEqual(requests[0].body.ranges, [[from + HOUR, from + 2 * HOUR]]);
  dom.window.close();
});

test('watchRange fragt fuer die Rohstufe die Minutenstufe an', async () => {
  const dom = load();
  const {fetcher, from} = await onDemandWithOffer(dom);
  await dom.window.HistoryExchange.watchRange('t1:page', {tier: 'raw', from, to: from + HOUR});
  assert.equal(posts(fetcher, '/request')[0].body.tier, '1m');
  dom.window.close();
});

test('ein erneutes Anmelden nach der Lieferung fragt nicht noch einmal', async () => {
  const dom = load();
  const {fetcher, stream, from} = await onDemandWithOffer(dom);
  const range = {tier: '1m', from: from + HOUR, to: from + 2 * HOUR};
  await dom.window.HistoryExchange.watchRange('t1:page', range);
  const [request] = posts(fetcher, '/request');
  stream.emit('deliver', {peer: 'p-fremd', req_id: request.body.req_id, seq: 0, final: true, tier: '1m', rows: []});
  await settle();
  await dom.window.HistoryExchange.watchRange('t1:page', range);
  assert.equal(posts(fetcher, '/request').length, 1);
  dom.window.close();
});

test('ein neues Angebot plant einen angemeldeten Zeitraum neu', async () => {
  const dom = load();
  const {fetcher, stream, from, offer} = await onDemandWithOffer(dom);
  await dom.window.HistoryExchange.watchRange('t1:page', {tier: '1m', from: from + HOUR, to: from + 2 * HOUR});
  const [request] = posts(fetcher, '/request');
  stream.emit('deliver', {peer: 'p-fremd', req_id: request.body.req_id, seq: 0, final: true, tier: '1m', rows: []});
  await settle();
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  assert.equal(posts(fetcher, '/request').length, 2);
  dom.window.close();
});

test('nach unwatchRange loest ein Angebot keine Nachfrage aus', async () => {
  const dom = load();
  const {fetcher, stream, offer} = await onDemandWithOffer(dom);
  dom.window.HistoryExchange.unwatchRange('t1:page');
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  assert.equal(posts(fetcher, '/request').length, 0);
  dom.window.close();
});

test('eine Anmeldung verfaellt nach zehn Minuten ohne Erneuerung', async () => {
  const dom = load();
  let clock = NOW;
  const {fetcher, stream, from, offer} = await onDemandWithOffer(dom, {now: () => clock});
  await dom.window.HistoryExchange.watchRange('t1:page', {tier: '1m', from: from + HOUR, to: from + 2 * HOUR});
  const [request] = posts(fetcher, '/request');
  stream.emit('deliver', {peer: 'p-fremd', req_id: request.body.req_id, seq: 0, final: true, tier: '1m', rows: []});
  await settle();
  clock += 11 * 60 * 1000;
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  assert.equal(posts(fetcher, '/request').length, 1);
  dom.window.close();
});

test('im Modus always ist watchRange wirkungslos', async () => {
  const dom = load();
  const {fetcher, source} = await startExchange(dom, {mode: 'always'});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const offer = pvOffer(dom, {0: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  const [request] = posts(fetcher, '/request');
  stream.emit('deliver', {peer: 'p-fremd', req_id: request.body.req_id, seq: 0, final: true, tier: '1m', rows: []});
  await settle();
  await dom.window.HistoryExchange.watchRange('t1:page', {tier: '1m', from: offer.from, to: offer.from + HOUR});
  assert.equal(posts(fetcher, '/request').length, 1);
  dom.window.close();
});

test('on-demand mit Freigabe bietet an, holt aber nur den angezeigten Zeitraum', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  dom.window.localStorage.setItem('history-exchange-serve', 'on');
  const {fetcher, source} = await startExchange(dom, {mode: null});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await until(() => posts(fetcher, '/offer').length > 0);
  assert.equal(posts(fetcher, '/offer').length, 1);
  assert.equal(dom.window.HistoryExchange.serving(), true);
  const {coverage} = pvOffer(dom, {0: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage});
  await settle();
  assert.equal(posts(fetcher, '/request').length, 0);
  dom.window.close();
});

test('im Modus always wird immer angeboten, auch mit gespeichertem off', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  dom.window.localStorage.setItem('history-exchange-serve', 'off');
  const {fetcher, source} = await startExchange(dom, {mode: 'always'});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await until(() => posts(fetcher, '/offer').length > 0);
  assert.equal(posts(fetcher, '/offer').length, 1);
  assert.equal(dom.window.HistoryExchange.serving(), true);
  dom.window.close();
});

test('setServe bietet sofort an und merkt sich die Wahl, ohne Wahl ist die Freigabe im Modus on-demand aus', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom, {mode: null});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  assert.equal(dom.window.HistoryExchange.serving(), false);
  await dom.window.HistoryExchange.setServe(true);
  assert.equal(dom.window.localStorage.getItem('history-exchange-serve'), 'on');
  assert.equal(posts(fetcher, '/offer').length, 1);
  assert.equal(dom.window.HistoryExchange.status().serving, true);
  dom.window.close();
});

test('ein storage-Ereignis schaltet die Freigabe um und ein unlesbarer Speicher wirft nicht', async () => {
  const dom = load();
  await dom.window.HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: NOW - 2 * HOUR, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const {fetcher, source} = await startExchange(dom, {mode: null});
  source.instances[0].emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  dom.window.localStorage.setItem('history-exchange-serve', 'on');
  dom.window.dispatchEvent(new dom.window.StorageEvent('storage', {key: 'history-exchange-serve', newValue: 'on'}));
  await until(() => posts(fetcher, '/offer').length > 0);
  assert.equal(dom.window.HistoryExchange.serving(), true);
  assert.equal(posts(fetcher, '/offer').length, 1);
  Object.defineProperty(dom.window, 'localStorage', {configurable: true, get() { throw new Error('SecurityError'); }});
  await dom.window.HistoryExchange.setServe(false);
  assert.equal(dom.window.HistoryExchange.serving(), false);
  dom.window.close();
});

test('watchRange mit Stufe 1h beobachtet die Stufe 5m', async () => {
  const dom = load();
  const {fetcher, source} = await startExchange(dom, {mode: null});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const span = dom.window.HistoryCoverage.rasterWindow('5m', NOW);
  const n = new Array(span.buckets).fill(0);
  n[1] = 1;
  stream.emit('offer', {peer: 'p-fremd', coverage: {'5m': {'role:pv': {from: span.from, step: 6 * HOUR, n}}}});
  await settle();
  await dom.window.HistoryExchange.watchRange('page', {tier: '1h', from: span.from + 6 * HOUR, to: span.from + 12 * HOUR});
  const requests = posts(fetcher, '/request');
  assert.equal(requests.length, 1);
  assert.equal(requests[0].body.tier, '5m');
  assert.deepEqual(requests[0].body.ranges, [[span.from + 6 * HOUR, span.from + 12 * HOUR]]);
  dom.window.close();
});

test('zweimal angemeldet innerhalb von fuenf Minuten fragt die letzte halbe Stunde genau einmal', async () => {
  const dom = load();
  let clock = NOW;
  const {fetcher, source} = await startExchange(dom, {mode: null, now: () => clock});
  const stream = source.instances[0];
  stream.emit('hello', {protocol: 1, peer_id: 'p-selbst', peers: []});
  await settle();
  const offer = pvOffer(dom, {167: 60});
  stream.emit('offer', {peer: 'p-fremd', coverage: offer.coverage});
  await settle();
  const range = {tier: '1m', from: NOW - 6 * HOUR, to: NOW};
  await dom.window.HistoryExchange.watchRange('tile', range);
  clock += 2 * 60000;
  await dom.window.HistoryExchange.watchRange('tile', range);
  const requests = posts(fetcher, '/request');
  assert.equal(requests.length, 1);
  assert.equal(requests[0].body.ranges.length, 1);
  assert.equal(requests[0].body.ranges[0][0], NOW - 30 * 60000);
  dom.window.close();
});
