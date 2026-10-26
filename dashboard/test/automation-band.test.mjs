// Tests for the target band math: pure functions, the spring and the reduced-motion path.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const sheetSource = fs.readFileSync(path.join(jsDir, 'automation-sheet.js'), 'utf8');
const bandSource = fs.readFileSync(path.join(jsDir, 'automation-band.js'), 'utf8');

// Loads the sheet first, because the band module borrows its rubber band.
function load({ reducedMotion = false } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const { window } = dom;
  window.matchMedia = (query) => ({ matches: reducedMotion && query.includes('reduce'), media: query });
  vm.runInContext(sheetSource, dom.getInternalVMContext());
  vm.runInContext(bandSource, dom.getInternalVMContext());
  return { api: window.__automationBand, sheet: window.__automationSheet, window };
}

// Fake animation frames: callbacks wait in a queue until the test runs them.
function fakeFrames(window) {
  const clock = { now: 0 };
  let queue = [];
  let nextId = 1;
  window.performance.now = () => clock.now;
  window.requestAnimationFrame = (cb) => {
    const id = nextId;
    nextId += 1;
    queue.push({ id, cb });
    return id;
  };
  window.cancelAnimationFrame = (id) => {
    queue = queue.filter((entry) => entry.id !== id);
  };
  // Runs frames of 16.67 ms until nothing is queued or the frame budget is spent.
  const run = (frames) => {
    let done = 0;
    while (queue.length && done < frames) {
      clock.now += 1000 / 60;
      const next = queue;
      queue = [];
      next.forEach((entry) => entry.cb(clock.now));
      done += 1;
    }
    return done;
  };
  return { clock, pending: () => queue.length, run };
}

// A minute series at a constant value, one row per minute.
function rows(n, value = 50) {
  return Array.from({ length: n }, (_, i) => ({ ts: i * 60000, avg: value }));
}

test('bandStats is null without data', () => {
  const { api } = load();
  assert.equal(api.bandStats([], 30, 20), null);
});

test('bandStats counts feed-in and draw below and above zero', () => {
  const { api } = load();
  // n = target + e: -20, -10, 30, 40
  const s = api.bandStats([-50, -40, 0, 10], 30, 20);
  assert.ok(Math.abs(s.fedKwh - (20 + 10) * 60 / 3.6e6) < 1e-12);
  assert.ok(Math.abs(s.drawnKwh - (30 + 40) * 60 / 3.6e6) < 1e-12);
  // |e| > 20 only for -50 and -40, over 4 minutes
  assert.ok(Math.abs(s.movesPerHour - 2 / (4 / 60)) < 1e-9);
});

test('bandStats counts a move only when the error is beyond tol', () => {
  const { api } = load();
  const s = api.bandStats([20, 21, -21, 0], 0, 20);
  assert.ok(Math.abs(s.movesPerHour - 2 / (4 / 60)) < 1e-9);
});

test('errorsFromRows needs an hour of rows', () => {
  const { api } = load();
  assert.equal(api.errorsFromRows(rows(59)).length, 0);
  assert.equal(api.errorsFromRows([]).length, 0);
});

test('errorsFromRows gives one error per row, zero on a flat series', () => {
  const { api } = load();
  const errors = api.errorsFromRows(rows(60, 50));
  assert.equal(errors.length, 60);
  assert.ok(errors.every((e) => e === 0));
});

test('errorsFromRows uses the median, not the mean, of the window', () => {
  const { api } = load();
  const values = rows(60, 50);
  values[30].avg = 160;
  const errors = api.errorsFromRows(values);
  // Window 25..35 holds ten 50s and one 160: median 50, mean 60.
  assert.equal(errors[30], 110);
  // The spike is not an outlier for its neighbours: median stays 50.
  assert.equal(errors[29], 0);
  assert.equal(errors[31], 0);
  // Edge window 0..5 is flat.
  assert.equal(errors[0], 0);
});

test('errorsFromRows rounds to 1 W', () => {
  const { api } = load();
  const values = rows(60, 50);
  values[30].avg = 160.4;
  values[31].avg = 50.6;
  const errors = api.errorsFromRows(values);
  assert.equal(errors[30], 110);
  assert.equal(errors[31], 1);
});

