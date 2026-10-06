// The shared revision panel shows an optional "active revision" line. Only
// the configuration page supplies it; every other page leaves it empty.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'revisions.js'), 'utf8');

function createPanel(config) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  installI18n(dom.window, { lang: 'en' });
  vm.runInContext(source, dom.getInternalVMContext());
  return factory(config);
}

test('a page without an active revision shows no line', () => {
  const panel = createPanel({ basePath: '/api/v1/layout' });
  assert.equal(panel.activeRevision, '');
  assert.equal(panel.activeRevisionText, '');
});

test('the active revision is read live from the owning page', () => {
  let revision = '';
  const panel = createPanel({ basePath: '/x', activeRevision: () => revision });
  assert.equal(panel.activeRevision, '');
  revision = '7e757377';
  assert.equal(panel.activeRevision, '7e757377');
  assert.equal(panel.activeRevisionText, 'Active revision 7e757377.');
});
