import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { fakeToastStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'notifications.js'),
  'utf8',
);

function createNotifications({ deviceResponse, status = 200 } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const requestedURLs = [];
  dom.window.fetch = async (url) => {
    requestedURLs.push(url);
    if (status === 404) return { ok: false, status: 404, json: async () => ({ code: 'device_not_found' }) };
    return { ok: true, status: 200, json: async () => deviceResponse || null };
  };
  Object.defineProperty(dom.window.document, 'visibilityState', { value: 'visible', configurable: true });
  const toasts = fakeToastStore();
  dom.window.Alpine = { store: (name) => (name === 'toasts' ? toasts : null) };
  // Der Poller startet sein setInterval erst auf alpine:init - hier nicht
  // ausgeloest, weil die Tests poll() direkt rufen. Ein echtes 10s-Intervall
  // wuerde den Testprozess offen halten.
  vm.runInContext(scriptSource, context);
  return { window: dom.window, toasts, requestedURLs };
}

// last_event uses value_template ({{ value_json.message }}) to reduce the
// topic payload to just the message string for .value - the Go registry has
// no json_attributes_topic support, so the full {at, message} document only
// exists in .last_message.payload, the raw untemplated MQTT payload.
function automationDevice({ message, at }) {
  return {
    id: 'automation',
    entities: [{
      object_id: 'last_event',
      value: message,
      last_message: { topic: 'outstation/automation/last_event', payload: JSON.stringify({ message, at }) },
    }],
  };
}

test('a new last_event value produces exactly one toast', async () => {
  const { window, toasts } = createNotifications({ deviceResponse: automationDevice({ message: 'info: Test - alles ok', at: 100 }) });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 1);
});

test('the same last_event value does not produce a second toast', async () => {
  const { window, toasts } = createNotifications({ deviceResponse: automationDevice({ message: 'info: Test - alles ok', at: 100 }) });
  await window.__automationNotifications.poll();
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 1);
});

test('a later at-timestamp produces a second toast', async () => {
  const { window, toasts } = createNotifications({ deviceResponse: automationDevice({ message: 'info: erstens', at: 100 }) });
  await window.__automationNotifications.poll();
  window.fetch = async () => ({ ok: true, status: 200, json: async () => automationDevice({ message: 'info: zweitens', at: 200 }) });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 2);
});

test('severity is derived from the message prefix', () => {
  const { window } = createNotifications();
  assert.equal(window.__automationNotifications.severityFromMessage('critical: Ausfall'), 'critical');
  assert.equal(window.__automationNotifications.severityFromMessage('warning: Achtung'), 'warning');
  assert.equal(window.__automationNotifications.severityFromMessage('info: Alles gut'), 'info');
});

test('a critical last_event is pushed with the critical severity', async () => {
  const { window, toasts } = createNotifications({ deviceResponse: automationDevice({ message: 'critical: Ausfall', at: 100 }) });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items[0].severity, 'critical');
});

test('polling is skipped while the document is not visible', async () => {
  const { window, toasts } = createNotifications({ deviceResponse: automationDevice({ message: 'info: x', at: 100 }) });
  Object.defineProperty(window.document, 'visibilityState', { value: 'hidden', configurable: true });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 0);
});

test('der Poller fragt die Einzelgeraete-Route, nicht die Geraeteliste', async () => {
  const { window, requestedURLs } = createNotifications({ deviceResponse: automationDevice({ message: 'info: x', at: 100 }) });
  await window.__automationNotifications.poll();
  assert.deepEqual(requestedURLs, ['/api/v1/devices/automation']);
});

test('eine Instanz ohne Automations-Dienst (404) bleibt still', async () => {
  const { window, toasts } = createNotifications({ status: 404 });
  // Kein throw: 404 ist hier der Normalfall, nicht ein Fehler. Vorher lieferte
  // das find() ueber die Geraeteliste in diesem Fall schlicht undefined.
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 0);
});