test('snap rounds to 5 W', () => {
  const { api } = load();
  assert.equal(api.snap(32.4), 30);
  assert.equal(api.snap(32.6), 35);
  assert.equal(api.snap(-12), -10);
  assert.equal(api.snap(7, 2), 8);
});

test('histogram covers -160 to +160 W in 5 W bins', () => {
  const { api } = load();
  const bins = api.histogram([0, 0, 7, -161, 160]);
  assert.equal(bins.length, 64);
  // Objects come from the vm realm, so compare plain copies.
  const plain = (o) => JSON.parse(JSON.stringify(o));
  assert.deepEqual(plain(bins[0]), { offset: -160, count: 0 });
  assert.deepEqual(plain(bins[32]), { offset: 0, count: 2 });
  assert.deepEqual(plain(bins[33]), { offset: 5, count: 1 });
  // -161 and +160 fall outside the range and are dropped.
  assert.equal(bins.reduce((sum, b) => sum + b.count, 0), 3);
});

test('LIMITS hold the target, tolerance and axis bounds', () => {
  const { api } = load();
  const plain = (o) => JSON.parse(JSON.stringify(o));
  assert.deepEqual(plain(api.LIMITS), {
    target: { min: -50, max: 150 },
    tol: { min: 5, max: 60 },
    axis: { min: -120, max: 160 },
  });
  assert.ok(Object.isFrozen(api.LIMITS));
});

test('rubber is the sheet formula, read from automation-sheet.js', () => {
  const { api, sheet } = load();
  assert.equal(api.rubber(-100, 600), sheet.rubber(-100, 600));
  assert.equal(api.rubber(-200, 600, 0.3), sheet.rubber(-200, 600, 0.3));
  assert.ok(Math.abs(api.rubber(-100, 600)) < 100);
});

test('spring settles exactly on the target and does not overshoot', () => {
  const { api, window } = load();
  const frames = fakeFrames(window);
  const v = { x: 30 };
  let max = -Infinity;
  const spring = api.spring(() => v.x, (n) => { v.x = n; max = Math.max(max, n); });
  spring.to(100);
  assert.equal(v.x, 30, 'nothing moves before a frame runs');
  frames.run(1);
  assert.ok(v.x > 30 && v.x < 100, 'the first frame moves part of the way');
  const ran = frames.run(240);
  assert.ok(ran < 240, 'the spring stops scheduling frames on its own');
  assert.equal(frames.pending(), 0);
  assert.equal(v.x, 100);
  assert.ok(max <= 100 + 1e-9, 'critically damped, no overshoot');
});

test('spring retargeted mid-flight keeps moving and settles on the new target', () => {
  const { api, window } = load();
  const frames = fakeFrames(window);
  const v = { x: 30 };
  const spring = api.spring(() => v.x, (n) => { v.x = n; });
  spring.to(100);
  frames.run(10);
  const mid = v.x;
  spring.to(-20);
  frames.run(240);
  assert.ok(mid > 30 && mid < 100);
  assert.equal(frames.pending(), 0);
  assert.equal(v.x, -20);
});

test('spring stop cancels the running frame', () => {
  const { api, window } = load();
  const frames = fakeFrames(window);
  const v = { x: 30 };
  const spring = api.spring(() => v.x, (n) => { v.x = n; });
  spring.to(100);
  assert.equal(frames.pending(), 1);
  spring.stop();
  assert.equal(frames.pending(), 0);
});

test('spring with reduced motion sets the value immediately and schedules nothing', () => {
  const { api, window } = load({ reducedMotion: true });
  const frames = fakeFrames(window);
  const v = { x: 30 };
  const spring = api.spring(() => v.x, (n) => { v.x = n; });
  spring.to(100);
  assert.equal(v.x, 100);
  assert.equal(frames.pending(), 0);
});

// ---------- mountBand: DOM part of the target band ----------

const SVG_NS = 'http://www.w3.org/2000/svg';

