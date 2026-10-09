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

function createEnergyPanel({ fetchImpl, basePath } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  if (basePath !== undefined) dom.window.__DASHBOARD_BASE_PATH__ = basePath;
  installI18n(dom.window);
  vm.runInContext(pickerSource, context);
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

test('loadModeNote explains the calculation for calculated and for auto without a load role', async () => {
  const { component } = createEnergyPanel({ fetchImpl: stubFetch() });
  await component.load();
  component.interpretation.load_mode = 'calculated';
  assert.match(component.loadModeNote, /PV \+ Netzbezug/);
  assert.match(component.loadModeNote, /Rest, keine Messung/);
  component.interpretation.load_mode = 'auto';
  assert.match(component.loadModeNote, /berechnet/); // no "load" role assigned in the fixture
  assert.match(component.loadModeNote, /Rest, keine Messung/);
  component.assignments.pv_power.role = 'load';
  assert.match(component.loadModeNote, /Gemessen/);
  assert.doesNotMatch(component.loadModeNote, /Rest, keine Messung/);
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

test('der Kombiniert-Modus warnt, wenn keine Entitaet die Rolle "Hausverbrauch" traegt', () => {
  const { component } = createEnergyPanel();
  component.assignments = {a: {role: 'pv'}};
  component.interpretation = {load_mode: 'combined'};
  assert.equal(component.showCombinedWithoutLoadWarning, true);
  component.assignments = {a: {role: 'pv'}, b: {role: 'load'}};
  assert.equal(component.showCombinedWithoutLoadWarning, false);
});

test('der Kombiniert-Modus erklaert die geaenderte Bedeutung der Rolle "Hausverbrauch"', () => {
  const { component } = createEnergyPanel();
  component.assignments = {b: {role: 'load'}};
  component.interpretation = {load_mode: 'combined'};
  assert.match(component.loadModeNote, /Teilverbraucher/);
  assert.match(component.loadModeNote, /doppelt gezählt/);
  component.interpretation = {load_mode: 'auto'};
  assert.doesNotMatch(component.loadModeNote, /Teilverbraucher/);
});

test('"Übriger Verbrauch ist ein Rest"-Hinweis gilt auch im Kombiniert-Modus', () => {
  const { component } = createEnergyPanel();
  component.assignments = {b: {role: 'load'}};
  component.interpretation = {load_mode: 'combined'};
  assert.match(component.loadModeNote, /„Übriger Verbrauch“ bleibt ein Rest/);
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
