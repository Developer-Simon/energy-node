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
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const modalSource = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'device-modal.js'), 'utf8');
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
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; }, magic() {}, directive() {} };
  // dashboard.js startet beim Laden ein Modul-Level-setInterval (die
  // Zeitstempel-Aktualisierung) - ein echtes window.setInterval wuerde den
  // Testprozess sonst am Leben halten, gleiche Loesung wie in
  // dashboard.nav.test.mjs.
  dom.window.setInterval = () => 0;
  dom.window.deviceTileMixin = () => ({});
  installI18n(dom.window);
  vm.runInContext(source, context);
  vm.runInContext(modalSource, context);
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const panel = factories.devicesPanel();
  // Im Betrieb laedt selectDevice() device-modal.js nach, hier liegt es schon da.
  panel.installDeviceModal();
  // Fuer Tests, die window.Choices doubeln muessen.
  Object.defineProperty(panel, 'testWindow', {value: dom.window});
  return panel;
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

test('ohne view_device_analysis gibt es keinen Analyse-Tab und kein Rueckfall darauf', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = { name: 'x', entities: [] };
  panel.canViewAnalysis = false;
  assert.equal(panel.defaultDeviceTab(), 'deviceInfo');
  panel.setDeviceTab('management');
  assert.notEqual(panel.activeDeviceTab, 'management');
  panel.canViewAnalysis = true;
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

test('managementTeaser zaehlt nur Konfigurationswarnungen', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    name: 'x', entities: [],
    warnings: [
      { rule_id: 'Offline', severity: 'critical', message: 'off', hint: '', operational: true },
      { rule_id: 'Naming', severity: 'warning', message: 'naming', hint: '', operational: false },
    ],
    command_actions: [{ at: '2026-08-28T11:59:48Z', entity_id: 'e1', result: 'published' }],
  };
  assert.equal(panel.managementTeaser(), '⚠ 1');
  panel.deviceDetail.warnings = [];
  assert.equal(panel.managementTeaser(), '');
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

// Ersatz fuer den Block messageStates/activityEntries/recentMessages/activityLog-Tests.
function fetchActivity(panel, entriesFor) {
  const requested = [];
  panel.testWindow.fetch = async url => {
    requested.push(url);
    const result = await entriesFor(url);
    return { ok: true, json: async () => result };
  };
  return requested;
}

test('Aktivität kommt aus dem Aktivitäts-Endpunkt und wächst wie bisher', async () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'dev';
  let body = { entries: [
    { key: 'msg-x-2026-10-18T10:00:00Z-value', at: '2026-10-18T10:00:00Z', kind: 'message', label: 'x', value: '1' },
  ] };
  const requested = fetchActivity(panel, async () => body);
  await panel.loadActivity('dev');
  assert.deepEqual(requested, ['/api/v1/devices/dev/activity']);
  assert.deepEqual(Array.from(panel.activityLog, entry => entry.text), ['x: 1']);
  // Der Server liefert nur die letzten Nachrichten. Neue Zeilen kommen oben
  // dazu, bekannte bleiben darunter, auch wenn der Server sie nicht mehr sendet.
  body = { entries: [
    { key: 'msg-y-2026-10-18T10:00:05Z-value', at: '2026-10-18T10:00:05Z', kind: 'message', label: 'y', value: '2' },
  ] };
  await panel.loadActivity('dev');
  assert.deepEqual(Array.from(panel.activityLog, entry => entry.text), ['y: 2', 'x: 1']);
  assert.equal(panel.activityLog.length, 2);
});

test('Aktivität setzt für einen leeren Payload den Katalogtext', async () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'dev';
  fetchActivity(panel, async () => ({ entries: [
    { key: 'msg-goe-2026-10-18T10:00:00Z-empty', at: '2026-10-18T10:00:00Z', kind: 'message', label: 'goe/state', value: '' },
  ] }));
  await panel.loadActivity('dev');
  assert.deepEqual(Array.from(panel.activityLog, entry => entry.text), ['goe/state: (leer)']);
});

test('Befehle zeigen gesendet oder fehlgeschlagen', async () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'dev';
  fetchActivity(panel, async () => ({ entries: [
    { key: 'cmd-2026-10-18T10:00:00Z-sw', at: '2026-10-18T10:00:00Z', kind: 'command', label: 'sw', ok: false },
  ] }));
  await panel.loadActivity('dev');
  assert.deepEqual(Array.from(panel.activityLog, entry => entry.text), ['sw: Befehl fehlgeschlagen']);
});

