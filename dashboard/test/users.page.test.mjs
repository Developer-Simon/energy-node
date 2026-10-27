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
  { key: 'user_management', category: 'system', requires: null, https_only: true, guest_allowed: false, ha_allowed: false, elevated: true },
  { key: 'tailscale', category: 'system', requires: null, https_only: false, guest_allowed: false, ha_allowed: false, elevated: true },
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
    // Home Assistant presets {admin, user}, null while they are not set.
    haRoles: null,
    haAccounts: [],
    companion: { paired: false },
    // Lifetime of a new pairing code in ms.
    pairingMs: 600000,
    policy: { min_length: 10, min: 8, max: 64 },
    sessions: [
      { id: 'a1b2c3d4e5f60708', username: 'anna', guest: false, restricted: false, expires_at: '2026-10-26T10:00:00Z', current: true },
      { id: '0f1e2d3c4b5a6978', username: 'anna', guest: false, restricted: true, expires_at: '2026-10-25T20:00:00Z', current: false },
      { id: 'ffeeddccbbaa9988', username: 'werkstatt', guest: true, restricted: false, expires_at: '2026-10-25T12:00:00Z', current: false },
    ],
  };
}

function route(server, url, method, body) {
  if (url === '/api/v1/auth/session') return { csrf_token: 'tok' };
  if (url === '/api/v1/users' && method === 'GET') {
    return {
      users: server.users.map((user) => ({ ...user, roles: [...user.roles] })),
      password_policy: { ...server.policy },
      guest_roles: [...server.guestRoles],
      ha_roles: server.haRoles ? { admin: [...server.haRoles.admin], user: [...server.haRoles.user] } : null,
      ha_accounts: server.haAccounts.map((account) => ({ ...account, roles: [...account.roles] })),
      companion: { ...server.companion },
      catalog: CATALOG,
      migration_notice: false,
      self: 'simon',
    };
  }
  if (url === '/api/v1/users/guest-roles' && method === 'PUT') {
    server.guestRoles = body.roles;
    return { guest_roles: body.roles };
  }
  if (url === '/api/v1/users/ha-roles' && method === 'PUT') {
    server.haRoles = { admin: [...body.admin], user: [...body.user] };
    return { ha_roles: server.haRoles };
  }
  if (url === '/api/v1/users/companion/pairing-code' && method === 'POST') {
    return { code: 'K7MQ2XWP9HDT', expires_at: new Date(Date.now() + server.pairingMs).toISOString() };
  }
  if (url === '/api/v1/users/companion/pairing-code' && method === 'DELETE') {
    return { status: 'pairing_code_dropped' };
  }
  if (url === '/api/v1/users/companion' && method === 'DELETE') {
    server.companion = { paired: false };
    return { status: 'unpaired' };
  }
  const haAccount = /^\/api\/v1\/users\/ha%3A([^/]+)$/.exec(url);
  if (haAccount && method === 'PUT') {
    const account = server.haAccounts.find((item) => item.ha_user_id === haAccount[1]);
    account.roles = [...body.roles];
    account.disabled = body.disabled;
    account.roles_custom = body.roles_custom;
    return { ha_user_id: account.ha_user_id };
  }
  if (url === '/api/v1/users' && method === 'POST') {
    server.users.push({ username: body.username, roles: [...body.roles], disabled: false, has_http_password: false });
    return { username: body.username };
  }
  if (url === '/api/v1/users/password-policy' && method === 'PUT') {
    server.policy = { ...server.policy, min_length: body.min_length };
    return { min_length: body.min_length };
  }
  if (url === '/api/v1/user-sessions' && method === 'GET') {
    return { sessions: server.sessions.map((session) => ({ ...session })) };
  }
  const session = /^\/api\/v1\/user-sessions\/([^/]+)$/.exec(url);
  if (session && method === 'DELETE') {
    server.sessions = server.sessions.filter((item) => item.id !== session[1]);
    return {};
  }
  if (/^\/api\/v1\/users\/[^/]+\/password$/.test(url) && method === 'PUT') {
    return {};
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

// Replaces window.setInterval and clearInterval, so a test can see which
// timers are running and fire them by hand.
function fakeTimers(window) {
  const active = new Map();
  let nextId = 1;
  window.setInterval = (fn, ms) => {
    const id = nextId;
    nextId += 1;
    active.set(id, { fn, ms });
    return id;
  };
  window.clearInterval = (id) => { active.delete(id); };
  return {
    get running() { return [...active.values()].map((timer) => timer.ms); },
    tick() { [...active.values()].forEach((timer) => timer.fn()); },
  };
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
  return { component, calls, server, stores, window: dom.window, timers: fakeTimers(dom.window) };
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

// Holds the next GET /api/v1/users after a PUT or POST until release() runs.
// The request itself still reaches the fake server, only its answer waits.
function holdReloadAfterWrite(window) {
  const realFetch = window.fetch;
  let writeDone = false;
  let releaseFn = null;
  window.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    if (method !== 'GET' && method !== 'HEAD') {
      const response = await realFetch(url, options);
      writeDone = true;
      return response;
    }
    if (url === '/api/v1/users' && writeDone) {
      const response = await realFetch(url, options);
      await new Promise((resolve) => { releaseFn = resolve; });
      return response;
    }
    return realFetch(url, options);
  };
  return {
    // Resolves once the reload is waiting, the moment Alpine would render.
    async waitForHeldReload() {
      while (!releaseFn) await new Promise((resolve) => setImmediate(resolve));
    },
    release() { releaseFn(); },
  };
}

test('reading a draft before the first load stores nothing and shows no unsaved tag', async () => {
  const { component } = createUsersPage();
  // A render can run before load() has filled presets and accounts.
  component.draftOf('preset:guest');
  component.draftOf('anna');
  assert.equal(component.drafts['preset:guest'], undefined, 'reading does not store a draft');
  assert.equal(component.drafts.anna, undefined, 'reading does not store a draft');

  await component.init();

  assert.equal(component.isDirty('preset:guest'), false, 'the guest preset is not unsaved');
  assert.equal(component.isDirty('anna'), false, 'the account is not unsaved');
  assert.ok(!component.tagsFor('preset:guest').includes('users.tag.unsaved'));
  assert.ok(!component.tagsFor('anna').includes('users.tag.unsaved'));
});

test('a change after the load still marks the card unsaved and saves', async () => {
  const { component, calls, server } = createUsersPage();
  component.draftOf('anna');
  await component.init();

  component.toggleCard('anna');
  component.toggleRole('anna', 'view_history');
  assert.equal(component.isDirty('anna'), true, 'the change is a draft');

  assert.equal(await component.save('anna'), true);
  assert.equal(writes(calls).filter((call) => call.method === 'PUT').length, 1);
  assert.ok(server.users.find((user) => user.username === 'anna').roles.includes('view_history'));
  assert.equal(component.isDirty('anna'), false);
});

test('save keeps the saved roles when the open card renders while the reload is pending', async () => {
  const { component, window } = createUsersPage();
  await component.init();
  component.toggleCard('anna');
  // Removing view_devices takes operate_devices with it.
  component.toggleRole('anna', 'view_devices');
  const hold = holdReloadAfterWrite(window);

  const saving = component.save('anna');
  await hold.waitForHeldReload();
  // The render effect of the open card runs before the reload has answered.
  component.draftOf(component.openId);
  hold.release();

  assert.equal(await saving, true);
  assert.equal(component.isDirty('anna'), false, 'the card does not show unsaved');
  assert.deepEqual([...component.draftOf('anna').roles], ['view_overview'], 'the draft shows the saved roles');
});

test('create keeps the saved roles when the open card renders while the reload is pending', async () => {
  const { component, window } = createUsersPage();
  await component.init();
  component.startCreate();
  component.creating.username = 'neu.user';
  component.creating.password = 'abcdefghij';
  component.toggleRole('draft:new', 'view_history');
  const hold = holdReloadAfterWrite(window);

  const creating = component.save('draft:new');
  await hold.waitForHeldReload();
  component.draftOf(component.openId);
  hold.release();

  assert.equal(await creating, true);
  assert.equal(component.isDirty('neu.user'), false, 'the new card does not show unsaved');
  assert.deepEqual([...component.draftOf('neu.user').roles], ['view_history'], 'the draft shows the created roles');
});

test('creating a card enables Anlegen only for valid name and password', async () => {
  const { component, calls } = createUsersPage();
  await component.init();

  component.startCreate();
  assert.equal(component.openId, 'draft:new');
  assert.equal(component.isDirty('draft:new'), true);
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
  assert.equal(component.isDirty('draft:new'), false);
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

// --- password length from the live policy ----------------------------------

test('before the users are loaded the minimum password length is 10', () => {
  const { component } = createUsersPage();
  assert.equal(component.passwordMin, 10);
});

test('password checks follow the live minimum from password_policy', async () => {
  const server = createServer();
  server.policy = { min_length: 12, min: 8, max: 64 };
  const { component, calls } = createUsersPage({ server });
  await component.init();
  assert.equal(component.passwordMin, 12);

  component.startCreate();
  component.creating.username = 'neu.user';
  component.creating.password = 'a'.repeat(11);
  assert.equal(component.createPasswordValid(), false, 'eleven characters are too short for 12');
  component.creating.password = 'ä'.repeat(12);
  assert.equal(component.createPasswordValid(), true, 'umlauts count once, twelve are enough');

  assert.equal(await component.resetPassword('anna', 'a'.repeat(11)), false);
  assert.equal(writes(calls).length, 0, 'a short reset sends nothing');
  assert.equal(await component.setHTTPPassword('anna', 'a'.repeat(11)), false);
  assert.equal(writes(calls).length, 0, 'a short HTTP password sends nothing');

  assert.equal(await component.resetPassword('anna', 'b'.repeat(12)), true);
  assert.equal(writes(calls).at(-1).url, '/api/v1/users/anna/password');
});

// --- password policy ---------------------------------------------------------

test('the policy draft saves through PUT password-policy and reloads the minimum', async () => {
  const { component, calls, server, stores, window } = createUsersPage();
  await component.init();
  assert.equal(component.policyDirty(), false);

  component.policyDraft = 12;
  assert.equal(component.policyDirty(), true);
  assert.equal(await component.savePolicy(), true);

  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/password-policy');
  assert.deepEqual(put.body, { min_length: 12 });
  assert.equal(server.policy.min_length, 12);
  assert.equal(component.policy.min_length, 12);
  assert.equal(component.passwordMin, 12);
  assert.equal(component.policyDirty(), false);
  assert.equal(stores.toasts.last(), window.I18n.t('users.policy.saved'));
});

test('a policy outside the bounds makes no request and reports the range', async () => {
  const { component, calls, window } = createUsersPage();
  await component.init();

  for (const value of [7, 65, 10.5]) {
    component.policyDraft = value;
    assert.notEqual(component.policyRangeError, '', `${value} is out of range`);
    assert.equal(await component.savePolicy(), false);
  }
  assert.equal(writes(calls).length, 0);
  component.policyDraft = 64;
  assert.equal(component.policyRangeError, '');
  assert.ok(window.I18n.t('users.policy.range_error', { min: 8, max: 64 }).includes('64'));
});

test('cancelPolicy drops the policy draft back to the saved minimum', async () => {
  const { component } = createUsersPage();
  await component.init();
  component.policyDraft = 14;
  component.cancelPolicy();
  assert.equal(component.policyDraft, 10);
  assert.equal(component.policyDirty(), false);
});

// --- elevated rights warning -------------------------------------------------

test('elevatedFor lists the held elevated keys of the draft', async () => {
  const { component } = createUsersPage();
  await component.init();
  assert.deepEqual([...component.elevatedFor('anna')], []);
  component.toggleRole('anna', 'tailscale');
  component.toggleRole('anna', 'user_management');
  assert.deepEqual([...component.elevatedFor('anna')], ['user_management', 'tailscale'], 'catalog order');
});

test('saving newly added elevated rights asks first, cancelling sends nothing', async () => {
  const { component, calls, stores, window } = createUsersPage();
  await component.init();
  stores.modal.answer = false;

  component.toggleRole('anna', 'tailscale');
  assert.equal(await component.save('anna'), false);

  assert.equal(writes(calls).length, 0, 'the request waits for the answer');
  assert.equal(stores.modal.calls.length, 1);
  assert.equal(stores.modal.calls[0].danger, true);
  assert.ok(stores.modal.calls[0].body.includes(window.I18n.t('users.role.tailscale.label')));
  assert.equal(component.isDirty('anna'), true, 'the draft is kept');
});

test('the elevated confirm names only the newly added permissions and accepting sends', async () => {
  const server = createServer();
  server.users.push({ username: 'ops', roles: ['tailscale', 'view_overview'], disabled: false, has_http_password: false });
  const { component, calls, stores, window } = createUsersPage({ server });
  await component.init();

  component.toggleRole('ops', 'user_management');
  assert.equal(await component.save('ops'), true);

  const body = stores.modal.calls[0].body;
  assert.ok(body.includes(window.I18n.t('users.role.user_management.label')));
  assert.ok(!body.includes(window.I18n.t('users.role.tailscale.label')), 'tailscale was already held');
  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/ops');
});

test('creating an account with an elevated right asks first', async () => {
  const { component, calls, stores } = createUsersPage();
  await component.init();
  component.startCreate();
  component.creating.username = 'neu.user';
  component.creating.password = 'abcdefghij';
  component.toggleRole('draft:new', 'tailscale');

  stores.modal.answer = false;
  assert.equal(await component.save('draft:new'), false);
  assert.equal(writes(calls).length, 0);
  assert.equal(stores.modal.calls.length, 1);

  stores.modal.answer = true;
  assert.equal(await component.save('draft:new'), true);
  assert.equal(writes(calls).filter((call) => call.method === 'POST').length, 1);
});

// --- sessions tab ------------------------------------------------------------

test('opening the sessions tab loads the list and polls every 30 seconds', async () => {
  const { component, calls, timers } = createUsersPage();
  await component.init();
  assert.equal(component.tab, 'signin');
  assert.deepEqual(timers.running, []);

  await component.setTab('sessions');
  assert.equal(component.tab, 'sessions');
  assert.equal(component.sessions.length, 3);
  assert.equal(component.sessionCountFor('anna'), 2);
  assert.equal(component.sessionCountFor('nobody'), 0);
  assert.deepEqual(timers.running, [30000]);

  const before = calls.filter((call) => call.url === '/api/v1/user-sessions').length;
  timers.tick();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(calls.filter((call) => call.url === '/api/v1/user-sessions').length, before + 1, 'the timer reloads');
});

test('the sign-in tab is the default and shows the own session and account cards', async () => {
  const { component } = createUsersPage();
  await component.init();
  assert.equal(component.signinShown, true);

  await component.setTab('accounts');
  assert.equal(component.tab, 'accounts');
  assert.equal(component.signinShown, false);

  await component.setTab('anything else');
  assert.equal(component.tab, 'signin');
  assert.equal(component.signinShown, true);
});

test('leaving the sessions tab stops the timer and opening it again does not stack a second one', async () => {
  const { component, timers } = createUsersPage();
  await component.init();
  await component.setTab('sessions');
  await component.setTab('sessions');
  assert.equal(timers.running.length, 1, 'one timer, not two');

  component.setTab('accounts');
  assert.deepEqual(timers.running, []);
  timers.tick();
  assert.deepEqual(timers.running, []);
});

test('endSession asks first, then sends DELETE and reloads the list', async () => {
  const { component, calls, server, stores, window } = createUsersPage();
  await component.init();
  await component.setTab('sessions');

  stores.modal.answer = false;
  assert.equal(await component.endSession('0f1e2d3c4b5a6978'), false);
  assert.equal(writes(calls).length, 0);

  stores.modal.answer = true;
  assert.equal(await component.endSession('0f1e2d3c4b5a6978'), true);
  const del = writes(calls).find((call) => call.method === 'DELETE');
  assert.equal(del.url, '/api/v1/user-sessions/0f1e2d3c4b5a6978');
  assert.equal(server.sessions.length, 2);
  assert.equal(component.sessions.length, 2, 'reloaded');
  assert.equal(stores.modal.calls.at(-1).danger, true);
  assert.equal(stores.toasts.last(), window.I18n.t('users.sessions.ended_toast', { name: 'anna' }));
});

test('the current session cannot be ended from the tab', async () => {
  const { component } = createUsersPage();
  await component.init();
  await component.setTab('sessions');
  const [current, other] = [component.sessions[0], component.sessions[1]];
  assert.equal(component.canEndSession(current), false);
  assert.equal(component.canEndSession(other), true);
});

// One Home Assistant account (admin) and the presets, for the tests below.
function haServer() {
  const server = createServer();
  server.haRoles = { admin: ['view_devices', 'view_overview'], user: ['view_overview'] };
  server.haAccounts = [{
    ha_user_id: 'u1', username: 'hanna', display_name: 'Hanna', ha_admin: true, roles_custom: false,
    roles: [], disabled: false, name_conflict: false,
  }];
  return server;
}

test('ha presets appear only when ha_roles is set', async () => {
  const plain = createUsersPage();
  await plain.component.init();
  assert.equal(plain.component.presets.length, 1);

  const { component } = createUsersPage({ server: haServer() });
  await component.init();
  // Copied out of the page's vm realm, so deepEqual compares plain arrays.
  assert.deepEqual([...component.presets.map((preset) => preset.id)], ['preset:guest', 'preset:ha_admin', 'preset:ha_user']);
  assert.equal(component.kindOf('preset:ha_admin'), 'ha_set');
});

test('saving the ha admin preset sends both lists', async () => {
  const { component, calls, server } = createUsersPage({ server: haServer() });
  await component.init();
  component.toggleRole('preset:ha_admin', 'view_history');
  assert.equal(await component.save('preset:ha_admin'), true);

  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/ha-roles');
  assert.deepEqual(put.body, {
    admin: ['view_history', 'view_overview', 'view_devices'].sort(),
    user: ['view_overview'],
  });
  assert.deepEqual(server.haRoles, put.body);
});

test('switching an ha account to custom starts from its preset', async () => {
  const { component } = createUsersPage({ server: haServer() });
  await component.init();
  const id = 'ha:u1';
  assert.equal(component.kindOf(id), 'ha');
  assert.equal(component.lockFor(id, 'view_history'), 'preset', 'the preset locks every entry');
  assert.equal(component.isDirty(id), false);

  component.setCustom(id, true);
  assert.deepEqual([...component.draftOf(id).roles], ['view_devices', 'view_overview']);
  assert.equal(component.isDirty(id), true, 'the switch alone is a change');
});

test('saving an ha account sends roles_custom', async () => {
  const { component, calls } = createUsersPage({ server: haServer() });
  await component.init();
  const id = 'ha:u1';
  component.setCustom(id, true);
  component.toggleRole(id, 'view_history');
  assert.equal(await component.save(id), true);

  const put = writes(calls).find((call) => call.method === 'PUT');
  assert.equal(put.url, '/api/v1/users/ha%3Au1');
  assert.deepEqual(put.body, {
    roles: ['view_devices', 'view_history', 'view_overview'],
    disabled: false,
    roles_custom: true,
  });
  assert.equal(component.accountFor(id).roles_custom, true, 'reloaded after the save');
});

test('pairing code counts down and disappears once paired', async () => {
  const { component, server, timers, stores, window } = createUsersPage();
  await component.init();
  assert.equal(component.pairingStage(), 'none');

  assert.equal(await component.createPairingCode(), true);
  assert.equal(component.pairingStage(), 'code');
  assert.equal(component.pairingCode, 'K7MQ2XWP9HDT');
  assert.ok(component.pairingRemaining >= 599 && component.pairingRemaining <= 600, 'ten minutes');
  assert.deepEqual(timers.running, [1000]);

  const before = component.pairingRemaining;
  timers.tick();
  assert.equal(component.pairingRemaining, before - 1, 'one second per tick');

  server.companion = { paired: true };
  await component.load();
  assert.equal(component.pairingCode, '', 'the code is used up');
  assert.equal(component.pairingStage(), 'paired');
  assert.deepEqual(timers.running, [], 'the countdown stops');
  assert.equal(stores.toasts.last(), window.I18n.t('users.ha.paired_toast'));
});

test('re-pairing keeps the code until a new paired_at arrives', async () => {
  const server = createServer();
  server.companion = { paired: true, paired_at: '2026-10-12T08:00:00Z' };
  const { component, server: srv, stores, window } = createUsersPage({ server });
  await component.init();
  assert.equal(component.pairingStage(), 'paired');

  assert.equal(await component.createPairingCode(), true);
  assert.equal(component.pairingStage(), 'code');

  await component.load();
  assert.equal(component.pairingCode, 'K7MQ2XWP9HDT', 'same paired_at keeps the code');
  assert.equal(component.pairingStage(), 'code');
  assert.notEqual(stores.toasts.last(), window.I18n.t('users.ha.paired_toast'));

  srv.companion = { paired: true, paired_at: '2026-10-26T09:00:00Z' };
  await component.load();
  assert.equal(component.pairingCode, '', 'new paired_at means the pairing is done');
  assert.equal(component.pairingStage(), 'paired');
  assert.equal(stores.toasts.last(), window.I18n.t('users.ha.paired_toast'));
});

test('pairing code expires', async () => {
  const server = createServer();
  server.pairingMs = 3000;
  const { component, timers, stores, window } = createUsersPage({ server });
  await component.init();
  await component.createPairingCode();
  assert.equal(component.pairingStage(), 'code');

  for (let tick = 0; tick < 10 && timers.running.length > 0; tick += 1) timers.tick();
  assert.equal(component.pairingStage(), 'none');
  assert.equal(component.pairingCode, '');
  assert.deepEqual(timers.running, []);
  assert.equal(stores.toasts.last(), window.I18n.t('users.ha.code_expired'));
});
