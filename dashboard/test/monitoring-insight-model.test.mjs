// Tests for the monitoring insight model (season and forecast): series, summary, charts, forecast state.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(jsDir, 'monitoring-insight-model.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.MonitoringInsight;
}

// Results come from the vm realm, so they are copied into this realm before strict comparison.
const plain = (v) => (v === undefined ? v : JSON.parse(JSON.stringify(v)));
const raw = load();
const M = Object.fromEntries(Object.entries(raw).map(([name, fn]) => [name, typeof fn === 'function' ? (...args) => plain(fn(...args)) : plain(fn)]));

const MONTHS = ['Januar', 'Februar', 'März', 'April', 'Mai', 'Juni', 'Juli', 'August', 'September', 'Oktober', 'November', 'Dezember'];

test('seasonSeries keeps gaps as null and sources', () => {
  const weeks = [
    { key: '2026-W01', source: 'stats', pv_kwh: 10 },
    { key: '2026-W03', source: 'days', pv_kwh: 30 },
  ];
  const s = M.seasonSeries(weeks, 2026, 'pv');
  assert.equal(s.length, 53);
  assert.equal(s[1].src, 'stats');
  assert.equal(s[2], null);
  assert.equal(s[3].src, 'days');
});

test('seasonSummary compares only common weeks', () => {
  const weeks = [
    { key: '2026-W01', source: 'days', pv_kwh: 10, load_kwh: 10, temp_c: 4 },
    { key: '2026-W02', source: 'days', pv_kwh: 20, load_kwh: 20, temp_c: 6 },
    { key: '2026-W03', source: 'days', pv_kwh: 30, load_kwh: 30, temp_c: 8 },
    { key: '2025-W01', source: 'days', pv_kwh: 10, load_kwh: 10, temp_c: 2 },
    { key: '2025-W03', source: 'days', pv_kwh: 20, load_kwh: 20, temp_c: 3 },
  ];
  const season = { year: 2026, week: 3, weeks };
  const pv = M.seasonSummary(season, 'pv');
  assert.equal(pv.cur, 40);
  assert.equal(pv.prev, 30);
  assert.equal(pv.skipped, 1);
  assert.ok(Math.abs(pv.diff - 33.3) < 0.1, String(pv.diff));
  assert.equal(pv.tone, 'is-up');
  assert.equal(M.seasonSummary(season, 'load').tone, 'is-down');
  // temp: Mittel 2026 (4 und 8) gegen Mittel 2025 (2 und 3), Gradunterschied
  const temp = M.seasonSummary(season, 'temp');
  assert.equal(temp.tone, '');
  assert.ok(Math.abs(temp.diff - (6 - 2.5)) < 0.01, String(temp.diff));
});

test('seasonSummary is null without a common week', () => {
  const onlyCurrent = { year: 2026, week: 3, weeks: [{ key: '2026-W01', source: 'days', pv_kwh: 10 }] };
  assert.equal(M.seasonSummary(onlyCurrent, 'pv'), null);
  assert.equal(M.seasonSummary({ year: 2026, week: 3, weeks: [] }, 'pv'), null);
});

test('forecastState learning with zero days', () => {
  const s = M.forecastState({ days: [], needed: 14 });
  assert.equal(s.kind, 'learning');
  assert.equal(s.have, 0);
  assert.equal(s.needed, 14);
  assert.equal(s.maeF, 0);
  assert.equal(s.maeM, 0);
  assert.equal(s.hits, 0);
});

test('forecastState picks the median when it is not worse', () => {
  const days = Array.from({ length: 14 }, (_, i) => ({
    day: `2026-11-${String(i + 1).padStart(2, '0')}`,
    actual_wh: 5000,
    forecast_wh: 6000,
    season_wh: 6000,
  }));
  const s = M.forecastState({ days, needed: 14 });
  assert.equal(s.kind, 'median');
  assert.equal(s.maeF, 1000);
  assert.equal(s.maeM, 1000);
  assert.equal(s.hits, 0);
  assert.equal(s.have, 14);
});

test('seasonChart returns an svg without NaN, also for an empty season', () => {
  const weeks = [{ key: '2026-W01', source: 'days', pv_kwh: 10 }, { key: '2026-W02', source: 'days', pv_kwh: 20 }];
  const full = M.seasonChart({ year: 2026, week: 2, weeks }, 'pv', 2, 600, false, MONTHS.map((m) => m.slice(0, 3)));
  assert.ok(full.svg.startsWith('<svg'));
  assert.ok(!full.svg.includes('NaN'));
  assert.ok(full.svg.includes('mi-svg'));
  const empty = M.seasonChart({ year: 2026, week: 1, weeks: [] }, 'pv', 3, 600, true, MONTHS.map((m) => m.slice(0, 3)));
  assert.ok(empty.svg.startsWith('<svg'));
  assert.ok(!empty.svg.includes('NaN'));
});

test('forecastChart returns an svg without NaN and fills empty slots', () => {
  const days = [{ day: '2026-11-01', actual_wh: 3000, forecast_wh: 3500, season_wh: 2000 }];
  const out = M.forecastChart(days, 14, 600, false);
  assert.ok(out.svg.startsWith('<svg'));
  assert.ok(!out.svg.includes('NaN'));
  assert.equal(out.svg.split('mi-slot').length - 1, 13);
  const none = M.forecastChart([], 14, 600, true);
  assert.ok(none.svg.startsWith('<svg'));
  assert.ok(!none.svg.includes('NaN'));
});
