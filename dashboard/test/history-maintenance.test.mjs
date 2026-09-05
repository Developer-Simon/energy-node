// dashboard/test/history-maintenance.test.mjs
// Der Verdichtungslauf: Rohdaten aelter als das Rohfenster werden zu
// Minutenmitteln, Minutenmittel aelter als das Minutenfenster zu
// Fuenf-Minuten-Mitteln. Entscheidend ist, dass verbrauchte Saetze
// verschwinden - sonst waechst der Speicher trotz Verdichtung weiter - und
// dass ein zweiter Lauf dieselbe Spanne nicht erneut verdichtet.
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
const sources = ['history-rollup.js', 'history-store.js', 'history-maintenance.js'].map(read);

const HOUR = 3600 * 1000;
const DAY = 24 * HOUR;

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {runScripts: 'outside-only', url: 'http://localhost/'});
  const context = dom.getInternalVMContext();
  dom.window.indexedDB = new FDBFactory();
  dom.window.IDBKeyRange = FDBKeyRange;
  sources.forEach(source => vm.runInContext(source, context));
  return dom;
}

const config = {rawWindowHours: 24, minuteWindowDays: 7, retentionMode: 'time', retentionHours: 6, budgetMB: 512};

test('compact fasst Rohdaten ausserhalb des Rohfensters zu Minutenmitteln zusammen und loescht sie', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const old = now - 30 * HOUR; // ausserhalb des 24h-Rohfensters
  await HistoryStore.writeRaw([
    {series: 'role:pv', ts: old, v: 100, u: 'W'},
    {series: 'role:pv', ts: old + 10000, v: 300, u: 'W'},
    {series: 'role:pv', ts: now - HOUR, v: 999, u: 'W'}, // innerhalb, bleibt roh
  ]);

  const result = await HistoryMaintenance.compact(config, now);
  assert.equal(result.toMinute, 1, 'die zwei alten Werte fallen in denselben Minuten-Bucket');
  // Der dritte Satz ist laengst abgeschlossen (eine Stunde her, weit ausserhalb
  // der zweiminuetigen Karenzzeit), also holt ihn die Vorabverdichtung fuer
  // den Austausch schon jetzt in die 1m-Stufe - ohne ihn aus raw zu loeschen.
  assert.equal(result.toMinuteEarly, 1);

  const minute = await HistoryStore.readRange('1m', 'role:pv', 0, now);
  assert.equal(minute.length, 2);
  const byAvg = Object.fromEntries(minute.map(row => [row.avg, row]));
  assert.equal(byAvg[200].min, 100);
  assert.equal(byAvg[200].max, 300);
  assert.equal(byAvg[200].n, 2);
  assert.ok(byAvg[999], 'der frueh verdichtete dritte Satz steht ebenfalls in der 1m-Stufe');

  const raw = await HistoryStore.readRange('raw', 'role:pv', 0, now);
  assert.equal(raw.length, 1, 'nur der Satz im Rohfenster darf als Rohdaten uebrig bleiben');
  assert.equal(raw[0].v, 999);
  dom.window.close();
});

test('ein zweiter compact-Lauf verdichtet dieselbe Spanne nicht erneut', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: now - 30 * HOUR, v: 100, u: 'W'}]);
  await HistoryMaintenance.compact(config, now);
  const second = await HistoryMaintenance.compact(config, now);
  assert.equal(second.toMinute, 0);
  assert.equal((await HistoryStore.readRange('1m', 'role:pv', 0, now)).length, 1);
  dom.window.close();
});

test('compact verdichtet Minutenmittel ausserhalb des Minutenfensters weiter zu Fuenf-Minuten-Mitteln', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const old = now - 10 * DAY; // ausserhalb des 7-Tage-Minutenfensters
  const base = Math.floor(old / 60000) * 60000;
  await HistoryStore.writeRollup('1m', [
    {series: 'role:pv', ts: base, min: 100, max: 100, avg: 100, n: 6, u: 'W'},
    {series: 'role:pv', ts: base + 60000, min: 200, max: 200, avg: 200, n: 6, u: 'W'},
  ]);

  const result = await HistoryMaintenance.compact(config, now);
  assert.equal(result.toFiveMinute, 1);
  const five = await HistoryStore.readRange('5m', 'role:pv', 0, now);
  assert.equal(five.length, 1);
  assert.equal(five[0].avg, 150);
  assert.equal(five[0].n, 12);
  assert.equal((await HistoryStore.readRange('1m', 'role:pv', 0, now)).length, 0);
  dom.window.close();
});

