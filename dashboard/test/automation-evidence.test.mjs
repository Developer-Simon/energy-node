// Tests for the before and after graphic of rule findings: pure string rendering, no text inside SVG.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const source = fs.readFileSync(path.join(jsDir, 'automation-evidence.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  // Das Skript laeuft im Kontext ohne Timer, der Einlauf braucht setTimeout.
  dom.window.setTimeout = setTimeout;
  vm.runInContext(source, dom.getInternalVMContext());
  return { api: dom.window.AutomationEvidence, document: dom.window.document };
}

const t = (key) => key;

function mount(document, html) {
  const host = document.createElement('div');
  host.innerHTML = html;
  return host;
}

const axisLabels = (host) => Array.from(host.querySelectorAll('.auto-learn-axis-label')).map((s) => s.textContent);
const bars = (host) => Array.from(host.querySelectorAll('path.auto-learn-bar'));

test('events shape draws a histogram bar per occupied bin', () => {
  const { api, document } = load();
  const from = 1700000000;
  const to = from + 14400;
  const html = api.render({
    shape: 'events',
    from,
    to,
    ist: [from + 1000, from + 1100, from + 9000],
    soll_spans: [[from + 1000, from + 9000]],
  }, t);
  const host = mount(document, html);
  assert.equal(bars(host).length, 2, 'two occupied bins of 600 s');
  assert.equal(host.querySelectorAll('rect.auto-learn-soll').length, 1);
  assert.equal(host.querySelectorAll('line.auto-learn-mark-event').length, 0, 'events are bars now');
  assert.ok(host.querySelectorAll('.auto-learn-axis-label').length >= 3);
});

test('events histogram gives the busiest bin full height and others at least 30 %', () => {
  const { api, document } = load();
  const from = 1700000000;
  const to = from + 14400;
  const busy = [1000, 1010, 1020, 1030];
  const html = api.render({ shape: 'events', from, to, ist: busy.map((s) => from + s).concat([from + 9000]), soll_spans: [] }, t);
  const host = mount(document, html);
  const heights = bars(host).map((b) => Number(b.getAttribute('data-h')));
  assert.equal(Math.max(...heights), 1);
  assert.ok(Math.min(...heights) >= 0.3, 'single event stays visible');
  assert.equal(heights.length, 2);
});

test('events shape writes the axis as HH:MM clock times with edge alignment', () => {
  const { api, document } = load();
  const from = 1700000000;
  const html = api.render({ shape: 'events', from, to: from + 14400, ist: [], soll_spans: [] }, t);
  const host = mount(document, html);
  const labels = axisLabels(host);
  assert.equal(labels.length, 3);
  labels.forEach((label) => assert.match(label, /^\d{2}:\d{2}$/));
  assert.ok(host.querySelector('.auto-learn-axis-label.is-start'));
  assert.ok(host.querySelector('.auto-learn-axis-label.is-end'));
});

test('events shape with from equal to produces no NaN and no bars', () => {
  const { api, document } = load();
  const at = 1700000000;
  const html = api.render({ shape: 'events', from: at, to: at, ist: [at], soll_spans: [[at, at]] }, t);
  assert.ok(!html.includes('NaN'));
  const host = mount(document, html);
  assert.equal(bars(host).length, 0);
});

test('scale shape puts the threshold line at ist_value / max in percent', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'scale',
    seen: [100, 400],
    ist_value: 300,
    soll_value: 500,
    max: 600,
    unit: 'W',
  }, t);
  const host = mount(document, html);
  const threshold = host.querySelector('line.auto-learn-mark-threshold');
  assert.ok(threshold, 'threshold line exists');
  assert.equal(Number(threshold.getAttribute('x1')).toFixed(4), ((300 / 600) * 100).toFixed(4));
  const soll = host.querySelector('line.auto-learn-mark-soll');
  assert.equal(Number(soll.getAttribute('x1')).toFixed(4), ((500 / 600) * 100).toFixed(4));
  assert.deepEqual(axisLabels(host), ['0 W', '300 W', '600 W']);
});

