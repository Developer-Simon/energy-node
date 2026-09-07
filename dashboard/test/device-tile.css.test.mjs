// Regressionstest fuer die Geraetekachel in base.css, Spec 2026-08-23
// Abschnitt 3. Kein DOM-Test: die Regeln sind rein visuell, geprueft wird
// darum der Regeltext selbst - dieselbe Technik wie layout-grid.css.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const css = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'css', 'base.css'),
  'utf8',
);

function ruleBody(selector) {
  const index = css.indexOf(`\n${selector} {`);
  assert.notEqual(index, -1, `Regel "${selector}" fehlt in base.css`);
  const open = css.indexOf('{', index);
  const close = css.indexOf('}', open);
  return css.slice(open + 1, close);
}

test('3.1/3.2 .device-tile traegt Radius 12 und keinen dekorativen Akzentstreifen mehr', () => {
  const body = ruleBody('.device-tile');
  assert.match(body, /border-radius:\s*var\(--radius-md\)/, 'Radius 12 (--radius-md) fehlt');
  assert.doesNotMatch(body, /border-left/, 'der Akzentstreifen auf jedem Geraet muss weg');
  assert.match(body, /border:\s*1px solid var\(--border-soft\)/, 'weicher Rand fehlt');
});

test('3.2 der Status-Punkt der Kopfzeile hat die drei Zustandsfarben', () => {
  assert.match(ruleBody('.device-tile-dot'), /border-radius:\s*50%/);
  assert.match(ruleBody('.device-tile-dot-ok'), /background:\s*var\(--ok\)/);
  assert.match(ruleBody('.device-tile-dot-bad'), /background:\s*var\(--bad\)/);
  assert.match(ruleBody('.device-tile-dot-warn'), /background:\s*var\(--warn\)/);
});

test('3.4 die Zaehlzeile rendert als Chips im Stil von .entity-group-count', () => {
  const chip = ruleBody('.device-tile-chip');
  assert.match(chip, /border-radius:\s*999px/);
  assert.match(chip, /background:\s*var\(--panel-alt\)/);
});

test('3.5 zwischen den Entitaetenzeilen der Kachel steht kein Trennstrich mehr', () => {
  assert.doesNotMatch(ruleBody('.device-tile-entity'), /border-top/);
});

test('3.7 der Schaltknopf der Kachel hat 2,75rem Trefferflaeche und eine eigene :active-Skalierung', () => {
  const button = ruleBody('.device-tile .entity-command-button');
  assert.match(button, /width:\s*2\.75rem/);
  assert.match(button, /height:\s*2\.75rem/);
  assert.match(button, /padding:\s*\.7rem/);
  assert.match(ruleBody('.device-tile .entity-command-button:active'), /transform:\s*scale\(\.94\)/);
});
