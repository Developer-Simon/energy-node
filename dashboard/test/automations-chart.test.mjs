// Reine Geometrie fuer die Szenario-Karten und die Regel-Zeitleisten:
// Zeitfenster, Y-Achse mit Toleranzband, Linien mit Luecken und Marker.
// automations-chart.js hat kein eigenes DOM, daher laedt der Test es
// zusammen mit automations-view.js (historyResultInfo fuer die Toene).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-view.js'), 'utf8'), context);
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-chart.js'), 'utf8'), context);
  return dom.window.__automationChart;
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich
// einmal durch JSON laufen lassen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const baseChart = (overrides = {}) => ({
  grid: [], output: [], target: 0, deadband: 0, events: [],
  from: 0, to: 600, width: 600, height: 100,
  ...overrides,
});

test('rangeBounds today starts at local midnight', () => {
  const chart = load();
  const nowMs = new Date(2026, 9, 23, 15, 30).getTime();
  const midnight = new Date(2026, 9, 23, 0, 0, 0, 0).getTime();
  const bounds = plain(chart.rangeBounds('today', nowMs));
  assert.equal(bounds.from, midnight / 1000);
  assert.equal(bounds.to, nowMs / 1000);
});

test('rangeBounds 30m and 6h end at now', () => {
  const chart = load();
  const nowMs = 1_800_000_000_000;
  assert.deepEqual(plain(chart.rangeBounds('30m', nowMs)), { from: 1_800_000_000 - 1800, to: 1_800_000_000 });
  assert.deepEqual(plain(chart.rangeBounds('6h', nowMs)), { from: 1_800_000_000 - 21600, to: 1_800_000_000 });
  assert.deepEqual(plain(chart.CHART_RANGES), ['30m', '6h', 'today']);
});

test('scenarioChart maps the band around the target', () => {
  const chart = load();
  const result = plain(chart.scenarioChart(baseChart({
    grid: [{ ts: 0, value: 30 }], target: 30, deadband: 20,
  })));
  assert.ok(result.band.height > 0);
  assert.ok(result.band.y >= 0);
  assert.ok(result.band.y + result.band.height <= 100);
  // Das Band 10..50 W liegt im Bild, die Achse hat 10 % Rand um -100..100 W.
  assert.ok(Math.abs(result.band.y - 29.17) < 0.1);
  assert.ok(Math.abs(result.band.height - 16.67) < 0.1);
});

test('scenarioChart breaks the line on a gap over three minutes', () => {
  const chart = load();
  const result = plain(chart.scenarioChart(baseChart({
    grid: [{ ts: 0, value: 10 }, { ts: 60, value: 20 }, { ts: 300, value: 30 }],
  })));
  assert.equal((result.gridPath.match(/M/g) || []).length, 2);
  assert.equal((result.gridPath.match(/L/g) || []).length, 1);
  assert.equal(result.outputPath, '');
});

test('scenarioChart reports empty without data', () => {
  const chart = load();
  const empty = plain(chart.scenarioChart(baseChart()));
  assert.equal(empty.empty, true);
  assert.equal(empty.gridPath, '');
  assert.equal(empty.outputPath, '');
  assert.deepEqual(empty.markers, []);
  const filled = plain(chart.scenarioChart(baseChart({ output: [{ ts: 10, value: 400 }] })));
  assert.equal(filled.empty, false);
  assert.notEqual(filled.outputPath, '');
});

test('scenarioChart places markers inside the range only', () => {
  const chart = load();
  const result = plain(chart.scenarioChart(baseChart({
    from: 100, to: 600,
    events: [
      { at: 50, result: 'fired' },
      { at: 300, result: 'blocked' },
      { at: 700, result: 'fired' },
    ],
  })));
  assert.equal(result.markers.length, 1);
  assert.equal(result.markers[0].kind, 'blocked');
});

test('scenarioChart keeps the y range around zero with at least 100 W', () => {
  const chart = load();
  const result = plain(chart.scenarioChart(baseChart({
    grid: [{ ts: 0, value: 5 }], target: 0, deadband: 0,
  })));
  // Der Ziffernblock enthaelt 0 und liegt innerhalb des Bildes.
  const zero = result.yTicks.find((tick) => tick.label === '0');
  assert.ok(zero);
  assert.ok(zero.y > 0 && zero.y < 100);
});

const lastTriggers = (...args) => load().lastTriggers(...args);