test('scale shape draws an Ist bar for ist_value only and a Soll bar for soll_value', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'scale', ist_value: 360, soll_value: 90, unit: 'min' }, t);
  const host = mount(document, html);
  const istBar = host.querySelector('rect.auto-learn-ist');
  assert.ok(istBar, 'Ist bar exists');
  assert.equal(host.querySelectorAll('.auto-learn-ist').length, 1, 'no seen band without seen');
  // 1.2 * 360 = 432 rounds up to 480 minutes
  assert.equal(Number(istBar.getAttribute('x')), 0);
  assert.equal(Number(istBar.getAttribute('width')).toFixed(3), ((360 / 480) * 100).toFixed(3));
  const sollBar = host.querySelector('rect.auto-learn-soll');
  assert.equal(Number(sollBar.getAttribute('width')).toFixed(3), ((90 / 480) * 100).toFixed(3));
  assert.deepEqual(axisLabels(host), ['0 min', '240 min', '480 min']);
});

test('scale shape without max falls back to a usable scale', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'scale', seen: [0, 200], ist_value: 300 }, t);
  const host = mount(document, html);
  const threshold = host.querySelector('line.auto-learn-mark-threshold');
  assert.ok(threshold);
  const x = Number(threshold.getAttribute('x1'));
  assert.ok(Number.isFinite(x) && x >= 0 && x <= 100, 'threshold stays inside the lane');
});

test('links shape with Missing marks the target as dashed', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'links',
    target: 'sensor.gone',
    missing: true,
    soll_names: ['Abendlicht'],
  }, t);
  const host = mount(document, html);
  assert.ok(host.querySelector('.is-missing'));
});

test('links shape shows several Ist chips on one target', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'links',
    target: 'switch.pump',
    ist_names: ['Regel A', 'Regel B'],
    soll_names: ['Szene'],
  }, t);
  const host = mount(document, html);
  const chips = Array.from(host.querySelectorAll('.auto-learn-chip')).map((c) => c.textContent);
  assert.ok(chips.includes('Regel A'));
  assert.ok(chips.includes('Regel B'));
  assert.ok(chips.includes('Szene'));
  assert.ok(html.includes('switch.pump'));
});

test('links shape without soll names shows only the target on the Soll lane', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'links', target: 'switch.pump', ist_names: ['Regel A'] }, t);
  const host = mount(document, html);
  const soll = host.querySelector('.auto-learn-lane.is-soll');
  assert.ok(soll);
  assert.equal(soll.querySelectorAll('.auto-learn-chip').length, 0);
  assert.match(soll.textContent, /switch\.pump/);
});

test('rule names are escaped and never become markup', () => {
  const { api, document } = load();
  const evil = '<img src=x onerror="alert(1)"><b>Regel</b>';
  const html = api.render({ shape: 'links', target: 'x.y', ist_names: [evil], soll_names: [evil] }, t);
  const host = mount(document, html);
  assert.equal(host.querySelectorAll('img').length, 0);
  assert.equal(host.querySelectorAll('b').length, 0);
  const chips = host.querySelectorAll('.auto-learn-chip');
  assert.equal(chips[0].textContent, evil);
});

test('rendered graphics contain no text element inside SVG', () => {
  const { api, document } = load();
  const shapes = [
    { shape: 'events', from: 1700000000, to: 1700014400, ist: [1700001000], soll_spans: [[1700001000, 1700002000]] },
    { shape: 'scale', seen: [0, 200], ist_value: 100, soll_value: 150, max: 300, unit: 'W' },
    { shape: 'links', target: 'a.b', ist_names: ['R'], soll_names: ['S'] },
    { shape: 'day', from: 0, to: 1440, ist: [1435, 0], ist_spans: [[420, 1350]], soll: [0], soll_spans: [[0, 60]] },
  ];
  shapes.forEach((ev) => {
    const host = mount(document, api.render(ev, t));
    assert.equal(host.querySelectorAll('svg text').length, 0, ev.shape);
  });
});

