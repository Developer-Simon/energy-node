// The example note under the masthead (beispiel-note.js): "Simulation aus"
// and "Simulation aus und Beispiel entfernen" POST to /api/v1/beispiel/stop,
// reload the page on success, and show the kept entries or the failure text
// instead. Same vm + jsdom technique as the other page tests.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'beispiel-note.js'), 'utf8');

const KEPT = 'Behalten, weil geändert: {names}';
const FAILED = 'Das hat nicht geklappt. Versuch es noch einmal.';

function setup() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  const { window } = dom;
  window.document.body.innerHTML = `
    <div class="beispiel-note" data-beispiel-note data-beispiel-kept="${KEPT}" data-beispiel-failed="${FAILED}">
      <div class="beispiel-note-body">
        <p>text</p>
        <div class="beispiel-note-actions">
          <button type="button" data-beispiel-off>Simulation aus</button>
          <button type="button" data-beispiel-remove>Simulation aus und Beispiel entfernen</button>
        </div>
        <p class="beispiel-note-msg" data-beispiel-msg hidden></p>
      </div>
    </div>`;
  const root = window.document.querySelector('[data-beispiel-note]');
  const calls = [];
  const reloads = { count: 0 };
  const state = { stopBody: { removed: [], kept: [] }, stopOk: true, sessionToken: 'tok-123' };
  const deps = {
    fetch: async (url, options) => {
      calls.push({ url, options });
      if (url.endsWith('/api/v1/auth/session')) {
        return { ok: true, status: 200, json: async () => ({ csrf_token: state.sessionToken }) };
      }
      return { ok: state.stopOk, status: state.stopOk ? 200 : 500, json: async () => state.stopBody };
    },
    reload: () => { reloads.count += 1; },
  };
  window.beispielNote.bind(root, deps);
  return { window, root, calls, reloads, state };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const stopCall = (calls) => calls.find((c) => c.url.endsWith('/api/v1/beispiel/stop'));

test('off button posts remove false with the CSRF token and reloads', async () => {
  const { root, calls, reloads, window } = setup();
  root.querySelector('[data-beispiel-off]').click();
  await flush();
  const call = stopCall(calls);
  assert.ok(call, 'stop endpoint was called');
  assert.equal(call.options.method, 'POST');
  assert.equal(call.options.headers['X-CSRF-Token'], 'tok-123');
  assert.equal(call.options.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(call.options.body), { remove: false });
  assert.equal(reloads.count, 1);
  assert.equal(root.querySelector('[data-beispiel-msg]').hidden, true);
  assert.equal(window.document.querySelector('[data-beispiel-off]').disabled, false);
});

test('remove button posts remove true and reloads', async () => {
  const { root, calls, reloads } = setup();
  root.querySelector('[data-beispiel-remove]').click();
  await flush();
  assert.deepEqual(JSON.parse(stopCall(calls).options.body), { remove: true });
  assert.equal(reloads.count, 1);
});

test('kept entries are named without their kind prefix and the page is not reloaded', async () => {
  const { root, reloads, state } = setup();
  state.stopBody = { removed: ['automation:x'], kept: ['device:beispiel_steckdose', 'scenario:beispiel_szene'] };
  root.querySelector('[data-beispiel-remove]').click();
  await flush();
  const msg = root.querySelector('[data-beispiel-msg]');
  assert.equal(msg.hidden, false);
  assert.equal(msg.textContent, 'Behalten, weil geändert: beispiel_steckdose, beispiel_szene');
  assert.equal(reloads.count, 0);
});

test('a failed request shows the failure text and does not reload', async () => {
  const { root, reloads, state } = setup();
  state.stopOk = false;
  root.querySelector('[data-beispiel-off]').click();
  await flush();
  const msg = root.querySelector('[data-beispiel-msg]');
  assert.equal(msg.hidden, false);
  assert.equal(msg.textContent, FAILED);
  assert.equal(reloads.count, 0);
});

test('stripKind removes only the first kind prefix', () => {
  const { window } = setup();
  const { stripKind } = window.beispielNote;
  assert.equal(stripKind('device:beispiel_steckdose'), 'beispiel_steckdose');
  assert.equal(stripKind('beispiel_plain'), 'beispiel_plain');
  assert.equal(stripKind('device:a:b'), 'a:b');
});
