// Die Lochgeometrie des Spotlights steht unter window.__obSpotlight und
// braucht kein Alpine. Die Datei laeuft in einem eigenen JSDOM-Kontext,
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
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding-spotlight.js'),
  'utf8',
);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return { spotlight: dom.window.__obSpotlight };
}

// Ein Spotlight auf einem Wurzelelement mit Abdunkelung und Ring. Die Feder
// wird nur erzeugt, press() braucht sie nicht.
function loadRing() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"><div class="ob-spot-dim"></div><div class="ob-spot-ring"><div class="ob-spot-ring-inner"></div></div></div></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  dom.window.obMotion = { createSpring: () => ({ values: null, set() {}, to() {}, shift() {} }) };
  vm.runInContext(source, dom.getInternalVMContext());
  const root = dom.window.document.getElementById('root');
  return { dom, ring: root.querySelector('.ob-spot-ring'), spot: dom.window.obSpotlight.create(root) };
}

test('press restarts the pressing class on the ring', () => {
  const { dom, ring, spot } = loadRing();
  spot.press();
  assert.equal(ring.classList.contains('is-pressing'), true);
  // Ein zweiter Puls waehrend des ersten laeuft neu, die Klasse bleibt gesetzt.
  spot.press();
  assert.equal(ring.classList.contains('is-pressing'), true);
  // Das Ende der Animation nimmt die Klasse wieder weg.
  const end = new dom.window.Event('animationend', { bubbles: true });
  Object.defineProperty(end, 'animationName', { value: 'ob-ring-press' });
  ring.dispatchEvent(end);
  assert.equal(ring.classList.contains('is-pressing'), false);
});

test('holePath() zeichnet die Flaeche mit einem runden Loch in evenodd', () => {
  const { spotlight } = load();
  const css = spotlight.holePath(100, 100, { x: 10, y: 10, w: 20, h: 20 }, 4);
  assert.ok(css.startsWith('path(evenodd, "M0 0H100V100H0Z'), css);
  assert.ok(css.includes('A4 4'), css);
});