test('das Byte-Konto sinkt durch die Verdichtung', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const old = now - 30 * HOUR;
  const rows = [];
  // 60 Rohwerte in einer Minute werden zu einem Minutensatz.
  for (let index = 0; index < 60; index += 1) rows.push({series: 'role:pv', ts: old + index * 1000, v: index, u: 'W'});
  await HistoryStore.writeRaw(rows);
  const before = await HistoryStore.bytesUsed();
  await HistoryMaintenance.compact(config, now);
  const after = await HistoryStore.bytesUsed();
  assert.ok(after < before, `erwartet weniger als ${before}, war ${after}`);
  assert.equal(after, HistoryStore.ROW_BYTES_ROLLUP);
  dom.window.close();
});

test('im Zeitmodus verschwinden Saetze aelter als die Aufbewahrungsdauer', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([
    {series: 'role:pv', ts: now - 8 * HOUR, v: 1, u: 'W'},
    {series: 'role:pv', ts: now - 1 * HOUR, v: 2, u: 'W'},
  ]);
  const result = await HistoryMaintenance.evict({...config, retentionMode: 'time', retentionHours: 6}, now);
  assert.equal(result.mode, 'time');
  assert.equal(result.removed, 1);
  const rows = await HistoryStore.readRange('raw', 'role:pv', 0, now);
  assert.deepEqual(rows.map(row => row.v), [2]);
  dom.window.close();
});

test('der Zeitmodus raeumt alle drei Stufen', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: now - 8 * HOUR, v: 1, u: 'W'}]);
  await HistoryStore.writeRollup('1m', [{series: 'role:pv', ts: now - 9 * HOUR, min: 1, max: 1, avg: 1, n: 1, u: 'W'}]);
  await HistoryStore.writeRollup('5m', [{series: 'role:pv', ts: now - 10 * HOUR, min: 1, max: 1, avg: 1, n: 1, u: 'W'}]);
  await HistoryMaintenance.evict({...config, retentionMode: 'time', retentionHours: 6}, now);
  assert.equal((await HistoryStore.readRange('raw', 'role:pv', 0, now)).length, 0);
  assert.equal((await HistoryStore.readRange('1m', 'role:pv', 0, now)).length, 0);
  assert.equal((await HistoryStore.readRange('5m', 'role:pv', 0, now)).length, 0);
  dom.window.close();
});

test('ein zweiter Zeitmodus-Lauf ohne neue Daten laeuft leer', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: now - 8 * HOUR, v: 1, u: 'W'}]);
  const timeConfig = {...config, retentionMode: 'time', retentionHours: 6};
  await HistoryMaintenance.evict(timeConfig, now);
  const second = await HistoryMaintenance.evict(timeConfig, now);
  assert.equal(second.removed, 0);
  dom.window.close();
});

test('im Speichermodus wird nichts geloescht, solange das Budget reicht', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: 1000, v: 1, u: 'W'}]);
  const result = await HistoryMaintenance.evict({...config, retentionMode: 'size', budgetMB: 512}, 40 * DAY);
  assert.equal(result.mode, 'size');
  assert.equal(result.removed, 0);
  assert.equal((await HistoryStore.readRange('raw', 'role:pv', 0, 40 * DAY)).length, 1);
  dom.window.close();
});

test('im Speichermodus faellt das Aelteste weg, bis das Konto unter die Marke sinkt', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const rows = [];
  // 2000 Saetze a 120 Byte = 240.000 Byte. Budget 0.2 MB = 209.715 Byte,
  // Zielmarke 90 % davon = 188.743 Byte.
  for (let index = 0; index < 2000; index += 1) {
    rows.push({series: 'role:pv', ts: now - (2000 - index) * 60000, v: index, u: 'W'});
  }
  await HistoryStore.writeRaw(rows);
  const budgetMB = 0.2;
  const result = await HistoryMaintenance.evict({...config, retentionMode: 'size', budgetMB}, now);
  assert.ok(result.removed > 0, 'erwartet, dass ueberhaupt evakuiert wird');
  const bytes = await HistoryStore.bytesUsed();
  assert.ok(bytes <= budgetMB * 1024 * 1024 * 0.9, `Konto ${bytes} liegt nicht unter der Zielmarke`);
  // Und zwar von vorne: die juengsten Saetze muessen ueberleben.
  const remaining = await HistoryStore.readRange('raw', 'role:pv', 0, now);
  assert.equal(remaining[remaining.length - 1].v, 1999);
  dom.window.close();
});

