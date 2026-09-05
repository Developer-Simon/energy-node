// dashboard/test/history-store.test.mjs
// Speicherschicht der Verlaufs-Historie gegen ein echtes IndexedDB-Verhalten.
// jsdom bringt kein IndexedDB mit, deshalb wird fake-indexeddb in das
// jsdom-window injiziert, bevor history-store.js laeuft.
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
const rollupSource = read('history-rollup.js');
const storeSource = read('history-store.js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  // Jeder Test bekommt eine eigene Factory, sonst teilen sich die Tests eine
  // Datenbank und die Reihenfolge wird bedeutsam.
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  vm.runInContext(rollupSource, context);
  vm.runInContext(storeSource, context);
  return dom;
}

test('writeRaw und readRange liefern nur den angefragten Bereich einer Serie', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 100, u: 'W'},
    {series: 'role:pv', ts: 2000, v: 200, u: 'W'},
    {series: 'role:pv', ts: 3000, v: 300, u: 'W'},
    {series: 'role:grid', ts: 2000, v: -50, u: 'W'},
  ]);
  const rows = await store.readRange('raw', 'role:pv', 1500, 3000);
  assert.deepEqual(rows.map(row => row.ts), [2000, 3000]);
  assert.equal(rows[0].v, 200);
  dom.window.close();
});

test('readRange ohne Serie liefert alle Serien im Zeitfenster', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 100, u: 'W'},
    {series: 'role:grid', ts: 1000, v: -50, u: 'W'},
    {series: 'role:grid', ts: 9000, v: -60, u: 'W'},
  ]);
  const rows = await store.readRange('raw', null, 0, 5000);
  assert.equal(rows.length, 2);
  dom.window.close();
});

test('readRange ohne Serie ueberspringt keine Serie, deren Name mit einer anderen beginnt', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  // readAllFiltered spult hinter dem Fenster auf die naechste Serie vor.
  // Springt es dabei auf series + 0xffff, sortiert das HINTER einem
  // laengeren Namen mit demselben Praefix - die Serie fiele stumm weg.
  // role:pv liegt komplett hinter dem Fenster und loest den Sprung aus.
  await store.writeRaw([
    {series: 'role:pv', ts: 9000, v: 1, u: 'W'},
    {series: 'role:pvx', ts: 1000, v: 2, u: 'W'},
    {series: 'role:pz', ts: 1000, v: 3, u: 'W'},
  ]);
  const rows = await store.readRange('raw', null, 0, 5000);
  assert.deepEqual(JSON.parse(JSON.stringify(rows.map(row => row.series))), ['role:pvx', 'role:pz']);
  dom.window.close();
});

test('readRange ohne Serie liefert Saetze am Fensterrand mit', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 1, u: 'W'},
    {series: 'role:pv', ts: 5000, v: 2, u: 'W'},
  ]);
  const rows = await store.readRange('raw', null, 1000, 5000);
  assert.equal(rows.length, 2, 'from und to sind beide einschliesslich');
  dom.window.close();
});

test('seriesNames liefert jede Serie genau einmal', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 1, u: 'W'},
    {series: 'role:pv', ts: 2000, v: 2, u: 'W'},
    {series: 'role:grid', ts: 1000, v: 3, u: 'W'},
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify((await store.seriesNames()).sort())), ['role:grid', 'role:pv']);
  dom.window.close();
});

test('das Byte-Konto waechst beim Schreiben und schrumpft beim Loeschen', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  assert.equal(await store.bytesUsed(), 0);
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 1, u: 'W'},
    {series: 'role:pv', ts: 2000, v: 2, u: 'W'},
  ]);
  assert.equal(await store.bytesUsed(), 2 * store.ROW_BYTES_RAW);
  const deleted = await store.deleteRange('raw', 'role:pv', 0, 1500);
  assert.equal(deleted, 1);
  assert.equal(await store.bytesUsed(), store.ROW_BYTES_RAW);
  dom.window.close();
});

test('das Byte-Konto faellt nicht unter null', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([{series: 'role:pv', ts: 1000, v: 1, u: 'W'}]);
  await store.deleteRange('raw', 'role:pv', 0, 5000);
  await store.deleteRange('raw', 'role:pv', 0, 5000);
  assert.equal(await store.bytesUsed(), 0);
  dom.window.close();
});

test('ein zweites Schreiben auf denselben Schluessel zaehlt das Konto nicht doppelt', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([{series: 'role:pv', ts: 1000, v: 1, u: 'W'}]);
  await store.writeRaw([{series: 'role:pv', ts: 1000, v: 9, u: 'W'}]);
  assert.equal(await store.bytesUsed(), store.ROW_BYTES_RAW);
  const rows = await store.readRange('raw', 'role:pv', 0, 5000);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].v, 9);
  dom.window.close();
});

test('meta speichert und liest Werte ueber Transaktionsgrenzen hinweg', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  assert.equal(await store.meta('raw_compacted_until'), undefined);
  await store.setMeta('raw_compacted_until', 1234);
  assert.equal(await store.meta('raw_compacted_until'), 1234);
  dom.window.close();
});