function makeSvg(window, width = 580) {
  const svg = window.document.createElementNS(SVG_NS, 'svg');
  window.document.body.appendChild(svg);
  svg.getBoundingClientRect = () => ({ left: 0, top: 0, width, height: 150, right: width, bottom: 150 });
  return svg;
}

function bandOpts(state, over = {}) {
  return {
    state,
    tolEditable: () => false,
    errors: () => [],
    labels: {
      feedIn: 'Feed-in',
      import: 'Import',
      axis: 'Grid power in W',
      axisShort: 'Grid in W',
      target: (v) => `Target ${v} W`,
      targetTol: (v, tol) => `Target ${v} W ± ${tol}`,
      valueText: (v, tol) => `Target ${v} W, tolerance ${tol} W`,
    },
    onChange: () => {},
    ...over,
  };
}

function press(window, svg, key, shiftKey = false) {
  svg.dispatchEvent(new window.KeyboardEvent('keydown', { key, shiftKey, bubbles: true }));
}

test('mountBand moves the target on arrow keys', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window);
  const state = { target: 30, tol: 20 };
  const band = api.mountBand(svg, bandOpts(state));
  press(window, svg, 'ArrowRight');
  assert.equal(state.target, 35);
  press(window, svg, 'ArrowRight', true);
  assert.equal(state.target, 55);
  press(window, svg, 'ArrowLeft');
  assert.equal(state.target, 50);
  press(window, svg, 'ArrowDown', true);
  assert.equal(state.target, 30);
  band.destroy();
});

test('mountBand draws side handles only while tolEditable is true', () => {
  const { api, window } = load({ reducedMotion: true });
  const state = { target: 30, tol: 20 };

  const fixed = makeSvg(window);
  api.mountBand(fixed, bandOpts(state, { tolEditable: () => false }));
  assert.equal(fixed.querySelector('[data-drag="l"]'), null);
  assert.equal(fixed.querySelector('[data-drag="r"]'), null);
  assert.ok(fixed.querySelector('[data-drag="move"]'), 'the window is always drawn');
  assert.ok(fixed.textContent.includes('Target 30 W'));
  assert.ok(!/Einspeisung|Bezug|Ziel/.test(fixed.textContent), 'no hardcoded German');

  const editable = makeSvg(window);
  api.mountBand(editable, bandOpts(state, { tolEditable: () => true }));
  assert.ok(editable.querySelector('[data-drag="l"]'));
  assert.ok(editable.querySelector('[data-drag="r"]'));
  assert.ok(editable.textContent.includes('Target 30 W ± 20'));
  assert.ok(!/Einspeisung|Bezug|Ziel/.test(editable.textContent), 'no hardcoded German');
});

test('mountBand setTarget springs to the value and reports every change', () => {
  const { api, window } = load({ reducedMotion: false });
  const frames = fakeFrames(window);
  const svg = makeSvg(window);
  const state = { target: 30, tol: 20 };
  let calls = 0;
  const band = api.mountBand(svg, bandOpts(state, { onChange: () => { calls += 1; } }));
  band.setTarget(60);
  frames.run(240);
  assert.equal(state.target, 60);
  assert.equal(frames.pending(), 0);
  assert.ok(calls > 0, 'onChange is called while the spring runs');
  band.destroy();
});

test('mountBand draws no histogram bars without errors but keeps the window', () => {
  const { api, window } = load({ reducedMotion: true });
  const empty = makeSvg(window);
  api.mountBand(empty, bandOpts({ target: 30, tol: 20 }, { errors: () => [] }));
  assert.equal(empty.querySelectorAll('rect.auto-band-bar').length, 0);
  assert.ok(empty.querySelector('[data-drag="move"]'));

  const filled = makeSvg(window);
  api.mountBand(filled, bandOpts({ target: 30, tol: 20 }, { errors: () => [0, 5, -3] }));
  assert.ok(filled.querySelectorAll('rect.auto-band-bar').length > 0);
});

test('mountBand sets aria-valuenow to the rounded target after a key press', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window);
  const state = { target: 30, tol: 20 };
  api.mountBand(svg, bandOpts(state));
  press(window, svg, 'ArrowRight');
  assert.equal(svg.getAttribute('aria-valuenow'), '35');
  assert.equal(svg.getAttribute('role'), 'slider');
  assert.equal(svg.getAttribute('aria-valuetext'), 'Target 35 W, tolerance 20 W');
});

