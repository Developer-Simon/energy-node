// Regression tests for energy.page.js: the energy-roles panel now also
// loads/saves the balance Interpretation (Teil A of
// knowhow/dashboard/energie-interpretation.md) alongside the role
// assignments it already handled - a body that dropped "interpretation"
// used to reset it server-side, so save() must always send both.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { attachStores } from './helpers/notify-stores.mjs';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy.page.js'),
  'utf8',
);
const pickerSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'device-picker.js'),
  'utf8',
);
const switchSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-switch.js'),
  'utf8',
);
const modelSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-model.js'),
  'utf8',
);
const chainsSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy-chains.js'),
  'utf8',
);

function createEnergyPanel({ fetchImpl, basePath, url } = {}) {
  // A real origin gives jsdom a localStorage; the default about:blank has none.
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only', ...(url ? { url } : {}) });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  if (basePath !== undefined) dom.window.__DASHBOARD_BASE_PATH__ = basePath;
  installI18n(dom.window);
  vm.runInContext(pickerSource, context);
  vm.runInContext(switchSource, context);
  vm.runInContext(modelSource, context);
  vm.runInContext(chainsSource, context);
  vm.runInContext(subDevicesSource, context);
  vm.runInContext(scriptSource, context);
  const component = factories.energyRolesPanel();
  const stores = attachStores(component);
  return { component, window: dom.window, stores };
}

const jsonResponse = (body, ok = true) => ({ ok, status: ok ? 200 : 400, json: async () => body });

const devicesResponse = [{
  name: 'Wechselrichter',
  entities: [
    { unique_id: 'pv_power', name: 'PV Leistung', object_id: 'pv_power', unit_of_measurement: 'W' },
    { unique_id: 'socket_power', name: 'Steckdose', object_id: 'socket_power', unit_of_measurement: 'W' },
  ],
}];

const rolesResponse = {
  assignments: { pv_power: { role: 'pv', scale: 1, invert: false } },
  interpretation: {
    gap_mode: 'diagnostic', load_mode: 'measured', gap_tolerance_mode: 'percent',
    gap_tolerance_w: 25, gap_tolerance_percent: 3,
    surplus_threshold_w: 600, import_threshold_w: 1200,
    battery_reserve_percent: 10,
  },
};

const energyResponse = {
  entities: [{ entity_id: 'pv_power', role: { role: 'pv' } }],
  balance: { total: 4000 },
  unassigned: [{ device_id: 'inverter', entity_id: 'socket_power', name: 'Steckdose', value: 340, unit: 'W' }],
  unassigned_count: 1,
};

function stubFetch({ roles = rolesResponse } = {}) {
  return async (url) => {
    if (url === '/api/v1/devices') return jsonResponse(devicesResponse);
    if (url === '/api/v1/energy/roles') return jsonResponse(roles);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
}

test('entity label drops a redundant device-name prefix that the entity name repeats', async () => {
  const devices = [{
    name: 'Trucki T2MG',
    entities: [
      { unique_id: 'a', name: 'Trucki T2MG DC Power', object_id: 'dc_power', unit_of_measurement: 'W' },
      { unique_id: 'b', name: 'PV Leistung', object_id: 'pv', unit_of_measurement: 'W' },
      { unique_id: 'c', name: 'Trucki T2MG', object_id: 'self', unit_of_measurement: 'W' },
    ],
  }];
  const fetchImpl = async (url) => {
    if (url === '/api/v1/devices') return jsonResponse(devices);
    if (url === '/api/v1/energy/roles') return jsonResponse({ assignments: {}, interpretation: {} });
    if (url === '/api/v1/energy') return jsonResponse({ entities: [], balance: { total: 0 }, unassigned: [], unassigned_count: 0 });
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  const labels = Object.fromEntries(component.entities.map(e => [e.id, e.label]));
  assert.equal(labels.a, 'Trucki T2MG / DC Power', 'wiederholter Gerätename wird aus dem Entitätstext entfernt');
  assert.equal(labels.b, 'Trucki T2MG / PV Leistung', 'ohne Präfix bleibt der Text unverändert');
  assert.equal(labels.c, 'Trucki T2MG', 'deckt der Entitätsname genau den Gerätenamen, bleibt nur der Gerätename');
});

test('the dashboard\'s own republished HA energy device is not offered as an assignable row', async () => {
  const devices = [
    {
      id: 'energy_node',
      name: 'Energy Node',
      entities: [
        { unique_id: 'energy_node_pv_power', name: 'PV-Leistung', object_id: 'pv_power', unit_of_measurement: 'W' },
        { unique_id: 'energy_node_grid_import', name: 'Netzbezug', object_id: 'grid_import', unit_of_measurement: 'W' },
      ],
    },
    {
      id: 'inverter',
      name: 'Wechselrichter',
      entities: [
        { unique_id: 'pv_power', name: 'PV Leistung', object_id: 'pv_power', unit_of_measurement: 'W' },
      ],
    },
  ];
  const fetchImpl = async (url) => {
    if (url === '/api/v1/devices') return jsonResponse(devices);
    if (url === '/api/v1/energy/roles') return jsonResponse({ assignments: {}, interpretation: {} });
    if (url === '/api/v1/energy') return jsonResponse({ entities: [], balance: { total: 0 }, unassigned: [], unassigned_count: 0 });
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  assert.deepEqual(component.entities.map(e => e.id), ['pv_power'], 'nur die Fremd-Entität bleibt zuweisbar');
});

test('load() fetches devices/roles/energy and hydrates assignments, interpretation and unassigned', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  assert.equal(component.assignments.pv_power.role, 'pv');
  // JSON round-trip: component.interpretation was built inside the vm
  // context's realm, so a direct deepEqual against the outer-realm fixture
  // object below would spuriously fail on prototype identity, not content.
  assert.deepEqual(JSON.parse(JSON.stringify(component.interpretation)), rolesResponse.interpretation);
  assert.equal(component.unassignedCount, 1);
  assert.equal(component.unassigned[0].entity_id, 'socket_power');
  assert.equal(component.balanceTotal, 4000);
});

test('save() sends both assignments and interpretation together', async () => {
  let sentBody = null;
  const fetchImpl = async (url, options) => {
    if (url === '/api/v1/devices') return jsonResponse(devicesResponse);
    if (url === '/api/v1/energy/roles' && (!options || options.method === undefined)) return jsonResponse(rolesResponse);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    if (url === '/api/v1/energy/roles' && options && options.method === 'PUT') {
      sentBody = JSON.parse(options.body);
      return jsonResponse(rolesResponse);
    }
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component, stores } = createEnergyPanel({ fetchImpl });
  await component.load();
  await component.save();
  assert.ok(sentBody.assignments);
  assert.ok(sentBody.interpretation);
  assert.equal(sentBody.interpretation.gap_mode, 'diagnostic');
  assert.equal(sentBody.interpretation.gap_tolerance_percent, 3);
  assert.equal(sentBody.interpretation.surplus_threshold_w, 600);
  assert.equal(sentBody.interpretation.import_threshold_w, 1200);
  assert.equal(stores.toasts.last(), 'Energie-Rollen und Interpretation gespeichert.');
});

test('showMeasuredWarning fires only when load_mode is measured without a "load" role assigned', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  assert.equal(component.interpretation.load_mode, 'measured');
  assert.equal(component.showMeasuredWarning, true);
  component.assignments.pv_power.role = 'load';
  assert.equal(component.showMeasuredWarning, false);
});

test('loadModeNote explains the calculation for calculated and for auto, the same text with or without a load role', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  const t = (key) => window.I18n.t(key);
  component.interpretation.load_mode = 'calculated';
  assert.match(component.loadModeNote, /PV \+ Netzbezug/);
  assert.equal(component.loadModeNote, t('energy.page.load_mode_note.calculated'));
  component.interpretation.load_mode = 'auto';
  assert.equal(component.loadModeNote, t('energy.page.load_mode_note.auto')); // no "load" role assigned in the fixture
  component.assignments.pv_power.role = 'load';
  assert.equal(component.loadModeNote, t('energy.page.load_mode_note.auto'));
});

test('toleranceWEquivalent converts a percent tolerance using the live balance total', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  // percent mode, 3% of 4000 W = 120 W
  assert.equal(component.toleranceWEquivalent, 120);
  component.interpretation.gap_tolerance_mode = 'absolute';
  component.interpretation.gap_tolerance_w = 25;
  assert.equal(component.toleranceWEquivalent, 25);
});

