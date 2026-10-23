// Geraetekontext des Automations-Assistenten: Rollen je Geraet, Stellglieder,
// Messwerte, Schalter, Verfuegbarkeit und Netzbilanz. Das Modul hat kein eigenes
// DOM, daher laedt der Test es in einen jsdom-Kontext mit echtem i18n.
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
  const i18n = installI18n(dom.window);
  const context = dom.getInternalVMContext();
  vm.runInContext(fs.readFileSync(path.join(jsDir, 'automation-wizard.js'), 'utf8'), context);
  return { wizard: dom.window.__automationWizard, t: (key, params) => i18n.t(key, params) };
}

// Objekte aus dem VM-Kontext haben andere Prototypen, daher vor dem Vergleich
// einmal durch JSON laufen lassen.
const plain = (value) => JSON.parse(JSON.stringify(value));

const devices = [
  {
    id: 'pv1', name: 'Wechselrichter Dach',
    entities: [
      { unique_id: 'pv1_limit', name: 'Leistungsgrenze', command_topic: 'werkstatt/pv1/limit', min_value: 50, max_value: 800, step: 10, unit_of_measurement: 'W', has_value: true, value: '500' },
      { unique_id: 'pv1_power', name: 'Leistung', state_topic: 'werkstatt/pv1/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '742' },
    ],
  },
  {
    id: 'pv2', name: 'Wechselrichter Garage',
    entities: [
      { unique_id: 'pv2_power', name: 'Leistung', state_topic: 'werkstatt/pv2/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '120' },
    ],
  },
  {
    id: 'wb', name: 'Wallbox',
    entities: [
      { unique_id: 'wb_limit', name: 'Ladeleistung', command_topic: 'werkstatt/wb/limit', min_value: 1400, max_value: 11000, step: 100, unit_of_measurement: 'W', has_value: true, value: '5000' },
      { unique_id: 'wb_switch', name: 'Laden', command_topic: 'werkstatt/wb/switch', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'ON' },
      { unique_id: 'wb_plug', name: 'Stecker', state_topic: 'werkstatt/wb/plug', template_supported: true, device_class: 'plug', has_value: true, value: 'ON' },
      { unique_id: 'wb_power', name: 'Ladeleistung Ist', state_topic: 'werkstatt/wb/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '1400' },
    ],
  },
  {
    id: 'plugs', name: 'Steckdosen',
    entities: [
      { unique_id: 'sw_a', name: 'Steckdose A', command_topic: 'werkstatt/plugs/a', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'OFF' },
      { unique_id: 'sw_b', name: 'Steckdose B', command_topic: 'werkstatt/plugs/b', payload_on: 'ON', payload_off: 'OFF', has_value: true, value: 'ON' },
      { unique_id: 'sw_c', name: 'Nur Ein', command_topic: 'werkstatt/plugs/c', payload_on: 'ON', has_value: true, value: 'ON' },
    ],
  },
];

const snapshot = {
  values: { pv: 862, wallbox: 1400, battery_soc: 0.64 },
  entities: [
    { device_id: 'pv1', entity_id: 'pv1_power', role: { role: 'pv' } },
    { device_id: 'pv2', entity_id: 'pv2_power', role: { role: 'pv' } },
    { device_id: 'wb', entity_id: 'wb_power', role: { role: 'wallbox' } },
  ],
};

const document = {
  rules: [{ id: 'rule-1', name: 'Nachtladung' }],
  scenarios: [
    { id: 'scen-1', name: 'Regler Dach', while: { regulator: { output: { topic: 'werkstatt/pv1/limit' } } } },
  ],
  settings: { latitude: 52.5, longitude: 13.4 },
};

const build = (wizard, overrides = {}) => wizard.buildContext({ devices, snapshot, document, ...overrides });

test('inverters list every pv device and disable the one without a limit', () => {
  const { wizard, t } = load();
  const ctx = plain(build(wizard));
  assert.deepEqual(ctx.inverters.map((item) => item.deviceName), ['Wechselrichter Dach', 'Wechselrichter Garage']);
  assert.equal(ctx.inverters[0].id, 'pv1_limit');
  assert.equal(ctx.inverters[0].disabled, t('automations.assistant.device.already_regulated', { name: 'Regler Dach' }));
  assert.equal(ctx.inverters[1].disabled, t('automations.assistant.device.no_limit'));
  assert.ok(ctx.inverters[1].disabled.length > 0);
});

test('an inverter remembers its measured pv entity', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard, { document: { rules: [], scenarios: [], settings: {} } }));
  const [dach, garage] = ctx.inverters;
  assert.equal(dach.measured.unique_id, 'pv1_power');
  assert.equal(dach.live, '742 W');
  assert.equal(garage.measured.unique_id, 'pv2_power');
  assert.equal(garage.live, '120 W');
  assert.equal(garage.disabled, load().t('automations.assistant.device.no_limit'));
});

test('an already regulated output is disabled with the scenario name', () => {
  const { wizard, t } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.inverters[0].disabled, t('automations.assistant.device.already_regulated', { name: 'Regler Dach' }));
  assert.ok(ctx.inverters[0].disabled.includes('Regler Dach'));
  assert.deepEqual(ctx.regulatedTopics, { 'werkstatt/pv1/limit': 'Regler Dach' });
});

test('wallbox entries carry the charging switch and the plug sensor', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.wallboxes.length, 1);
  const [box] = ctx.wallboxes;
  assert.equal(box.deviceName, 'Wallbox');
  assert.equal(box.id, 'wb_limit');
  assert.equal(box.switchEntity.unique_id, 'wb_switch');
  assert.equal(box.plugEntity.unique_id, 'wb_plug');
  assert.equal(box.measured.unique_id, 'wb_power');
  assert.equal(box.live, '1400 W');
  assert.equal(box.disabled, '');
});

test('switches need payload_on and payload_off', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.deepEqual(ctx.switches.map((item) => item.id), ['wb_switch', 'sw_a', 'sw_b']);
  assert.equal(ctx.switches[1].deviceName, 'Steckdosen');
  assert.equal(ctx.switches[1].live, 'OFF');
});

test('socAvailable follows the snapshot values', () => {
  const { wizard } = load();
  assert.equal(build(wizard).socAvailable, true);
  const without = build(wizard, { snapshot: { values: { pv: 10 }, entities: [] } });
  assert.equal(without.socAvailable, false);
});

test('hasLocation needs both coordinates and usedIds lists rules and scenarios', () => {
  const { wizard } = load();
  const ctx = plain(build(wizard));
  assert.equal(ctx.hasLocation, true);
  assert.deepEqual(ctx.usedIds, ['rule-1', 'scen-1']);
  const noCoords = plain(build(wizard, { document: { rules: [], scenarios: [], settings: { latitude: 52.5 } } }));
  assert.equal(noCoords.hasLocation, false);
});

test('gridNet matches the energy model for split and signed grid roles', () => {
  const { wizard } = load();
  assert.equal(wizard.gridNet({ values: { grid_import: 300, grid_export: 0 } }), 300);
  assert.equal(wizard.gridNet({ values: { grid: -470 } }), -470);
  assert.equal(wizard.gridNet({ values: { pv: 800 } }), null);
});

test('devicesFor returns the list for each filter', () => {
  const { wizard } = load();
  const ctx = build(wizard);
  assert.equal(wizard.devicesFor('inverter', ctx), ctx.inverters);
  assert.equal(wizard.devicesFor('wallbox', ctx), ctx.wallboxes);
  assert.equal(wizard.devicesFor('switch', ctx), ctx.switches);
});
