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

// Learned values as the endpoint delivers them: key, value, n, optional year list and device fields.
const lv = (key, value, n = 12, extra = {}) => ({ key, value, n, ...extra });

test('dayRows splits windows over midnight', () => {
  const values = [
    lv('site/night_rest', '23:15', 12),
    lv('site/day_start', '06:30', 12),
    lv('device/w1/usual_on', '22:00', 12, { device_name: 'Waschmaschine', device_icon: 'ico-wash' }),
    lv('device/w1/usual_off', '02:00', 12, { device_name: 'Waschmaschine', device_icon: 'ico-wash' }),
  ];
  const rows = M.dayRows(values);
  assert.equal(rows[0].id, 'house');
  assert.equal(rows[0].kind, 'night');
  assert.equal(rows[0].sure, true);
  assert.equal(rows[0].name, null);
  assert.equal(rows[0].nameKey, 'monitoring.day.house');
  const dev = rows.find((r) => r.id === 'w1');
  assert.equal(dev.kind, 'on');
  assert.equal(dev.name, 'Waschmaschine');
  assert.equal(dev.icon, 'ico-wash');
  assert.equal(dev.segs.length, 2);
  assert.equal(dev.sure, true);
  assert.equal(M.onAt(rows, 60).on.includes('Waschmaschine'), true);
  assert.equal(M.onAt(rows, 600).on.includes('Waschmaschine'), false);
  assert.equal(M.onAt(rows, 0).night, true);
  assert.equal(M.onAt(rows, 600).night, false);
});

test('dayRows point and free rows', () => {
  const point = M.dayRows([lv('device/p1/usual_on', '07:00', 4, { device_name: 'Kaffee' })]);
  const pr = point.find((r) => r.id === 'p1');
  assert.equal(pr.kind, 'point');
  assert.equal(pr.sure, false);
  assert.ok(Math.abs(pr.segs[0].width - 15 / 14.4) < 1e-9, String(pr.segs[0].width));

  const offOnly = M.dayRows([lv('device/p2/usual_off', '18:00', 12, { device_name: 'Pumpe' })]);
  assert.equal(offOnly.find((r) => r.id === 'p2').kind, 'point');

  const free = M.dayRows([lv('device/f1/usual_runtime_s', 2400, 12, { device_name: 'Rasenmäher' })]);
  const fr = free.find((r) => r.id === 'f1');
  assert.equal(fr.kind, 'free');
  assert.equal(fr.minutes, 40);
  assert.deepEqual(fr.segs, []);
});

test('dayRows with empty values returns one free house row without minutes', () => {
  const rows = M.dayRows([]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].id, 'house');
  assert.equal(rows[0].kind, 'free');
  assert.equal(rows[0].minutes, null);
});

test('runRows derives the duration', () => {
  const values = [
    lv('device/d1/cycle_wh', 900, 12, { device_name: 'Spülmaschine', programs: [490, 1350] }),
    lv('device/d1/op_power_w', 1800, 12, { device_name: 'Spülmaschine' }),
  ];
  const rows = M.runRows(values);
  assert.equal(rows.length, 1);
  const r = rows[0];
  assert.equal(r.seconds, 1800);
  assert.equal(r.wh, 900);
  assert.equal(r.powerW, 1800);
  assert.equal(r.sure, true);
  assert.ok(Math.abs(r.ticks[0] - 490 / 1350 * 100) < 0.01, String(r.ticks[0]));
  assert.equal(r.ticks[1], 100);
});

test('yearBand needs four weeks', () => {
  const week = (w, v) => ({ week: `2026-W${String(w).padStart(2, '0')}`, value: v });
  const on3 = [week(1, '22:00'), week(2, '22:00'), week(3, '22:00')];
  const off3 = [week(1, '02:00'), week(2, '02:00'), week(3, '02:00')];
  assert.equal(M.yearBand({ year: on3 }, { year: off3 }), null);
  const on4 = [...on3, week(4, '22:00')];
  const off4 = [...off3, week(4, '02:00')];
  const band = M.yearBand({ year: on4 }, { year: off4 });
  assert.ok(band);
  assert.ok(band.svg.includes('mi-yr-band'));
  assert.ok(band.svg.includes('mi-yr-line'));
  assert.ok(!band.svg.includes('NaN'));
});

test('dayRows house row without locale keeps the name key and no name', () => {
  const rows = M.dayRows([lv('site/night_rest', '23:15'), lv('site/day_start', '06:30')]);
  assert.equal(rows[0].kind, 'night');
  assert.equal(rows[0].name, null);
  assert.equal(rows[0].nameKey, 'monitoring.day.house');
  const free = M.dayRows([]);
  assert.equal(free[0].name, null);
  assert.equal(free[0].nameKey, 'monitoring.day.house');
});

test('dayRows sorts devices by name with the given locale', () => {
  const values = [
    lv('device/b/usual_runtime_s', 600, 12, { device_name: 'Zisterne' }),
    lv('device/a/usual_runtime_s', 600, 12, { device_name: 'Ahorn' }),
  ];
  const withLocale = M.dayRows(values, 'de').slice(1).map((r) => r.name);
  assert.deepEqual(withLocale, ['Ahorn', 'Zisterne']);
  const noLocale = M.dayRows(values).slice(1).map((r) => r.name);
  assert.deepEqual(noLocale, ['Ahorn', 'Zisterne']);
});

test('dayRows with equal on and off minute is a point, not a full day', () => {
  const rows = M.dayRows([
    lv('device/e1/usual_on', '12:00', 12, { device_name: 'Gleich' }),
    lv('device/e1/usual_off', '12:00', 12, { device_name: 'Gleich' }),
  ]);
  const r = rows.find((x) => x.id === 'e1');
  assert.equal(r.kind, 'point');
  assert.equal(r.segs.length, 1);
  assert.ok(Math.abs(r.segs[0].width - 15 / 14.4) < 1e-9, String(r.segs[0].width));
  assert.equal(M.onAt(rows, 12 * 60 + 5).on.includes('Gleich'), false);
});