test('gapModeIncludesInLoad mirrors gap_mode for the Bilanzlücke toggle', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  assert.equal(component.interpretation.gap_mode, 'diagnostic');
  assert.equal(component.gapModeIncludesInLoad, false);
  component.gapModeIncludesInLoad = true;
  assert.equal(component.interpretation.gap_mode, 'unknown_consumer');
  component.gapModeIncludesInLoad = false;
  assert.equal(component.interpretation.gap_mode, 'diagnostic');
});

const batteryDevicesResponse = [{
  name: 'BMS',
  entities: [
    { unique_id: 'pv_power', name: 'PV Leistung', object_id: 'pv_power', unit_of_measurement: 'W' },
    { unique_id: 'bank_a_soc', name: 'Bank A Füllstand', object_id: 'bank_a_soc', unit_of_measurement: '%' },
  ],
}];

function stubBatteryFetch({ assignments = {}, withoutCapacity = 0 } = {}) {
  return async (url) => {
    if (url === '/api/v1/devices') return jsonResponse(batteryDevicesResponse);
    if (url === '/api/v1/energy/roles') return jsonResponse({ assignments, interpretation: {} });
    if (url === '/api/v1/energy') return jsonResponse({
      entities: [], balance: { total: 0 }, unassigned: [], unassigned_count: 0,
      battery_soc_without_capacity: withoutCapacity,
    });
    throw new Error(`unexpected fetch ${url}`);
  };
}

test('load() nimmt Prozent-Entitäten auf und hydratisiert capacity_kwh', async () => {
  const { component } = createEnergyPanel({
    fetchImpl: stubBatteryFetch({ assignments: { bank_a_soc: { role: 'battery_soc', capacity_kwh: 12.8 } } }),
  });
  await component.load();
  assert.equal(component.entities.length, 2, 'die %-Entität muss in der Liste stehen');
  assert.equal(component.assignments.bank_a_soc.role, 'battery_soc');
  assert.equal(component.assignments.bank_a_soc.capacity_kwh, 12.8);
});

test('roleOptionsFor bietet je Einheit nur die passenden Rollen an', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubBatteryFetch() });
  await component.load();
  // Array.from with the outer Array constructor sidesteps the same
  // cross-realm identity issue as the JSON round-trip above.
  const socValues = Array.from(component.roleOptionsFor('%'), o => o.value);
  const powerValues = Array.from(component.roleOptionsFor('W'), o => o.value);
  assert.deepEqual(socValues, ['battery_soc']);
  assert.ok(powerValues.includes('pv') && powerValues.includes('heat_pump'));
  assert.ok(!powerValues.includes('battery_soc'), 'die SoC-Rolle darf in W-Zeilen nicht auftauchen');
});

test('save() schickt capacity_kwh für SoC-Zeilen und scale/invert für Leistungszeilen', async () => {
  let sentBody = null;
  const base = stubBatteryFetch({ assignments: { bank_a_soc: { role: 'battery_soc', capacity_kwh: 12.8 } } });
  const { component } = createEnergyPanel({
    fetchImpl: async (url, options) => {
      if (options && options.method === 'PUT') {
        sentBody = JSON.parse(options.body);
        return jsonResponse({});
      }
      return base(url);
    },
  });
  await component.load();
  component.assignments.pv_power.role = 'pv';
  component.assignments.pv_power.scale = 2;
  await component.save();

  assert.deepEqual(sentBody.assignments.bank_a_soc, { role: 'battery_soc', capacity_kwh: 12.8 });
  assert.deepEqual(sentBody.assignments.pv_power, { role: 'pv', scale: 2, invert: false });
});

test('save() sendet eine explizit auf "Keine Rolle" gesetzte Zuweisung statt sie wegzufiltern', async () => {
  // Regression: eine Entity, die der Backend-Heuristik zufolge (z.B. ein
  // ApSystems-Wechselrichter mit "pv" im Namen) bereits als "pv" aufgeloest
  // ist, wird im Formular auf "Keine Rolle" gestellt. Ohne diese Zuweisung
  // im PUT-Body faellt der Server beim naechsten Laden zurueck auf
  // inferRole() und weist wieder "pv" zu - see energy.go inferRole().
  let sentBody = null;
  const { component } = createEnergyPanel({
    fetchImpl: async (url, options) => {
      if (options && options.method === 'PUT') {
        sentBody = JSON.parse(options.body);
        return jsonResponse({});
      }
      return stubFetch()(url, options);
    },
  });
  await component.load();
  assert.equal(component.assignments.pv_power.role, 'pv');
  component.assignments.pv_power.role = '';
  await component.save();

  assert.ok(
    Object.prototype.hasOwnProperty.call(sentBody.assignments, 'pv_power'),
    'eine explizit geleerte Rolle muss als Override gespeichert werden, sonst greift wieder die Heuristik',
  );
  assert.equal(sentBody.assignments.pv_power.role, '');
});

test('showSocCapacityWarning folgt battery_soc_without_capacity aus dem Snapshot', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubBatteryFetch({ withoutCapacity: 2 }) });
  await component.load();
  assert.equal(component.socWithoutCapacity, 2);
  assert.equal(component.showSocCapacityWarning, true);

  const { component: quiet } = createEnergyPanel({ fetchImpl: stubBatteryFetch({ withoutCapacity: 0 }) });
  await quiet.load();
  assert.equal(quiet.showSocCapacityWarning, false);
});

test('payload trägt die Batterie-Reserve mit', () => {
  const { component } = createEnergyPanel();
  component.interpretation.battery_reserve_percent = 15;
  assert.equal(component.payload().interpretation.battery_reserve_percent, 15);
});

// 0 ist das Abschalten und muss als 0 im Payload landen - nicht als
// fehlendes Feld, sonst setzt der Server die Vorgabe zurueck.
test('payload schickt eine abgeschaltete Reserve als ausdrückliche 0', () => {
  const { component } = createEnergyPanel();
  component.interpretation.battery_reserve_percent = 0;
  assert.equal(component.payload().interpretation.battery_reserve_percent, 0);
  assert.ok('battery_reserve_percent' in component.payload().interpretation);
});

test('save sends the CSRF token from the session and the page is read only without edit_energy', async () => {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({csrf_token: 'tok', edit_energy: true});
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(rolesResponse);
    if (url.endsWith('/api/v1/energy')) return jsonResponse(energyResponse);
    throw new Error(`unexpected ${url}`);
  };
  const {component} = createEnergyPanel({fetchImpl});
  await component.load();
  assert.equal(component.canEdit, true);
  await component.save();
  const put = calls.find(call => call.options.method === 'PUT');
  assert.equal(put.options.headers['X-CSRF-Token'], 'tok');

  const readOnly = createEnergyPanel({fetchImpl: async url => (url.endsWith('/api/v1/auth/session')
    ? jsonResponse({csrf_token: 'tok', edit_energy: false}) : fetchImpl(url))});
  await readOnly.component.load();
  assert.equal(readOnly.component.canEdit, false);
});

test('without a session API the page stays editable', async () => {
  const fetchImpl = async url => {
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({}, false);
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(rolesResponse);
    return jsonResponse(energyResponse);
  };
  const {component} = createEnergyPanel({fetchImpl});
  await component.load();
  assert.equal(component.canEdit, true);
  assert.equal(component.csrfToken, '');
});

test('energy-roles-changed reloads the page and focusRows flashes and unhides rows', async (t) => {
  t.mock.timers.enable({apis: ['setTimeout']});
  let loads = 0;
  const fetchImpl = async url => {
    if (url.endsWith('/api/v1/devices')) loads += 1;
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({edit_energy: true});
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(rolesResponse);
    return jsonResponse(energyResponse);
  };
  const {component, window} = createEnergyPanel({fetchImpl});
  component.$nextTick = fn => fn();
  component.init();
  await component.load();
  window.dispatchEvent(new window.CustomEvent('energy-roles-changed'));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(loads, 2);
  component.hiddenIds = {socket_power: true};
  component.focusRows(['socket_power']);
  assert.equal(component.hiddenExpanded, true);
  assert.equal(component.flash.socket_power, true);
  component._setTimeout = (fn, ms) => setTimeout(fn, ms);
  component.focusRows(['socket_power']);
  t.mock.timers.tick(1600);
  assert.equal(component.flash.socket_power, undefined);
});

