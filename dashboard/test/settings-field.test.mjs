// settings-field.js: saveSettingsFields keeps the rest of the settings document,
// writesPerDay rounds the write rate and a rejected PUT keeps its HTTP code.
// Run with `node --test test/settings-field.test.mjs` from dashboard/.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'settings-field.js'), 'utf8');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  dom.window.eval(source);
  return dom.window;
}

// Fake fetch for GET and PUT on /api/v1/settings. Answers the PUT with putStatus.
function fakeFetch({ document, putStatus = 200 }) {
  const calls = [];
  const fetchFn = async (url, options = {}) => {
    calls.push({ url, method: options.method || 'GET', body: options.body });
    if (options.method === 'PUT') {
      const ok = putStatus >= 200 && putStatus < 300;
      return {
        ok,
        status: putStatus,
        json: async () => (ok ? JSON.parse(options.body) : { message: 'forbidden' }),
      };
    }
    return { ok: true, status: 200, json: async () => document };
  };
  return { fetchFn, calls };
}

test('saveSettingsFields merges the patch into the stored document', async () => {
  const window = load();
  const { fetchFn, calls } = fakeFetch({ document: { theme: 'signalgelb', sweep_interval_seconds: 300 } });
  window.fetch = fetchFn;
  const fetchJSON = (url, options) => window.eval('settingsRequest')(url, options);

  const saved = await window.saveSettingsFields({ sweep_interval_seconds: 600 }, fetchJSON);

  const put = calls.find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/settings');
  const body = JSON.parse(put.body);
  assert.equal(body.sweep_interval_seconds, 600);
  assert.equal(body.theme, 'signalgelb');
  assert.deepEqual(saved, body);
});

test('writesPerDay turns the interval into writes per day', () => {
  const window = load();
  assert.equal(window.writesPerDay(300), 288);
  assert.equal(window.writesPerDay(0), 0);
});

test('a 403 on PUT rejects with an error that carries the code', async () => {
  const window = load();
  const { fetchFn } = fakeFetch({ document: { theme: 'mint' }, putStatus: 403 });
  window.fetch = fetchFn;
  const fetchJSON = (url, options) => window.eval('settingsRequest')(url, options);

  await assert.rejects(
    window.saveSettingsFields({ health_score_threshold: 120 }, fetchJSON),
    (error) => {
      assert.equal(error.status, 403);
      return true;
    },
  );
});
