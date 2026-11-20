// Design-Sperre fuer die Basis aller app-modal. Kein DOM-Test: geprueft wird
// der Regeltext gegen die Bewegungstabelle der Spec und gegen die Templates.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const read = (...parts) => fs.readFileSync(path.join(here, '..', ...parts), 'utf8');
const base = read('internal', 'webui', 'static', 'css', 'base.css');
const manager = read('internal', 'webui', 'static', 'css', 'manager.css');
const baseHTML = read('internal', 'webui', 'templates', 'base.html');
const editorHTML = read('internal', 'webui', 'templates', 'layout-editor.html');

// Text vom ersten `selector {` bis zur naechsten `}`.
function block(css, selector) {
  const start = css.indexOf(`${selector} {`);
  assert.ok(start >= 0, `kein Block ${selector}`);
  return css.slice(start, css.indexOf('}', start) + 1);
}

// Text des ersten @media (query)-Blocks, der selector enthaelt (Klammerzaehlung).
function mediaBody(css, query, selector) {
  let from = 0;
  for (;;) {
    const at = css.indexOf(`@media (${query})`, from);
    assert.ok(at >= 0, `kein @media (${query}) mit ${selector}`);
    const open = css.indexOf('{', at);
    let depth = 0;
    let end = -1;
    for (let i = open; i < css.length; i++) {
      if (css[i] === '{') depth++;
      else if (css[i] === '}' && --depth === 0) {
        end = i;
        break;
      }
    }
    const body = css.slice(open + 1, end);
    if (body.includes(selector)) return body;
    from = end;
  }
}

test('die Basis hat keinen Akzentstreifen mehr, dafür Radius, Lichtkante und Schatten', () => {
  const rule = block(base, '.app-modal');
  assert.doesNotMatch(rule, /border-left/);
  assert.match(rule, /border-radius:\s*var\(--radius-lg\)/);
  assert.match(rule, /border-top-color:\s*var\(--modal-edge\)/);
  assert.match(rule, /box-shadow:\s*var\(--modal-shadow\)/);
  assert.match(rule, /color-mix\(in srgb, var\(--panel\) 84%, transparent\)/);
  assert.match(rule, /backdrop-filter:\s*blur\(24px\) saturate\(160%\)/);
});
test('die Modal-Tokens gibt es dunkel und im Tageslicht', () => {
  assert.match(base, /--modal-shadow:\s*0 1\.75rem 3\.5rem -\.75rem rgba\(0, 0, 0, \.55\), 0 \.3rem \.9rem rgba\(0, 0, 0, \.3\)/);
  assert.match(base, /--modal-scrim:\s*rgba\(0, 0, 0, \.5\)/);
  assert.match(base, /:root\[data-theme="tageslicht"\]\s*\{[^}]*--modal-edge:\s*rgba\(255, 255, 255, \.9\)/s);
  assert.match(base, /:root\[data-theme="tageslicht"\]\s*\{[^}]*--modal-scrim:\s*rgba\(20, 26, 34, \.32\)/s);
});
test('Danger färbt keinen Rand mehr', () => {
  assert.doesNotMatch(base, /\.app-modal-danger\s*\{[^}]*border/);
});
test('das Sheet steigt aus translateY(3rem) scale(.98) in 400ms auf --layout-ease auf', () => {
  assert.match(base, /@starting-style\s*\{\s*\.app-modal\[open\]\s*\{[^}]*translateY\(3rem\) scale\(\.98\)/);
  assert.match(base, /\.app-modal\[open\]\s*\{[^}]*transform \.4s var\(--layout-ease\)/s);
  assert.match(base, /\.app-modal\s*\{[^}]*transform \.22s var\(--ease-out\)/s);
});
test('der Hinweis wächst aus scale(.94) in 240ms und schließt in 160ms', () => {
  assert.match(base, /@starting-style\s*\{\s*\.app-modal\.app-modal-alert\[open\]\s*\{[^}]*scale\(\.94\)/);
  assert.match(base, /\.app-modal\.app-modal-alert\[open\]\s*\{[^}]*transform \.24s var\(--ease-out\)/s);
  assert.match(base, /\.app-modal\.app-modal-alert\s*\{[^}]*width:\s*min\(21rem, calc\(100vw - 2rem\)\)/s);
});
test('reduzierte Bewegung blendet nur über, reduzierte Transparenz ist deckend', () => {
  const reduced = mediaBody(base, 'prefers-reduced-motion: reduce', '.app-modal');
  assert.doesNotMatch(reduced, /transform/);
  assert.match(mediaBody(base, 'prefers-reduced-transparency: reduce', '.app-modal'), /backdrop-filter:\s*none/);
  assert.match(mediaBody(base, 'prefers-contrast: more', '.app-modal'), /border-color:\s*var\(--text-muted\)/);
});
test('der tote Neuigkeiten-Dialog ist weg', () => {
  for (const name of ['update-bar-modal', 'whatsnew-head', 'whatsnew-empty', 'whatsnew-tile']) {
    assert.doesNotMatch(base, new RegExp(`\\.${name}\\b`), name);
  }
  assert.match(base, /\.whatsnew-detail\b/, 'die Highlight-Details bleiben');
});
test('der Picker korrigiert nur noch Breite und Innenabstand', () => {
  const rule = block(manager, '.app-modal.device-picker');
  assert.doesNotMatch(rule, /border-left-width|border-radius|overflow/);
  assert.match(rule, /padding:\s*0/);
});
test('Bestätigung und Layout-Guard sind Hinweise', () => {
  assert.match(baseHTML, /<dialog class="app-modal app-modal-alert" x-data aria-labelledby="app-modal-title"/);
  assert.match(editorHTML, /<dialog id="layout-guard-modal" class="app-modal app-modal-danger app-modal-alert"/);
});
