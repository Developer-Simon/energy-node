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

test('holePath() zeichnet die Flaeche mit einem runden Loch in evenodd', () => {
  const { spotlight } = load();
  const css = spotlight.holePath(100, 100, { x: 10, y: 10, w: 20, h: 20 }, 4);
  assert.ok(css.startsWith('path(evenodd, "M0 0H100V100H0Z'), css);
  assert.ok(css.includes('A4 4'), css);
});
