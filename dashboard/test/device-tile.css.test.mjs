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

test('3.1 .device-tile traegt Radius 12, weichen Rand und die Akzent-Seitenleiste wie die Kompaktkachel', () => {
  const body = ruleBody('.device-tile');
  assert.match(body, /border-radius:\s*var\(--radius-md\)/, 'Radius 12 (--radius-md) fehlt');
  assert.match(body, /border:\s*1px solid var\(--border-soft\)/, 'weicher Rand fehlt');
  assert.match(body, /border-left:\s*3px solid var\(--accent\)/, 'die Theme-farbene Seitenleiste (wie .compact-card) fehlt');
});

test('3.2 die Seitenleiste faerbt sich bei offline/degraded um, wie .compact-card.is-offline', () => {
  assert.match(ruleBody('.device-tile.is-offline'), /border-left-color:\s*var\(--bad\)/);
  assert.match(ruleBody('.device-tile.is-degraded'), /border-left-color:\s*var\(--warn\)/);
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
