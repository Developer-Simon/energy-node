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

// Hilfen fuer die Favoritenreihe und das Modal (Task 11): Regeln samt
// Position im Text und die Bloecke einer Media-Query.
function allRules() {
  const rules = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(css)) !== null) {
    rules.push({ selector: m[1].trim(), body: m[2], index: m.index });
  }
  return rules;
}

function mediaBlocks(prefix) {
  const blocks = [];
  let i = css.indexOf(prefix);
  while (i !== -1) {
    const open = css.indexOf('{', i);
    let depth = 0;
    let j = open;
    for (; j < css.length; j++) {
      if (css[j] === '{') depth++;
      else if (css[j] === '}') {
        depth--;
        if (depth === 0) break;
      }
    }
    blocks.push({ start: open + 1, end: j, body: css.slice(open + 1, j) });
    i = css.indexOf(prefix, j);
  }
  return blocks;
}

test('11 kein transition: all im Geraete-Modal', () => {
  const offenders = allRules()
    .filter((rule) => rule.selector.includes('.device-modal-'))
    .filter((rule) => /transition:\s*all\b/.test(rule.body))
    .map((rule) => rule.selector);
  assert.deepEqual(offenders, [], 'transition: all ist im Modal verboten');
});

test('11 device-modal-star hat eine prefers-reduced-motion-Regel', () => {
  const reduced = mediaBlocks('@media (prefers-reduced-motion: reduce)');
  assert.ok(
    reduced.some((block) => /\.device-modal-star\b/.test(block.body)),
    'reduced-motion-Regel fuer .device-modal-star fehlt',
  );
});

test('11 die Hover-Regel des Sterns steht unter (hover: hover) and (pointer: fine)', () => {
  const fine = mediaBlocks('@media (hover: hover) and (pointer: fine)');
  assert.ok(
    fine.some((block) => /\.device-modal-star\b/.test(block.body) && /:hover/.test(block.body)),
    'keine Hover-Regel fuer .device-modal-star im Feinzeiger-Block',
  );
  const outside = allRules().filter((rule) =>
    rule.selector.includes('.device-modal-star') &&
    (rule.selector.includes(':hover') || rule.selector.includes(':focus-within')) &&
    !fine.some((block) => rule.index > block.start && rule.index < block.end),
  );
  assert.deepEqual(outside.map((rule) => rule.selector), [], 'Stern-Hover ausserhalb des Feinzeiger-Blocks');
});

test('11 die eintretende Favoritenkachel blendet mit 220 ms var(--ease-out) ein', () => {
  const rule = allRules().find((r) => r.selector.includes('.device-modal-hero') && /animation:/.test(r.body));
  assert.ok(rule, 'Animationsregel fuer die Favoritenreihe fehlt');
  assert.match(rule.body, /220ms\s+var\(--ease-out\)/);
  assert.match(css, /@keyframes device-modal-hero-enter\s*\{[^}]*opacity:\s*0[^}]*scale\(\.96\)/);
});