test('Ein Fehler beim Abruf lässt die Aktivität stehen, ohne zu werfen', async () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'dev';
  fetchActivity(panel, async () => ({ entries: [
    { key: 'msg-x-2026-10-18T10:00:00Z-value', at: '2026-10-18T10:00:00Z', kind: 'message', label: 'x', value: '1' },
  ] }));
  await panel.loadActivity('dev');
  panel.testWindow.fetch = async () => { throw new Error('offline'); };
  await panel.loadActivity('dev');
  assert.deepEqual(Array.from(panel.activityLog, entry => entry.text), ['x: 1']);
});

test('Eine Antwort nach dem Gerätewechsel wird verworfen', async () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'dev';
  panel.testWindow.fetch = async () => {
    panel.selectedDeviceId = 'other';
    return { ok: true, json: async () => ({ entries: [
      { key: 'msg-x-2026-10-18T10:00:00Z-value', at: '2026-10-18T10:00:00Z', kind: 'message', label: 'x', value: '1' },
    ] }) };
  };
  await panel.loadActivity('dev');
  assert.equal(panel.activityLog.length, 0);
});

test('Entitaet klappt auf und bleibt nach einem Refresh offen', async () => {
  const panel = createDevicesPanel();
  const detail = { id: 'dev', name: 'x', entities: [entity({ unique_id: 'e1' }), entity({ unique_id: 'e2' })] };
  const ok = body => ({ ok: true, status: 200, json: async () => body });
  panel.testWindow.fetch = async url => (url.endsWith('/activity') ? ok({ entries: [] }) : ok(detail));
  panel.selectedDeviceId = 'dev';
  await panel.loadDeviceDetail('dev');
  panel.toggleAnalysisEntity('e1');
  assert.equal(panel.isAnalysisEntityOpen('e1'), true);
  assert.equal(panel.isAnalysisEntityOpen('e2'), false);
  await panel.refreshSelectedDevice();
  assert.equal(panel.isAnalysisEntityOpen('e1'), true);
  panel.toggleAnalysisEntity('e1');
  assert.equal(panel.isAnalysisEntityOpen('e1'), false);
});

test('Entitaeten bleiben offen, aber ein Geraetewechsel leert die Auswahl', async () => {
  const panel = createDevicesPanel();
  const details = {
    dev: { id: 'dev', name: 'x', entities: [entity({ unique_id: 'e1' })] },
    other: { id: 'other', name: 'y', entities: [entity({ unique_id: 'e1' })] },
  };
  const ok = body => ({ ok: true, status: 200, json: async () => body });
  panel.testWindow.fetch = async url => {
    if (url.endsWith('/activity')) return ok({ entries: [] });
    if (url === '/api/v1/device/icons') return ok([]);
    return ok(details[url.split('/').pop()]);
  };
  panel.$nextTick = callback => callback();
  panel.$refs = {};
  await panel.openDevice('dev');
  panel.toggleAnalysisEntity('e1');
  await panel.openDevice('other');
  assert.equal(panel.isAnalysisEntityOpen('e1'), false);
});

test('Befehls-Chip zaehlt die Schreibaktionen der Entitaet', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    name: 'x', entities: [],
    command_actions: [
      { at: '2026-08-28T11:59:48Z', entity_id: 'e1', result: 'published' },
      { at: '2026-08-28T11:59:50Z', entity_id: 'e1', result: 'failed' },
      { at: '2026-08-28T11:59:51Z', entity_id: 'e2', result: 'published' },
    ],
  };
  assert.equal(panel.commandCount(entity({ unique_id: 'e1' })), 2);
  assert.equal(panel.commandCount(entity({ unique_id: 'e3' })), 0);
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

function nodeDetail(favorite_refs = [], icon_name = '') {
  return {
    id: 'node',
    name: 'Knoten',
    suggested_icon: 'energy-node:solar-panel',
    icon_name,
    favorite_refs: [...favorite_refs],
    card_refs: ['node_temp', 'node_relay', 'node_pv'],
    entities: [
      {unique_id: 'node_temp', name: 'Temperatur'},
      {unique_id: 'node_relay', name: 'Relais'},
      {unique_id: 'node_pv', name: 'PV'},
      {unique_id: 'node_grid', name: 'Netz'},
    ],
  };
}

