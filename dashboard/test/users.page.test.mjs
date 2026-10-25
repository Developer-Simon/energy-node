// Tests for users.page.js: drafts per card, the save request shapes (PUT for an
// account, PUT guest-roles for the guest preset) and the rules for creating an
// account. users-model.js is loaded first, as base.html does. Run with
// `npm test` from dashboard/ (Node's built-in test runner plus jsdom), same
// approach as tailscale.page.test.mjs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';
import { attachStores, fakeModalStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');
const modelSource = fs.readFileSync(path.join(jsDir, 'users-model.js'), 'utf8');
const pageSource = fs.readFileSync(path.join(jsDir, 'users.page.js'), 'utf8');

// A small catalog in the shape of GET /api/v1/users.
const CATALOG = [
  { key: 'view_overview', category: 'view', requires: null, https_only: false, guest_allowed: true, ha_allowed: true },
  { key: 'view_devices', category: 'view', requires: null, https_only: false, guest_allowed: true, ha_allowed: true },
  { key: 'view_history', category: 'view', requires: null, https_only: false, guest_allowed: true, ha_allowed: true },
  { key: 'operate_devices', category: 'operate', requires: 'view_devices', https_only: false, guest_allowed: true, ha_allowed: true },
  { key: 'user_management', category: 'system', requires: null, https_only: true, guest_allowed: false, ha_allowed: false },
  { key: 'http_login', category: 'system', requires: null, https_only: false, guest_allowed: false, ha_allowed: false },
];

// The fake server: its state answers GET and records what PUT/POST change.
function createServer() {
  return {
    users: [
      { username: 'anna', roles: ['operate_devices', 'view_devices', 'view_overview'], disabled: false, has_http_password: false },
      { username: 'werkstatt', roles: ['view_overview'], disabled: false, has_http_password: false },
    ],
    guestRoles: ['view_overview'],
  };
}

function route(server, url, method, body) {
  if (url === '/api/v1/auth/session') return { csrf_token: 'tok' };
  if (url === '/api/v1/users' && method === 'GET') {
    return {
      users: server.users.map((user) => ({ ...user, roles: [...user.roles] })),
      guests: [],
      guest_roles: [...server.guestRoles],
      catalog: CATALOG,
      migration_notice: false,
      self: 'simon',
    };
  }
  if (url === '/api/v1/users/guest-roles' && method === 'PUT') {
    server.guestRoles = body.roles;
    return { guest_roles: body.roles };
  }
  const account = /^\/api\/v1\/users\/([^/]+)$/.exec(url);
  if (account && method === 'PUT') {
    const user = server.users.find((item) => item.username === account[1]);
    user.roles = body.roles;
    user.disabled = body.disabled;
    return { username: user.username };
  }
  throw new Error(`unexpected ${method} ${url}`);
}

function createUsersPage({ server = createServer() } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'https://dashboard.local/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  const calls = [];
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    const body = options.body ? JSON.parse(options.body) : undefined;
    calls.push({ url, method, body, headers: options.headers || {} });
    const payload = route(server, url, method, body);
    return { ok: true, status: 200, json: async () => payload };
  };
  installI18n(dom.window);
  vm.runInContext(modelSource, context);
  vm.runInContext(pageSource, context);
  const component = factories.usersPage();
  const stores = attachStores(component, { modal: fakeModalStore({ answer: true }) });
  return { component, calls, server, stores };
}

const writes = (calls) => calls.filter((call) => call.method !== 'GET' && call.url !== '/api/v1/auth/session');

test('draft survives switching cards and marks the closed card unsaved', async () => {
  const { component } = createUsersPage();
  await component.init();

  component.toggleCard('anna');
  component.toggleRole('anna', 'view_history');
  component.toggleCard('werkstatt');

  assert.equal(component.openId, 'werkstatt');
  assert.equal(component.isDirty('anna'), true);
  assert.ok(component.drafts.anna.roles.includes('view_history'), 'the anna draft kept its change');
  assert.equal(component.isDirty('werkstatt'), false);
  assert.ok(component.tagsFor('anna').includes('users.tag.unsaved'), 'the closed card shows unsaved');
  assert.ok(!component.tagsFor('werkstatt').includes('users.tag.unsaved'));

  component.toggleCard('anna');
  assert.ok(component.drafts.anna.roles.includes('view_history'), 'reopening shows the same draft');
  assert.ok(!component.tagsFor('anna').includes('users.tag.unsaved'), 'an open card does not show unsaved');
});

test('save sends PUT with normalized roles and clears the draft', async () => {
  const { component, calls, server } = createUsersPage();
  await component.init();

  component.toggleCard('anna');
  // Removing view_devices takes operate_devices with it, it requires view_devices.
  component.toggleRole('anna', 'view_devices');
  const saved = await component.save('anna');

  assert.equal(saved, true);
  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/anna');
  assert.deepEqual(put.body, { roles: ['view_overview'], disabled: false });
  assert.equal(put.headers['X-CSRF-Token'], 'tok');

  assert.equal(component.drafts.anna, undefined, 'the draft is cleared');
  assert.equal(component.isDirty('anna'), false);
  assert.equal(component.justSaved('anna'), true);
  assert.deepEqual(component.accounts.find((user) => user.username === 'anna').roles, ['view_overview'], 'data reloaded');
  assert.deepEqual(server.users.find((user) => user.username === 'anna').roles, ['view_overview']);
});

test('creating a card enables Anlegen only for valid name and password', async () => {
  const { component, calls } = createUsersPage();
  await component.init();

  component.startCreate();
  assert.equal(component.openId, 'new');
  assert.equal(component.isDirty('new'), true);
  assert.equal(component.createValid(), false, 'empty values are not valid');

  component.creating.username = 'anna';
  component.creating.password = 'abcdefghij';
  assert.equal(component.createValid(), false, 'an existing name is taken');

  component.creating.username = 'Neu Name';
  assert.equal(component.createValid(), false, 'spaces and capitals are not allowed');

  component.creating.username = 'guest-neu';
  assert.equal(component.createValid(), false, 'guest- is reserved');

  component.creating.username = 'neu.user';
  component.creating.password = 'abcdefghi';
  assert.equal(component.createValid(), false, 'nine characters are too short');

  component.creating.password = 'abcdefghij';
  assert.equal(component.createValid(), true, 'a free name and ten characters enable Anlegen');

  component.cancelCreate();
  assert.equal(component.creating, null);
  assert.equal(component.openId, null);
  assert.equal(component.isDirty('new'), false);
  assert.equal(writes(calls).length, 0, 'nothing was sent while editing the new card');
});

test('guest preset saves through guest-roles', async () => {
  const { component, calls, server } = createUsersPage();
  await component.init();

  component.toggleCard('preset:guest');
  component.toggleRole('preset:guest', 'view_history');
  assert.equal(component.isDirty('preset:guest'), true);

  await component.save('preset:guest');

  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/guest-roles');
  assert.deepEqual(put.body, { roles: ['view_history', 'view_overview'] });
  assert.deepEqual(server.guestRoles, ['view_history', 'view_overview']);
  assert.equal(component.drafts['preset:guest'], undefined);
  assert.equal(component.isDirty('preset:guest'), false);
  // Copied out of the page's vm realm, so deepEqual compares plain arrays.
  assert.deepEqual([...component.presets[0].roles], ['view_history', 'view_overview']);
});
