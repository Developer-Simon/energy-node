// Reine Kandidaten-Listen fuer die Automationen-Editoren: Rollen-Gruppen,
// Stellglieder, Messwerte, Schalter und die Chip-Auswahl. Das Modul hat kein
// eigenes DOM, daher laedt der Test es in einen jsdom-Kontext mit echtem i18n.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', 'internal', 'webui', 'static', 'js');

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automations-devices.js'), 'utf8'), context);
  return dom.window.__automationDevices;
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich
// einmal durch JSON laufen lassen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const inverter = {
  id: 'inverter', name: 'Wechselrichter',
  entities: [{
    unique_id: 'inv_limit', name: 'Leistungsgrenze', command_topic: 'werkstatt/inv/limit',
    min_value: 0, max_value: 800, step: 1, unit_of_measurement: 'W', has_value: true, value: 500,
  }],
};
const wallbox = {
  id: 'wallbox', name: 'Wallbox',
  entities: [{
    unique_id: 'wb_current', name: 'Ladestrom', command_topic: 'werkstatt/wb/current',
    min_value: 6, max_value: 16, step: 1, unit_of_measurement: 'A', has_value: true, value: 10,
  }],
};
const assignments = {
  inv_limit: { role: 'pv' },
  wb_current: { role: 'wallbox' },
};

test('deviceRoleGroup prefers pv over other roles on the same device', () => {
  const devices = load();
  const device = {
    id: 'mixed', name: 'Gemischt',
    entities: [{ unique_id: 'a' }, { unique_id: 'b' }, { unique_id: 'c' }],
  };
  const roles = {
    a: { role: 'grid' },
    b: { role: 'wallbox' },
    c: { role: 'pv' },
  };
  assert.equal(devices.deviceRoleGroup(device, roles), 'inverter');
  assert.equal(devices.deviceRoleGroup({ id: 'x', entities: [{ unique_id: 'z' }] }, roles), 'other');
  assert.equal(devices.deviceRoleGroup({ id: 'y', entities: [{ unique_id: 'b' }] }, roles), 'wallbox');
});

test('actuator candidates need min and max and mark consumers unfit for producer', () => {
  const devices = load();
  const noBounds = { id: 'nb', name: 'Ohne Grenzen', entities: [{ unique_id: 'nb_x', name: 'X', command_topic: 't' }] };
  const candidates = plain(devices.actuatorCandidates([inverter, wallbox, noBounds], assignments, 'producer'));
  assert.deepEqual(candidates.map((c) => c.id), ['inv_limit', 'wb_current']);
  const [pv, wb] = candidates;
  assert.equal(pv.fits, true);
  assert.equal(pv.group, 'inverter');
  assert.equal(pv.subtitle, 'Leistungsgrenze · 0 bis 800 W');
  assert.equal(wb.fits, false);
  assert.equal(wb.group, 'wallbox');
});

test('meter candidates show the live value with sign', () => {
  const devices = load();
  const grid = {
    id: 'meter', name: 'Netzzähler',
    entities: [{
      unique_id: 'grid_power', name: 'Netzleistung', state_topic: 'werkstatt/grid/state',
      template_supported: true, value: 74, unit_of_measurement: 'W',
    }],
  };
  const candidates = plain(devices.meterCandidates([inverter, grid], { grid_power: { role: 'grid' } }));
  assert.equal(candidates.length, 1);
  assert.equal(candidates[0].group, 'grid');
  assert.equal(candidates[0].fits, true);
  assert.equal(candidates[0].valueText, 'jetzt +74 W');
});

test('meter candidates keep an entity without a value so a saved meter stays selectable', () => {
  const devices = load();
  const meter = {
    id: 'meter2', name: 'Zweitzähler',
    entities: [{ unique_id: 'meter2_power', name: 'Leistung', state_topic: 'werkstatt/m2/state', template_supported: true }],
  };
  const candidates = plain(devices.meterCandidates([meter], {}));
  assert.equal(candidates.length, 1);
  assert.equal(candidates[0].id, 'meter2_power');
  assert.equal(candidates[0].valueText, '');
});