test('full day axis puts minute 1435 and minute 0 on the two edges and labels 00:00 to 24:00', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'day',
    ist: [1435, 0],
    soll: [0, 1435],
    ist_spans: [],
    soll_spans: [],
  }, t, { full: true });
  const host = mount(document, html);
  const ist = bars(host);
  assert.equal(ist.length, 2);
  const first = ist.find((b) => Number(b.getAttribute('data-x')) < 1);
  const last = ist.find((b) => Number(b.getAttribute('data-x')) > 90);
  assert.ok(first, 'bar at minute 0 starts on the left edge');
  assert.ok(last, 'bar at minute 1435 sits on the right edge');
  assert.ok(Number(last.getAttribute('data-x')) + Number(last.getAttribute('data-w')) <= 100.0001);
  const soll = Array.from(host.querySelectorAll('line.auto-learn-mark-soll')).map((m) => Number(m.getAttribute('x1')));
  assert.deepEqual(soll.map((x) => x.toFixed(3)), ['0.000', '99.653']);
  assert.deepEqual(axisLabels(host), ['00:00', '06:00', '12:00', '18:00', '24:00']);
});

test('day shape draws Ist bands from ist_spans and Soll bands from soll_spans', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'day',
    from: 0,
    to: 1440,
    ist: [],
    ist_spans: [[420, 1350]],
    soll: [],
    soll_spans: [[0, 1440]],
  }, t, { full: true });
  const host = mount(document, html);
  const istRect = host.querySelector('rect.auto-learn-ist');
  assert.ok(istRect, 'ist band exists');
  assert.ok(istRect.classList.contains('auto-learn-soft'), 'ist band is translucent');
  assert.equal(Number(istRect.getAttribute('x')).toFixed(3), ((420 / 1440) * 100).toFixed(3));
  assert.equal(Number(istRect.getAttribute('width')).toFixed(3), ((930 / 1440) * 100).toFixed(3));
  const sollRect = host.querySelector('rect.auto-learn-soll');
  assert.equal(Number(sollRect.getAttribute('x')), 0);
  assert.equal(Number(sollRect.getAttribute('width')), 100);
});

test('full day clamps Ist bands that lie outside the axis', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [], ist_spans: [[1300, 1600]], soll: [], soll_spans: [] }, t, { full: true });
  const host = mount(document, html);
  const rect = host.querySelector('rect.auto-learn-ist');
  assert.ok(Number(rect.getAttribute('x')) >= 0);
  assert.ok(Number(rect.getAttribute('x')) + Number(rect.getAttribute('width')) <= 100.0001);
});

test('full day wraps minutes outside 0 to 1440 onto the axis', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [-30, 1500], soll: [], soll_spans: [], ist_spans: [] }, t, { full: true });
  const host = mount(document, html);
  const xs = bars(host).map((b) => Number(b.getAttribute('data-x')));
  assert.equal(xs.length, 2);
  assert.ok(Math.abs(xs[0] - (1410 / 1440) * 100) < 0.5, 'minus 30 is 23:30');
  assert.ok(Math.abs(xs[1] - (60 / 1440) * 100) < 0.5, '1500 is 01:00');
});

test('day histogram uses 10 minute bins on the full day and draws reference lines behind the bars', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [600, 605, 1200], soll: [], soll_spans: [], ist_spans: [] }, t, { full: true });
  const host = mount(document, html);
  assert.equal(bars(host).length, 2, '600 and 605 share a bin');
  const grids = host.querySelectorAll('line.auto-learn-grid');
  assert.ok(grids.length >= 5, 'major reference lines in both lanes');
  const svg = host.querySelector('.auto-learn-lane-svg');
  assert.ok(svg.firstElementChild.classList.contains('auto-learn-grid'), 'grid is drawn first, so behind the bars');
  assert.ok(host.querySelector('.auto-learn-ticks'), 'axis row carries the minor ticks');
});

test('day histogram gives a single event at least 30 % height', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [100, 100, 100, 100, 700], soll: [], soll_spans: [], ist_spans: [] }, t, { full: true });
  const heights = bars(mount(document, html)).map((b) => Number(b.getAttribute('data-h')));
  assert.equal(Math.max(...heights), 1);
  assert.ok(Math.min(...heights) >= 0.3);
});

test('windowOf centres a midnight cluster and returns a window around 00:00', () => {
  const { api } = load();
  const win = api.windowOf({ shape: 'day', ist: [1435, 5], soll: [1437], ist_spans: [], soll_spans: [] });
  assert.equal(win.full, false);
  assert.ok(win.from < 0 || win.to > 1440, `window ${win.from}..${win.to} crosses midnight`);
  assert.ok(win.to - win.from >= 120);
  assert.equal(win.from % 30 === 0, true);
  assert.equal(win.to % 30, 0);
});

