// Tests for the monitoring page component. The component is called directly
// (the automations.page.test.mjs convention): fetch is faked, Alpine is a stub
// that only captures the factory.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { catalog, installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const modelSource = fs.readFileSync(path.join(jsDir, 'monitoring-model.js'), 'utf8');
const pageSource = fs.readFileSync(path.join(jsDir, 'monitoring.page.js'), 'utf8');

const NOW = Math.floor(Date.now() / 1000);

function response(body) {
  return { ok: true, status: 200, json: async () => JSON.parse(JSON.stringify(body)) };
}

function createPanel({ roles = ['edit_setup'], notices = null, ackReply = null, failMonitoring = false } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  installI18n(dom.window);
  let factory;
  dom.window.Alpine = { data: (_name, fn) => { factory = fn; } };
  const calls = [];
  const list = notices || [
    {
      id: 'learn:shade:225:15', source: 'learn', severity: 'info', code: 'shade',
      params: { kind: 'shade', from: '14:35', to: '15:49', kwh_lost: 5.7, weeks: [4, 5, 6] },
      steps: [], first_seen: NOW - 3600, ack: null,
    },
    {
      id: 'energy:unassigned', source: 'energy', severity: 'info', code: 'unassigned',
      params: { count: 2, names: ['Werkstatt', 'Garage'], more: 0 },
      steps: [{ code: 'assign_roles', target: 'energy.focus', ids: ['a'] }],
      first_seen: NOW - 2 * 86400, ack: { at: NOW - 60, user: 'admin' },
    },
  ];
  dom.window.fetch = async (url, options) => {
    calls.push({ url, options });
    if (url === '/api/v1/monitoring' && failMonitoring) {
      return { ok: false, status: 500, json: async () => ({}) };
    }
    if (url === '/api/v1/monitoring') {
      return response({ notices: list, new_count: list.filter((n) => !n.ack).length, learned: { enabled: true, values: [] } });
    }
    if (url === '/api/v1/auth/session') {
      return response({ csrf_token: 'tok-1', roles });
    }
    if (url === '/api/v1/monitoring/ack') {
      return response(ackReply || { notices: list, new_count: 0, learned: { enabled: true, values: [] } });
    }
    throw new Error('unexpected fetch ' + url);
  };
  vm.runInContext(modelSource, context);
  vm.runInContext(pageSource, context);
  const component = factory();
  component.$refs = {};
  component.$nextTick = (callback) => callback();
  component.$watch = () => {};
  return { component, calls };
}

test('load() splits the notices into a new and an acknowledged group', async () => {
  const { component } = createPanel();
  await component.load();
  const groups = component.groups();
  assert.equal(groups.length, 2);
  assert.equal(groups.map((g) => g.id).join(','), 'new,known');
  assert.equal(groups[0].items[0].id, 'learn:shade:225:15');
  assert.equal(groups[1].items[0].id, 'energy:unassigned');
  assert.equal(component.newCount, 1);
});

test('ack() posts the id and the acknowledgement with the session token', async () => {
  const { component, calls } = createPanel();
  await component.load();
  await component.ack('learn:shade:225:15');
  const post = calls.find((call) => call.url === '/api/v1/monitoring/ack');
  assert.ok(post, 'ack request was sent');
  assert.equal(post.options.method, 'POST');
  assert.equal(post.options.headers['X-CSRF-Token'], 'tok-1');
  assert.deepEqual(JSON.parse(post.options.body), { id: 'learn:shade:225:15', ack: true });
});

test('without edit_setup the card offers no acknowledge button', async () => {
  const { component } = createPanel({ roles: ['view_energy'] });
  await component.load();
  const html = component.cardHtml(component.notices[0]);
  assert.equal(html.includes('data-act="ack"'), false);
  assert.equal(html.includes('data-act="unack"'), false);
  assert.ok(html.includes('Quittieren können nur Nutzer mit dem Recht Einrichten.'));
});

test('a failed monitoring load sets the failed flag and shows no groups', async () => {
  const { component } = createPanel({ failMonitoring: true });
  await component.load();
  assert.equal(component.failed, true);
  assert.equal(component.loaded, true);
  assert.equal(component.groups().length, 0);
});

test('the load error text is in the catalog and the template shows it on failure', () => {
  assert.equal(catalog('de')['monitoring.load_failed'], 'Monitoring konnte nicht geladen werden.');
  const template = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'monitoring.html'), 'utf8');
  assert.match(template, /x-show="failed"[^>]*>\{\{t "monitoring\.load_failed"\}\}</);
  assert.match(template, /x-if="loaded && !failed && !list\(\)\.length"/);
});

test('goLearned(key) sets learnedFocus and opens the learned view', () => {
  const { component } = createPanel();
  component.goLearned('device/plug/usual_on');
  assert.equal(component.learnedFocus, 'device/plug/usual_on');
  assert.equal(component.view, 'learned');
  component.goLearned();
  assert.equal(component.learnedFocus, '');
});

test('goLearned(key) clears the focus first so a repeated key is followed again', () => {
  const { component } = createPanel();
  const writes = [];
  let value = component.learnedFocus;
  Object.defineProperty(component, 'learnedFocus', {
    get: () => value,
    set: (v) => { writes.push(v); value = v; },
  });
  component.goLearned('site/day_start');
  component.goLearned('site/day_start');
  assert.deepEqual(writes, ['', 'site/day_start', '', 'site/day_start']);
  assert.equal(value, 'site/day_start');
});