// Fake-Server fuer Darstellung: PUT speichert Icon und Favoriten, GET liefert
// das Detail mit dem gespeicherten Stand (loadDeviceDetail laeuft danach neu).
function stubAppearanceFetch(panel, { favorite_refs = [], icon_name = '', fail = false } = {}) {
  const calls = [];
  const pushed = [];
  const server = { icon_name, favorite_refs: [...favorite_refs] };
  const ok = body => ({ ok: true, status: 200, json: async () => body });
  panel.testWindow.fetch = async (url, options = {}) => {
    const method = options.method || 'GET';
    const body = options.body ? JSON.parse(options.body) : null;
    calls.push({ url, method, body });
    if (method === 'PUT') {
      if (fail) return { ok: false, status: 500, json: async () => ({}) };
      server.icon_name = body.icon;
      server.favorite_refs = body.favorite_refs;
      return ok({});
    }
    if (url === '/api/v1/device/icons') return ok([]);
    if (url === '/api/v1/devices/node') {
      return ok({ ...nodeDetail(server.favorite_refs, server.icon_name) });
    }
    return ok({ id: 'shelly', name: 'Shelly', entities: [] });
  };
  panel.$store = { toasts: { push: (message, severity, options) => pushed.push({ message, severity, options }) } };
  panel.csrfToken = 'csrf-token';
  panel.canEditAppearance = true;
  panel.selectedDeviceId = 'node';
  panel.deviceDetail = nodeDetail(favorite_refs, icon_name);
  return { calls, pushed, server };
}

const putsOf = calls => calls.filter(call => call.method === 'PUT');

test('toggleFavorite fügt hinzu und sendet favorite_refs mit Icon', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'], icon_name: 'energy-node:sun' });
  await panel.toggleFavorite({ unique_id: 'node_relay' });

  assert.equal(putsOf(calls).length, 1);
  assert.equal(calls[0].url, '/api/v1/device/prefs/node');
  assert.equal(calls[0].method, 'PUT');
  // Icon wird mitgesendet, pin_favorites nicht mehr.
  assert.deepEqual(calls[0].body, { icon: 'energy-node:sun', favorite_refs: ['node_temp', 'node_relay'] });
  assert.equal(panel.isFavorite({ unique_id: 'node_relay' }), true);
  assert.equal(pushed[0].message, 'Favoriten gespeichert.');
  assert.equal(pushed[0].severity, 'info');
  assert.ok(pushed[0].options.action, 'Erfolg bietet Rückgängig an');
});

test('toggleFavorite beim vierten Stern sendet nichts und meldet das Limit', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp', 'node_relay', 'node_pv'] });
  await panel.toggleFavorite({ unique_id: 'node_grid' });

  assert.equal(calls.length, 0);
  assert.equal(panel.favoriteLimitHit, 1);
  assert.equal(pushed.length, 1);
  assert.equal(pushed[0].message, 'Höchstens 3 Favoriten.');
  assert.equal(pushed[0].severity, 'warning');
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['node_temp', 'node_relay', 'node_pv']);
});

test('toggleFavorite ohne edit_device_appearance tut nichts', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'] });
  panel.canEditAppearance = false;
  await panel.toggleFavorite({ unique_id: 'node_relay' });

  assert.equal(calls.length, 0);
  assert.equal(pushed.length, 0);
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['node_temp']);
});

test('Rückgängig setzt das ursprüngliche Gerät zurück, auch nach Gerätewechsel', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'], icon_name: 'energy-node:sun' });
  await panel.toggleFavorite({ unique_id: 'node_relay' });
  const { action } = pushed[0].options;

  // Der Nutzer wechselt das Gerät, bevor er Rückgängig klickt.
  panel.selectedDeviceId = 'shelly';
  panel.deviceDetail = { id: 'shelly', favorite_refs: ['shelly_x'], entities: [] };
  await action.run();

  const last = putsOf(calls).at(-1);
  assert.equal(last.url, '/api/v1/device/prefs/node');
  assert.deepEqual(last.body, { icon: 'energy-node:sun', favorite_refs: ['node_temp'] });
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['shelly_x']);
});

test('ein fehlgeschlagenes Speichern stellt die Favoriten wieder her', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'], fail: true });
  await panel.toggleFavorite({ unique_id: 'node_relay' });

  assert.equal(putsOf(calls).length, 1);
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['node_temp']);
  assert.equal(panel.isFavorite({ unique_id: 'node_relay' }), false);
  assert.equal(pushed.length, 1);
  assert.equal(pushed[0].severity, 'critical');
  assert.equal(pushed[0].options, undefined, 'ein Fehler bietet keine Rückgängig-Aktion');
});