test('compact verdichtet eine abgeschlossene Minute schon frueh in die 1m-Stufe, ohne raw zu loeschen', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const closed = now - 5 * 60000; // laengst vorbei, aber weit innerhalb des 24h-Rohfensters
  await HistoryStore.writeRaw([
    {series: 'role:pv', ts: closed, v: 100, u: 'W'},
    {series: 'role:pv', ts: closed + 10000, v: 300, u: 'W'},
  ]);

  const result = await HistoryMaintenance.compact(config, now);
  assert.equal(result.toMinute, 0, 'die Minute ist noch nicht aelter als das Rohfenster');
  assert.equal(result.toMinuteEarly, 1, 'aber schon frueh verdichtet');
  assert.equal(result.minuteRows.length, 1, 'minuteRows traegt auch die frueh verdichteten Saetze');
  assert.equal(result.minuteRows[0].avg, 200);

  const minute = await HistoryStore.readRange('1m', 'role:pv', 0, now);
  assert.equal(minute.length, 1);
  assert.equal(minute[0].avg, 200);

  const raw = await HistoryStore.readRange('raw', 'role:pv', 0, now);
  assert.equal(raw.length, 2, 'die Rohansicht des Tages bleibt unangetastet');
  dom.window.close();
});

test('eine noch offene Minute (innerhalb der Karenzzeit) wird nicht frueh verdichtet', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const open = now - 30000; // 30s her, innerhalb EXCHANGE_GRACE_MS
  await HistoryStore.writeRaw([{series: 'role:pv', ts: open, v: 100, u: 'W'}]);

  const result = await HistoryMaintenance.compact(config, now);
  assert.equal(result.toMinuteEarly, 0);
  assert.equal((await HistoryStore.readRange('1m', 'role:pv', 0, now)).length, 0);
  assert.equal((await HistoryStore.readRange('raw', 'role:pv', 0, now)).length, 1);
  dom.window.close();
});

test('ein zweiter fruehe-Verdichtung-Lauf verdichtet dieselbe Spanne nicht erneut', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: now - 5 * 60000, v: 100, u: 'W'}]);
  await HistoryMaintenance.compact(config, now);
  const second = await HistoryMaintenance.compact(config, now);
  assert.equal(second.toMinuteEarly, 0);
  assert.equal((await HistoryStore.readRange('1m', 'role:pv', 0, now)).length, 1);
  dom.window.close();
});

test('die reguläre Verdichtung raeumt frueh verdichtete Rohdaten nach Ablauf des Rohfensters trotzdem auf', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  const ts = now - 5 * 60000;
  await HistoryStore.writeRaw([{series: 'role:pv', ts, v: 100, u: 'W'}]);
  await HistoryMaintenance.compact(config, now);
  assert.equal((await HistoryStore.readRange('raw', 'role:pv', 0, now)).length, 1, 'raw bleibt nach der fruehen Verdichtung erhalten');

  const later = now + 25 * HOUR; // die Minute liegt jetzt ausserhalb des 24h-Rohfensters
  const result = await HistoryMaintenance.compact(config, later);
  assert.equal(result.toMinute, 1, 'die regulaere Verdichtung erfasst dieselbe Minute noch einmal');
  assert.equal((await HistoryStore.readRange('raw', 'role:pv', 0, later)).length, 0, 'jetzt wird die Rohdatei wirklich geloescht');
  const minute = await HistoryStore.readRange('1m', 'role:pv', 0, later);
  assert.equal(minute.length, 1, 'kein doppelter Bucket');
  assert.equal(minute[0].avg, 100);
  dom.window.close();
});

test('run fuehrt Verdichtung und Aufbewahrung in einem Durchgang aus', async () => {
  const dom = load();
  const {HistoryStore, HistoryMaintenance} = dom.window;
  const now = 40 * DAY;
  await HistoryStore.writeRaw([{series: 'role:pv', ts: now - 30 * HOUR, v: 1, u: 'W'}]);
  const result = await HistoryMaintenance.run({...config, retentionMode: 'time', retentionHours: 48}, now);
  assert.equal(result.compacted.toMinute, 1);
  assert.equal(result.evicted.mode, 'time');
  dom.window.close();
});