// Hidden rows: the set lives in energy.json, shared by every browser.
function hiddenFetch({ roles = rolesResponse, edit = true, patchOk = true, calls = [] } = {}) {
  return async (url, options = {}) => {
    calls.push({ url, options });
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({ edit_energy: edit, csrf_token: 't' });
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
    if (url.endsWith('/api/v1/energy/roles') && options.method === 'PATCH') {
      return jsonResponse(patchOk ? {} : { message: 'forbidden' }, patchOk);
    }
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(roles);
    return jsonResponse(energyResponse);
  };
}

test('load() takes the hidden ids from energy.json', async () => {
  const { component } = createEnergyPanel({ fetchImpl: hiddenFetch({ roles: { ...rolesResponse, hidden: ['socket_power'] } }) });
  await component.load();
  assert.equal(component.isHidden('socket_power'), true);
  assert.deepEqual(component.hiddenEntities.map(entity => entity.id), ['socket_power']);
});

test('a row with a role is never hidden and offers no hide button', async () => {
  const { component } = createEnergyPanel({ fetchImpl: hiddenFetch() });
  await component.load();
  component.hiddenIds = { pv_power: true };
  assert.equal(component.isHidden('pv_power'), false);
  assert.equal(component.canHide('pv_power'), false);
  assert.equal(component.canHide('socket_power'), true);
});

test('toggleHidden saves through PATCH and tells the other pages', async () => {
  const calls = [];
  const { component, window } = createEnergyPanel({ fetchImpl: hiddenFetch({ calls }) });
  let changed = 0;
  window.addEventListener('energy-roles-changed', () => { changed += 1; });
  await component.load();
  await component.toggleHidden('socket_power');
  const [first] = calls.filter(call => call.options.method === 'PATCH');
  assert.equal(first.url, '/api/v1/energy/roles');
  assert.deepEqual(JSON.parse(first.options.body), { hidden: { socket_power: true } });
  assert.equal(first.options.headers['X-CSRF-Token'], 't');
  assert.equal(component.isHidden('socket_power'), true);
  assert.equal(changed, 1);
  await component.toggleHidden('socket_power');
  const patches = calls.filter(call => call.options.method === 'PATCH');
  assert.deepEqual(JSON.parse(patches[1].options.body), { hidden: { socket_power: false } });
  assert.equal(component.isHidden('socket_power'), false);
});

test('toggleHidden reverts and shows a toast when the server refuses', async () => {
  const { component, stores } = createEnergyPanel({ fetchImpl: hiddenFetch({ patchOk: false }) });
  await component.load();
  await component.toggleHidden('socket_power');
  assert.equal(component.isHidden('socket_power'), false);
  assert.equal(stores.toasts.criticals.length, 1);
});

test('load() moves legacy localStorage ids to energy.json once and drops assigned ones', async () => {
  const calls = [];
  const { component, window } = createEnergyPanel({ fetchImpl: hiddenFetch({ calls }), url: 'http://localhost/' });
  window.localStorage.setItem('energy-roles-hidden-ids', JSON.stringify({ socket_power: true, pv_power: true }));
  await component.load();
  const patches = calls.filter(call => call.options.method === 'PATCH');
  assert.equal(patches.length, 1);
  assert.deepEqual(JSON.parse(patches[0].options.body), { hidden: { socket_power: true } });
  assert.equal(window.localStorage.getItem('energy-roles-hidden-ids'), null);
  assert.equal(component.isHidden('socket_power'), true);
  assert.equal(component.isHidden('pv_power'), false);
});

test('without edit rights the legacy list is ignored, neither sent nor deleted', async () => {
  const calls = [];
  const { component, window } = createEnergyPanel({ fetchImpl: hiddenFetch({ calls, edit: false }), url: 'http://localhost/' });
  window.localStorage.setItem('energy-roles-hidden-ids', JSON.stringify({ socket_power: true }));
  await component.load();
  assert.equal(calls.some(call => call.options.method === 'PATCH'), false);
  assert.notEqual(window.localStorage.getItem('energy-roles-hidden-ids'), null);
  assert.equal(component.isHidden('socket_power'), false);
  assert.equal(component.canHide('socket_power'), false);
});

test('load() sends an unhide for a server id that now carries a role', async () => {
  const calls = [];
  const { component } = createEnergyPanel({ fetchImpl: hiddenFetch({ calls, roles: { ...rolesResponse, hidden: ['pv_power'] } }) });
  await component.load();
  const [patch] = calls.filter(call => call.options.method === 'PATCH');
  assert.deepEqual(JSON.parse(patch.options.body), { hidden: { pv_power: false } });
  assert.equal(component.isHidden('pv_power'), false);
});

test('the page loads categories and groups, offers custom roles and saves both', async () => {
  const calls = [];
  const devices = [{...devicesResponse[0], id: 'inverter'}];
  const roles = {...rolesResponse,
    categories: {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}},
    groups: {garage: {label: 'Garage', members: {devices: ['inverter'], groups: []}}}};
  const fetchImpl = async (url, options = {}) => {
    calls.push({url, options});
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({edit_energy: true, csrf_token: 't'});
    if (url.endsWith('/api/v1/device/icons')) return jsonResponse([{name: 'mdi:home', labelKey: 'device_icon.home'}]);
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devices);
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(roles);
    return jsonResponse(energyResponse);
  };
  const {component} = createEnergyPanel({fetchImpl});
  await component.load();
  assert.deepEqual(JSON.parse(JSON.stringify(component.customRoleOptions().map(option => option.value))), ['custom:werkstatt']);
  component.addGroup();
  const newId = Object.keys(component.groups).find(id => id !== 'garage');
  component.groups[newId].label = 'Werkstatt Nord';
  await component.save();
  const body = JSON.parse(calls.find(call => call.options.method === 'PUT').options.body);
  assert.ok(body.categories.werkstatt);
  assert.ok(body.groups.garage);
  assert.equal(Object.keys(body.groups).length, 2);
});

test('saving after removing a used category shows the server error', async () => {
  const fetchImpl = async (url, options = {}) => {
    if (options.method === 'PUT') return jsonResponse({code: 'energy_roles_rejected', message: 'x', message_key: 'error.energy_roles_rejected.category_unknown', params: {category: 'werkstatt'}}, false);
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({edit_energy: true});
    if (url.endsWith('/api/v1/device/icons')) return jsonResponse([]);
    if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse({...rolesResponse, categories: {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}}});
    return jsonResponse(energyResponse);
  };
  const {component, stores} = createEnergyPanel({fetchImpl});
  await component.load();
  component.removeCategory('werkstatt');
  await component.save();
  assert.equal(stores.toasts.criticals.length, 1);
});

const categoryRemovalFetch = referencesResponse => async (url, options = {}) => {
  if (url.includes('/api/v1/energy/category-references')) return referencesResponse(url);
  if (url.endsWith('/api/v1/auth/session')) return jsonResponse({edit_energy: true});
  if (url.endsWith('/api/v1/device/icons')) return jsonResponse([]);
  if (url.endsWith('/api/v1/devices')) return jsonResponse(devicesResponse);
  if (url.endsWith('/api/v1/energy/roles')) return jsonResponse({...rolesResponse, categories: {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}}});
  return jsonResponse(energyResponse);
};

test('removing a category lists the automations that use it before it is removed', async () => {
  const urls = [];
  const fetchImpl = categoryRemovalFetch(url => {
    urls.push(url);
    return jsonResponse({rules: [{id: 'r1', name: 'Sauna on'}]});
  });
  const {component} = createEnergyPanel({fetchImpl});
  component.$refs = {};
  await component.load();
  await component.askRemoveCategory('werkstatt');
  assert.deepEqual(urls, ['/api/v1/energy/category-references?id=werkstatt']);
  assert.equal(component.categoryRemoval.loading, false);
  assert.deepEqual(JSON.parse(JSON.stringify(component.categoryRemoval.rules)), [{id: 'r1', name: 'Sauna on'}]);
  assert.ok(component.categories.werkstatt, 'category stays until confirmed');
  component.confirmRemoveCategory();
  assert.equal(component.categories.werkstatt, undefined);
  assert.equal(component.categoryRemoval, null);
});

