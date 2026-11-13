// Regressionstest fuer das Skeleton-Vokabular in base.css, Spec 2026-11-13
// (lazy settings + skeletons). Kein DOM-Test: die Regeln sind rein visuell,
// geprueft wird der Regeltext selbst, wie in device-tile.css.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const full = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'css', 'base.css'),
  'utf8',
);
// Ab dem Skeleton-Abschnitt suchen; fehlt er, schlagen die Einzelfaelle fehl.
const start = full.indexOf('Das Skeleton-Vokabular');
const css = start === -1 ? '' : full.slice(start);

const BLOCKS = [
  'sk', 'sk-shell', 'sk-layer', 'sk-form', 'sk-list', 'sk-chips', 'sk-chart',
  'sk-bars', 'sk-acc', 'sk-graph', 'sk-entry', 'sk-spark', 'sk-line', 'sk-seg',
  'is-arriving',
];

test('base.css defines skeleton tokens', () => {
  assert.ok(css.includes('--sk-fill: color-mix(in srgb, var(--text) 9%, transparent)'), '--sk-fill fehlt');
  assert.ok(css.includes('--sk-glow: color-mix(in srgb, var(--text) 13%, transparent)'), '--sk-glow fehlt');
  assert.ok(css.includes('--sk-row: 46px'), '--sk-row fehlt');
});

test('skeleton shapes share sweep and breathing', () => {
  assert.match(
    css,
    /\.sk::before[^{]*\{[^}]*animation:\s*sk-sweep 2\.4s linear -\.25s infinite,\s*sk-breathe 1\.8s ease-in-out infinite alternate/,
    '.sk::before animiert nicht mit Sweep und Breathing',
  );
  assert.match(css, /@keyframes sk-sweep\s*\{[\s\S]*?-120vw[\s\S]*?20vw/, '@keyframes sk-sweep fehlt oder hat falsche Werte');
  assert.match(css, /@keyframes sk-breathe\s*\{[^}]*opacity:\s*\.72/, '@keyframes sk-breathe fehlt oder hat falsche Werte');
});

test('skeleton layer appears after 160 ms', () => {
  assert.match(
    css,
    /\.sk-layer\s*\{\s*animation:\s*sk-in \.2s var\(--ease-out\) \.16s both/,
    '.sk-layer hat nicht die 160 ms Verzoegerung',
  );
});

test('arrival staggers cards', () => {
  assert.match(css, /\.is-arriving[^{]*\{[^}]*card-in \.26s/, '.is-arriving animiert nicht mit card-in .26s');
  assert.match(css, /\.is-arriving[^{]*nth-child\(2\)[^{]*\{[^}]*\.04s/, 'nth-child(2) fehlt die .04s Verzoegerung');
  assert.match(css, /\.is-arriving[^{]*nth-child\(3\)[^{]*\{[^}]*\.08s/, 'nth-child(3) fehlt die .08s Verzoegerung');
});

test('reduced motion stops the loop', () => {
  assert.match(
    css,
    /@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.sk::before[^{]*\{\s*animation:\s*none/,
    'im reduced-motion-Block fehlt .sk::before mit animation: none',
  );
});

test('every building block is defined', () => {
  for (const cls of BLOCKS) {
    const escaped = cls.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    // Auch als Teil eines Kompositums (.sk-entry.sk-spark) zaehlt der Selektor.
    const re = new RegExp(`\\.${escaped}(?![\\w-])[^{}]*\\{`);
    assert.match(css, re, `kein Selektor fuer .${cls}`);
  }
});