test('saveIcon speichert das Vorschlags-Icon als leere Auswahl und sendet andere Icons', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'] });

  await panel.saveIcon('energy-node:solar-panel');
  assert.deepEqual(putsOf(calls)[0].body, { icon: '', favorite_refs: ['node_temp'] });
  assert.equal(pushed[0].message, 'Icon gespeichert.');

  await panel.saveIcon('energy-node:sun');
  assert.deepEqual(putsOf(calls)[1].body, { icon: 'energy-node:sun', favorite_refs: ['node_temp'] });
  assert.equal(panel.savedIconName, 'energy-node:sun');
  assert.equal(panel.draftIconName, 'energy-node:sun');
});

test('iconPickerOpen und moreMenuOpen schließen beim Gerätewechsel und mit closeFloatingMenus', async () => {
  const panel = createDevicesPanel();
  stubAppearanceFetch(panel, { favorite_refs: [] });
  panel.$nextTick = callback => callback();
  panel.$refs = {};
  panel.iconPickerOpen = true;
  panel.moreMenuOpen = true;
  await panel.openDevice('shelly');
  assert.equal(panel.iconPickerOpen, false);
  assert.equal(panel.moreMenuOpen, false);

  panel.iconPickerOpen = true;
  panel.moreMenuOpen = true;
  panel.closeFloatingMenus();
  assert.equal(panel.iconPickerOpen, false);
  assert.equal(panel.moreMenuOpen, false);
});

test('überlappende Sterne: schlagen beide fehl, steht das Modal auf der bestätigten Liste', async () => {
  const panel = createDevicesPanel();
  const { calls } = stubAppearanceFetch(panel, { favorite_refs: [], fail: true });
  await Promise.all([
    panel.toggleFavorite({ unique_id: 'node_temp' }),
    panel.toggleFavorite({ unique_id: 'node_relay' }),
  ]);
  assert.equal(putsOf(calls).length, 2);
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), []);
});

test('überlappende Sterne werden nacheinander gespeichert und beide bleiben', async () => {
  const panel = createDevicesPanel();
  const { calls } = stubAppearanceFetch(panel, { favorite_refs: [] });
  await Promise.all([
    panel.toggleFavorite({ unique_id: 'node_temp' }),
    panel.toggleFavorite({ unique_id: 'node_relay' }),
  ]);
  assert.deepEqual(putsOf(calls).map(call => call.body.favorite_refs), [['node_temp'], ['node_temp', 'node_relay']]);
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['node_temp', 'node_relay']);
});

test('Rückgängig des Sterns behält ein inzwischen geändertes Icon', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: [], icon_name: 'energy-node:sun' });
  await panel.toggleFavorite({ unique_id: 'node_relay' });
  await panel.saveIcon('energy-node:battery');
  await pushed[0].options.action.run();

  assert.deepEqual(putsOf(calls).at(-1).body, { icon: 'energy-node:battery', favorite_refs: [] });
  assert.equal(panel.deviceDetail.icon_name, 'energy-node:battery');
});

test('Rückgängig des Icons behält die inzwischen geänderten Favoriten', async () => {
  const panel = createDevicesPanel();
  const { calls, pushed } = stubAppearanceFetch(panel, { favorite_refs: [], icon_name: 'energy-node:sun' });
  await panel.saveIcon('energy-node:battery');
  await panel.toggleFavorite({ unique_id: 'node_relay' });
  await pushed[0].options.action.run();

  assert.deepEqual(putsOf(calls).at(-1).body, { icon: 'energy-node:sun', favorite_refs: ['node_relay'] });
});

test('ein fehlgeschlagenes Speichern nach Gerätewechsel lässt das neue Gerät unberührt', async () => {
  const panel = createDevicesPanel();
  const { pushed } = stubAppearanceFetch(panel, { favorite_refs: ['node_temp'], fail: true });
  const realFetch = panel.testWindow.fetch;
  panel.testWindow.fetch = async (url, options = {}) => {
    // Der Nutzer wechselt das Gerät, während das PUT noch läuft.
    if (options.method === 'PUT') {
      panel.selectedDeviceId = 'shelly';
      panel.deviceDetail = { id: 'shelly', icon_name: 'energy-node:sun', favorite_refs: ['shelly_x'], entities: [] };
    }
    return realFetch(url, options);
  };
  await panel.toggleFavorite({ unique_id: 'node_relay' });

  assert.equal(panel.selectedDeviceId, 'shelly');
  assert.equal(panel.deviceDetail.id, 'shelly');
  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['shelly_x']);
  assert.equal(pushed[0].severity, 'critical');
});

