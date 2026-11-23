// Tests for the monitoring forecast and year components (window.MonitoringInsight drives the numbers).
// Alpine is a stub that only captures the factories, fetch is faked, storage can be made to throw.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const modelSource = fs.readFileSync(path.join(jsDir, 'monitoring-insight-model.js'), 'utf8');
const componentSource = fs.readFileSync(path.join(jsDir, 'monitoring-insight.js'), 'utf8');

const FORECAST_PATH = '/api/v1/monitoring/forecast';

function response(body) {
  return { ok: true, status: 200, json: async () => JSON.parse(JSON.stringify(body)) };
}

function create({ forecast = { needed: 14, days: [], readers: [] }, season = null, fail = false, throwStorage = false } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', url: 'http://localhost/' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  if (throwStorage) {
    Object.defineProperty(dom.window, 'localStorage', { configurable: true, get() { throw new Error('storage denied'); } });
  }
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  const calls = [];
  dom.window.fetch = async (url, options) => {
    calls.push({ url, options });
    if (fail) return { ok: false, status: 500, json: async () => ({}) };
    return response({ forecast, season });
  };
  vm.runInContext(modelSource, context);
  vm.runInContext(componentSource, context);
  return { dom, factories, calls };
}

function forecastComponent(options) {
  const { dom, factories, calls } = create(options);
  const component = factories.monitoringForecast();
  component.$refs = {};
  return { component, calls, dom };
}

test('monitoringForecast loads once', async () => {
  const { component, calls } = forecastComponent();
  await component.load();
  await component.load();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, FORECAST_PATH);
  assert.equal(component.loaded, true);
});

test('monitoringForecast shows the learning status for a fresh node', async () => {
  const { component } = forecastComponent({ forecast: { needed: 14, days: [], readers: [] } });
  await component.load();
  assert.equal(component.status.kind, 'learning');
  assert.equal(component.learning, true);
  assert.ok(component.status.text.includes('14'), component.status.text);
});

test('monitoringForecast lists the readers and says so when there are none', async () => {
  const readers = [{ id: 'a', name: 'Garage Speicher' }, { id: 'b', name: 'Warmwasser' }];
  const withReaders = forecastComponent({ forecast: { needed: 14, days: [], readers } });
  await withReaders.component.load();
  assert.ok(withReaders.component.useText.includes('Garage Speicher'));
  assert.ok(withReaders.component.useText.includes('Warmwasser'));

  const none = forecastComponent({ forecast: { needed: 14, days: [], readers: [] } });
  await none.component.load();
  assert.equal(none.component.useText, none.dom.window.I18n.t('monitoring.forecast.use_none'));
});

test('monitoringForecast reports a load error', async () => {
  const { component } = forecastComponent({ fail: true });
  await component.load();
  assert.equal(component.failed, true);
  assert.equal(component.loaded, false);
  assert.ok(component.failedText.length > 0);
});

test('monitoringYear keeps the metric in storage', () => {
  const { dom, factories } = create();
  const component = factories.monitoringYear(() => null);
  component.$refs = {};
  component.setMetric('base');
  assert.equal(component.metric, 'base');
  assert.equal(dom.window.localStorage.getItem('monitoring.season.metric'), 'base');
});

test('monitoringYear works with a throwing storage', () => {
  const { factories } = create({ throwStorage: true });
  const component = factories.monitoringYear(() => null);
  component.$refs = {};
  assert.equal(component.metric, 'pv');
  assert.doesNotThrow(() => component.setMetric('base'));
  assert.equal(component.metric, 'base');
});