test('bounds liefert aeltesten und neuesten Zeitstempel ueber alle Stufen', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  assert.deepEqual(JSON.parse(JSON.stringify(await store.bounds())), {oldest: null, newest: null});
  await store.writeRaw([
    {series: 'role:pv', ts: 5000, v: 1, u: 'W'},
    {series: 'role:grid', ts: 1000, v: 2, u: 'W'},
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify(await store.bounds())), {oldest: 1000, newest: 5000});
  dom.window.close();
});

test('writeRollup schreibt in die angefragte Stufe und zaehlt mit dem Rollup-Gewicht', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRollup('1m', [{series: 'role:pv', ts: 60000, min: 1, max: 9, avg: 5, n: 6, u: 'W'}]);
  const rows = await store.readRange('1m', 'role:pv', 0, 120000);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].avg, 5);
  assert.equal(await store.bytesUsed(), store.ROW_BYTES_ROLLUP);
  assert.equal((await store.readRange('raw', 'role:pv', 0, 120000)).length, 0);
  dom.window.close();
});

test('deleteKeys entfernt genau die genannten Saetze und bucht das Konto zurueck', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRaw([
    {series: 'role:pv', ts: 1000, v: 1, u: 'W'},
    {series: 'role:pv', ts: 2000, v: 2, u: 'W'},
    {series: 'role:grid', ts: 1000, v: 3, u: 'W'},
  ]);
  const removed = await store.deleteKeys('raw', [['role:pv', 1000], ['role:grid', 1000]]);
  assert.equal(removed, 2);
  assert.equal(await store.bytesUsed(), store.ROW_BYTES_RAW);
  const rest = await store.readRange('raw', null, 0, 9000);
  assert.deepEqual(JSON.parse(JSON.stringify(rest.map(row => row.series))), ['role:pv']);
  dom.window.close();
});

test('coverage zaehlt vorhandene Saetze je Serie in Rasterbuckets', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRollup('1m', [
    {series: 'role:pv', ts: 0, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
    {series: 'role:pv', ts: 1000, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
    {series: 'role:pv', ts: 7000, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
    {series: 'role:grid', ts: 500, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const result = await store.coverage('1m', 0, 10000, 5000);
  assert.deepEqual(JSON.parse(JSON.stringify(result['role:pv'].n)), [2, 1]);
  assert.deepEqual(JSON.parse(JSON.stringify(result['role:grid'].n)), [1, 0]);
  assert.equal(result['role:pv'].from, 0);
  assert.equal(result['role:pv'].step, 5000);
  dom.window.close();
});

test('coverage laesst Saetze ausserhalb des Fensters aus', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRollup('1m', [
    {series: 'role:pv', ts: 100, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
    {series: 'role:pv', ts: 99000, min: 1, max: 1, avg: 1, n: 60, u: 'W'},
  ]);
  const result = await store.coverage('1m', 1000, 10000, 5000);
  assert.equal(result['role:pv'], undefined);
  dom.window.close();
});

test('writeMissing ergaenzt fehlende Saetze und laesst vorhandene unberuehrt', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRollup('1m', [
    {series: 'role:pv', ts: 1000, min: 5, max: 5, avg: 5, n: 60, u: 'W'},
  ]);
  const added = await store.writeMissing('1m', [
    {series: 'role:pv', ts: 1000, min: 9, max: 9, avg: 9, n: 1, u: 'W'},
    {series: 'role:pv', ts: 2000, min: 7, max: 7, avg: 7, n: 60, u: 'W'},
  ]);
  assert.equal(added, 1);
  const rows = await store.readRange('1m', 'role:pv', 0, 9999);
  assert.equal(rows.length, 2);
  // Der vorhandene Satz behaelt seinen eigenen Wert - fremde Daten
  // ueberschreiben das eigene Messgut nicht.
  assert.equal(rows[0].avg, 5);
  assert.equal(rows[1].avg, 7);
  dom.window.close();
});

test('writeMissing zaehlt nur die wirklich geschriebenen Saetze ins Byte-Konto', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  await store.writeRollup('1m', [
    {series: 'role:pv', ts: 1000, min: 5, max: 5, avg: 5, n: 60, u: 'W'},
  ]);
  const before = await store.bytesUsed();
  await store.writeMissing('1m', [
    {series: 'role:pv', ts: 1000, min: 9, max: 9, avg: 9, n: 1, u: 'W'},
    {series: 'role:pv', ts: 2000, min: 7, max: 7, avg: 7, n: 60, u: 'W'},
  ]);
  assert.equal(await store.bytesUsed(), before + store.ROW_BYTES_ROLLUP);
  dom.window.close();
});

test('writeMissing ist idempotent', async () => {
  const dom = load();
  const store = dom.window.HistoryStore;
  const rows = [{series: 'role:pv', ts: 1000, min: 5, max: 5, avg: 5, n: 60, u: 'W'}];
  assert.equal(await store.writeMissing('1m', rows), 1);
  assert.equal(await store.writeMissing('1m', rows), 0);
  dom.window.close();
});