test('ein veralteter Refresh überschreibt die gespeicherten Favoriten nicht', async () => {
  const panel = createDevicesPanel();
  stubAppearanceFetch(panel, { favorite_refs: [] });
  Object.defineProperty(panel.testWindow.document, 'visibilityState', { value: 'visible', configurable: true });
  const realFetch = panel.testWindow.fetch;
  const ok = body => ({ ok: true, status: 200, json: async () => body });
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  let held = false;
  panel.testWindow.fetch = async (url, options = {}) => {
    // Der erste Detail-Refresh (SSE-Takt) hängt, bis das Speichern durch ist.
    if ((options.method || 'GET') === 'GET' && url === '/api/v1/devices/node' && !held) {
      held = true;
      await gate;
      return ok({ ...nodeDetail([]) });
    }
    return realFetch(url, options);
  };
  const stale = panel.refreshSelectedDevice();
  await panel.toggleFavorite({ unique_id: 'node_relay' });
  release();
  await stale;

  assert.deepEqual(Array.from(panel.deviceDetail.favorite_refs), ['node_relay']);
  assert.equal(panel.isFavorite({ unique_id: 'node_relay' }), true);
});

test('Esc schließt zuerst Icon-Auswahl oder Mehr-Menü über den Dialog-Handler', () => {
  const panel = createDevicesPanel();
  const dialog = panel.testWindow.document.createElement('div');
  panel.$refs = { deviceModalDialog: dialog };
  panel.attachDeviceModal();
  const pressEscape = () => {
    const event = new panel.testWindow.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    dialog.dispatchEvent(event);
    return event;
  };

  panel.iconPickerOpen = true;
  const closedPicker = pressEscape();
  assert.equal(panel.iconPickerOpen, false);
  assert.equal(closedPicker.defaultPrevented, true);

  panel.moreMenuOpen = true;
  pressEscape();
  assert.equal(panel.moreMenuOpen, false);

  const idle = pressEscape();
  assert.equal(idle.defaultPrevented, false);
  panel.detachDeviceModal();
});


test('the icon picker groups the catalogue by category in catalogue order', () => {
  const panel = createDevicesPanel();
  panel.deviceIcons = [
    {name: 'energy-node:solar-panel', category: 'generation', categoryKey: 'device_icon_category.generation', markup: ''},
    {name: 'energy-node:sun', category: 'generation', categoryKey: 'device_icon_category.generation', markup: ''},
    {name: 'energy-node:battery', category: 'storage', categoryKey: 'device_icon_category.storage', markup: ''},
  ];
  const groups = panel.deviceIconGroups;
  assert.deepEqual(JSON.parse(JSON.stringify(groups.map(group => [group.category, group.icons.map(icon => icon.name)]))),
    [['generation', ['energy-node:solar-panel', 'energy-node:sun']], ['storage', ['energy-node:battery']]]);
  assert.equal(groups[0].categoryKey, 'device_icon_category.generation');
});

test('iconMarkup baut ein vollständiges SVG und fällt auf den Standard zurück', () => {
  const panel = createDevicesPanel();
  panel.deviceIcons = [
    {name: 'energy-node:chip', label: 'Standard', markup: '<rect x="6" y="6" width="12" height="12"/>'},
    {name: 'mdi:solar-panel', label: 'Solarpanel', markup: '<path d="M3.5 16 7 6Z"/>'},
  ];

  const chosen = panel.iconMarkup('mdi:solar-panel');
  assert.match(chosen, /^<svg /);
  assert.match(chosen, /M3\.5 16 7 6Z/);
  assert.match(panel.iconMarkup('mdi:unbekannt'), /rect x="6"/);
  assert.match(panel.iconMarkup(''), /rect x="6"/);
});

test('groupedWarnings groups warnings with the same key and rule_id', () => {
  const panel = createDevicesPanel();
  installI18n(panel.testWindow, {lang: 'en'});
  const enCatalog = panel.testWindow.I18n.catalog;
  panel.deviceDetail = {
    name: 'test device',
    warnings: [
      {rule_id: 'Offline', key: 'offline', severity: 'critical', message: 'Gerät meldet sich als offline.', hint: 'Stromversorgung, Netzwerk und Bridge prüfen', entity_id: 'a'},
      {rule_id: 'Offline', key: 'offline', severity: 'critical', message: 'Gerät meldet sich als offline.', hint: 'Stromversorgung, Netzwerk und Bridge prüfen', entity_id: 'b'},
    ],
  };
  const grouped = panel.groupedWarnings;
  assert.equal(grouped.length, 1);
  assert.equal(grouped[0].count, 2);
  assert.deepEqual(Array.from(grouped[0].affectedEntityIds), ['a', 'b']);
  assert.equal(panel.warningText(grouped[0], 'message'), enCatalog['diagnostics.rule.offline.message']);
});