test('midnight cluster draws both events and the Soll mark inside the zoomed window', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [1435, 5], soll: [1437], ist_spans: [], soll_spans: [] }, t);
  const host = mount(document, html);
  assert.equal(bars(host).length, 2, 'both events drawn');
  assert.equal(host.querySelectorAll('line.auto-learn-mark-soll').length, 1);
  const labels = axisLabels(host);
  assert.ok(labels.length >= 2);
  labels.forEach((label) => assert.match(label, /^\d{2}:\d{2}$/));
});

test('windowOf of values spread over the whole day returns the full day', () => {
  const { api } = load();
  const win = api.windowOf({ shape: 'day', ist: [0, 360, 720, 1080], soll: [], ist_spans: [], soll_spans: [] });
  assert.deepEqual({ ...win }, { from: 0, to: 1440, full: true });
  assert.equal(api.zoomable({ shape: 'day', ist: [0, 360, 720, 1080], soll: [], ist_spans: [], soll_spans: [] }), false);
});

test('windowOf without any values returns the full day', () => {
  const { api } = load();
  assert.deepEqual({ ...api.windowOf({ shape: 'day', ist: [], soll: [] }) }, { from: 0, to: 1440, full: true });
});

test('windowOf widens to contain every Soll mark and span', () => {
  const { api } = load();
  const win = api.windowOf({ shape: 'day', ist: [420, 430, 440], soll: [400], ist_spans: [], soll_spans: [[600, 630]] });
  assert.equal(win.full, false);
  assert.ok(win.from <= 400 && win.to >= 630, `window ${win.from}..${win.to}`);
});

test('zoomable is true for a clustered day and false for scale, events and a full day', () => {
  const { api } = load();
  assert.equal(api.zoomable({ shape: 'day', ist: [420, 430, 440], soll: [], ist_spans: [], soll_spans: [] }), true);
  assert.equal(api.zoomable({ shape: 'scale', ist_value: 300, soll_value: 60 }), false);
  assert.equal(api.zoomable({ shape: 'events', from: 1700000000, to: 1700014400, ist: [], soll_spans: [] }), false);
  assert.equal(api.zoomable({ shape: 'day', ist: [0, 720], soll: [], ist_spans: [], soll_spans: [] }), false);
});

test('zoomed axis labels are HH:MM with minor ticks', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'day', ist: [420, 430, 440], soll: [], ist_spans: [], soll_spans: [] }, t);
  const host = mount(document, html);
  const labels = axisLabels(host);
  assert.ok(labels.length >= 3);
  labels.forEach((label) => assert.match(label, /^\d{2}:\d{2}$/));
  assert.ok(host.querySelectorAll('.auto-learn-ticks line').length > labels.length, 'minor ticks exist');
});

test('unknown shape or missing evidence renders an empty string', () => {
  const { api } = load();
  assert.equal(api.render({ shape: 'unknown' }, t), '');
  assert.equal(api.render(null, t), '');
  assert.equal(api.render(undefined, t), '');
});

const quarters = (fn) => Array.from({ length: 96 }, (_, i) => fn(i));

test('curve shape draws exactly one dashed line, the best curve, in the Soll layer', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'curve',
    ist_q: quarters((i) => (i < 40 ? 0 : 900)),
    soll_q: quarters((i) => Math.sin((i / 96) * Math.PI) * 1000),
    best_q: quarters((i) => Math.sin((i / 96) * Math.PI) * 1000),
  }, t);
  const host = mount(document, html);
  const dashed = host.querySelectorAll('polyline[stroke-dasharray]');
  assert.equal(dashed.length, 1);
  assert.equal(dashed[0].classList.contains('auto-learn-best'), true);
  assert.equal(dashed[0].closest('.auto-learn-plot-svg.is-soll') !== null, true);
  assert.equal(dashed[0].getAttribute('points').trim().split(/\s+/).length, 96);
  assert.equal(host.querySelectorAll('polyline.auto-learn-ist').length, 1);
});

