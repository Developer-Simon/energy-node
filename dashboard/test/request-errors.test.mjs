import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'revisions.js'), 'utf8');

async function loadRevisionsWith(response, lang = 'de') {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  dom.window.fetch = async () => response(dom.window);
  installI18n(dom.window, { lang });
  vm.runInContext(source, dom.getInternalVMContext());
  const toasts = [];
  const panel = factory({ basePath: '/api/v1/layout' });
  panel.$refs = {};
  panel.$store = { toasts: { push: (message) => toasts.push(message) } };
  await panel.load();
  return toasts;
}

test('a non-JSON error page shows the fallback text, not a SyntaxError', async () => {
  const toasts = await loadRevisionsWith(() => ({
    ok: false,
    status: 502,
    json: async () => { throw new SyntaxError('Unexpected token'); },
  }));
  assert.deepEqual(toasts, [catalog('de')['common.request_failed']]);
});

test('an unknown code shows the server message', async () => {
  const body = JSON.stringify({ code: 'brand_new', message: 'Neu vom Server' });
  const toasts = await loadRevisionsWith(() => ({
    ok: false,
    status: 400,
    json: async () => JSON.parse(body),
  }));
  assert.deepEqual(toasts, ['Neu vom Server']);
});

test('a known code is translated and keeps the detail', async () => {
  const body = JSON.stringify({ code: 'layout_revisions_failed', message: 'open x: denied', detail: 'open x: denied' });
  const toasts = await loadRevisionsWith(() => ({
    ok: false,
    status: 500,
    json: async () => JSON.parse(body),
  }), 'en');
  const expectedMessage = catalog('en')['error.with_detail']
    .replace('{message}', catalog('en')['error.layout_revisions_failed'])
    .replace('{detail}', 'open x: denied');
  assert.deepEqual(toasts, [expectedMessage]);
});