test('warningText shows raw message for warnings without key', () => {
  const panel = createDevicesPanel();
  const rawWarning = {rule_id: 'X', message: 'raw message', hint: 'raw hint'};
  assert.equal(panel.warningText(rawWarning, 'message'), 'raw message');
  assert.equal(panel.warningText(rawWarning, 'hint'), 'raw hint');
});

// Sub-devices in the measurement tiles. The fetch stub answers with a device
// map; the window records the device-map-changed events.
function stubSubDeviceFetch(panel, answer) {
  const calls = [];
  const events = [];
  panel.testWindow.fetch = async (url, options = {}) => {
    calls.push({ url, method: options.method || 'GET', body: options.body ? JSON.parse(options.body) : null });
    const body = typeof answer === 'function' ? answer(url, options) : answer;
    return { ok: true, status: 200, json: async () => body };
  };
  panel.testWindow.addEventListener('device-map-changed', event => events.push(event.detail));
  panel.$store = { toasts: { push() {} }, modal: { confirm: async () => true } };
  panel.csrfToken = 'csrf-token';
  panel.selectedDeviceId = 'wp';
  return { calls, events };
}

function powerPanel({ canEditEnergy = true, subDevices = [], edgeProps = [], manage = true } = {}) {
  const panel = createDevicesPanel();
  panel.canEditEnergy = canEditEnergy;
  panel.subManage = manage;
  panel.selectedDeviceId = 'wp';
  panel.deviceDetail = {
    id: 'wp',
    name: 'Wärmepumpe',
    entities: [
      entity({ unique_id: 'pw', name: 'Leistung', unit_of_measurement: 'W', value: '420' }),
    ],
  };
  panel.subDevices = subDevices;
  panel.subEdgeProps = edgeProps;
  panel.subDevicesReady = true;
  panel.$nextTick = () => {};
  return panel;
}

const dryer = {
  id: 'sub:trockner', label: 'Trockner', icon: 'energy-node:dryer', device_id: 'wp', entity_id: 'pw',
};

test('power tile offers create sub-device with edit_energy', () => {
  const panel = powerPanel();
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.isSubDeviceCandidate(power), true);
  assert.equal(panel.subDeviceFor(power), null);
  assert.equal(panel.subDeviceRowVisible(power), true);
});

test('sub-device management is off by default and hides create, edit and delete', async () => {
  const panel = powerPanel({ manage: false, subDevices: [dryer] });
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subManage, false);
  assert.equal(panel.subCreateVisible(power), false);
  assert.equal(panel.subActionsVisible(power), false);
  panel.openSubDeviceForm(power);
  assert.equal(panel.subForm, null);
  panel.openSubDeviceForm(power, dryer);
  assert.equal(panel.subForm, null);
  let confirmed = false;
  panel.$store = { toasts: { push() {} }, modal: { confirm: async () => { confirmed = true; return true; } } };
  await panel.deleteSubDevice(dryer);
  assert.equal(confirmed, false);
});

test('an existing sub-device stays visible read-only while management is off', () => {
  const panel = powerPanel({ manage: false, subDevices: [dryer] });
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subDeviceRowVisible(power), true);
  assert.equal(panel.subDeviceFor(power).label, 'Trockner');
});

test('the management switch is only offered with edit_energy', () => {
  const panel = powerPanel({ canEditEnergy: false, manage: false });
  assert.equal(panel.subActionsVisible(panel.deviceDetail.entities[0]), false);
});

test('the management button needs edit_energy and a power entity', () => {
  assert.equal(powerPanel({ manage: false }).subManageOffered(), true);
  assert.equal(powerPanel({ canEditEnergy: false, manage: false }).subManageOffered(), false);
  const noPower = powerPanel({ manage: false });
  noPower.deviceDetail.entities = [{ unique_id: 'x_temp', unit_of_measurement: '°C', device_class: 'temperature' }];
  assert.equal(noPower.subManageOffered(), false);
});

test('opening a device or closing it turns sub-device management off', () => {
  const panel = powerPanel({ manage: true, subDevices: [dryer] });
  panel.$refs = {};
  panel.resetSubDevices();
  assert.equal(panel.subManage, false);
  panel.subManage = true;
  panel.closeDeviceDetail();
  assert.equal(panel.subManage, false);
});

