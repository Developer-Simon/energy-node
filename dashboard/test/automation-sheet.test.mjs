// Tests for the bottom sheet drag module: pure functions and the attach flow in jsdom.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'automation-sheet.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { api: dom.window.__automationSheet, window: dom.window };
}

function pointer(window, target, type, clientY) {
  target.dispatchEvent(new window.MouseEvent(type, { clientY, bubbles: true }));
}

test('rubber resists more the further it goes', () => {
  const { api } = load();
  assert.ok(Math.abs(api.rubber(-100, 600)) < 100);
  assert.ok(Math.abs(api.rubber(-200, 600)) - Math.abs(api.rubber(-100, 600)) < Math.abs(api.rubber(-100, 600)));
});

test('shouldClose on a slow drag past half and on a fast flick', () => {
  const { api } = load();
  assert.equal(api.shouldClose(320, 0, 600), true);
  assert.equal(api.shouldClose(80, 0, 600), false);
  assert.equal(api.shouldClose(80, 1500, 600), true);
});

test('project extends a flick by the deceleration formula', () => {
  const { api } = load();
  assert.equal(api.project(0), 0);
  assert.ok(Math.abs(api.project(1000) - (0.998 / 0.002)) < 1e-9);
});

test('attach follows the pointer and calls onClose once', () => {
  const { api, window } = load();
  const grab = window.document.createElement('div');
  const sheet = window.document.createElement('div');
  let clock = 0;
  let closes = 0;
  const detach = api.attach(grab, sheet, { onClose: () => { closes += 1; }, now: () => clock });
  pointer(window, grab, 'pointerdown', 100);
  clock = 100;
  pointer(window, grab, 'pointermove', 500);
  assert.equal(sheet.style.transform, 'translateY(400px)');
  assert.equal(sheet.style.transition, 'none');
  clock = 110;
  pointer(window, grab, 'pointerup', 500);
  assert.equal(closes, 1);
  pointer(window, grab, 'pointerup', 500);
  assert.equal(closes, 1, 'a second release without a press does nothing');
  detach();
});

test('a slow short drag springs back without onClose', async () => {
  const { api, window } = load();
  const grab = window.document.createElement('div');
  const sheet = window.document.createElement('div');
  let clock = 0;
  let closes = 0;
  const detach = api.attach(grab, sheet, { onClose: () => { closes += 1; }, now: () => clock });
  pointer(window, grab, 'pointerdown', 100);
  clock = 100;
  pointer(window, grab, 'pointermove', 180);
  clock = 200;
  pointer(window, grab, 'pointermove', 180);
  clock = 500;
  pointer(window, grab, 'pointerup', 180);
  assert.equal(closes, 0);
  assert.equal(sheet.style.transform, '', 'the sheet goes back to its place');
  await new Promise((resolve) => setTimeout(resolve, 450));
  assert.equal(sheet.style.transition, '', 'the spring transition is cleared afterwards');
  detach();
});

test('a fast flick closes even when the drag is short', () => {
  const { api, window } = load();
  const grab = window.document.createElement('div');
  const sheet = window.document.createElement('div');
  let clock = 0;
  let closes = 0;
  const detach = api.attach(grab, sheet, { onClose: () => { closes += 1; }, now: () => clock });
  pointer(window, grab, 'pointerdown', 100);
  clock = 10;
  pointer(window, grab, 'pointermove', 150);
  clock = 20;
  pointer(window, grab, 'pointermove', 180);
  clock = 30;
  pointer(window, grab, 'pointerup', 180);
  assert.equal(closes, 1);
  detach();
});

test('an upward drag is damped by rubber', () => {
  const { api, window } = load();
  const grab = window.document.createElement('div');
  const sheet = window.document.createElement('div');
  let clock = 0;
  const detach = api.attach(grab, sheet, { onClose: () => {}, now: () => clock });
  pointer(window, grab, 'pointerdown', 300);
  clock = 100;
  pointer(window, grab, 'pointermove', 200);
  assert.equal(sheet.style.transform, `translateY(${api.rubber(-100, 600)}px)`);
  detach();
});
