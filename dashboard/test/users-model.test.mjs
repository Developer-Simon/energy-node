import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function loadModel() {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only' });
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'users-model.js'), 'utf8'), dom.getInternalVMContext());
  return dom.window.UsersModel;
}

// Mirrors the flags of internal/auth/roles.go for the keys these tests use.
const entry = (key, category, flags = {}) => ({
  key, category, requires: '', https_only: false, guest_allowed: true, ha_allowed: true, ...flags,
});
const catalog = [
  entry('view_automations', 'view'),
  entry('view_config', 'view'),
  entry('automations', 'setup', { requires: 'view_automations' }),
  entry('operate_automations', 'operate', { requires: 'view_automations' }),
  entry('edit_config', 'setup', { requires: 'view_config' }),
  entry('view_diagnostics', 'view'),
  entry('view_logs', 'system', { requires: 'view_diagnostics', https_only: true, guest_allowed: false }),
  entry('system_actions', 'system', { https_only: true, guest_allowed: false }),
  entry('user_management', 'system', { https_only: true, guest_allowed: false }),
  entry('http_login', 'system', { guest_allowed: false, ha_allowed: false }),
];
const plain = value => JSON.parse(JSON.stringify(value));
const local = { id: 'anna', kind: 'local' };
const alone = { self: false, otherActiveAdmins: 1 };

test('turning a view off drops dependent permissions', () => {
  const model = loadModel();
  const roles = ['automations', 'edit_config', 'operate_automations', 'view_automations', 'view_config'];
  const withoutAutomationsView = roles.filter(key => key !== 'view_automations');
  assert.deepEqual(plain(model.normalize(catalog, withoutAutomationsView)), ['edit_config', 'view_config']);
});

test('group master: off → all allowed, mixed → all allowed, on → none', () => {
  const model = loadModel();
  const views = ['view_automations', 'view_config'];
  const setupAll = ['automations', 'edit_config'];
  assert.deepEqual(plain(model.toggleGroup(catalog, local, views, 'setup', alone)), [...setupAll, ...views].sort());
  assert.deepEqual(plain(model.toggleGroup(catalog, local, ['automations', ...views], 'setup', alone)), [...setupAll, ...views].sort());
  assert.deepEqual(plain(model.toggleGroup(catalog, local, [...setupAll, ...views], 'setup', alone)), views);
});

test('group master falls back to off when nothing can be added', () => {
  const model = loadModel();
  // edit_config is allowed only with view_config, so switching on changes nothing.
  const roles = ['automations', 'view_automations'];
  assert.equal(model.groupState(catalog, 'local', 'setup', roles).state, 'mixed');
  assert.deepEqual(plain(model.toggleGroup(catalog, local, roles, 'setup', alone)), ['view_automations']);
});

test('last admin keeps user_management and stays active', () => {
  const model = loadModel();
  const roles = ['user_management', 'view_diagnostics'];
  const lastAdmin = model.locks(catalog, local, roles, { self: false, otherActiveAdmins: 0 });
  assert.equal(lastAdmin.user_management, 'last_admin');
  assert.equal(lastAdmin.__active, 'last_admin');

  const withOther = model.locks(catalog, local, roles, { self: false, otherActiveAdmins: 1 });
  assert.equal(withOther.user_management, undefined, 'another active admin frees the permission');
  assert.equal(withOther.__active, undefined);

  const own = model.locks(catalog, local, roles, { self: true, otherActiveAdmins: 1 });
  assert.equal(own.__active, 'self', 'the own account cannot be deactivated');
});

test('http warning lists withheld keys in catalog order', () => {
  const model = loadModel();
  const roles = ['view_diagnostics', 'system_actions', 'view_logs', 'http_login'];
  assert.deepEqual(plain(model.httpWarning(catalog, roles, false)), { withheld: ['view_logs', 'system_actions'], safe: false });
  assert.deepEqual(plain(model.httpWarning(catalog, roles, true)), { withheld: ['view_logs', 'system_actions'], safe: true });
  assert.equal(model.httpWarning(catalog, roles.filter(key => key !== "http_login"), false), null);
});

test('ha accounts never offer http_login', () => {
  const model = loadModel();
  const keysOf = kind => model.available(catalog, kind).map(item => item.key);
  assert.ok(keysOf('local').includes('http_login'));
  assert.ok(!keysOf('ha').includes('http_login'));
  assert.ok(!keysOf('ha_set').includes('http_login'));
  assert.ok(!keysOf('guest').includes('system_actions'), 'guest access leaves out HTTPS-only system rights');
});

// Elevated permissions (catalog flag elevated): the warning lists only the ones
// a save adds, so these helpers are checked on their own catalog.
const elevatedCatalog = [
  entry('view_config', 'view'),
  entry('system_actions', 'system', { elevated: true }),
  entry('tailscale', 'system', { elevated: true }),
  entry('user_management', 'system', { elevated: true }),
];

test('elevatedKeys lists the held elevated keys in catalog order and ignores unknown keys', () => {
  const model = loadModel();
  const roles = ['user_management', 'view_config', 'tailscale', 'unknown_key'];
  assert.deepEqual(plain(model.elevatedKeys(elevatedCatalog, roles)), ['tailscale', 'user_management']);
  assert.deepEqual(plain(model.elevatedKeys(elevatedCatalog, ['view_config'])), []);
});

test('newlyElevated lists only the elevated keys the save adds', () => {
  const model = loadModel();
  const before = ['tailscale'];
  const after = ['system_actions', 'tailscale', 'user_management', 'view_config'];
  assert.deepEqual(plain(model.newlyElevated(elevatedCatalog, before, after)), ['system_actions', 'user_management']);
  assert.deepEqual(plain(model.newlyElevated(elevatedCatalog, before, ['tailscale'])), []);
  assert.deepEqual(plain(model.newlyElevated(elevatedCatalog, [], ['view_config'])), []);
});

test('groupSessions keeps the server order per username and returns an empty map for none', () => {
  const model = loadModel();
  const sessions = [
    { id: 'a1', username: 'anna', guest: false, restricted: false, current: true },
    { id: 'w1', username: 'werkstatt', guest: false, restricted: true, current: false },
    { id: 'a2', username: 'anna', guest: false, restricted: false, current: false },
  ];
  const grouped = plain(model.groupSessions(sessions));
  assert.deepEqual(grouped.anna.map((session) => session.id), ['a1', 'a2']);
  assert.deepEqual(grouped.werkstatt.map((session) => session.id), ['w1']);
  assert.deepEqual(plain(model.groupSessions([])), {});
});

test('ha account without own selection locks every entry as preset', () => {
  const model = loadModel();
  const offered = catalog.filter(item => item.ha_allowed).map(item => item.key);
  assert.ok(offered.length > 0);
  const locked = model.locks(catalog, { kind: 'ha', custom: false }, [], alone);
  offered.forEach(key => assert.equal(locked[key], 'preset', key));
  assert.equal(locked.http_login, undefined, 'entries the kind never offers are not listed');

  const custom = model.locks(catalog, { kind: 'ha', custom: true }, ['view_config'], alone);
  assert.equal(Object.values(custom).includes('preset'), false, 'a own selection unlocks the entries');
  const withoutView = model.locks(catalog, { kind: 'ha', custom: true }, [], alone);
  assert.equal(withoutView.edit_config, 'requires', 'the dependency lock still applies');
});

test('ha kinds never offer http_login', () => {
  const model = loadModel();
  assert.equal(model.available(catalog, 'ha').some(item => item.key === 'http_login'), false);
  assert.equal(model.available(catalog, 'ha_set').some(item => item.key === 'http_login'), false);
});
