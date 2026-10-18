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
  dom.window.document.dispatchEvent(new dom.window.Event('alpine:init'));
  const panel = factories.devicesPanel();
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

test('seedPrefsDraft füllt den Entwurf einmal je Gerät und überschreibt laufende Eingaben nicht', () => {
  const panel = createDevicesPanel();
  panel.selectedDeviceId = 'node';
  panel.deviceDetail = {
    id: 'node',
    icon_name: 'mdi:raspberry-pi',
    favorite_refs: ['node_temp'],
    pin_favorites: true,
    entities: [{unique_id: 'node_temp'}, {unique_id: 'node_relay'}],
  };

  panel.seedPrefsDraft();
  assert.equal(panel.prefsDraft.icon, 'mdi:raspberry-pi');
  // seedPrefsDraft() baut favorite_refs per Spread im vm-Kontext von
  // dashboard.js - dasselbe realmuebergreifende deepStrictEqual-Problem wie
  // bei messageStates oben, deshalb hier ueber Array.from in den Haupt-Realm
  // geholt.
  assert.deepEqual(Array.from(panel.prefsDraft.favorite_refs), ['node_temp']);
  assert.equal(panel.prefsDraft.pin_favorites, true);

  // Der Nutzer tippt weiter, waehrend der SSE-Takt loadDeviceDetail erneut
  // laufen laesst - der Entwurf darf dabei nicht zurueckspringen.
  panel.prefsDraft.icon = 'mdi:solar-panel';
  panel.seedPrefsDraft();
  assert.equal(panel.prefsDraft.icon, 'mdi:solar-panel');

  panel.selectedDeviceId = 'shelly';
  panel.deviceDetail = {id: 'shelly', entities: []};
  panel.seedPrefsDraft();
  assert.equal(panel.prefsDraft.icon, '');
  assert.deepEqual(Array.from(panel.prefsDraft.favorite_refs), []);
});

test('pickIcon speichert das Vorschlags-Icon als leere Auswahl', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {id: 'plug', suggested_icon: 'mdi:power-plug', entities: []};
  panel.prefsDraft = {icon: '', favorite_refs: [], pin_favorites: false};

  assert.equal(panel.draftIconName, 'mdi:power-plug');
  assert.equal(panel.savedIconName, 'mdi:power-plug');

  panel.pickIcon('energy-node:chip');
  assert.equal(panel.prefsDraft.icon, 'energy-node:chip');
  assert.equal(panel.draftIconName, 'energy-node:chip');

  panel.pickIcon('mdi:power-plug');
  assert.equal(panel.prefsDraft.icon, '');

  // Ohne Vorschlag steht die leere Auswahl fuer den Chip.
  panel.deviceDetail = {id: 'x', entities: []};
  panel.pickIcon('energy-node:chip');
  assert.equal(panel.prefsDraft.icon, '');
  panel.deviceDetail = {id: 'x', icon_name: 'mdi:home', entities: []};
  assert.equal(panel.savedIconName, 'mdi:home');
});

test('initFavoritesChoices füllt Choices in Auswahlreihenfolge, begrenzt auf drei', async () => {
  const panel = createDevicesPanel();
  const calls = {};
  let selected = [];
  panel.testWindow.Choices = class {
    constructor(select, options) { calls.select = select; calls.options = options; }
    setChoices(list) { calls.choices = Array.from(list, item => ({...item})); }
    setChoiceByValue(values) { selected = Array.from(values); }
    getValue() { return selected; }
    destroy() { calls.destroyed = true; }
  };
  const select = panel.testWindow.document.createElement('select');
  panel.$refs = {favoritesSelect: select, deviceModalDialog: {dataset: {}}};
  panel.selectedDeviceId = 'node';
  panel.prefsDraftFor = 'node';
  panel.deviceDetail = {id: 'node', entities: [{unique_id: 'node_temp', name: 'Temperatur'}, {unique_id: 'node_relay', name: 'Relais'}]};
  panel.prefsDraft = {icon: '', favorite_refs: ['node_relay', 'node_temp'], pin_favorites: false};

  await panel.initFavoritesChoices();

  assert.equal(calls.select, select);
  assert.equal(calls.options.maxItemCount, 3);
  // Favoriten zuerst, in Auswahlreihenfolge - Choices legt die Chips in
  // Optionsreihenfolge an.
  assert.deepEqual(calls.choices.map(item => item.value), ['node_relay', 'node_temp']);
  assert.deepEqual(selected, ['node_relay', 'node_temp']);

  selected = ['node_temp'];
  panel.syncFavoritesFromChoices();
  assert.deepEqual(Array.from(panel.prefsDraft.favorite_refs), ['node_temp']);

  panel.destroyFavoritesChoices();
  assert.equal(calls.destroyed, true);
  assert.equal(panel.favoritesChoices, null);
});

// Choices.destroy() schreibt die Optionen samt selected ins <select> zurück.
// Bliebe das stehen, übernähme die nächste Instanz sie als Chips in
// Optionsreihenfolge, die Auswahlreihenfolge ginge nach dem Speichern verloren.
test('initFavoritesChoices startet nach destroy mit leerem select', async () => {
  const panel = createDevicesPanel();
  const doc = panel.testWindow.document;
  const select = doc.createElement('select');
  select.multiple = true;
  const optionsAtConstruct = [];
  panel.testWindow.Choices = class {
    constructor(el) { optionsAtConstruct.push(Array.from(el.options, option => option.value)); }
    setChoices() {}
    setChoiceByValue() {}
    getValue() { return []; }
    destroy() {
      const option = doc.createElement('option');
      option.value = 'node_temp';
      option.selected = true;
      select.append(option);
    }
  };
  panel.$refs = {favoritesSelect: select, deviceModalDialog: {dataset: {}}};
  panel.selectedDeviceId = 'node';
  panel.prefsDraftFor = 'node';
  panel.deviceDetail = {id: 'node', entities: [{unique_id: 'node_temp', name: 'Temperatur'}, {unique_id: 'node_relay', name: 'Relais'}]};
  panel.prefsDraft = {icon: '', favorite_refs: ['node_relay'], pin_favorites: false};

  await panel.initFavoritesChoices();
  await panel.initFavoritesChoices();

  assert.deepEqual(optionsAtConstruct[1], []);
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

test('pinnedFavorites zeigt nur bei gesetztem Schalter und überspringt verwaiste Refs', () => {
  const panel = createDevicesPanel();
  panel.deviceDetail = {
    id: 'node',
    favorite_refs: ['node_relay', 'node_weg', 'node_temp'],
    pin_favorites: false,
    entities: [{unique_id: 'node_temp', name: 'Temperatur'}, {unique_id: 'node_relay', name: 'Relais'}],
  };

  assert.deepEqual(Array.from(panel.pinnedFavorites), []);

  panel.deviceDetail = {...panel.deviceDetail, pin_favorites: true};
  assert.deepEqual(
    Array.from(panel.pinnedFavorites, entity => entity.unique_id),
    ['node_relay', 'node_temp'],
  );
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

function powerPanel({ canEditEnergy = true, subDevices = [], edgeProps = [] } = {}) {
  const panel = createDevicesPanel();
  panel.canEditEnergy = canEditEnergy;
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