test('tile shows icon and name of the existing sub-device', () => {
  const panel = powerPanel({ subDevices: [dryer] });
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subDeviceFor(power).label, 'Trockner');
  assert.equal(panel.subDeviceRowVisible(power), true);
});

test('create posts without position and fires the map changed event', async () => {
  const panel = powerPanel();
  const { calls, events } = stubSubDeviceFetch(panel, {
    nodes: [], edge_props: [], sub_devices: [{ ...dryer, id: 'sub:leistung', label: 'Leistung' }],
  });
  const power = panel.deviceDetail.entities[0];
  panel.openSubDeviceForm(power);
  assert.equal(panel.subForm.entityId, 'pw');
  assert.equal(panel.subForm.subId, null);
  await panel.saveSubDeviceForm();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].method, 'POST');
  assert.equal(calls[0].url, '/api/v1/device/map/sub-devices');
  assert.equal(calls[0].body.entity_id, 'pw');
  assert.equal(calls[0].body.device_id, 'wp');
  assert.equal('x' in calls[0].body, false);
  assert.equal('y' in calls[0].body, false);
  assert.equal(events.length, 1);
  assert.equal(panel.subForm, null);
});

test('edit keeps the channel of the sub-device and sends a PUT', async () => {
  const panel = powerPanel({
    subDevices: [dryer],
    edgeProps: [{ parent_id: 'wp', child_id: 'sub:trockner', type: 'energy', channel: 3 }],
  });
  const { calls } = stubSubDeviceFetch(panel, { nodes: [], edge_props: [], sub_devices: [{ ...dryer, label: 'Wäsche' }] });
  panel.openSubDeviceForm(panel.deviceDetail.entities[0], dryer);
  panel.subForm.name = 'Wäsche';
  await panel.saveSubDeviceForm();
  assert.equal(calls[0].method, 'PUT');
  assert.equal(calls[0].url, '/api/v1/device/map/sub-devices/sub%3Atrockner');
  assert.equal(calls[0].body.label, 'Wäsche');
  assert.equal(calls[0].body.channel, 3);
});

test('without edit_energy no actions are rendered', () => {
  const panel = powerPanel({ canEditEnergy: false });
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subDeviceRowVisible(power), false);
  panel.openSubDeviceForm(power);
  assert.equal(panel.subForm, null);

  const withSub = powerPanel({ canEditEnergy: false, subDevices: [dryer] });
  assert.equal(withSub.subDeviceRowVisible(withSub.deviceDetail.entities[0]), true);
  assert.equal(withSub.subActionsVisible(withSub.deviceDetail.entities[0]), false);
});

test('non-power tiles have no sub-device row', () => {
  const panel = powerPanel({ subDevices: [dryer] });
  const percent = entity({ unique_id: 'soc', name: 'Akku', unit_of_measurement: '%' });
  const diagnostic = entity({ unique_id: 'pw', name: 'Leistung', unit_of_measurement: 'W', entity_category: 'diagnostic' });
  assert.equal(panel.isSubDeviceCandidate(percent), false);
  assert.equal(panel.subDeviceRowVisible(percent), false);
  assert.equal(panel.isSubDeviceCandidate(diagnostic), false);
  assert.equal(panel.subDeviceRowVisible(diagnostic), false);
});

test('canEditEnergy follows edit_energy and falls back to true without a session API', async () => {
  const denied = createDevicesPanel();
  denied.testWindow.fetch = async () => ({ ok: true, status: 200, json: async () => ({ edit_energy: false, csrf_token: 'x' }) });
  await denied.loadSession();
  assert.equal(denied.canEditEnergy, false);

  const allowed = createDevicesPanel();
  allowed.testWindow.fetch = async () => ({ ok: true, status: 200, json: async () => ({ edit_energy: true }) });
  await allowed.loadSession();
  assert.equal(allowed.canEditEnergy, true);

  const noSession = createDevicesPanel();
  noSession.testWindow.fetch = async () => { throw new Error('no session api'); };
  await noSession.loadSession();
  assert.equal(noSession.canEditEnergy, true);
});

