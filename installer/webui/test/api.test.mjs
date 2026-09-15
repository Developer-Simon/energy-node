import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

function load() {
  const dom = loadScripts(['api.js']);
  dom.window.Api.configure({ basePath: '', token: 'tok' });
  return dom;
}

test('jede Anfrage traegt das Token als Kopfzeile', async () => {
  const { window } = load();
  let seen = null;
  window.fetch = async (url, init) => {
    seen = { url, init };
    return { ok: true, status: 200, json: async () => ({}) };
  };
  await window.Api.get('/api/bootstrap');
  assert.equal(seen.url, '/api/bootstrap');
  assert.equal(seen.init.headers['X-Installer-Token'], 'tok');
  assert.equal(seen.init.body, undefined);
});

test('post schickt JSON und liest JSON', async () => {
  const { window } = load();
  let seen = null;
  window.fetch = async (url, init) => {
    seen = { url, init };
    return { ok: true, status: 202, json: async () => ({ run_id: 'run-1' }) };
  };
  const result = await window.Api.post('/api/run', { mode: 'install' });
  assert.equal(seen.init.method, 'POST');
  assert.equal(seen.init.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(seen.init.body), { mode: 'install' });
  assert.equal(result.run_id, 'run-1');
});

test('put schickt JSON', async () => {
  const { window } = load();
  let seen = null;
  window.fetch = async (url, init) => {
    seen = init;
    return { ok: true, status: 200, json: async () => ({ steps: {} }) };
  };
  await window.Api.put('/api/selection', { steps: { 40: true } });
  assert.equal(seen.method, 'PUT');
  assert.deepEqual(JSON.parse(seen.body), { steps: { 40: true } });
});

test('eine Fehlerantwort wird zu ApiError mit Code, Detail und Status', async () => {
  const { window } = load();
  window.fetch = async () => ({
    ok: false,
    status: 409,
    json: async () => ({ error: 'HOSTKEY_UNKNOWN', detail: 'SHA256:abc' }),
  });
  await assert.rejects(() => window.Api.post('/api/connect', {}), (err) => {
    assert.ok(err instanceof window.ApiError);
    assert.equal(err.code, 'HOSTKEY_UNKNOWN');
    assert.equal(err.detail, 'SHA256:abc');
    assert.equal(err.status, 409);
    return true;
  });
});

test('eine Fehlerantwort ohne JSON-Rumpf bekommt trotzdem einen Code', async () => {
  const { window } = load();
  window.fetch = async () => ({ ok: false, status: 500, json: async () => { throw new Error('not json'); } });
  await assert.rejects(() => window.Api.get('/api/plan'), (err) => err.code === 'BACKEND_ERROR' && err.status === 500);
});

test('ein nicht erreichbarer Wirt wird zu NETWORK', async () => {
  const { window } = load();
  window.fetch = async () => { throw new TypeError('Failed to fetch'); };
  await assert.rejects(() => window.Api.get('/api/plan'), (err) => err.code === 'NETWORK' && err.status === 0);
});

test('url() haengt Token und Parameter an - fuer EventSource, das keine Kopfzeilen kann', () => {
  const { window } = load();
  window.Api.configure({ basePath: '/installer', token: 'a b' });
  assert.equal(window.Api.url('/api/events', { since: 7 }), '/installer/api/events?token=a%20b&since=7');
});
