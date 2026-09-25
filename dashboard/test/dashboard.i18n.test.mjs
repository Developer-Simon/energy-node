// The runtime status bar labels come from the catalog via I18n.t, so a
// language switch changes them without a reload of dashboard.js.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function createStatusPanel(I18n) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'http://localhost/',
  });
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.setInterval = () => 0;
  if (I18n) dom.window.I18n = I18n;
  vm.runInContext(source, dom.getInternalVMContext());
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return factories.runtimeStatusPanel();
}

const english = {
  t: (key, params = {}) => ({
    'status.loading': 'Loading status ...',
    'status.unavailable': 'Status unavailable',
    'status.ok': 'Operational',
    'status.degraded': 'Degraded',
    'status.uptime.days_hours': `${params.days} d ${params.hours} h`,
    'status.uptime.hours_minutes': `${params.hours} h ${params.minutes} min`,
    'status.uptime.minutes': `${params.minutes} min`,
  }[key] || key),
};

test('statusLabel reads the catalog', () => {
  const panel = createStatusPanel(english);
  panel.loading = true;
  assert.equal(panel.statusLabel, 'Loading status ...');
  panel.loading = false;
  panel.error = 'x';
  assert.equal(panel.statusLabel, 'Status unavailable');
  panel.error = '';
  panel.status = { status: 'ok' };
  assert.equal(panel.statusLabel, 'Operational');
  panel.status = { status: 'degraded' };
  assert.equal(panel.statusLabel, 'Degraded');
});

test('formatUptime uses the catalog units', () => {
  const panel = createStatusPanel(english);
  assert.equal(panel.formatUptime(2 * 86400 + 3 * 3600), '2 d 3 h');
  assert.equal(panel.formatUptime(3 * 3600 + 5 * 60), '3 h 5 min');
  assert.equal(panel.formatUptime(90), '1 min');
  assert.equal(panel.formatUptime('x'), '-');
});

test('without the i18n runtime the keys stay visible instead of throwing', () => {
  const panel = createStatusPanel(undefined);
  panel.status = { status: 'ok' };
  assert.equal(panel.statusLabel, 'status.ok');
});
