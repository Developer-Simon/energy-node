// Tests for config-status.js: which settings/status belongs to one's own
// save, and how long to wait for it. Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'config-status.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.ConfigStatus;
}

test('classify ignores a status of another revision', () => {
  const S = load();
  assert.equal(S.classify({ received: true, runtime_status: 'ok', config_revision: 'old' }, 'new'), 'pending');
  assert.equal(S.classify({ received: true, runtime_status: 'ok', config_revision: 'new' }, 'new'), 'applied');
  assert.equal(S.classify({ received: true, runtime_status: 'rejected', config_revision: 'new' }, 'new'), 'rejected');
  assert.equal(S.classify({ received: false }, 'new'), 'pending');
  assert.equal(S.classify(null, 'new'), 'pending');
  assert.equal(S.classify({ received: true, runtime_status: 'ok', config_revision: '' }, ''), 'pending', 'no revision, no claim');
});

test('watch polls until its own revision answers', async () => {
  const S = load();
  const answers = [
    { received: true, runtime_status: 'ok', config_revision: 'old' },
    { received: true, runtime_status: 'rejected', config_revision: 'new', error: 'x' },
  ];
  let sleeps = 0;
  const result = await S.watch({ fetchStatus: async () => answers.shift(), revision: 'new', sleep: async () => { sleeps += 1; } });
  assert.equal(result.state, 'rejected');
  assert.equal(result.status.error, 'x');
  assert.equal(sleeps, 1);
});

test('watch gives up after the timeout', async () => {
  const S = load();
  let clock = 0;
  const result = await S.watch({
    fetchStatus: async () => { throw new Error('offline'); },
    revision: 'new', timeoutMs: 3000, intervalMs: 1000,
    now: () => clock, sleep: async ms => { clock += ms; },
  });
  assert.equal(result.state, 'no_response');
});

test('labels and error texts', () => {
  const S = load();
  assert.equal(S.label('applied'), 'Übernommen');
  assert.equal(S.label('rejected'), 'Abgelehnt');
  assert.equal(S.label('no_response'), 'Dienst antwortet nicht');
  assert.equal(S.label('pending'), 'Ausstehend');
  assert.equal(S.errorText({ error: 'roh', error_code: 'unbekannt' }), 'roh');
  S.ERROR_TEXTS.demo_code = 'übersetzt';
  assert.equal(S.errorText({ error: 'roh', error_code: 'demo_code' }), 'übersetzt');
});

test('battery error codes have German texts', () => {
  const S = load();
  for (const code of ['bank_a_voltage_required', 'bank_b_voltage_required', 'charge_source_required',
    'discharge_source_required', 'current_only_on_dc', 'ac_source_in_dc_system']) {
    assert.ok(S.ERROR_TEXTS[code], code);
    assert.doesNotMatch(S.ERROR_TEXTS[code], /;/, 'no semicolons in UI texts');
  }
});
