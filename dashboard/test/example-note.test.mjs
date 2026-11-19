// The example note under the masthead (example-note.js): "Simulation aus"
// and "Simulation aus und Beispiel entfernen" POST to /api/v1/example/stop,
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
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'example-note.js'), 'utf8');

const KEPT = 'Behalten, weil geändert: {names}';
const FAILED = 'Das hat nicht geklappt. Versuch es noch einmal.';

function setup() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  const { window } = dom;
  window.document.body.innerHTML = `
    <div class="example-note" data-example-note data-example-kept="${KEPT}" data-example-failed="${FAILED}">
      <div class="example-note-body">
        <p>text</p>
        <div class="example-note-actions">
          <button type="button" data-example-off>Simulation aus</button>
          <button type="button" data-example-remove>Simulation aus und Beispiel entfernen</button>
        </div>
        <p class="example-note-msg" data-example-msg hidden></p>
      </div>
    </div>`;
  const root = window.document.querySelector('[data-example-note]');
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
  window.exampleNote.bind(root, deps);
  return { window, root, calls, reloads, state };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const stopCall = (calls) => calls.find((c) => c.url.endsWith('/api/v1/example/stop'));

test('off button posts remove false with the CSRF token and reloads', async () => {
  const { root, calls, reloads, window } = setup();
  root.querySelector('[data-example-off]').click();
  await flush();
  const call = stopCall(calls);
  assert.ok(call, 'stop endpoint was called');
  assert.equal(call.options.method, 'POST');
  assert.equal(call.options.headers['X-CSRF-Token'], 'tok-123');
  assert.equal(call.options.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(call.options.body), { remove: false });
  assert.equal(reloads.count, 1);
  assert.equal(root.querySelector('[data-example-msg]').hidden, true);
  assert.equal(window.document.querySelector('[data-example-off]').disabled, false);
});

test('remove button posts remove true and reloads', async () => {
  const { root, calls, reloads } = setup();
  root.querySelector('[data-example-remove]').click();
  await flush();
  assert.deepEqual(JSON.parse(stopCall(calls).options.body), { remove: true });
  assert.equal(reloads.count, 1);
});

test('kept entries are named without their kind prefix and the page is not reloaded', async () => {
  const { root, reloads, state } = setup();
  state.stopBody = { removed: ['automation:x'], kept: ['device:example_plug', 'scenario:example_scene'] };
  root.querySelector('[data-example-remove]').click();
  await flush();
  const msg = root.querySelector('[data-example-msg]');
  assert.equal(msg.hidden, false);
  assert.equal(msg.textContent, 'Behalten, weil geändert: example_plug, example_scene');
  assert.equal(reloads.count, 0);
});

test('a failed request shows the failure text and does not reload', async () => {
  const { root, reloads, state } = setup();
  state.stopOk = false;
  root.querySelector('[data-example-off]').click();
  await flush();
  const msg = root.querySelector('[data-example-msg]');
  assert.equal(msg.hidden, false);
  assert.equal(msg.textContent, FAILED);
  assert.equal(reloads.count, 0);
});

test('stripKind removes only the first kind prefix', () => {
  const { window } = setup();
  const { stripKind } = window.exampleNote;
  assert.equal(stripKind('device:example_plug'), 'example_plug');
  assert.equal(stripKind('example_plain'), 'example_plain');
  assert.equal(stripKind('device:a:b'), 'a:b');
});
