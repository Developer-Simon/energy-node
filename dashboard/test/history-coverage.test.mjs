// dashboard/test/history-coverage.test.mjs
// Reine Rechenlogik des Verlauf-Austauschs: Rasterbau, Vergleich zweier
// Deckungsraster und die Planung der Nachfragen. Laeuft ohne jsdom - das
// Modul beruehrt weder DOM noch IndexedDB.
import { test } from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'history-coverage.js'),
  'utf8',
);

function load() {
  const context = { window: {} };
  vm.createContext(context);
  vm.runInContext(source, context);
  return context.window.HistoryCoverage;
}

const HOUR = 3600000;

test('rasterWindow rastert den Fensterbeginn auf ein Vielfaches der Schrittweite', () => {
  const coverage = load();
  const now = 1000 * HOUR + 1234;
  const window1m = coverage.rasterWindow('1m', now);
  assert.equal(window1m.stepMs, HOUR);
  assert.equal(window1m.from % HOUR, 0);
  assert.equal(window1m.to, now);
  assert.equal(window1m.buckets, Math.ceil((now - window1m.from) / HOUR));
});

test('census zaehlt Zeitstempel in ihre Rasterbuckets', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const stamps = [from + 60000, from + 120000, from + HOUR + 60000];
  const result = coverage.census('1m', stamps, now);
  assert.equal(result.from, from);
  assert.equal(result.step, HOUR);
  assert.equal(result.n[0], 2);
  assert.equal(result.n[1], 1);
});

test('missingRanges liefert die Bereiche, in denen der Peer mehr hat', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const own = { from, step: HOUR, n: [60, 0, 60, 60] };
  const peer = { from, step: HOUR, n: [60, 60, 60, 60] };
  const ranges = coverage.missingRanges(own, peer, now);
  assert.deepEqual(ranges, [[from + HOUR, from + 2 * HOUR]]);
});

test('missingRanges fasst benachbarte Buckets zu einem Bereich zusammen', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const own = { from, step: HOUR, n: [60, 0, 0, 60] };
  const peer = { from, step: HOUR, n: [60, 60, 60, 60] };
  assert.deepEqual(coverage.missingRanges(own, peer, now), [[from + HOUR, from + 3 * HOUR]]);
});

test('missingRanges laesst den Bucket aus, in den now faellt', () => {
  const coverage = load();
  // now liegt in der Mitte eines Buckets; der Peer hat dort mehr Saetze,
  // trotzdem darf daraus keine Nachfrage entstehen.
  const now = 1000 * HOUR + 1800000;
  const { from, buckets } = coverage.rasterWindow('1m', now);
  const n = new Array(buckets).fill(60);
  const own = { from, step: HOUR, n: [...n] };
  const peer = { from, step: HOUR, n: [...n] };
  own.n[buckets - 1] = 10;
  peer.n[buckets - 1] = 55;
  assert.deepEqual(coverage.missingRanges(own, peer, now), []);
});

test('plan waehlt je Serie den Peer mit dem groessten Ueberschuss', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const own = { '1m': { 'role:pv': { from, step: HOUR, n: [60, 0, 0, 60] } } };
  const offers = {
    'p-schwach': { '1m': { 'role:pv': { from, step: HOUR, n: [60, 60, 0, 60] } } },
    'p-stark': { '1m': { 'role:pv': { from, step: HOUR, n: [60, 60, 60, 60] } } },
  };
  const jobs = coverage.plan(own, offers, now);
  assert.equal(jobs.length, 1);
  assert.equal(jobs[0].peer, 'p-stark');
  assert.equal(jobs[0].tier, '1m');
  assert.equal(jobs[0].series, 'role:pv');
  assert.deepEqual(jobs[0].ranges, [[from + HOUR, from + 3 * HOUR]]);
});

test('plan liefert nichts, wenn kein Peer mehr hat als man selbst', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const own = { '1m': { 'role:pv': { from, step: HOUR, n: [60, 60, 60, 60] } } };
  const offers = { 'p-x': { '1m': { 'role:pv': { from, step: HOUR, n: [60, 60, 0, 60] } } } };
  assert.deepEqual(coverage.plan(own, offers, now), []);
});

test('plan kennt Serien, die man selbst gar nicht hat', () => {
  const coverage = load();
  const now = 1000 * HOUR;
  const { from } = coverage.rasterWindow('1m', now);
  const offers = { 'p-x': { '1m': { 'role:wallbox': { from, step: HOUR, n: [60, 60, 0, 0] } } } };
  const jobs = coverage.plan({}, offers, now);
  assert.equal(jobs.length, 1);
  assert.equal(jobs[0].series, 'role:wallbox');
  assert.deepEqual(jobs[0].ranges, [[from, from + 2 * HOUR]]);
});
