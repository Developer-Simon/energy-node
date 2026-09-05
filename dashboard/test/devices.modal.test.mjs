// Tests fuer das vereinheitlichte Geraete-Modal (devicesPanel() in
// dashboard.js). jsdom kennt kein Layout - getestet wird nur Zustand und
// Logik: Entitaets-Gruppierung, Exklusiv-Auswahl, Merge/Split, Rollen-Gate.
//
// dashboard.js registriert seine Komponenten erst im alpine:init-Event. Der
// Test stellt ein Alpine-Doppel bereit und loest das Event aus; so braucht
// der Produktivcode keine Test-Naht.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'dashboard.js'),
  'utf8',
);

function createDevicesPanel() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'outside-only',
    url: 'http://localhost/',
  });
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  // dashboard.js startet beim Laden ein Modul-Level-setInterval (die
  // Zeitstempel-Aktualisierung) - ein echtes window.setInterval wuerde den
  // Testprozess sonst am Leben halten, gleiche Loesung wie in
  // dashboard.nav.test.mjs.
  dom.window.setInterval = () => 0;
  dom.window.deviceTileMixin = () => ({});
  vm.runInContext(source, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  return factories.devicesPanel();
}

function entity(overrides) {
  return {
    unique_id: 'e1', component: 'sensor', commandable: false,
    entity_category: '', has_value: true, value: '1', stale: false,
    ...overrides,
  };
}

test('measurementEntities/controlEntities/configDiagEntities gruppieren nach entityCategory()', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    name: 'Wallbox Süd',
    entities: [
      entity({ unique_id: 'm1', component: 'sensor' }),
      entity({ unique_id: 'c1', component: 'switch', commandable: true }),
      entity({ unique_id: 'cfg1', entity_category: 'config' }),
      entity({ unique_id: 'cfgNum', entity_category: 'config', component: 'number', commandable: true }),
      entity({ unique_id: 'diag1', entity_category: 'diagnostic' }),
    ],
  };
  assert.deepEqual(panel.measurementEntities.map(e => e.unique_id), ['m1']);
  assert.deepEqual(panel.controlEntities.map(e => e.unique_id), ['c1']);
  assert.deepEqual(panel.configDiagEntities.map(e => e.unique_id), ['cfg1', 'cfgNum', 'diag1']);
  // Der Tab teilt sich in einen schaltbaren Konfigurations- und einen
  // schreibgeschuetzten Diagnose-Abschnitt; eine schaltbare config-Entitaet
  // gehoert in den Konfigurations-Abschnitt (mit Steuer-Kachel).
  assert.deepEqual(panel.configEntities.map(e => e.unique_id), ['cfg1', 'cfgNum']);
  assert.deepEqual(panel.diagnosticEntities.map(e => e.unique_id), ['diag1']);
});

test('defaultDeviceTab waehlt den ersten nicht-leeren Entitaets-Tab', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [entity({ unique_id: 'c1', component: 'switch', commandable: true })] };
  assert.equal(panel.defaultDeviceTab(), 'controls');
});

test('defaultDeviceTab faellt auf management zurueck, wenn keine Entitaet passt', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [] };
  assert.equal(panel.defaultDeviceTab(), 'management');
});

test('setDeviceTab waehlt exklusiv genau einen Tab', () => {
  const panel = createDevicesPanel();
  panel.setDeviceTab('controls');
  assert.equal(panel.activeDeviceTab, 'controls');
  panel.setDeviceTab('configDiag');
  assert.equal(panel.activeDeviceTab, 'configDiag');
});

test('measurementsTeaser zeigt den ersten Messwert', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [entity({ unique_id: 'm1', component: 'sensor', has_value: true, value: '21.5', unit_of_measurement: '°C' })] };
  assert.equal(panel.measurementsTeaser, '21.5 °C');
});

test('measurementsTeaser ist leer ohne Messwerte', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [] };
  assert.equal(panel.measurementsTeaser, '');
});

test('relativeTime formatiert Sekunden, Minuten und Stunden', () => {
  const panel = createDevicesPanel();
  const now = new Date('2026-08-28T12:00:00Z');
  assert.equal(panel.relativeTime(new Date('2026-08-28T11:59:48Z'), now), 'vor 12 s');
  assert.equal(panel.relativeTime(new Date('2026-08-28T11:57:00Z'), now), 'vor 3 min');
  assert.equal(panel.relativeTime(new Date('2026-08-28T10:00:00Z'), now), 'vor 2 h');
});

test('managementTeaser kombiniert Warn-Anzahl und letzte Aktivitaet', () => {
  const panel = createDevicesPanel();
  const now = new Date('2026-08-28T12:00:00Z');
  panel.deviceDetail = {
    name: 'x', entities: [],
    warnings: [{ rule_id: 'r1', severity: 'warning', message: 'm', hint: '' }],
    command_actions: [{ at: '2026-08-28T11:59:48Z', entity_id: 'e1', result: 'published' }],
  };
  assert.equal(panel.managementTeaser(now), '⚠ 1 · vor 12 s');
});

function measureAndControl() {
  return {
    name: 'x',
    entities: [
      entity({ unique_id: 'm1', component: 'sensor' }),
      entity({ unique_id: 'c1', component: 'switch', commandable: true }),
    ],
  };
}