test('a failing category reference check shows an error and still allows removal', async () => {
  const fetchImpl = categoryRemovalFetch(() => jsonResponse({}, false));
  const {component} = createEnergyPanel({fetchImpl});
  component.$refs = {};
  await component.load();
  await component.askRemoveCategory('werkstatt');
  assert.equal(component.categoryRemoval.error, true);
  assert.equal(component.categoryRemoval.loading, false);
  component.confirmRemoveCategory();
  assert.equal(component.categories.werkstatt, undefined);
});

test('opening a rule from the removal dialog focuses it in the automations panel', async () => {
  const {component, window} = createEnergyPanel({fetchImpl: categoryRemovalFetch(() => jsonResponse({rules: []}))});
  component.$refs = {};
  const events = [];
  window.addEventListener('dashboard-open-panel', event => events.push(JSON.parse(JSON.stringify(event.detail))));
  await component.load();
  await component.askRemoveCategory('werkstatt');
  component.openRemovalRule('r1');
  assert.deepEqual(events, [{panel: 'automations-panel', automationFocus: 'r1'}]);
  assert.equal(component.categoryRemoval, null);
});

test('clicking a role row asks the plant view to highlight its device', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: stubFetch() });
  const seen = [];
  window.addEventListener('energy-plant-focus', event => seen.push(event.detail.entityId));
  component.onRowClick('pv_power');
  assert.deepEqual(seen, ['pv_power']);
});

test('energy-focus-group flashes the group card', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: stubFetch() });
  component.init();
  component._setTimeout = () => null;
  window.CSS = { escape: value => value };
  window.dispatchEvent(new window.CustomEvent('energy-focus-group', { detail: { id: 'uv' } }));
  assert.equal(component.flashGroup, 'uv');
});

test('group members are listed as chips, groups with a group: id', () => {
  const { component } = createEnergyPanel();
  component.devicesList = [{ id: 'wb', name: 'Wallbox', manufacturer: '', icon: '' }];
  component.groups = {
    garage: { label: 'Garage', members: { devices: ['wb'], groups: ['keller'] } },
    keller: { label: 'Keller', members: { devices: [], groups: [] } },
  };
  assert.deepEqual(JSON.parse(JSON.stringify(component.groupMembers('garage'))), [
    { id: 'group:keller', kind: 'group', name: 'Keller' },
    { id: 'wb', kind: 'device', name: 'Wallbox' },
  ]);
  component.removeGroupMember('garage', 'group:keller');
  component.removeGroupMember('garage', 'wb');
  assert.deepEqual(JSON.parse(JSON.stringify(component.groups.garage.members)), { devices: [], groups: [] });
});

test('members picked for a group leave the group they were in before', async () => {
  const { component } = createEnergyPanel();
  component.devicesList = [
    { id: 'wb', name: 'Wallbox', manufacturer: 'Shelly', icon: '' },
    { id: 'saw', name: 'Kreissäge', manufacturer: 'Shelly', icon: '' },
  ];
  component.groups = {
    garage: { label: 'Garage', members: { devices: [], groups: [] } },
    keller: { label: 'Keller', members: { devices: ['saw'], groups: [] } },
  };
  const pending = component.addGroupMembers('garage');
  const offered = component.picker.items;
  assert.equal(offered.find(item => item.id === 'saw').memberOf, 'Keller');
  assert.ok(offered.some(item => item.id === 'group:keller'));
  assert.ok(!offered.some(item => item.id === 'group:garage'));
  component.togglePick('saw');
  component.togglePick('group:keller');
  component.closePicker(true);
  await pending;
  assert.deepEqual(JSON.parse(JSON.stringify(component.groups.garage.members)), { devices: ['saw'], groups: ['keller'] });
  assert.deepEqual(JSON.parse(JSON.stringify(component.groups.keller.members.devices)), []);
});

test('the settings accordion keeps exactly one section open', () => {
  const { component } = createEnergyPanel();
  component.$nextTick = fn => fn();
  assert.equal(component.openSection, 'roles', 'roles are open by default');
  component.toggleSection('groups');
  assert.equal(component.isSectionOpen('groups'), true);
  assert.equal(component.isSectionOpen('roles'), false);
  component.toggleSection('groups');
  assert.equal(component.openSection, '', 'a second click closes the open section');
});

test('focusing a group or rows opens the matching section', () => {
  const { component } = createEnergyPanel();
  component.$nextTick = () => {};
  component._setTimeout = () => 0;
  component.toggleSection('interpretation');
  component.focusGroup('garage');
  assert.equal(component.openSection, 'groups');
  component.focusRows(['pv_power']);
  assert.equal(component.openSection, 'roles');
});

test('the switch "count as a whole" picks the first consumer category and clears the role when off', () => {
  const { component } = createEnergyPanel();
  component.categories = {
    werkstatt: { label: 'Werkstatt', base: 'consumer', color: 'cat_2', icon: '' },
    dach: { label: 'Dach', base: 'producer', color: 'cat_1', icon: '' },
  };
  component.groups = { garage: { label: 'Garage', members: { devices: [], groups: [] } } };
  assert.equal(component.groupCountsWhole('garage'), false);
  assert.deepEqual(JSON.parse(JSON.stringify(component.groupRoleOptions().map(option => option.value))), ['custom:werkstatt']);
  component.setGroupWhole('garage', true);
  assert.equal(component.groups.garage.role, 'custom:werkstatt');
  component.setGroupWhole('garage', false);
  assert.equal(component.groups.garage.role, '');
  assert.equal(component.groupCountsWhole('garage'), false);
});

test('without a consumer category the switch stays on and waits for one', () => {
  const { component } = createEnergyPanel();
  component.groups = { garage: { label: 'Garage', members: { devices: [], groups: [] } } };
  component.setGroupWhole('garage', true);
  assert.equal(component.groups.garage.role || '', '');
  assert.equal(component.groupCountsWhole('garage'), true);
});

test('a new category from a group card becomes its role and opens the categories', () => {
  const { component } = createEnergyPanel();
  component.$nextTick = () => {};
  component.groups = { garage: { label: 'Garage', members: { devices: [], groups: [] } } };
  component.addCategoryForGroup('garage');
  const [id] = Object.keys(component.categories);
  assert.equal(component.categories[id].base, 'consumer');
  assert.equal(component.groups.garage.role, `custom:${id}`);
  assert.equal(component.openSection, 'categories');
});

test('save() tells the device map and the plant view, the page skips its own event', async () => {
  let loads = 0;
  const fetchImpl = async (url, options) => {
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({edit_energy: true});
    if (url === '/api/v1/devices') { loads += 1; return jsonResponse(devicesResponse); }
    if (url === '/api/v1/energy/roles') return jsonResponse(rolesResponse);
    return jsonResponse(energyResponse);
  };
  const {component, window} = createEnergyPanel({fetchImpl});
  const seen = [];
  window.addEventListener('energy-roles-changed', event => seen.push(event.detail && event.detail.source));
  component.init();
  await component.load();
  loads = 0;
  await component.save();
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(seen, ['energy-page']);
  assert.equal(loads, 0, 'no reload after its own save');
});

test('die Statuszeile nennt Quelle, Rueckfall und Zaehlerabweichung', () => {
  const { component, window } = createEnergyPanel();
  const t = (key, params) => window.I18n.t(key, params);
  component.liveBalance = {load_source: 'calculated', load_fallback: '', meter_check: 'deviates', meter_deviation: -212.4};
  assert.equal(component.loadStatus, `${t('energy.page.load_status.calculated')} ${t('energy.page.load_status.meter_deviates', {deviation: '212 W'})}`);
  component.liveBalance = {load_source: 'measured', load_fallback: 'no_grid', meter_check: ''};
  assert.equal(component.loadStatus, t('energy.page.load_status.fallback_no_grid'));
  component.liveBalance = {load_source: 'missing', load_fallback: '', meter_check: ''};
  assert.equal(component.loadStatus, t('energy.page.load_status.missing'));
  component.liveBalance = {load_source: 'calculated', load_fallback: '', meter_check: 'ok', meter_deviation: 4};
  assert.equal(component.loadStatus, `${t('energy.page.load_status.calculated')} ${t('energy.page.load_status.meter_ok')}`);
  component.liveBalance = null;
  assert.equal(component.loadStatus, '');
});

