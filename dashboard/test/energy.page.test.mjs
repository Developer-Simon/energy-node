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

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'energy.page.js'),
  'utf8',
);

function createEnergyPanel({ fetchImpl, basePath } = {}) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  if (basePath !== undefined) dom.window.__DASHBOARD_BASE_PATH__ = basePath;
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
  assert.match(component.loadModeNote, /„Übriger Verbrauch" bleibt ein Rest/);
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
