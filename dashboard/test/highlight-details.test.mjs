// Tests for highlight-details.js: the shared details of a highlight. The
// logic is the one of whatsnew.page.js, ported to window.highlightDetails.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'highlight-details.js'), 'utf8');

function create() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'https://dashboard.local/' });
  installI18n(dom.window);
  vm.runInContext(source, dom.getInternalVMContext());
  return { details: dom.window.highlightDetails, window: dom.window };
}

test('text uses the page language and appends the PR number', () => {
  const { details } = create();
  assert.equal(details.text({ text: 'x', pr: 7 }), 'x (#7)');
  assert.equal(details.text({ text: 'layers', pr: 7, i18n: { de: 'Ebenen' } }), 'Ebenen (#7)');
  assert.equal(details.text({ text: 'layers', pr: 7, i18n: { fr: 'couches' } }), 'layers (#7)');
  assert.equal(details.text({ text: 'layers' }), 'layers');
});

test('detailText uses the page language and falls back to English', () => {
  const { details } = create();
  assert.equal(details.detailText({ details: { text: { en: 'a', de: 'b' } } }), 'b');
  assert.equal(details.detailText({ details: { text: { en: 'a' } } }), 'a');
});

test('imageFrame without crop fills the frame', () => {
  const { details } = create();
  assert.deepEqual({ ...details.imageFrame({ width: 1000, height: 500 }, true) },
    { aspect: '1000 / 500', left: '0%', top: '0%', width: '100%', height: '100%' });
});

test('imageFrame with crop zooms into the detail', () => {
  const { details } = create();
  assert.deepEqual({ ...details.imageFrame({ width: 1000, height: 500, crop: [0.5, 0.2, 0.5, 0.4] }, true) },
    { aspect: '500 / 200', left: '-100%', top: '-50%', width: '200%', height: '250%' });
});

test('imageFrame with cropped false shows the whole image', () => {
  const { details } = create();
  assert.deepEqual({ ...details.imageFrame({ width: 1000, height: 500, crop: [0.5, 0.2, 0.5, 0.4] }, false) },
    { aspect: '1000 / 500', left: '0%', top: '0%', width: '100%', height: '100%' });
});

test('onToggle scrolls the opened row into view', () => {
  const { details, window } = create();
  const timers = [];
  window.setTimeout = (fn, ms) => { timers.push({ fn, ms }); };
  let options = null;
  const target = { open: true, scrollIntoView(o) { options = o; } };
  details.onToggle({ target });
  assert.equal(timers.length, 1);
  assert.equal(timers[0].ms, 330);
  assert.equal(options, null);
  timers[0].fn();
  assert.equal(options.block, 'nearest');
  assert.equal(options.behavior, 'smooth');
});

test('onToggle does nothing when the row closes', () => {
  const { details, window } = create();
  const timers = [];
  window.setTimeout = (fn, ms) => { timers.push({ fn, ms }); };
  details.onToggle({ target: { open: false, scrollIntoView() { throw new Error('must not scroll'); } } });
  assert.equal(timers.length, 0);
});