// ---------- Preisskala der Niedrigtarif-Vorlage ----------

test('priceX maps 0 to 40 ct across the width and priceValue snaps to 0.5 ct', () => {
  const { api } = load();
  assert.equal(api.priceX(0, 400), 0);
  assert.equal(api.priceX(40, 400), 400);
  assert.equal(api.priceX(20, 400), 200);
  assert.equal(api.priceValue(201, 400), 20);
  assert.equal(api.priceValue(203, 400), 20.5);
  assert.equal(api.priceValue(-50, 400), 0);
  assert.equal(api.priceValue(999, 400), 40);
});

test('mountPriceScale moves the threshold on arrow keys in 0.5 ct steps, shift in 2 ct', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 400);
  const state = { thr: 15 };
  const scale = api.mountPriceScale(svg, priceOpts(state));
  press(window, svg, 'ArrowRight');
  assert.equal(state.thr, 15.5);
  press(window, svg, 'ArrowRight', true);
  assert.equal(state.thr, 17.5);
  press(window, svg, 'ArrowDown');
  assert.equal(state.thr, 17);
  scale.destroy();
});

test('mountPriceScale reports slider roles and the threshold label', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 400);
  const scale = api.mountPriceScale(svg, priceOpts({ thr: 10 }));
  assert.equal(svg.getAttribute('role'), 'slider');
  assert.equal(svg.getAttribute('aria-valuemin'), '0');
  assert.equal(svg.getAttribute('aria-valuemax'), '40');
  assert.equal(svg.getAttribute('aria-valuenow'), '10');
  assert.equal(svg.getAttribute('aria-valuetext'), '10 ct pro kWh');
  assert.ok(svg.innerHTML.includes('unter 10 ct'));
  assert.ok(svg.innerHTML.includes('data-drag="thr"'));
  scale.destroy();
});

test('mountPriceScale draws the current price marker only with a value', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const withPrice = makeSvg(window, 400);
  const scale = api.mountPriceScale(withPrice, { ...priceOpts({ thr: 15 }), now: () => 12.5 });
  assert.ok(withPrice.innerHTML.includes('auto-price-now'));
  scale.destroy();
  const without = makeSvg(window, 400);
  const empty = api.mountPriceScale(without, { ...priceOpts({ thr: 15 }), now: () => null });
  assert.equal(without.innerHTML.includes('auto-price-now'), false);
  empty.destroy();
});

function priceOpts(state) {
  return {
    state,
    now: () => null,
    labels: {
      threshold: (v) => `unter ${v} ct`,
      valueText: (v) => `${v} ct pro kWh`,
    },
    onChange: () => {},
  };
}

// ---------- Leistungsskala des Überschuss-Schritts ----------

test('powerMax keeps 6000 W and grows to the next full 1000 W above a larger value', () => {
  const { api } = load();
  assert.equal(api.powerMax(0), 6000);
  assert.equal(api.powerMax(6000), 6000);
  assert.equal(api.powerMax(6001), 7000);
  assert.equal(api.powerMax(6500), 7000);
  assert.equal(api.powerMax(NaN), 6000);
});

test('mountPowerScale moves the threshold in 100 W steps, shift in 400 W, and stops at 0', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 600);
  const state = { thr: 1500 };
  const scale = api.mountPowerScale(svg, powerOpts(state));
  press(window, svg, 'ArrowRight');
  assert.equal(state.thr, 1600);
  press(window, svg, 'ArrowRight', true);
  assert.equal(state.thr, 2000);
  press(window, svg, 'ArrowLeft');
  assert.equal(state.thr, 1900);
  state.thr = 0;
  press(window, svg, 'ArrowDown');
  assert.equal(state.thr, 0);
  scale.destroy();
});

