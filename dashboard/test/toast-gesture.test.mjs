// Die Rechenfunktionen des Wischens stehen unter window.__toastGesture und
// brauchen kein Alpine: das Skript registriert die Direktive erst bei
// alpine:init.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'toast-gesture.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { gesture: dom.window.__toastGesture, window: dom.window };
}

test('project() liefert die Auslaufstrecke wie beim Scrollen', () => {
  const { gesture } = load();
  assert.equal(gesture.project(0), 0);
  assert.ok(Math.abs(gesture.project(1000) - 499) < 1, 'bei 1000 px/s etwa 499 px');
  assert.ok(gesture.project(-1000) < 0);
});

test('rubberband() folgt dem Zeiger anfangs und bleibt unter der Elementbreite', () => {
  const { gesture } = load();
  assert.equal(gesture.rubberband(0, 300), 0);
  assert.ok(gesture.rubberband(-20, 300) > -20 && gesture.rubberband(-20, 300) < -10);
  assert.ok(Math.abs(gesture.rubberband(-5000, 300)) < 300);
});

test('shouldDismiss() entscheidet nach dem Endpunkt des Schwungs, nicht nach dem Loslasspunkt', () => {
  const { gesture } = load();
  assert.equal(gesture.shouldDismiss(40, 0, 300), false, 'kurz geschoben, kein Schwung');
  assert.equal(gesture.shouldDismiss(40, 600, 300), true, 'kurz, aber schnell nach rechts');
  assert.equal(gesture.shouldDismiss(200, 0, 300), true, 'weit genug geschoben');
  assert.equal(gesture.shouldDismiss(200, -800, 300), false, 'weit, aber mit Schwung zurueck');
});

test('releaseVelocity() rechnet aus dem Verlauf px/s und kommt ohne Verlauf auf 0', () => {
  const { gesture } = load();
  assert.equal(gesture.releaseVelocity([]), 0);
  assert.equal(gesture.releaseVelocity([{ x: 0, t: 0 }]), 0);
  assert.equal(gesture.releaseVelocity([{ x: 0, t: 100 }, { x: 30, t: 130 }]), 1000);
  assert.equal(gesture.releaseVelocity([{ x: 0, t: 100 }, { x: 30, t: 100 }]), 0, 'gleicher Zeitstempel teilt nicht durch 0');
});

test('alpine:init registriert die Direktive toast-gesture', () => {
  const { window } = load();
  const registered = [];
  window.Alpine = { directive: (name) => registered.push(name) };
  window.document.dispatchEvent(new window.Event('alpine:init'));
  assert.deepEqual(registered, ['toast-gesture']);
});