test('curve shape axis runs from 0 to 24 h with 0, 6, 12, 18 and 24 as labels', () => {
  const { api, document } = load();
  const host = mount(document, api.render({ shape: 'curve', ist_q: quarters(() => 500) }, t));
  assert.deepEqual(axisLabels(host), ['00:00', '06:00', '12:00', '18:00', '24:00']);
  assert.ok(host.querySelector('.auto-learn-axis-label.is-start'));
  assert.ok(host.querySelector('.auto-learn-axis-label.is-end'));
});

test('curve shape fills only where the best curve lies above the Ist curve', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'curve',
    ist_q: quarters((i) => (i >= 10 && i < 20 ? 100 : 500)),
    best_q: quarters(() => 500),
  }, t);
  const host = mount(document, html);
  const areas = host.querySelectorAll('polygon.auto-learn-throttled');
  assert.equal(areas.length, 1, 'one run of ten quarters');
  assert.equal(areas[0].getAttribute('points').trim().split(/\s+/).length, 20, 'best forward, Ist back');
  assert.ok(!html.includes('NaN'));
});

test('curve shape with missing or empty arrays renders nothing and does not throw', () => {
  const { api } = load();
  assert.equal(api.render({ shape: 'curve' }, t), '');
  assert.equal(api.render({ shape: 'curve', ist_q: null, soll_q: 'x', best_q: [] }, t), '');
  const html = api.render({ shape: 'curve', ist_q: [1, 'x', NaN, 300], best_q: null }, t);
  assert.ok(!html.includes('NaN'));
  assert.ok(html.length > 0);
});

test('prototype keys are not shapes', () => {
  const { api } = load();
  ['constructor', 'toString', 'hasOwnProperty', 'valueOf', '__proto__'].forEach((shape) => {
    assert.equal(api.render({ shape }, t), '', shape);
  });
});

test('curve shape puts Ist and Soll into one plot as high as two lanes', () => {
  const { api, document } = load();
  const host = mount(document, api.render({
    shape: 'curve',
    unit: '%',
    ist_q: quarters((i) => (i < 40 ? 10 : 97)),
    soll_q: quarters((i) => (i < 48 ? 10 + i : 97)),
  }, t));
  assert.equal(host.querySelectorAll('.auto-learn-plot').length, 1);
  assert.equal(host.querySelectorAll('.auto-learn-lane').length, 0, 'no separate lanes');
  const svgs = host.querySelectorAll('.auto-learn-plot-area > svg.auto-learn-plot-svg');
  assert.equal(svgs.length, 2);
  svgs.forEach((svg) => assert.equal(svg.getAttribute('viewBox'), '0 0 100 20'));
  assert.equal(svgs[1].classList.contains('is-soll'), true);
  assert.equal(svgs[1].querySelectorAll('polyline.auto-learn-soll-line').length, 1);
  assert.equal(svgs[0].querySelectorAll('polyline.auto-learn-ist').length, 1);
  assert.equal(host.querySelectorAll('.auto-learn-plot-key').length, 2);
});

test('curve shape without Soll draws only the Ist layer and one legend key', () => {
  const { api, document } = load();
  const host = mount(document, api.render({ shape: 'curve', ist_q: quarters(() => 50) }, t));
  assert.equal(host.querySelectorAll('svg.auto-learn-plot-svg').length, 1);
  assert.equal(host.querySelectorAll('.auto-learn-plot-key').length, 1);
});

test('scale shape with scales draws one Ist and Soll bar pair per parameter', () => {
  const { api, document } = load();
  const html = api.render({
    shape: 'scale',
    ist_value: 0.8,
    soll_value: 0.2,
    scales: [
      { name: 'gain', ist: 0.8, soll: 0.2 },
      { name: 'deadband_w', ist: 50, soll: 10, unit: 'W' },
    ],
  }, t);
  const host = mount(document, html);
  const istBars = host.querySelectorAll('rect.auto-learn-ist');
  assert.equal(istBars.length, 2, 'one Ist bar per parameter');
  assert.equal(host.querySelectorAll('rect.auto-learn-soll').length, 2, 'one Soll bar per parameter');
  const names = Array.from(host.querySelectorAll('.auto-learn-scale-bar')).map((n) => n.textContent);
  assert.deepEqual(names, ['automations.learn.param.gain', 'automations.learn.param.deadband_w']);
  const deadbandIst = istBars[1];
  // the deadband bar scales on its own maximum, 50 * 1.2 = 60 W
  assert.equal(Number(deadbandIst.getAttribute('width')).toFixed(3), ((50 / 60) * 100).toFixed(3));
  assert.deepEqual(axisLabels(host).slice(-1), ['60 W']);
});

