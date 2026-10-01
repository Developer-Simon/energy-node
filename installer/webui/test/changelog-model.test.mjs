import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

const { window } = loadScripts(['changelog-model.js']);
const M = window.ChangelogModel;

// Objekte aus dem jsdom-Kontext haben andere Prototypen als die des Tests.
const plain = (value) => JSON.parse(JSON.stringify(value));

const entry = (text, extra = {}) => ({ text, breaking: false, ...extra });
const release = (version, groups) => ({ version, date: '2026-09-21', groups });
const group = (type, entries) => ({ type, label: type, entries });

const DOCUMENT = {
  schema_version: 1,
  components: [
    { id: 'service:shelly', label: 'Shelly', kind: 'service', version: 'v0.4.2', releases: [
      release('v0.4.2', [group('fix', [entry('retry the rpc call', { scope: 'shelly', pr: 60 })])]),
      release('v0.4.1', [group('feat', [entry('add the power preset', { scope: 'shelly' })])]),
      release('v0.4.0', [group('feat', [entry('old thing')])]),
    ] },
    { id: 'dashboard', label: 'Dashboard', kind: 'app', version: 'v0.7.5', releases: [
      release('v0.7.5', [
        group('feat', [entry('add a versions page', { scope: 'dashboard', pr: 51 })]),
        group('fix', [entry('stop duplicate mounts', { scope: 'dashboard' }), entry('fix the badge', { scope: 'dashboard' })]),
      ]),
      release('v0.7.4', [group('feat', [entry('drop the legacy node block', { scope: 'dashboard', breaking: true, pr: 14 })])]),
      release('v0.7.3', [group('fix', [entry('ancient fix')])]),
    ] },
    { id: 'bootstrap', label: 'Bootstrap', kind: 'tool', version: 'v0.1.6', releases: [] },
  ],
};

const view = (installed) => ({ bundle_version: 'v0.7.5', installed, document: DOCUMENT });

test('compare orders by X.Y.Z numerically and ignores the v prefix', () => {
  assert.equal(M.compare('v0.7.10', 'v0.7.9'), 1);
  assert.equal(M.compare('0.7.5', 'v0.7.5'), 0);
  assert.equal(M.compare('v0.6.99', 'v0.7.0'), -1);
});

test('compare treats a version it cannot read as equal, so nothing is cut off by it', () => {
  assert.equal(M.compare('dev', 'v0.7.5'), 0);
  assert.equal(M.compare('v0.7.5', ''), 0);
});

test('slice keeps the releases above the installed version up to the package version', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  const byId = Object.fromEntries(rows.map((row) => [row.id, row]));
  assert.deepEqual(plain(byId.dashboard.releases.map((r) => r.version)), ['v0.7.5', 'v0.7.4']);
  assert.deepEqual(plain(byId['service:shelly'].releases.map((r) => r.version)), ['v0.4.2']);
  assert.equal(byId.dashboard.from, 'v0.7.3');
  assert.equal(byId.dashboard.to, 'v0.7.5');
  assert.equal(byId.dashboard.isNew, false);
});

test('slice drops a component that is up to date and one without any entries', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.5', 'service:shelly': 'v0.4.2', bootstrap: 'v0.1.6' }));
  assert.deepEqual(plain(rows), []);
});

test('slice shows only the newest release for a component with no installed version', () => {
  const rows = M.slice(view({}));
  const byId = Object.fromEntries(rows.map((row) => [row.id, row]));
  assert.deepEqual(plain(byId.dashboard.releases.map((r) => r.version)), ['v0.7.5']);
  assert.equal(byId.dashboard.isNew, true);
  assert.equal(byId.dashboard.from, null);
  assert.equal(rows.some((row) => row.id === 'bootstrap'), false, 'no entries, no row');
});

test('slice treats an unreadable installed version like none', () => {
  const rows = M.slice(view({ dashboard: 'dev' }));
  assert.equal(rows.find((row) => row.id === 'dashboard').isNew, true);
});

test('slice orders components by kind and keeps document order inside a kind', () => {
  const rows = M.slice(view({}));
  assert.deepEqual(plain(rows.map((row) => row.id)), ['dashboard', 'service:shelly']);
});

test('slice of an empty or missing response is empty', () => {
  assert.deepEqual(plain(M.slice(null)), []);
  assert.deepEqual(plain(M.slice({ installed: {} })), []);
});

test('summarize counts each entry once and breaking on top', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  assert.deepEqual(plain(M.summarize(rows)), { feat: 2, fix: 3, other: 0, breaking: 1, total: 5, components: 2 });
});

test('breaking lists the breaking entries with their component and version', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3' }));
  assert.deepEqual(plain(M.breaking(rows)), [
    { component: 'Dashboard', version: 'v0.7.4', text: 'drop the legacy node block', scope: 'dashboard', pr: 14 },
  ]);
});

test('scopes and kinds are the distinct values in view, sorted / in screen order', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  assert.deepEqual(plain(M.scopes(rows)), ['dashboard', 'shelly']);
  assert.deepEqual(plain(M.kinds(rows)), ['app', 'service']);
});

test('filter by kind keeps only that kind', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  assert.deepEqual(plain(M.filter(rows, { kind: 'service' }).map((row) => row.id)), ['service:shelly']);
});

test('filter by scope prunes entries, then empty groups, releases and components', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  const filtered = M.filter(rows, { scope: 'shelly' });
  assert.deepEqual(plain(filtered.map((row) => row.id)), ['service:shelly']);
  assert.equal(filtered[0].releases[0].groups[0].entries.length, 1);
});

test('filter by text matches entry text and scope, case-insensitively', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  assert.equal(M.summarize(M.filter(rows, { query: 'BADGE' })).total, 1);
  assert.equal(M.summarize(M.filter(rows, { query: 'shelly' })).total, 1, 'the scope is searched too');
  assert.equal(M.summarize(M.filter(rows, { query: 'no such thing' })).total, 0);
});

test('filter never changes its input', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3' }));
  const before = JSON.stringify(rows);
  M.filter(rows, { scope: 'dashboard', query: 'badge' });
  assert.equal(JSON.stringify(rows), before);
});

test('an empty filter returns everything', () => {
  const rows = M.slice(view({ dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' }));
  assert.equal(M.summarize(M.filter(rows, {})).total, M.summarize(rows).total);
});
