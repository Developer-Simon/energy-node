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

// The poller now reads /api/v1/automation/notification, which returns the
// {at, message} document already parsed from the last_event state topic. No
// more entities.find() over a single-device payload, no more JSON.parse of a
// raw last_message slot that an availability heartbeat could clobber.
function createNotifications({ notification, status = 200 } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  const requestedURLs = [];
  dom.window.fetch = async (url) => {
    requestedURLs.push(url);
    if (status === 404) return { ok: false, status: 404, json: async () => ({ code: 'automation_not_found' }) };
    if (status === 204) return { ok: true, status: 204, json: async () => { throw new Error('204 has no body'); } };
    return { ok: true, status: 200, json: async () => notification || null };
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

test('a new notification produces exactly one toast', async () => {
  const { window, toasts } = createNotifications({ notification: { message: 'info: Test - alles ok', at: 100 } });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 1);
});

test('the same notification does not produce a second toast', async () => {
  const { window, toasts } = createNotifications({ notification: { message: 'info: Test - alles ok', at: 100 } });
  await window.__automationNotifications.poll();
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 1);
});

test('a later at-timestamp produces a second toast', async () => {
  const { window, toasts } = createNotifications({ notification: { message: 'info: erstens', at: 100 } });
  await window.__automationNotifications.poll();
  window.fetch = async () => ({ ok: true, status: 200, json: async () => ({ message: 'info: zweitens', at: 200 }) });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 2);
});

test('severity is derived from the message prefix', () => {
  const { window } = createNotifications();
  assert.equal(window.__automationNotifications.severityFromMessage('critical: Ausfall'), 'critical');
  assert.equal(window.__automationNotifications.severityFromMessage('warning: Achtung'), 'warning');
  assert.equal(window.__automationNotifications.severityFromMessage('info: Alles gut'), 'info');
});

test('a critical notification is pushed with the critical severity', async () => {
  const { window, toasts } = createNotifications({ notification: { message: 'critical: Ausfall', at: 100 } });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items[0].severity, 'critical');
});

test('polling is skipped while the document is not visible', async () => {
  const { window, toasts } = createNotifications({ notification: { message: 'info: x', at: 100 } });
  Object.defineProperty(window.document, 'visibilityState', { value: 'hidden', configurable: true });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 0);
});

test('der Poller fragt den Notification-Endpunkt, nicht die Geraeteroute', async () => {
  const { window, requestedURLs } = createNotifications({ notification: { message: 'info: x', at: 100 } });
  await window.__automationNotifications.poll();
  assert.deepEqual(requestedURLs, ['/api/v1/automation/notification']);
});

test('eine Instanz ohne Automations-Dienst (404) bleibt still', async () => {
  const { window, toasts } = createNotifications({ status: 404 });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 0);
});

test('ein Dienst ohne bisheriges Ereignis (204) bleibt still', async () => {
  const { window, toasts } = createNotifications({ status: 204 });
  await window.__automationNotifications.poll();
  assert.equal(toasts.items.length, 0);
});
