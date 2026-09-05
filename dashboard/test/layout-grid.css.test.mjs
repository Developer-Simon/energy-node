// Regressionstest fuer den Rasterblock in base.css. Kein DOM-Test: die
// Regeln wirken nur mit echtem Layout, und jsdom rechnet kein CSS Grid.
// Geprueft wird darum der Regeltext selbst - das genuegt, um die zwei
// Eigenschaften festzunageln, die hier mehrfach versehentlich zurueckfielen:
// zeilenweise gleiche Hoehe und kein Zusatz-Margin an den Karten.
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

test('.layout-grid streckt die Kacheln einer Zeile auf gleiche Hoehe', () => {
  const body = ruleBody('.layout-grid');
  assert.match(body, /align-items:\s*stretch/);
  assert.doesNotMatch(body, /align-items:\s*start/);
});

test('.layout-grid nutzt denselben Abstand in beide Richtungen', () => {
  const body = ruleBody('.layout-grid');
  const gap = /gap:\s*([^;]+);/.exec(body);
  assert.ok(gap, 'gap fehlt in .layout-grid');
  assert.equal(gap[1].trim().split(/\s+/).length, 1, 'gap muss ein einzelner Wert sein, sonst weichen Zeilen- und Spaltenabstand ab');
});

test('Karten im Raster tragen keinen eigenen vertikalen Abstand mehr', () => {
  const body = ruleBody('.layout-grid-item > *');
  assert.match(body, /margin:\s*0/);
  assert.match(body, /height:\s*100%/);
});

test('keine Energiekarte bringt einen eigenen Margin ins Raster mit', () => {
  const offenders = [...css.matchAll(/^\.energy-[a-z-]+-card\s*\{([^}]*)\}/gm)]
    .filter(match => /margin:\s*(?!0)/.test(match[1]))
    .map(match => match[0].split('{')[0].trim());
  assert.deepEqual(offenders, []);
});

test('jede Energiekarte traegt denselben Rahmen wie die uebrigen Karten', () => {
  for (const selector of [
    '.energy-flow-card', '.energy-status-card', '.energy-ring-card',
    '.energy-board-card', '.energy-schema-card', '.energy-band-card', '.energy-day-card',
  ]) {
    const body = ruleBody(selector);
    assert.match(body, /border:\s*1px solid var\(--border-soft\)/, `${selector}: weicher Rand fehlt`);
    assert.match(body, /border-radius:\s*12px/, `${selector}: Radius 12 fehlt`);
  }
});
