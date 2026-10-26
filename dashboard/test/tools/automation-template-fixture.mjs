// Erzeugt das Fixture der gebauten Assistenten-Vorlagen. Die Vorlagen laufen wie in
// automation-templates.test.mjs in einem jsdom-Kontext mit echtem i18n. Der Kontext ist
// fest: Wechselrichter, Wallbox mit Schalter und Stecker, ein Schalter, Standort und
// battery_soc. Die Dokumente sind die Eingabe der Dienstvalidierung in Go und Python.
//
// CLI: node test/tools/automation-template-fixture.mjs > test/fixtures/automation-templates-built.json
import vm from 'node:vm';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from '../helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const jsDir = path.join(here, '..', '..', 'internal', 'webui', 'static', 'js');

const LIMITS = { scenarios: 8, rules: 16 };

const DEVICES = [
  {
    id: 'pv1', name: 'Wechselrichter Dach',
    entities: [
      { unique_id: 'pv1_limit', name: 'Leistungsgrenze', command_topic: 'werkstatt/pv1/limit', min_value: 50, max_value: 800, step: 10, unit_of_measurement: 'W', has_value: true, value: '500' },
      { unique_id: 'pv1_power', name: 'Leistung', state_topic: 'werkstatt/pv1/power', template_supported: true, unit_of_measurement: 'W', has_value: true, value: '742' },
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
];

// Wallbox ohne Schalter: ihr Ladeschalter fehlt, der Stecker bleibt.
const DEVICES_WITHOUT_WALLBOX_SWITCH = DEVICES.map((device) => (device.id === 'wb'
  ? { ...device, entities: device.entities.filter((entity) => entity.unique_id !== 'wb_switch') }
  : device));

const SNAPSHOT = {
  values: { pv: 862, grid_export: 0, grid_import: 0, battery_soc: 64 },
  entities: [
    { device_id: 'pv1', entity_id: 'pv1_power', role: { role: 'pv' } },
    { device_id: 'wb', entity_id: 'wb_power', role: { role: 'wallbox' } },
  ],
};

const SETTINGS = {
  tick_interval_s: 10, settling_seconds: 60, balance_max_age_s: 30,
  history_limit: 10, history_persist: true, publish_min_interval_s: 30, regulator_interval_s: 10,
  latitude: 52.5, longitude: 13.4,
};

const ALL_DAYS = [0, 1, 2, 3, 4, 5, 6];
const WORKDAYS = [0, 1, 2, 3, 4];

function loadTemplates() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window, { lang: 'de' });
  const context = dom.getInternalVMContext();
  for (const file of ['automation-wizard.js', 'automation-templates.js']) {
    vm.runInContext(fs.readFileSync(path.join(jsDir, file), 'utf8'), context);
  }
  return { wizard: dom.window.__automationWizard, templates: dom.window.__automationTemplates.TEMPLATES };
}

// Objekte aus dem VM-Kontext in gewoehnliches JSON (ohne undefined) umwandeln.
const plain = (value) => JSON.parse(JSON.stringify(value));

// Varianten: [Vorlagen-ID, Geraete des Kontexts, Antwort-Abweichungen]. Ohne Abweichung
// gelten die Standardantworten der Vorlage.
function variants() {
  return [
    ['zero_feed', DEVICES, {}],
    ['zero_feed', DEVICES, { period: 'always' }],
    ['zero_feed', DEVICES, { period: 'fixed', window: { from: '08:00', to: '18:00', days: WORKDAYS } }],
    ['zero_feed', DEVICES, { measure: 'last' }],
    ['surplus_wallbox', DEVICES, {}],
    ['surplus_wallbox', DEVICES, { mode: 'battery', soc: 40 }],
    ['surplus_wallbox', DEVICES_WITHOUT_WALLBOX_SWITCH, {}],
    ['surplus_switch', DEVICES, {}],
    ['surplus_switch', DEVICES, { off: 'runtime', runtime: 120 }],
    ['surplus_switch', DEVICES, { extras: { weekdays: false, weekdays_value: WORKDAYS, repeat: false } }],
    ['battery_warn', DEVICES, {}],
    ['battery_warn', DEVICES, { below: 10, severity: 'critical' }],
    ['time_switch', DEVICES, {}],
    ['time_switch', DEVICES, { window: { from: '06:00', to: '08:00', days: WORKDAYS } }],
    ['pv_missing', DEVICES, {}],
    ['pv_missing', DEVICES, { limit: 50, minutes: 60, severity: 'critical' }],
  ];
}

// Alle Publish-Topics des Kontexts als Praefixe: die Dienstprüfung laesst nur diese zu.
function publishPrefixes(devices) {
  const topics = new Set();
  for (const device of devices) {
    for (const entity of device.entities) {
      if (entity.command_topic) topics.add(entity.command_topic);
    }
  }
  return [...topics].sort();
}

function buildFixture() {
  const { wizard, templates } = loadTemplates();
  const byId = new Map(templates.map((template) => [template.id, template]));
  const usedIds = [];
  const entries = variants().map(([templateId, devices, overrides]) => (
    buildVariant(wizard, byId.get(templateId), devices, overrides, usedIds)
  ));
  return { documents: packDocuments(entries) };
}

// Ein Eintrag je Variante. Die gebauten IDs wandern in usedIds, damit sie eindeutig bleiben.
function buildVariant(wizard, template, devices, overrides, usedIds) {
  const document = { rules: [], scenarios: [], settings: SETTINGS };
  const ctx = plain(wizard.buildContext({ devices, snapshot: SNAPSHOT, document }));
  const answers = plain({ ...wizard.defaultAnswers(template, ctx), ...overrides });
  const result = plain(template.build(answers, { ...ctx, usedIds }));
  usedIds.push(result.entry.id);
  return result;
}

// Ausgang, den ein Szenario regelt. Die Dienstpruefung laesst je Dokument jeden
// Ausgang nur einmal geregelt zu.
const regulatedTopic = (scenario) => scenario.while?.regulator?.output?.topic ?? null;

// Eintraege in das erste Dokument mit Platz legen: hoechstens 8 Szenarien, 16 Regeln,
// und kein zweites Szenario auf demselben geregelten Ausgang.
function packDocuments(entries) {
  const documents = [];
  for (const { kind, entry } of entries) {
    const key = kind === 'scenario' ? 'scenarios' : 'rules';
    const topic = kind === 'scenario' ? regulatedTopic(entry) : null;
    let doc = documents.find((candidate) => candidate[key].length < LIMITS[key]
      && (topic === null || !candidate.scenarios.some((scenario) => regulatedTopic(scenario) === topic)));
    if (!doc) {
      doc = { version: 1, settings: { ...SETTINGS, publish_allowed_prefixes: publishPrefixes(DEVICES) }, rules: [], scenarios: [] };
      documents.push(doc);
    }
    doc[key].push(entry);
  }
  return documents;
}

export { buildFixture };

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  process.stdout.write(`${JSON.stringify(buildFixture(), null, 2)}\n`);
}