test('scale shape without scales keeps the single pair', () => {
  const { api, document } = load();
  const html = api.render({ shape: 'scale', ist_value: 360, soll_value: 90, unit: 'min' }, t);
  const host = mount(document, html);
  assert.equal(host.querySelectorAll('.auto-learn-scale-bar').length, 0);
  assert.equal(host.querySelectorAll('rect.auto-learn-ist').length, 1);
});

test('events shape draws the replayed triggers of the Soll lane as bars', () => {
  const { api, document } = load();
  const from = 1_000_000;
  const html = api.render({ shape: 'events', from, to: from + 14400, ist: [from + 600, from + 620], soll: [from + 3000, from + 3100], soll_spans: [] }, t);
  const host = mount(document, html);
  const sollLane = host.querySelector('.auto-learn-lane.is-soll');
  assert.ok(sollLane, 'a Soll lane exists');
  assert.ok(sollLane.querySelectorAll('path.auto-learn-bar').length >= 1, 'replayed triggers are drawn');
});

// Die Balken einer Skala tragen ihre Staffel als --i, der Einlauf verzoegert danach.
test('scale bars carry their stagger index for the entrance', () => {
  const { api } = load();
  const html = api.render({
    shape: 'scale',
    scales: [{ name: 'a', ist: 100, soll: 50 }, { name: 'b', ist: 30, soll: 20 }],
  }, t);
  assert.match(html, /class="auto-learn-ist" style="--i:0"/);
  assert.match(html, /class="auto-learn-ist" style="--i:1"/);
  assert.match(html, /class="auto-learn-soll auto-learn-soft" style="--i:1"/);
});

// Der Einlauf läuft nur beim ersten Einfuegen. Gleiche Aktualisierungen ersetzen nichts,
// geaenderte Inhalte nach dem Einlauf kommen ohne die Klasse is-enter.
test('mount runs the entrance once and leaves later updates still', async () => {
  const { api, document } = load();
  const el = document.createElement('div');
  const ev = { shape: 'scale', ist_value: 300, soll_value: 90, unit: 'min' };
  api.mount(el, ev, t);
  assert.equal(el.classList.contains('is-enter'), true);
  assert.ok(el.querySelector('.auto-learn-is'));
  const first = el.firstChild;
  api.mount(el, { ...ev }, t);
  assert.equal(el.firstChild, first, 'same content keeps the DOM');
  await new Promise((resolve) => setTimeout(resolve, 750));
  assert.equal(el.classList.contains('is-enter'), false);
  api.mount(el, { ...ev, ist_value: 310 }, t);
  assert.notEqual(el.firstChild, first, 'changed content is rendered');
  assert.equal(el.classList.contains('is-enter'), false, 'an update does not enter again');
});

// Die Einlauf-Regeln bleiben an die Klasse gebunden und haben eine reduzierte Fassung.
test('the evidence entrance is gated by is-enter and has a reduced-motion rule', () => {
  const css = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'css', 'automations.css'), 'utf8');
  assert.match(css, /\.auto-learn-evidence\.is-enter \.auto-learn-is \{ animation: auto-learn-chart-in 200ms cubic-bezier\(0\.23, 1, 0\.32, 1\)/);
  assert.match(css, /\.auto-learn-evidence\.is-enter \.auto-learn-plot-svg\.is-soll \{ animation: auto-learn-trace 240ms/);
  assert.match(css, /transform-origin: left center/);
  assert.match(css, /scaleX\(0\.02\)/);
  const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce)'));
  assert.match(reduced, /auto-learn-chart-fade 200ms ease-out both/);
  assert.doesNotMatch(css, /(^|\n)\.auto-learn-is \{[^}]*animation/);
});
