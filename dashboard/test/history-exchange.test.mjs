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

const here = path.dirname(fileURLToPath(import.meta.url));
const read = name => fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', name), 'utf8');

const HOUR = 3600000;
const NOW = 1000 * HOUR;

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
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

async function startExchange(dom, {fetchImpl, eventSourceImpl} = {}) {
  const fetcher = fetchImpl || fakeFetch();
  const source = eventSourceImpl || fakeEventSource();
  const started = await dom.window.HistoryExchange.start({
    basePath: '', fetchImpl: fetcher, eventSourceImpl: source, now: () => NOW,
  });
  return {started, fetcher, source};
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
  await new Promise(resolve => setTimeout(resolve, 20));

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