test('das Template bietet nur noch Automatisch, Berechnet und Gemessen an', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'energy.html'), 'utf8');
  const modes = [...html.matchAll(/name="load-mode" value="([a-z]+)"/g)].map(m => m[1]);
  assert.deepEqual(modes, ['auto', 'calculated', 'measured']);
  assert.match(html, /x-text="loadStatus"/);
});

// Switch rows of the energy configuration (task 8 of the switch role plan).
const plain = value => JSON.parse(JSON.stringify(value));

const plugDevices = [{
  id: 'plug',
  name: 'Plug',
  entities: [{ unique_id: 'plug_power', name: 'Leistung', object_id: 'power', unit_of_measurement: 'W' }],
}];
const plugSwitches = [{ device_id: 'plug', entity_id: 's', name: 'Plug Steckdose', state: 'on' }];

const multiDevices = [{
  id: 'pm',
  name: 'Mehrfachsteckdose',
  entities: [{ unique_id: 'pm_power', name: 'Leistung', object_id: 'power', unit_of_measurement: 'W' }],
}];
const multiSwitches = [
  { device_id: 'pm', entity_id: 's1', name: 'Mehrfach Kanal 1', state: 'off' },
  { device_id: 'pm', entity_id: 's2', name: 'Mehrfach Kanal 2', state: 'off' },
];

const emptyRoles = { assignments: {}, interpretation: {} };

function switchFetch({ devices, switches = [], roles = emptyRoles, onPut } = {}) {
  return async (url, options) => {
    if (url === '/api/v1/devices') return jsonResponse(devices);
    if (url === '/api/v1/devices/switches') return jsonResponse(switches);
    if (url === '/api/v1/energy/roles' && options && options.method === 'PUT') {
      if (onPut) onPut(JSON.parse(options.body));
      return jsonResponse(roles);
    }
    if (url === '/api/v1/energy/roles') return jsonResponse(roles);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
}

test('switches appear as their own role group with the detected role', async () => {
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches }) });
  await component.load();
  assert.deepEqual(component.visibleSwitchEntities.map(entity => entity.id), ['s']);
  assert.equal(component.visibleSwitchEntities[0].unit, 'switch');
  assert.equal(component.visibleSwitchEntities[0].label, 'Plug / Steckdose');
  assert.equal(component.assignments.s.role, 'switch');
  assert.equal(component.assignments.s.channel, 1);
  assert.ok(!component.visibleEnergyEntities.some(entity => entity.id === 's'), 'a switch is not an energy value row');
  assert.deepEqual(plain(component.roleOptionsFor('switch')), [{ value: 'switch', label: 'Schaltet' }]);
});

test('saving without touching a detected switch writes nothing for it', async () => {
  let sent = null;
  const { component } = createEnergyPanel({
    fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches, onPut: body => { sent = body; } }),
  });
  await component.load();
  assert.equal(component.assignments.s.role, 'switch', 'the switch is detected, so it is shown as a switch');
  await component.save();
  assert.equal(Object.prototype.hasOwnProperty.call(sent.assignments, 's'), false, 'the detected switch stays out of the PUT body');
});

test('saving a changed switch writes the role and the channel above 1', async () => {
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: multiDevices, switches: multiSwitches }) });
  await component.load();
  component.assignments.s2.role = 'switch';
  component.assignments.s2.channel = 2;
  const { assignments } = component.payload();
  assert.deepEqual(plain(assignments.s2), { role: 'switch', channel: 2 });
  assert.equal(Object.prototype.hasOwnProperty.call(assignments, 's1'), false);
});

test('a switch set back from the detected role to no role is written as an explicit no role', async () => {
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches }) });
  await component.load();
  component.assignments.s.role = '';
  assert.deepEqual(plain(component.payload().assignments.s), { role: '' });
});

test('a saved switch role is written back unchanged', async () => {
  const roles = { assignments: { s: { role: 'switch' } }, interpretation: {} };
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches, roles }) });
  await component.load();
  assert.deepEqual(plain(component.payload().assignments.s), { role: 'switch' });
});

test('a saved explicit no role for a switch is kept on every save', async () => {
  const roles = { assignments: { s: { role: '' } }, interpretation: {} };
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches, roles }) });
  await component.load();
  assert.deepEqual(plain(component.payload().assignments.s), { role: '' });
});

test('the channel field shows only for a device with two switches', async () => {
  const { component } = createEnergyPanel({ fetchImpl: switchFetch({ devices: multiDevices, switches: multiSwitches }) });
  await component.load();
  assert.equal(component.switchChannelCount('s1'), 2);
  assert.equal(component.switchChannelCount('s2'), 2);
  const single = createEnergyPanel({ fetchImpl: switchFetch({ devices: plugDevices, switches: plugSwitches }) });
  await single.component.load();
  assert.equal(single.component.switchChannelCount('s'), 1);
});

test('after a save the next payload is judged against the saved state', async () => {
  let sent = null;
  const { component } = createEnergyPanel({
    fetchImpl: switchFetch({ devices: multiDevices, switches: multiSwitches, onPut: body => { sent = body; } }),
  });
  await component.load();
  component.assignments.s2.role = 'switch';
  component.assignments.s2.channel = 2;
  await component.save();
  assert.deepEqual(plain(sent.assignments.s2), { role: 'switch', channel: 2 });
  component.assignments.s2.role = '';
  assert.deepEqual(plain(component.payload().assignments.s2), { role: '' }, 'the saved switch is cleared, not dropped');
});

test('the energy template offers the switch group and a channel field for multi-switch devices', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'energy.html'), 'utf8');
  assert.match(html, /energy\.roles\.section\.switch_values/);
  assert.match(html, /visibleSwitchEntities/);
  assert.match(html, /type="number" min="1" max="16"/);
  assert.match(html, /\{\{t "energy\.page\.channel"\}\}/);
  assert.match(html, /isSwitchRow\(entity\.id\)/);
});

test('a saved switch role without a loaded switch row is written back unchanged', async () => {
  let sent = null;
  const roles = { assignments: { gone: { role: 'switch', channel: 2 } }, interpretation: {} };
  const { component } = createEnergyPanel({
    fetchImpl: switchFetch({ devices: plugDevices, switches: [], roles, onPut: body => { sent = body; } }),
  });
  await component.load();
  await component.save();
  assert.deepEqual(plain(sent.assignments.gone), { role: 'switch', channel: 2 });
});

// Sub-devices: a power row takes the label of its sub-device from the device map.
const subDevicesSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-subdevices.js'),
  'utf8',
);
const subDeviceMap = {
  sub_devices: [{ id: 'sub:wm', label: 'Waschmaschine', device_id: 'wechselrichter', entity_id: 'socket_power' }],
};
const labelOfRow = (component, id) => component.entities.find(entity => entity.id === id).label;

