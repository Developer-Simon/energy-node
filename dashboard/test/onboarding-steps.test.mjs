// Orte der Einrichtungsschritte in onboarding-steps.js. Jeder Schritt des
// Servers (internal/onboarding/steps.go) hat genau einen Ort, und umgekehrt.
// Run with `npm test` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'onboarding-steps.js'),
  'utf8',
);
const goSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'onboarding', 'steps.go'),
  'utf8',
);

// Die Katalog-IDs stehen in steps.go als Step{ID: "…"}.
const CATALOG_IDS = [...goSource.matchAll(/\{ID: "([a-z]+)"/g)].map((m) => m[1]);

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.obSteps;
}

test('the Go catalog IDs are read from steps.go', () => {
  assert.equal(CATALOG_IDS.length, 16);
});

test('every setup step has a place and the server catalog IDs match', () => {
  const { setup } = load();
  const ids = setup.map((s) => s.id);
  assert.equal(new Set(ids).size, ids.length, 'IDs are unique');
  assert.deepEqual([...ids].sort(), [...CATALOG_IDS].sort());
});

test('each setup place has panel, optional page and prepare, and a target', () => {
  const { setup } = load();
  for (const step of setup) {
    assert.equal(typeof step.panel, 'string', `${step.id}: panel`);
    assert.ok(step.panel.length > 0, `${step.id}: panel is not empty`);
    if (step.page !== undefined) assert.equal(typeof step.page, 'string', `${step.id}: page`);
    if (step.prepare !== undefined) assert.equal(typeof step.prepare, 'function', `${step.id}: prepare`);
    assert.equal(typeof step.target, 'string', `${step.id}: target`);
    assert.ok(['change', 'leave'].includes(step.check), `${step.id}: check`);
  }
});

test('every setup target uses data-tour', () => {
  const { setup } = load();
  for (const step of setup) {
    assert.equal(step.target, `[data-tour="${step.id}"]`, step.id);
  }
});