test('lastTriggers keeps the four newest and spans the oldest of them', () => {
  const now = 1_000_000;
  const ev = [12, 47, 51, 138, 290].map((m) => ({ at: now - m * 60, result: 'fired' }));
  const r = lastTriggers(ev, now);
  assert.equal(r.marks.length, 4);
  assert.equal(r.empty, false);
  assert.ok(Math.abs(r.marks[0].x - (100 - (12 / (138 * 1.12)) * 92 - 4) / 100) < 1e-9);
  assert.equal(r.marks.filter((m) => m.showLabel).length, 3); // 47 und 51 liegen zu eng
});

test('lastTriggers uses at least 30 minutes', () => {
  const now = 1_000_000;
  const r = lastTriggers([{ at: now - 60, result: 'fired' }], now);
  assert.ok(Math.abs(r.marks[0].x - (100 - (1 / 30) * 92 - 4) / 100) < 1e-9);
});

test('lastTriggers without events is empty', () => {
  assert.deepEqual(plain(lastTriggers([], 1).marks), []);
  assert.equal(lastTriggers([], 1).empty, true);
});

test('lastTriggers labels clock times and tones the marks', () => {
  process.env.TZ = 'UTC';
  const now = 1_000_000;
  const r = plain(lastTriggers([
    { at: now - 600, result: 'blocked' },
    { at: now - 3600, result: 'fired' },
  ], now));
  assert.equal(r.marks[0].tone, 'bad');
  assert.equal(r.marks[1].tone, 'ok');
  // 1_000_000 s ist 13:46:40 UTC, also now - 600 s = 13:36 und now - 3600 s = 12:46.
  assert.equal(r.marks[0].label, '13:36');
  assert.equal(r.sinceLabel, '12:46');
  assert.equal(r.marks[0].showLabel, true);
});

test('triggerTimeline colours fired, blocked and scenario events', () => {
  const chart = load();
  const timeline = plain(chart.triggerTimeline([
    { at: 0, result: 'fired' },
    { at: 10, result: 'blocked' },
    { at: 20, result: 'started' },
    { at: 30, result: 'recovered' },
    { at: 40, result: 'ended' },
    { at: 50, result: 'fallback' },
    { at: 60, result: 'jump' },
    { at: 900, result: 'fired' },
  ], 0, 100, 100));
  assert.deepEqual(timeline, [
    { x: 0, tone: 'ok' },
    { x: 10, tone: 'bad' },
    { x: 20, tone: 'ok' },
    { x: 30, tone: 'ok' },
    { x: 40, tone: 'off' },
    { x: 50, tone: 'bad' },
    { x: 60, tone: 'info' },
  ]);
});

test('bandPreview centres the target and marks a value outside the band', () => {
  const chart = load();
  const preview = plain(chart.bandPreview({ target: 30, deadband: 20, net: 104 }));
  assert.equal(preview.target, 50);
  assert.equal(preview.inside, false);
  assert.ok(preview.now > preview.target);
  assert.equal(preview.zone.left, 45);
  assert.equal(preview.zone.width, 10);
});

test('bandPreview widens the axis for far values', () => {
  const chart = load();
  const preview = plain(chart.bandPreview({ target: 30, deadband: 20, net: 900 }));
  assert.ok(preview.now <= 100);
  assert.equal(preview.inside, false);
});

test('sparkline draws the band and breaks gaps', () => {
  const chart = load();
  const result = plain(chart.sparkline({
    points: [{ ts: 0, value: 10 }, { ts: 60, value: 20 }, { ts: 300, value: 30 }],
    target: 20, deadband: 10, from: 0, to: 300, width: 150, height: 34,
  }));
  assert.equal(result.empty, false);
  assert.equal((result.path.match(/M/g) || []).length, 2);
  assert.equal((result.path.match(/L/g) || []).length, 1);
  assert.ok(result.band.height > 0);
  assert.ok(result.band.y >= 0);
  assert.ok(result.band.y + result.band.height <= 34);
});

test('sparkline is empty without points', () => {
  const chart = load();
  const result = plain(chart.sparkline({ points: [], target: 0, deadband: 0, from: 0, to: 300, width: 150, height: 34 }));
  assert.equal(result.empty, true);
  assert.equal(result.path, '');
  assert.ok(Number.isFinite(result.band.y));
  assert.ok(Number.isFinite(result.band.height));
});