test('power row is labelled after its sub-device', async () => {
  const fetchImpl = async (url) => {
    if (url === '/api/v1/device/map') return jsonResponse(subDeviceMap);
    if (url === '/api/v1/devices') return jsonResponse(devicesResponse);
    if (url === '/api/v1/energy/roles') return jsonResponse(rolesResponse);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  assert.equal(labelOfRow(component, 'socket_power'), 'Wechselrichter · Waschmaschine');
  assert.equal(labelOfRow(component, 'pv_power'), 'Wechselrichter / PV Leistung', 'ohne Untergerät bleibt die alte Beschriftung');
});

test('row label falls back without the device map', async () => {
  const fetchImpl = async (url) => {
    if (url === '/api/v1/device/map') throw new Error('map down');
    if (url === '/api/v1/devices') return jsonResponse(devicesResponse);
    if (url === '/api/v1/energy/roles') return jsonResponse(rolesResponse);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  assert.equal(labelOfRow(component, 'socket_power'), 'Wechselrichter / Steckdose');
  assert.equal(component.loading, false);
});

test('device-map-changed reloads the device map and relabels the row', async () => {
  let map = { sub_devices: [] };
  const fetchImpl = async (url) => {
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({ edit_energy: true });
    if (url === '/api/v1/device/map') return jsonResponse(map);
    if (url === '/api/v1/devices') return jsonResponse(devicesResponse);
    if (url === '/api/v1/energy/roles') return jsonResponse(rolesResponse);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component, window } = createEnergyPanel({ fetchImpl });
  component.init();
  await component.load();
  assert.equal(labelOfRow(component, 'socket_power'), 'Wechselrichter / Steckdose');
  map = subDeviceMap;
  window.dispatchEvent(new window.CustomEvent('device-map-changed', { detail: { source: 'devicemap' } }));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(labelOfRow(component, 'socket_power'), 'Wechselrichter · Waschmaschine');
});

test('sub-device row label joins the meter device name and the sub-device label', async () => {
  const devices = [{ id: 'plug', name: 'Plug Entfeuchter', entities: [
    { unique_id: 'socket_power', name: 'Steckdose', object_id: 'socket_power', unit_of_measurement: 'W' },
  ] }];
  const map = { sub_devices: [{ id: 'sub:wm', label: 'Waschmaschine', device_id: 'plug', entity_id: 'socket_power' }] };
  const fetchImpl = async (url) => {
    if (url === '/api/v1/device/map') return jsonResponse(map);
    if (url === '/api/v1/devices') return jsonResponse(devices);
    if (url === '/api/v1/energy/roles') return jsonResponse({ assignments: {}, interpretation: {} });
    if (url === '/api/v1/energy') return jsonResponse({ entities: [], balance: { total: 0 }, unassigned: [], unassigned_count: 0 });
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  assert.equal(labelOfRow(component, 'socket_power'), 'Plug Entfeuchter · Waschmaschine');
});

// The categories overview lists every sub-device of the device map, read only.
const devicesWithIds = devicesResponse.map(device => ({ ...device, id: 'wechselrichter' }));

test('categories overview lists each sub-device with icon, label and meter device', async () => {
  const map = { sub_devices: [{ id: 'sub:wm', label: 'Waschmaschine', icon: 'plug', device_id: 'wechselrichter', entity_id: 'socket_power' }] };
  const fetchImpl = async (url) => {
    if (url === '/api/v1/device/map') return jsonResponse(map);
    if (url === '/api/v1/device/icons') return jsonResponse([{ name: 'plug', markup: '<path d="M1 1"/>' }]);
    if (url === '/api/v1/devices') return jsonResponse(devicesWithIds);
    if (url === '/api/v1/energy/roles') return jsonResponse(rolesResponse);
    if (url === '/api/v1/energy') return jsonResponse(energyResponse);
    throw new Error(`unexpected fetch ${url}`);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  await component.load();
  assert.deepEqual(plain(component.subDeviceEntries), [
    { id: 'sub:wm', label: 'Waschmaschine', iconMarkup: '<path d="M1 1"/>', meter: 'Wechselrichter' },
  ]);
  assert.deepEqual(plain(component.categories), {}, 'the overview is not part of the user categories');
});

test('categories overview is empty without sub-devices', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  assert.deepEqual(plain(component.subDeviceEntries), []);
});

// Measurement chains: the chain endpoints answer with the saved energy
// config, the page takes chains, role_needed and the roles of the rows the
// chain touched from that answer.
const chainDevices = [{
  name: 'Wechselrichter',
  entities: [
    { unique_id: 'pv_power', name: 'PV Leistung', object_id: 'pv_power', unit_of_measurement: 'W' },
    { unique_id: 'socket_power', name: 'Steckdose', object_id: 'socket_power', unit_of_measurement: 'W' },
    { unique_id: 'extra_power', name: 'Zusatz', object_id: 'extra_power', unit_of_measurement: 'W' },
  ],
}];

const savedAfterAdd = {
  assignments: {
    pv_power: { role: '' },
    socket_power: { role: 'load' },
    extra_power: { role: '' },
  },
  chains: { neue_messkette: { label: 'Neue Messkette', points: ['socket_power', 'pv_power'], counting: 'socket_power' } },
  role_needed: [],
};

function chainFetch({ calls = [], saved = {}, chainOk = true, snapshotChains = [] } = {}) {
  return async (url, options = {}) => {
    calls.push({ url, options });
    if (url.endsWith('/api/v1/auth/session')) return jsonResponse({ edit_energy: true, csrf_token: 't' });
    if (url.endsWith('/api/v1/devices')) return jsonResponse(chainDevices);
    if (url.endsWith('/api/v1/energy/roles') && options.method === 'PATCH') return jsonResponse({});
    if (url.endsWith('/api/v1/energy/roles')) return jsonResponse(rolesResponse);
    if (url.startsWith('/api/v1/energy/chains')) {
      return chainOk ? jsonResponse(saved) : jsonResponse({ message: 'refused' }, false);
    }
    if (url.endsWith('/api/v1/energy')) return jsonResponse({ ...energyResponse, chains: snapshotChains });
    throw new Error(`unexpected fetch ${url}`);
  };
}

const chainWriteCalls = calls => calls.filter(call => ['POST', 'PUT', 'DELETE'].includes(call.options.method));
const plainJSON = value => JSON.parse(JSON.stringify(value));

test('addChain posts the picked points in selection order and takes the saved chains and roles', async () => {
  const calls = [];
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch({ calls, saved: savedAfterAdd }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.openPicker = async () => ['socket_power', 'pv_power'];
  await component.addChain();
  const post = chainWriteCalls(calls)[0];
  assert.equal(post.options.method, 'POST');
  assert.equal(post.url, '/api/v1/energy/chains');
  assert.equal(post.options.headers['X-CSRF-Token'], 't');
  assert.deepEqual(JSON.parse(post.options.body), {
    label: window.I18n.t('energy.chains.default_name'),
    points: ['socket_power', 'pv_power'],
    counting: 'socket_power',
  });
  assert.deepEqual(plainJSON(component.chains), savedAfterAdd.chains);
  assert.equal(component.assignments.socket_power.role, 'load');
  assert.equal(component.assignments.pv_power.role, '', 'the old role of a point is replaced by the saved one');
});

test('addChain keeps an unsaved draft on another row', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch({ saved: savedAfterAdd }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.assignments.extra_power.role = 'load';
  component.openPicker = async () => ['socket_power', 'pv_power'];
  await component.addChain();
  assert.equal(component.assignments.extra_power.role, 'load');
});

test('a refused chain call changes nothing and shows a toast', async () => {
  const { component, stores } = createEnergyPanel({ fetchImpl: chainFetch({ chainOk: false }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.openPicker = async () => ['socket_power', 'pv_power'];
  await component.addChain();
  assert.deepEqual(plainJSON(component.chains), {});
  assert.equal(component.assignments.pv_power.role, 'pv');
  assert.equal(stores.toasts.criticals.length, 1);
});

test('addChain does nothing without edit rights or with fewer than two points', async () => {
  const calls = [];
  const { component } = createEnergyPanel({ fetchImpl: chainFetch({ calls, saved: savedAfterAdd }) });
  component.$nextTick = fn => fn();
  await component.load();
  let pickerOpened = 0;
  component.openPicker = async () => { pickerOpened += 1; return ['pv_power']; };
  await component.addChain();
  assert.equal(chainWriteCalls(calls).length, 0);
  component.canEdit = false;
  await component.addChain();
  assert.equal(pickerOpened, 1, 'without edit rights the picker does not open');
});

test('sectionMeta counts the measurement chains', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch() });
  await component.load();
  window.I18n.tn = (key, n, params) => `${key}|${n}|${params.n}`;
  component.chains = { a: {}, b: {} };
  assert.equal(component.sectionMeta('chains'), 'energy.section.chains_meta|2|2');
  component.chains = {};
  assert.equal(component.sectionMeta('chains'), 'energy.section.chains_meta|0|0');
});

test('live chain values load with at least one chain, even with the chains section closed, every five seconds', async () => {
  const calls = [];
  const { component, window } = createEnergyPanel({
    fetchImpl: chainFetch({ calls, snapshotChains: [{ id: 'm', label: 'Balkon', segments: [] }] }),
  });
  component.$nextTick = fn => fn();
  component.init();
  await component.load();
  let now = 100000;
  component._now = () => now;
  const liveLoads = () => calls.filter(call => call.url === '/api/v1/energy').length;
  const fireRegistryUpdated = async () => {
    window.dispatchEvent(new window.CustomEvent('registry-updated', { detail: {} }));
    await new Promise(resolve => setImmediate(resolve));
  };

  component.openSection = 'chains';
  let before = liveLoads();
  await fireRegistryUpdated();
  assert.equal(liveLoads(), before, 'no chains, nothing to load');

  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.openSection = 'roles';
  before = liveLoads();
  await fireRegistryUpdated();
  assert.equal(liveLoads(), before + 1, 'a closed chains section still loads, its findings belong to the notices');
  assert.deepEqual(Object.keys(component.chainStates), ['m']);

  now += 5000;
  component.openSection = 'chains';
  before = liveLoads();
  await fireRegistryUpdated();
  assert.equal(liveLoads(), before + 1);
  assert.deepEqual(Object.keys(component.chainStates), ['m']);

  now += 1000;
  before = liveLoads();
  await fireRegistryUpdated();
  assert.equal(liveLoads(), before, 'within five seconds nothing loads');

  now += 5000;
  before = liveLoads();
  await fireRegistryUpdated();
  assert.equal(liveLoads(), before + 1);
});

test('removing the last but one point asks first and then deletes the chain', async () => {
  const calls = [];
  const { component, stores } = createEnergyPanel({ fetchImpl: chainFetch({ calls, saved: { assignments: {}, role_needed: [] } }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };

  stores.modal.answer = false;
  await component.removeChainPoint('m', 'socket_power');
  assert.equal(stores.modal.calls.length, 1);
  assert.equal(chainWriteCalls(calls).length, 0, 'a refused question writes nothing');

  stores.modal.answer = true;
  await component.removeChainPoint('m', 'socket_power');
  const del = chainWriteCalls(calls)[0];
  assert.equal(del.options.method, 'DELETE');
  assert.equal(del.url, '/api/v1/energy/chains/m');
  assert.deepEqual(plainJSON(component.chains), {});
});

test('insert, remove, counting and rename save the changed chain with PUT', async () => {
  const calls = [];
  const { component } = createEnergyPanel({ fetchImpl: chainFetch({ calls, saved: { assignments: {}, chains: {}, role_needed: [] } }) });
  component.$nextTick = fn => fn();
  await component.load();

  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.openPicker = async () => ['extra_power'];
  await component.insertChainPoint('m', 1);

  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power', 'extra_power'], counting: 'pv_power' } };
  await component.removeChainPoint('m', 'pv_power');

  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  await component.setChainCounting('m', 'socket_power');

  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'socket_power' } };
  await component.renameChain('m', 'Neuer Name');

  const puts = chainWriteCalls(calls).filter(call => call.options.method === 'PUT');
  assert.equal(puts.length, 4);
  assert.ok(puts.every(call => call.url === '/api/v1/energy/chains/m'));
  assert.deepEqual(JSON.parse(puts[0].options.body), { label: 'Balkon', points: ['pv_power', 'extra_power', 'socket_power'], counting: 'pv_power', direction: '' });
  assert.deepEqual(JSON.parse(puts[1].options.body), { label: 'Balkon', points: ['socket_power', 'extra_power'], counting: 'socket_power', direction: '' });
  assert.equal(JSON.parse(puts[2].options.body).counting, 'socket_power');
  assert.equal(JSON.parse(puts[3].options.body).label, 'Neuer Name');
});

test('dismissRoleNeeded clears the entry through PATCH', async () => {
  const calls = [];
  const { component } = createEnergyPanel({ fetchImpl: chainFetch({ calls }) });
  await component.load();
  component.roleNeeded = ['socket_power', 'pv_power'];
  await component.dismissRoleNeeded('socket_power');
  const patch = calls.find(call => call.options.method === 'PATCH');
  assert.equal(patch.url, '/api/v1/energy/roles');
  assert.deepEqual(JSON.parse(patch.options.body), { role_needed: { socket_power: false } });
  assert.deepEqual(component.roleNeeded, ['pv_power']);
});

test('chainHintOf names the chain of a point, isChainReference marks the non counting ones, openChain opens the section', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  assert.deepEqual(plainJSON(component.chainHintOf('socket_power')), { id: 'm', label: 'Balkon', counting: 'pv_power' });
  assert.equal(component.chainHintOf('extra_power'), null);
  assert.equal(component.isChainReference('socket_power'), true);
  assert.equal(component.isChainReference('pv_power'), false);
  component.$nextTick = fn => fn();
  component.openChain('m');
  assert.equal(component.openSection, 'chains');
  assert.equal(component.flashChain, 'm');
});

test('a chain direction change saves with PUT, one picked point gets a message', async () => {
  const calls = [];
  const { component, stores, window } = createEnergyPanel({ fetchImpl: chainFetch({ calls, saved: { assignments: {}, chains: {}, role_needed: [] } }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  await component.setChainDirection('m', 'fixed_up');
  const put = chainWriteCalls(calls)[0];
  assert.equal(put.options.method, 'PUT');
  assert.equal(JSON.parse(put.options.body).direction, 'fixed_up');

  component.openPicker = async () => ['extra_power'];
  await component.addChain();
  assert.equal(stores.toasts.last(), window.I18n.t('energy.chains.pick_two'));
});

test('chain path, point value and loss read the labels and the live state', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = {
    m: {
      points: [
        { entity_id: 'pv_power', has_value: true, value: 1500 },
        { entity_id: 'socket_power', has_value: false, value: 0 },
      ],
      segments: [{ from: 'socket_power', to: 'pv_power', available: true, loss_w: 9, efficiency: 0.97 }],
    },
  };
  assert.equal(component.chainPathOf(component.chains.m), 'Wechselrichter / PV Leistung → Wechselrichter / Steckdose');
  assert.equal(component.chainLabelOf('pv_power'), 'Wechselrichter / PV Leistung');
  assert.equal(component.chainPointValue('m', 'pv_power'), '1,5 kW');
  assert.equal(component.chainPointValue('m', 'socket_power'), '–');
  assert.equal(component.chainLossText('m', 0), '−9 W · 97 %', 'matched in either flow direction');
  assert.equal(component.chainLossText('m', 1), '–');
});

test('applyChainResponse leaves the role rows alone when the answer has no assignments', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  await component.load();
  const chain = { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' };
  component.applyChainResponse({ chains: { m: chain }, role_needed: [] }, ['pv_power']);
  assert.equal(component.assignments.pv_power.role, 'pv');
  assert.deepEqual(plainJSON(component.chains), { m: chain });
});

test('a refused rename puts the saved label back into the name input', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch({ chainOk: false }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  window.document.body.innerHTML = '<div data-chain-id="m"><input class="energy-groups-name" value="Balkon"></div>';
  const input = window.document.querySelector('.energy-groups-name');
  input.value = 'Neuer Name';
  await component.renameChain('m', 'Neuer Name');
  assert.equal(input.value, 'Balkon');
  assert.equal(component.chains.m.label, 'Balkon');
});

test('a refused direction change takes the choice back', async () => {
  const fetchImpl = async url => {
    if (String(url).startsWith('/api/v1/energy/chains')) return { ok: false, status: 409, json: async () => ({ error: 'nein' }) };
    return chainFetch({ calls: [], saved: { assignments: {}, chains: {}, role_needed: [] } })(url);
  };
  const { component } = createEnergyPanel({ fetchImpl });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power', direction: 'auto' } };
  await component.setChainDirection('m', 'fixed_up');
  assert.equal(component.chains.m.direction, 'auto');
});

test('the chain value helpers give x-roll the raw numbers and dashes keep them empty', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch({ calls: [], saved: { assignments: {}, chains: {}, role_needed: [] } }) });
  await component.load();
  component.chains = { m: { label: 'B', points: ['a', 'b'], counting: 'a' } };
  component.chainStates = { m: { points: [{ entity_id: 'a', has_value: true, value: 900 }, { entity_id: 'b', has_value: false }], segments: [{ from: 'a', to: 'b', available: true, loss_w: 9, efficiency: 0.97 }] } };
  assert.equal(component.chainPointRaw('m', 'a'), 900);
  assert.equal(component.chainPointRaw('m', 'b'), '');
  assert.equal(component.chainLossRaw('m', 0), 9);
  assert.equal(component.chainLossPowerText('m', 0), '−9 W');
  assert.equal(component.chainEfficiencyText('m', 0), '97 %');
  assert.equal(Math.round(component.chainEfficiencyRaw('m', 0)), 97);
  component.chainStates = {};
  assert.equal(component.chainLossRaw('m', 0), '');
  assert.equal(component.chainEfficiencyText('m', 0), '');
});

test('a chain request from the map opens the chain once the page has loaded', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch({ calls: [], saved: { assignments: {}, chains: {}, role_needed: [] } }) });
  component.$nextTick = fn => fn();
  window.__energyChainRequest__ = 'm';
  await component.load();
  assert.equal(component.flashChain, 'm');
  assert.equal(component.isSectionOpen('chains'), true);
  assert.equal(window.__energyChainRequest__, null);
});