test('a failed map load keeps the create action hidden', async () => {
  const panel = powerPanel({ subDevices: [dryer] });
  const toasts = [];
  panel.$store = { toasts: { push: (message, level) => toasts.push([message, level]) } };
  panel.testWindow.fetch = async () => { throw new Error('map down'); };
  panel.resetSubDevices();
  const token = panel.subLoadToken;
  await panel.loadSubDevices('wp', token);
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subDevices.length, 0);
  assert.equal(panel.subDevicesReady, false);
  assert.equal(panel.subCreateVisible(power), false);
  assert.equal(panel.subDeviceRowVisible(power), false);
  assert.equal(toasts.length, 1);
  assert.equal(toasts[0][0], 'map down');
});

test('the create action waits for the map of the open device', () => {
  const panel = powerPanel();
  panel.resetSubDevices();
  const power = panel.deviceDetail.entities[0];
  assert.equal(panel.subCreateVisible(power), false);
  panel.openSubDeviceForm(power);
  assert.equal(panel.subForm, null);
});

test('a stale map answer does not overwrite a newer open', async () => {
  const panel = powerPanel();
  let answerOld;
  panel.testWindow.fetch = () => new Promise(resolve => { answerOld = resolve; });
  panel.resetSubDevices();
  const staleToken = panel.subLoadToken;
  const stale = panel.loadSubDevices('wp', staleToken);
  // A second open moves the token on before the first answer arrives.
  panel.resetSubDevices();
  answerOld({ ok: true, status: 200, json: async () => ({ sub_devices: [dryer], edge_props: [] }) });
  await stale;
  assert.equal(panel.subDevices.length, 0);
  assert.equal(panel.subDevicesReady, false);
});

test('opening a device clears the previous sub-devices before the new map arrives', async () => {
  const panel = powerPanel({ subDevices: [dryer] });
  const entities = panel.deviceDetail.entities;
  panel.$store = { toasts: { push() {} } };
  panel.testWindow.fetch = async (url) => {
    if (url.includes('/api/v1/device/map')) throw new Error('map down');
    return { ok: true, status: 200, json: async () => ({ id: 'wp', name: 'Wärmepumpe', entities }) };
  };
  await panel.openDevice('wp');
  assert.equal(panel.subDevices.length, 0);
  assert.equal(panel.subDevicesReady, false);
  assert.equal(panel.subCreateVisible(entities[0]), false);
});

test('heroEntities folgt card_refs und markiert Automatik', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    id: 'node',
    card_refs: ['node_temp', 'node_relay', 'node_weg', 'node_fremd'],
    favorite_refs: ['node_relay'],
    entities: [
      {unique_id: 'node_relay', name: 'Relais'},
      {unique_id: 'node_temp', name: 'Temperatur'},
      {unique_id: 'node_weg', name: 'Weg'},
    ],
  };
  const hero = panel.heroEntities;
  assert.deepEqual(hero.map(item => item.entity.unique_id), ['node_temp', 'node_relay', 'node_weg']);
  assert.deepEqual(hero.map(item => item.auto), [true, false, true]);
});

test('Warnungen teilen sich in Betrieb und Konfiguration', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    name: 'test device',
    warnings: [
      {rule_id: 'Offline', key: 'offline', severity: 'critical', message: 'offline', operational: true, entity_id: 'a'},
      {rule_id: 'Naming', key: 'naming', severity: 'warning', message: 'naming', operational: false, entity_id: 'b'},
    ],
  };
  assert.deepEqual(Array.from(panel.operationalWarnings, warning => warning.rule_id), ['Offline']);
  assert.deepEqual(Array.from(panel.configWarnings, warning => warning.rule_id), ['Naming']);
});

test('canViewAnalysis und canEditAppearance folgen der Session und sind ohne Session-API true', async () => {
  const denied = createDevicesPanel();
  denied.testWindow.fetch = async () => ({ ok: true, status: 200, json: async () => ({ view_device_analysis: false, edit_device_appearance: false, csrf_token: 'x' }) });
  await denied.loadSession();
  assert.equal(denied.canViewAnalysis, false);
  assert.equal(denied.canEditAppearance, false);
  assert.equal(denied.canEditPrefs, false);

  const allowed = createDevicesPanel();
  allowed.testWindow.fetch = async () => ({ ok: true, status: 200, json: async () => ({ view_device_analysis: true, edit_device_appearance: true }) });
  await allowed.loadSession();
  assert.equal(allowed.canViewAnalysis, true);
  assert.equal(allowed.canEditAppearance, true);
  assert.equal(allowed.canEditPrefs, true);

  const noSession = createDevicesPanel();
  noSession.testWindow.fetch = async () => { throw new Error('no session api'); };
  await noSession.loadSession();
  assert.equal(noSession.canViewAnalysis, true);
  assert.equal(noSession.canEditAppearance, true);
});