test('mountPowerScale grows its maximum with the threshold and shows ticks in 1000 W', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 600);
  const state = { thr: 6000 };
  const scale = api.mountPowerScale(svg, powerOpts(state));
  assert.equal(svg.getAttribute('aria-valuemax'), '6000');
  assert.ok(svg.innerHTML.includes('>6000<'));
  press(window, svg, 'ArrowRight');
  assert.equal(state.thr, 6100);
  assert.equal(svg.getAttribute('aria-valuemax'), '7000');
  assert.ok(svg.innerHTML.includes('>7000<'));
  assert.ok(svg.innerHTML.includes('>1000<'));
  scale.destroy();
});

test('mountPowerScale reports slider roles, the threshold pill and the current surplus marker', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const withNow = makeSvg(window, 600);
  const scale = api.mountPowerScale(withNow, { ...powerOpts({ thr: 2500 }), now: () => 1200 });
  assert.equal(withNow.getAttribute('role'), 'slider');
  assert.equal(withNow.getAttribute('aria-valuenow'), '2500');
  assert.equal(withNow.getAttribute('aria-valuetext'), '2500 W');
  assert.ok(withNow.innerHTML.includes('2500 W'));
  assert.ok(withNow.innerHTML.includes('auto-power-now'));
  scale.destroy();
  const without = makeSvg(window, 600);
  const empty = api.mountPowerScale(without, { ...powerOpts({ thr: 2500 }), now: () => null });
  assert.equal(without.innerHTML.includes('auto-power-now'), false);
  empty.destroy();
});

function powerOpts(state) {
  return {
    state,
    now: () => null,
    tickLabel: (v) => String(v),
    labels: {
      threshold: (v) => `${v} W`,
      valueText: (v) => `${v} W`,
    },
    onChange: () => {},
  };
}

// Akzentfarbe auf der Seite der Schwelle: Preis links, Ueberschuss rechts (ab der Schwelle startet die Vorlage).
function accentRect(svg) {
  return [...svg.querySelectorAll('rect')].find((el) => el.getAttribute('fill') === 'var(--accent)');
}

test('mountPriceScale fills the accent left of the threshold', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 600);
  const scale = api.mountPriceScale(svg, priceOpts({ thr: 20 }));
  const fill = accentRect(svg);
  assert.equal(fill.getAttribute('x'), '0');
  assert.equal(fill.getAttribute('width'), '300');
  scale.destroy();
});

test('mountPowerScale fills the accent right of the threshold', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 600);
  const scale = api.mountPowerScale(svg, powerOpts({ thr: 3000 }));
  const fill = accentRect(svg);
  assert.equal(fill.getAttribute('x'), '300');
  assert.equal(fill.getAttribute('width'), '300');
  scale.destroy();
});

// Ein Ziehen am Griff: Das Maximum wird beim Mounten bestimmt und bleibt bis zum Loslassen gleich,
// auch wenn der Wert ueber 6000 W geht. Der rechte Rand ist dann die Grenze.
test('mountPowerScale keeps its maximum constant during a drag', () => {
  const { api, window } = load({ reducedMotion: true });
  fakeFrames(window);
  const svg = makeSvg(window, 600);
  svg.setPointerCapture = () => {};
  const state = { thr: 5000 };
  const scale = api.mountPowerScale(svg, powerOpts(state));
  assert.equal(svg.getAttribute('aria-valuemax'), '6000');
  const pointer = (type, target, clientX) => {
    const ev = new window.Event(type, { bubbles: true });
    Object.defineProperty(ev, 'clientX', { value: clientX });
    Object.defineProperty(ev, 'pointerId', { value: 1 });
    target.dispatchEvent(ev);
  };
  const handle = svg.querySelector('[data-drag="thr"]');
  pointer('pointerdown', handle, 500);
  const maxes = [];
  for (const x of [520, 560, 590, 600, 600]) {
    pointer('pointermove', svg, x);
    maxes.push(svg.getAttribute('aria-valuemax'));
    assert.ok(state.thr <= 6000, `Wert ${state.thr} darf das Maximum nicht ueberschreiten`);
  }
  assert.deepEqual([...new Set(maxes)], ['6000']);
  assert.equal(state.thr, 6000, 'der Griff hat den Wert wirklich gezogen');
  pointer('pointerup', svg, 600);
  scale.destroy();
});