const findingSince = '2026-11-06T10:00:00Z';
const driftFinding = {
  kind: 'drift', chain_id: 'm', chain_label: 'Balkon', since: findingSince, from: 'pv_power', to: 'socket_power',
  suggestion: { entity_id: 'socket_power', invert: true },
};

test('a chain finding is listed in the notices and goes away with the next live state', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  assert.equal(component.findingNotices.length, 1);
  assert.ok(component.findingNotices[0].items[0].text.startsWith('Balkon: Der Verlust'), component.findingNotices[0].text);
  assert.equal(component.findingNotices[0].items[0].canApply, true);
  assert.equal(component.hasEfficiencyTab, false, 'no efficiency tab in this page');

  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [] } };
  assert.deepEqual(plainJSON(component.findingNotices), []);
});

test('applying a suggestion only fills the draft, focuses the row and saves nothing', async () => {
  const calls = [];
  const { component, stores, window } = createEnergyPanel({ fetchImpl: chainFetch({ calls }) });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  const before = calls.length;
  component.applyFinding(component.findingNotices[0].items[0]);
  assert.equal(component.assignments.socket_power.invert, true, 'the invert flips');
  assert.equal(component.flash.socket_power, true, 'the row is focused');
  assert.equal(stores.toasts.last(), window.I18n.t('energy.chains.draft_applied'));
  assert.deepEqual(chainWriteCalls(calls.slice(before)), [], 'nothing is written');
  assert.equal(calls.slice(before).filter(call => call.options.method && call.options.method !== 'GET').length, 0);
});

