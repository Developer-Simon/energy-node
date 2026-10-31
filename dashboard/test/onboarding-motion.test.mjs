// Die Rechenfunktionen der Bewegung stehen unter window.__obMotion und
// brauchen kein Alpine. Die Datei laeuft in einem eigenen JSDOM-Kontext,
// wie bei toast-gesture.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding-motion.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { motion: dom.window.__obMotion };
}

test('project() liefert die Auslaufstrecke wie beim Scrollen', () => {
  const { motion } = load();
  assert.ok(Math.abs(motion.project(1000) - 499) < 1, 'bei 1000 px/s etwa 499 px');
});

test('rubberband() ist am Anfang 0 und waechst sublinear', () => {
  const { motion } = load();
  assert.equal(motion.rubberband(0, 400), 0);
  const near = motion.rubberband(100, 400);
  const far = motion.rubberband(200, 400);
  assert.ok(far > near, 'waechst mit der Verschiebung');
  assert.ok(far < 2 * near, 'aber langsamer als linear');
});

test('releaseVelocity() rechnet 100 px in 100 ms als 1000 px/s', () => {
  const { motion } = load();
  assert.equal(motion.releaseVelocity([{ v: 0, t: 0 }, { v: 100, t: 100 }]), 1000);
});

test('integrate() mit damping 1 ueberschwingt nie', () => {
  const { motion } = load();
  const target = 100;
  let value = 0;
  let velocity = 0;
  for (let i = 0; i < 200; i++) {
    [value, velocity] = motion.integrate(value, velocity, target, 1 / 60, { response: 0.42, damping: 1 });
    assert.ok(value <= target, `Schritt ${i}: ${value} ueber dem Ziel`);
  }
  assert.ok(Math.abs(value - target) < 0.5, 'am Ende am Ziel');
});
