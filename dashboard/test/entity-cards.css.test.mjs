// Regressionstest fuer den Feinschliff an entity_value / entity_group in
// base.css, Spec 2026-08-23 Abschnitt 5 (und der Radius-/Streifen-Teil aus
// Abschnitt 7). Rein visuelle Regeln - geprueft wird der Regeltext selbst,
// wie in layout-grid.css.test.mjs.
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

// Block-Koerper hinter einer Regel, deren Selektor css woertlich enthaelt
// (auch als Teil einer Selektorliste, anders als ruleBody in
// layout-grid.css.test.mjs).
function blockAfter(selectorText) {
  const at = css.indexOf(selectorText);
  assert.notEqual(at, -1, `Selektor "${selectorText}" fehlt in base.css`);
  const open = css.indexOf('{', at);
  const close = css.indexOf('}', open);
  return css.slice(open + 1, close);
}

test('5.1 beide Karten bekommen einen Tastaturfokus und eine Hover-Ruecklmeldung', () => {
  assert.match(blockAfter('button.entity-value-card:focus-visible'), /outline:\s*2px solid var\(--accent\)/);
  assert.match(blockAfter('button.entity-group-chip:focus-visible'), /outline:\s*2px solid var\(--accent\)/);
  assert.match(css, /button\.entity-value-card:hover/);
  assert.match(css, /button\.entity-group-chip:hover/);
});

test('5.2 der Druck ist asymmetrisch - schnelle Hinbewegung, langsame Rueckkehr', () => {
  // Grundregel = langsame Rueckkehr
  assert.match(blockAfter('.entity-value-card {'), /transition:\s*transform \.16s/);
  assert.match(blockAfter('.entity-group-chip {'), /transition:\s*transform \.16s/);
  // :active = schnelle Hinbewegung
  assert.match(blockAfter('button.entity-value-card:active'), /transition:\s*transform \.08s/);
  assert.match(blockAfter('button.entity-group-chip:active'), /transition:\s*transform \.08s/);
});

test('5.3 ein veralteter Wert graut auch den Statuspunkt aus, nicht nur die Zahl', () => {
  assert.match(blockAfter('.entity-value-card.stale .entity-value-dot'), /background:\s*var\(--text-faint\)/);
  assert.match(blockAfter('.entity-group-chip.stale .entity-group-chip-dot'), /background:\s*var\(--text-faint\)/);
});

test('5.4 ein wartender Befehl laesst den Statuspunkt pulsieren, mit Farbfallback', () => {
  assert.match(css, /@keyframes entity-dot-pulse/);
  assert.match(blockAfter('button.entity-value-card[aria-busy="true"] .entity-value-dot'), /animation:\s*entity-dot-pulse/);
  // prefers-reduced-motion: reduce schaltet die Animation ab, der Punkt
  // bleibt aber in der Wartefarbe
  const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce) {', css.indexOf('entity-dot-pulse')));
  assert.match(reduced, /\[aria-busy="true"\] \.entity-value-dot[^}]*animation:\s*none/);
});

test('7 die Wert-Karte traegt Radius 12 und die Akzent-Seitenleiste wie die Kompaktkachel', () => {
  const body = blockAfter('.entity-value-card {');
  assert.match(body, /border-radius:\s*var\(--radius-md\)/);
  assert.match(body, /border-left:\s*3px solid var\(--accent\)/);
});

test('7 die Seitenleiste der Wert-Karte faerbt sich bei offline/stale um', () => {
  assert.match(blockAfter('.entity-value-card.is-offline'), /border-left-color:\s*var\(--bad\)/);
  assert.match(blockAfter('.entity-value-card.stale {'), /border-left-color:\s*var\(--text-faint\)/);
});