test('applying a scale suggestion multiplies the scale and only with edit rights', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  const scaleFinding = { kind: 'unit', chain_id: 'm', chain_label: 'Balkon', since: findingSince, from: 'pv_power', to: 'socket_power', suggestion: { entity_id: 'pv_power', scale_factor: 0.001 } };
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [scaleFinding] } };
  component.assignments.pv_power.scale = 1000;
  component.applyFinding({ finding: scaleFinding, canApply: true });
  assert.equal(component.assignments.pv_power.scale, 1);

  component.canEdit = false;
  component.applyFinding({ finding: scaleFinding, canApply: false });
  assert.equal(component.assignments.pv_power.scale, 1, 'without edit rights nothing changes');
});

test('an energy draft from the map is applied once the page has loaded, not saved', async () => {
  const calls = [];
  const { component, window, stores } = createEnergyPanel({ fetchImpl: chainFetch({ calls }) });
  component.$nextTick = fn => fn();
  window.__energyDraftRequest__ = { entity_id: 'socket_power', invert: true };
  await component.load();
  assert.equal(component.assignments.socket_power.invert, true);
  assert.equal(window.__energyDraftRequest__, null);
  assert.equal(stores.toasts.last(), window.I18n.t('energy.chains.draft_applied'));
  assert.deepEqual(chainWriteCalls(calls), []);
});

test('an energy draft event applies to the loaded rows and ignores a draft for an unknown entity', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  component.init();
  await component.load();
  window.dispatchEvent(new window.CustomEvent('energy-apply-draft', { detail: { entity_id: 'socket_power', invert: true } }));
  assert.equal(component.assignments.socket_power.invert, true);
  window.dispatchEvent(new window.CustomEvent('energy-apply-draft', { detail: { entity_id: 'missing_power', invert: true } }));
  assert.equal(component.assignments.missing_power, undefined);
});

test('the efficiency link exists only when the efficiency tab is on the page and opens it', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch() });
  await component.load();
  assert.equal(component.hasEfficiencyTab, false);
  window.document.body.innerHTML = '<button role="tab" aria-controls="efficiency-panel"></button>';
  assert.equal(component.hasEfficiencyTab, true);
  const seen = [];
  window.addEventListener('dashboard-open-panel', event => seen.push(event.detail));
  component.openEfficiency();
  assert.deepEqual(plainJSON(seen), [{ panel: 'efficiency-panel' }]);
});

test('a finding tile title is the chain label and the short kind title', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  assert.equal(component.findingNotices[0].title, `Balkon · ${window.I18n.t('diagnostics.rule.chain_drift.title')}`);
});

test('finding details are closed by default and the toggle opens and closes them', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  const key = component.findingNotices[0].key;
  assert.equal(component.isFindingOpen(key), false, 'closed by default');
  component.toggleFinding(key);
  assert.equal(component.isFindingOpen(key), true, 'the toggle opens it');
  component.toggleFinding(key);
  assert.equal(component.isFindingOpen(key), false, 'the toggle closes it again');
});

test('an open finding tile stays open after a live refresh with the same key', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  const key = component.findingNotices[0].key;
  component.toggleFinding(key);
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [{ ...driftFinding }] } };
  assert.equal(component.findingNotices[0].key, key);
  assert.equal(component.isFindingOpen(key), true, 'the refresh keeps the open state');
});

test('the apply button of a finding tile needs edit rights and a suggestion', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] } };
  assert.equal(component.findingNotices[0].items[0].canApply, true);
  component.canEdit = false;
  assert.equal(component.findingNotices[0].items[0].canApply, false, 'without edit rights no apply button');
});

test('several findings of one chain share one tile whose details list each finding with its own apply button', async () => {
  const { component, window } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  const sign = { kind: 'sign', chain_id: 'm', chain_label: 'Balkon', since: findingSince, from: 'pv_power', to: 'socket_power', suggestion: null };
  component.chains = { m: { label: 'Balkon', points: ['pv_power', 'socket_power'], counting: 'pv_power' } };
  component.chainStates = { m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding, sign] } };
  assert.equal(component.findingNotices.length, 1, 'one tile for the chain');
  const [group] = component.findingNotices;
  assert.equal(group.title, window.I18n.t('chains.finding.group_title', { chain: 'Balkon', n: 2 }));
  assert.equal(group.items.length, 2);
  assert.deepEqual(plainJSON(group.items.map(item => item.finding.kind)), ['sign', 'drift'], 'the server order of the kinds');
  assert.equal(group.items[0].canApply, false, 'no suggestion, no apply button');
  assert.equal(group.items[1].canApply, true, 'the drift suggestion has its own apply button');
  component.toggleFinding(group.key);
  assert.equal(component.isFindingOpen(group.key), true);
});

test('findings of two chains give two tiles', async () => {
  const { component } = createEnergyPanel({ fetchImpl: chainFetch() });
  component.$nextTick = fn => fn();
  await component.load();
  component.chainStates = {
    m: { id: 'm', label: 'Balkon', points: [], segments: [], findings: [driftFinding] },
    n: { id: 'n', label: 'Dach', points: [], segments: [], findings: [{ ...driftFinding, chain_id: 'n', chain_label: 'Dach' }] },
  };
  assert.deepEqual(plainJSON(component.findingNotices.map(group => group.key)), ['m', 'n']);
});

test('the energy template renders one tile per chain with a title toggle and the efficiency link outside it', () => {
  const html = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'templates', 'energy.html'), 'utf8');
  const start = html.indexOf('class="energy-chain-findings"');
  const end = html.indexOf('</article>', start);
  const tile = html.slice(start, end);
  assert.ok(tile.includes('<template x-for="item in findingNotices"'), 'loops over the groups');
  assert.ok(/<button type="button" class="energy-chain-finding-toggle"[^>]*aria-controls="/.test(tile), 'the toggle carries aria-controls');
  assert.ok(/aria-expanded="/.test(tile), 'the toggle carries aria-expanded');
  const toggleStart = tile.indexOf('energy-chain-finding-toggle');
  const toggleEnd = tile.indexOf('</button>', toggleStart);
  const toggle = tile.slice(toggleStart, toggleEnd);
  assert.ok(toggle.includes('x-text="item.title"'), 'the title sits inside the toggle');
  assert.ok(!toggle.includes('openEfficiency'), 'the efficiency link is not part of the toggle');
  assert.ok(tile.includes('x-on:click="openEfficiency()"'), 'the link still opens the efficiency tab');
  assert.ok(tile.includes('x-for="detail in item.items"'), 'the details loop over the findings');
});