test('chipList puts the selection first and counts all', () => {
  const devices = load();
  const candidates = ['a', 'b', 'c', 'd', 'e'].map((id) => ({ id, fits: true }));
  const result = plain(devices.chipList(candidates, 'd', 2));
  assert.equal(result.chips[0].id, 'd');
  assert.equal(result.chips.length, 2);
  assert.equal(result.more, 5);
  assert.equal(result.missing, false);
  // Ohne Grenze liefert die Liste alle, die Zeile kuerzt per CSS.
  assert.deepEqual(plain(devices.chipList(candidates, 'd')).chips.map((c) => c.id), ['d', 'a', 'b', 'c', 'e']);
});

test('live values also come from numeric strings, short texts stay as they are', () => {
  const devices = load();
  const meter = { id: 'm', name: 'Netz', entities: [
    { unique_id: 'p', name: 'Leistung', state_topic: 'm/p', template_supported: true, unit_of_measurement: 'W', value: '-1432' },
    { unique_id: 's', name: 'Ausgang', state_topic: 'm/s', template_supported: true, value: 'ON' },
  ] };
  const [power, state] = plain(devices.meterCandidates([meter], {}));
  assert.equal(power.valueText, 'jetzt -1432 W');
  assert.equal(state.valueText, 'jetzt ON');
  const limit = { ...inverter, entities: [{ ...inverter.entities[0], value: '800' }] };
  assert.equal(plain(devices.actuatorCandidates([limit], assignments, 'producer'))[0].valueText, 'jetzt +800 W');
});

test('entityChip describes any entity with role, name and value', () => {
  const devices = load();
  const chip = plain(devices.entityChip([wallbox], assignments, 'wb_current'));
  assert.equal(chip.deviceName, 'Wallbox');
  assert.equal(chip.subtitle, 'Ladestrom');
  assert.equal(chip.tone, 'wallbox');
  assert.equal(chip.valueText, 'jetzt 10 A');
  assert.equal(devices.entityChip([wallbox], assignments, 'gone'), null);
});

test('balance fields get role icons and custom categories the layers icon', () => {
  const devices = load();
  const fields = plain(devices.balanceFieldCandidates([['grid_export', 'Netzeinspeisung'], ['pv', 'PV'], ['battery_soc', 'SoC'], ['custom:x', 'Eigene']]));
  assert.deepEqual(fields.map((f) => [f.id, f.icon, f.tone]), [
    ['grid_export', 'ico-grid', 'grid'], ['pv', 'ico-sun', 'pv'], ['battery_soc', 'ico-battery', ''], ['custom:x', 'ico-layers', ''],
  ]);
});

test('picker items carry no member badge', () => {
  const devices = load();
  assert.equal(plain(devices.pickerItems([{ id: 'a', deviceName: 'A', group: 'other', fits: true }]))[0].memberOf, '');
});

test('chipList flags a saved id that no longer exists', () => {
  const devices = load();
  const candidates = [{ id: 'a', fits: true }, { id: 'b', fits: true }];
  const result = plain(devices.chipList(candidates, 'gone'));
  assert.equal(result.missing, true);
  assert.deepEqual(result.chips.map((c) => c.id), ['a', 'b']);
});

test('no candidates gives an empty chip list', () => {
  const devices = load();
  assert.deepEqual(plain(devices.chipList([], null)), { chips: [], more: 0, missing: false });
  assert.deepEqual(plain(devices.chipList([], 'gone')), { chips: [], more: 0, missing: true });
});

test('pickerItems maps candidates to the picker item format', () => {
  const devices = load();
  const items = plain(devices.pickerItems(devices.actuatorCandidates([inverter, wallbox], assignments, 'producer')));
  assert.equal(items[0].id, 'inv_limit');
  assert.equal(items[0].name, 'Wechselrichter');
  assert.equal(items[0].section, 'Wechselrichter');
  assert.equal(items[0].filter, 'inverter');
  assert.equal(items[0].iconRef, 'ico-inverter');
  assert.equal(items[0].tone, 'pv');
  assert.equal(items[0].dim, false);
  assert.equal(items[1].dim, true);
});
