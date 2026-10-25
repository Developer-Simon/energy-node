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