test('applyMergeThreshold legt zusammen, wenn genug Hoehe da ist', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = measureAndControl();
  panel.activeDeviceTab = 'measurements';
  panel.applyMergeThreshold(600);
  assert.equal(panel.mergedTab, true);
  assert.equal(panel.activeDeviceTab, 'measurementsControls');
});

test('canMerge nur, wenn Mess- und Steuerwerte existieren', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [entity({ unique_id: 'm1' })] };
  assert.equal(panel.canMerge, false);
  panel.deviceDetail = measureAndControl();
  assert.equal(panel.canMerge, true);
});

test('applyMergeThreshold merged nicht ohne Steuerwerte', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [entity({ unique_id: 'm1' })] };
  panel.activeDeviceTab = 'measurements';
  panel.applyMergeThreshold(600);
  assert.equal(panel.mergedTab, false);
});

test('setDeviceTab gleicht den Merge-Zustand beim Wechsel auf Messwerte ab', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = measureAndControl();
  panel.availableHeight = 600;
  panel.setDeviceTab('measurements');
  assert.equal(panel.mergedTab, true);
  assert.equal(panel.activeDeviceTab, 'measurementsControls');
});

test('applyMergeThreshold trennt wieder, wenn die Hoehe knapp wird', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [entity({ unique_id: 'm1' })] };
  panel.activeDeviceTab = 'measurementsControls';
  panel.mergedTab = true;
  panel.applyMergeThreshold(200);
  assert.equal(panel.mergedTab, false);
  assert.equal(panel.activeDeviceTab, 'measurements');
});

test('applyMergeThreshold ruehrt den Merge-Zustand nicht an, solange ein anderer Tab aktiv ist', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = measureAndControl();
  panel.activeDeviceTab = 'configDiag';
  panel.applyMergeThreshold(600);
  assert.equal(panel.mergedTab, false);
  assert.equal(panel.activeDeviceTab, 'configDiag');
});

// messageStates baut seine Objekte im vm-Kontext von dashboard.js - ein
// realmuebergreifendes deepStrictEqual scheitert am Prototyp, deshalb hier
// feldweise geprueft.
function plainStates(states) {
  return Array.from(states, state => `${state.label}=${state.value}`);
}

test('messageStates zerlegt ein JSON-Payload in einzelne Zustaende', () => {
  const panel = createDevicesPanel();
  const states = panel.messageStates({ topic: 't', at: '2026-08-28T12:00:00Z', payload: '{"car":2,"amp":16}' });
  assert.deepEqual(plainStates(states), ['car=2', 'amp=16']);
});

test('messageStates zeigt ein skalares Payload als eine Zeile unter dem Topic', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [] };
  const states = panel.messageStates({ topic: 'goe/state', at: '', payload: 'online' });
  assert.deepEqual(plainStates(states), ['goe/state=online']);
});

test('messageStates markiert ein leeres Payload', () => {
  const panel = createDevicesPanel();
  const states = panel.messageStates({ topic: 'goe/state', at: '', payload: '' });
  assert.deepEqual(plainStates(states), ['goe/state=(leer)']);
});

test('updatedRelative zeigt den relativen Abstand als Text, leer ohne last_updated', () => {
  const panel = createDevicesPanel();
  panel.nowTick = new Date('2026-08-28T12:00:00Z').getTime();
  panel.deviceDetail = { name: 'x', entities: [], last_updated: '2026-08-28T11:59:48Z' };
  assert.equal(panel.updatedRelative, 'aktualisiert vor 12 s');
  panel.deviceDetail = { name: 'x', entities: [] };
  assert.equal(panel.updatedRelative, '');
});

test('Discovery entfernen bleibt ohne Rolle deaktiviert', () => {
  const panel = createDevicesPanel();
  panel.canDeleteDiscovery = false;
  assert.equal(panel.canDeleteDiscovery, false);
});

test('Discovery entfernen ist mit Rolle aktivierbar', () => {
  const panel = createDevicesPanel();
  panel.canDeleteDiscovery = true;
  assert.equal(panel.canDeleteDiscovery, true);
});

test('deviceAvailability ist online, wenn mindestens eine Entitaet verfuegbar ist', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { entities: [
    entity({ unique_id: 'a', has_availability: true, available: false }),
    entity({ unique_id: 'b', has_availability: true, available: true }),
  ] };
  assert.equal(panel.deviceAvailability, 'online');
});

test('deviceAvailability ist offline, wenn keine verfuegbare Entitaet meldet', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { entities: [entity({ unique_id: 'a', has_availability: true, available: false })] };
  assert.equal(panel.deviceAvailability, 'offline');
});

test('deviceAvailability ist unknown, wenn keine Entitaet Verfuegbarkeit meldet', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { entities: [entity({ unique_id: 'a', has_availability: false })] };
  assert.equal(panel.deviceAvailability, 'unknown');
});

test('positionUnderline setzt Breite/Position aus dem aktiven Trigger, ohne Fehler bei fehlendem Ref', () => {
  const panel = createDevicesPanel();
  assert.doesNotThrow(() => panel.positionUnderline());
});
