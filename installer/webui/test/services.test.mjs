import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { realCatalog } from './helpers/mount.mjs';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { MANIFEST, MANIFEST_UPDATE, SELECTION, MODULE_MANIFEST, MODULE_SELECTION } from './helpers/fixtures.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

function load(lang = 'de') {
  const { window } = loadScripts(['i18n.js', 'services.js']);
  window.I18n.catalog = realCatalog(lang);
  const shell = { t: (key, params) => window.I18n.t(key, params) };
  return { S: window.Services, shell };
}

test('die Ausfuehrung hat eine Station je Schritt in Manifest-Reihenfolge', () => {
  const { S, shell } = load();
  const groups = S.runGroups(MANIFEST, shell);
  assert.deepEqual(plain(groups.map((group) => group.label)), [
    'Systempakete', 'MQTT-Broker', 'Firewall', 'Tailscale', 'Python-Pakete', 'Dashboard und MQTT-Brücke', 'HTTPS über Caddy',
    'APsystems', 'Batterie-SoC', 'Shelly', 'Trucki', 'Tuya', 'Automation',
  ]);
  assert.ok(groups.every((group) => group.ids.length === 1));
});

test('serviceUnits nennt Bruecke, Dienste nach Auswahl und Dashboard mit Unit', () => {
  const { S, shell } = load();
  const services = S.serviceUnits(MANIFEST, SELECTION, shell);
  assert.deepEqual(plain(services.map((s) => [s.name, s.on])), [
    ['MQTT-Brücke', true], ['Automation', true], ['APsystems', true], ['Batterie-SoC', false],
    ['Shelly', true], ['Trucki', true], ['Tuya', true], ['Dashboard', true],
  ]);
  assert.equal(services[0].unit, 'energy-node-dashboard.service');
  assert.equal(services[2].unit, 'apsystems-ez1.service');
});

test('groupOf und stepLabel liefern Nummer und Namen fuer die Diagnose', () => {
  const { S, shell } = load();
  const groups = S.runGroups(MANIFEST, shell);
  assert.equal(S.groupOf(groups, '50').number, 5);
  assert.equal(S.groupOf(groups, '83').number, 10);
  assert.equal(S.groupOf(groups, '99'), null);
  assert.equal(S.stepLabel(MANIFEST, '83', shell), 'Shelly');
  assert.equal(S.stepLabel(MANIFEST, '50', shell), 'Python-Pakete');
});

test('Pflichtschritte sind immer gewaehlt, Dienste nur mit true', () => {
  const { S } = load();
  assert.equal(S.isSelected({ id: '10' }, { steps: {} }), true);
  const service = { id: '83', service_id: 'shelly', optional: true, default: true };
  assert.equal(S.isSelected(service, { steps: {} }), false);
  assert.equal(S.isSelected(service, { steps: { 83: true } }), true);
  assert.equal(S.isSelected({ id: '40', optional: true }, { steps: { 40: true } }), true);
});

// Ein Systemschritt, den die Auswahl auf dem Node noch nicht kennt (15 kam
// mit einem Update), gilt wie in step.sh und plan.sh mit seiner
// Manifest-Vorgabe. Sonst schriebe das erste Speichern "15": false.
test('ein unbekannter Systemschritt folgt der Manifest-Vorgabe', () => {
  const { S } = load();
  assert.equal(S.isSelected({ id: '15', optional: true, default: true }, { steps: {} }), true);
  assert.equal(S.isSelected({ id: '35', optional: true, default: false }, { steps: {} }), false);
  assert.equal(S.isSelected({ id: '15', optional: true, default: true }, { steps: { 15: false } }), false);
  assert.equal(S.isSelected({ id: '70', optional: true }, null), false);
});

// Schritt 35 (Firewall-Freigabe fuer den Shelly-Wake-Webhook) ist Opt-in:
// Manifest-Vorgabe aus, eigener Schalter in der Konfiguration, in der
// Ausfuehrung aber Teil der Station "Firewall".
const WEBHOOK_STEP = { id: '35', optional: true, default: false, requires: '83' };
function withWebhookStep() {
  const steps = MANIFEST.steps.slice();
  steps.splice(steps.findIndex((step) => step.id === '30') + 1, 0, WEBHOOK_STEP);
  return Object.assign({}, MANIFEST, { steps });
}

test('der Shelly-Webhook ist ein eigener Schalter und ohne Zustimmung aus', () => {
  const { S, shell } = load();
  const manifest = withWebhookStep();
  const tuningOf = (selection) => S.modules(manifest, selection, shell).tuning;
  const row = tuningOf({ steps: Object.assign({}, SELECTION.steps, { 35: false }) }).find((m) => m.id === '35');
  assert.equal(row.name, 'Shelly-Webhook');
  assert.match(row.hint, /8082/);
  assert.equal(row.on, false);
  // Eine Auswahl von vor dem Schritt (Update) nennt 35 nicht: bleibt aus.
  const fromNode = tuningOf({ source: 'node', steps: SELECTION.steps }).find((m) => m.id === '35');
  assert.equal(fromNode.on, false);
  assert.equal(fromNode.known, false);
  const opted = tuningOf({ steps: { 35: true, 83: true } }).find((m) => m.id === '35');
  assert.equal(opted.on, true);
});

test('der Shelly-Webhook erscheint erst, wenn der Shelly-Dienst an ist', () => {
  const { S, shell } = load();
  const manifest = withWebhookStep();
  const shown = (steps) => S.modules(manifest, { steps }, shell).tuning.filter((m) => m.available).map((m) => m.id);
  assert.ok(shown({ 83: true }).includes('35'));
  assert.ok(!shown({ 83: false }).includes('35'), 'ohne Shelly kein Webhook-Schalter');
  assert.ok(!shown({}).includes('35'), 'ein nicht genannter Shelly-Dienst ist aus');
  const noShelly = Object.assign({}, manifest, { steps: manifest.steps.filter((step) => step.id !== '83') });
  assert.ok(!S.modules(noShelly, { steps: { 35: true } }, shell).tuning.some((m) => m.available && m.id === '35'),
    'ein Bundle ohne Shelly-Dienst zeigt den Schalter nie');
});

test('in der Ausfuehrung laeuft der Shelly-Webhook unter der Firewall', () => {
  const { S, shell } = load();
  const manifest = withWebhookStep();
  const groups = S.runGroups(manifest, shell);
  assert.deepEqual(plain(groups.map((group) => group.label)), [
    'Systempakete', 'MQTT-Broker', 'Firewall', 'Tailscale', 'Python-Pakete', 'Dashboard und MQTT-Brücke', 'HTTPS über Caddy',
    'APsystems', 'Batterie-SoC', 'Shelly', 'Trucki', 'Tuya', 'Automation',
  ]);
  assert.deepEqual(plain(groups[2].ids), ['30', '35']);
  assert.equal(S.groupOf(groups, '35').number, 3);
  assert.equal(S.stepLabel(manifest, '35', shell), 'Shelly-Wake-Webhook in der Firewall');
});

test('dropUnmet nimmt das Opt-in zurueck, sobald der benoetigte Schritt aus ist', () => {
  const { S } = load();
  const manifest = withWebhookStep();
  const steps = { 35: true, 83: false, 40: true };
  S.dropUnmet(manifest, steps);
  assert.deepEqual(plain(steps), { 35: false, 83: false, 40: true });
  const kept = { 35: true, 83: true };
  S.dropUnmet(manifest, kept);
  assert.equal(kept['35'], true);
});

test('requiresMet with a list needs every step', () => {
  const { S } = load();
  const manifest = Object.assign({}, MANIFEST, {
    steps: MANIFEST.steps.concat([{ id: '89', optional: true, default: false, requires: '81,83,88' }]),
  });
  const step = manifest.steps.find((s) => s.id === '89');
  assert.equal(S.requiresMet(manifest, step, { steps: { 81: true, 83: true, 88: true } }), true);
  assert.equal(S.requiresMet(manifest, step, { steps: { 81: true, 83: false, 88: true } }), false);
  // Ein unbekannter Schritt in der Liste zaehlt als nicht erfuellt.
  const unknown = { id: '90', optional: true, requires: '81,99' };
  assert.equal(S.requiresMet(manifest, unknown, { steps: { 81: true } }), false);
});

test('dropUnmet switches 89 off when 88 is off', () => {
  const { S } = load();
  const manifest = Object.assign({}, MANIFEST, {
    steps: MANIFEST.steps.concat([{ id: '89', optional: true, default: false, requires: '81,83,88' }]),
  });
  const steps = { 81: true, 83: true, 88: false, 89: true };
  S.dropUnmet(manifest, steps);
  assert.equal(steps['89'], false);
});

function loadWithFormat(lang = 'de') {
  const { window } = loadScripts(['i18n.js', 'format.js', 'services.js']);
  window.I18n.catalog = realCatalog(lang);
  const shell = { lang, t: (key, params) => window.I18n.t(key, params), tn: (key, n, params) => window.I18n.tn(key, n, params) };
  return { S: window.Services, shell };
}

// Ausstehende Systempakete (Schritt 15), wie Vorschau und Diagnose sie nennen.
test('systemUpdatesText nennt Zahl und Stand, ohne Bericht nichts', () => {
  const { S, shell } = loadWithFormat();
  const at = '2026-10-04T06:12:00+00:00';
  assert.equal(S.systemUpdatesText({ count: 12, checked_at: at }, shell), '12 Updates · Stand 04.10.');
  assert.equal(S.systemUpdatesText({ count: 1, checked_at: at }, shell), '1 Update · Stand 04.10.');
  assert.equal(S.systemUpdatesText({ count: 0, checked_at: at }, shell), 'aktuell · Stand 04.10.');
  assert.equal(S.systemUpdatesText({ count: 3 }, shell), '3 Updates');
  assert.equal(S.systemUpdatesText(null, shell), '');
  assert.equal(S.systemUpdatesText(undefined, shell), '');
  const en = loadWithFormat('en');
  assert.equal(en.S.systemUpdatesText({ count: 2, checked_at: at }, en.shell), '2 updates · as of 4 Oct');
});

test('systemPackagesText listet Paket und Versionen', () => {
  const { S } = loadWithFormat();
  assert.equal(S.systemPackagesText({ count: 2, packages: [
    { name: 'libssl3', from: '3.0.11', to: '3.0.13' },
    { name: 'linux-image-6.6', to: '6.6.51' },
  ] }), 'libssl3 3.0.11 → 3.0.13, linux-image-6.6 6.6.51');
  assert.equal(S.systemPackagesText({ count: 0, packages: [] }), '');
  assert.equal(S.systemPackagesText(null), '');
});

// --- Module (Konfiguration): Dienste, Feinabstimmung, Beispiel -----------------

const TUNING_IDS = ['15', '17', '35', '41'];
const MODULE_IDS = ['88', '81', '82', '83', '84', '85', '40', '70', '15', '17', '35', '41'];

test('modules splits services, tuning and example in the order of the draft', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  const m = S.modules(MODULE_MANIFEST, MODULE_SELECTION, shell);
  assert.deepEqual(plain(m.services.map((mod) => mod.id)), ['88', '81', '82', '83', '84', '85', '40', '70']);
  assert.deepEqual(plain(m.tuning.map((mod) => mod.id)), TUNING_IDS);
  assert.equal(m.example.id, '89');
  assert.equal(m.services[0].name, 'Automation');
  assert.equal(m.services[0].state, 'Regeln laufen');
  assert.equal(m.services[0].icon, 'automation');
  assert.equal(m.services[0].hint, shell.t('configure.hint.service.automation'));
  assert.equal(m.services[2].on, false);
  assert.equal(m.services[2].state, 'aus');
  assert.equal(m.services[2].short, 'Batterie');
  assert.deepEqual(plain(m.count), { on: 7, total: 8 });
});

test('modules lists an unmet tuning step as unavailable and forgets its state', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  const selection = { source: 'node', steps: { 83: false, 35: true, 40: false, 41: true } };
  S.dropUnmet(MODULE_MANIFEST, selection.steps);
  let m = S.modules(MODULE_MANIFEST, selection, shell);
  const byId = (list, id) => list.filter((mod) => mod.id === id)[0];
  assert.equal(byId(m.tuning, '35').available, false);
  assert.equal(byId(m.tuning, '41').available, false);
  assert.equal(m.unavailable, 'Nicht verfügbar: Shelly-Webhook (braucht Shelly), Tailscale-Logs im RAM (braucht Tailscale)');
  // Shelly und Tailscale wieder an: die Module sind verfuegbar, aber aus.
  selection.steps[83] = true;
  selection.steps[40] = true;
  m = S.modules(MODULE_MANIFEST, selection, shell);
  assert.equal(byId(m.tuning, '35').available, true);
  assert.equal(byId(m.tuning, '35').on, false);
  assert.equal(byId(m.tuning, '41').available, true);
  assert.equal(byId(m.tuning, '41').on, false);
});

test('modules has no unavailable line when every requirement is met', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  assert.equal(S.modules(MODULE_MANIFEST, MODULE_SELECTION, shell).unavailable, '');
});

test('modules counts only the services the manifest has', () => {
  const { S, shell } = load();
  const noDevices = Object.assign({}, MODULE_MANIFEST, {
    steps: MODULE_MANIFEST.steps.filter((step) => !['84', '85', '70'].includes(step.id)),
  });
  assert.equal(S.modules(noDevices, MODULE_SELECTION, shell).count.total, 5);
  const noTuning = Object.assign({}, MODULE_MANIFEST, {
    steps: MODULE_MANIFEST.steps.filter((step) => !['17', '41'].includes(step.id)),
  });
  assert.deepEqual(plain(S.modules(noTuning, MODULE_SELECTION, shell).tuning.map((mod) => mod.id)), ['15', '35']);
});

test('an unknown optional system step lands in tuning', () => {
  const { S, shell } = load();
  const step = { id: '99', optional: true, default: true };
  const manifest = Object.assign({}, MODULE_MANIFEST, { steps: MODULE_MANIFEST.steps.concat([step]) });
  assert.equal(S.groupOfStep(step), 'tuning');
  const m = S.modules(manifest, MODULE_SELECTION, shell);
  const mod = m.tuning.filter((item) => item.id === '99')[0];
  assert.ok(mod, 'Schritt 99 steht in tuning');
  assert.equal(mod.name, 'step.99');
});

test('every optional step of make_bundle.sh has a group', () => {
  const { S } = load();
  const script = fs.readFileSync(path.join(here, '..', '..', '..', 'scripts', 'build', 'make_bundle.sh'), 'utf8');
  const match = /OPTIONAL_STEPS=\(([0-9 ]+)\)/.exec(script);
  assert.ok(match, 'make_bundle.sh lists OPTIONAL_STEPS');
  for (const id of match[1].trim().split(/\s+/)) {
    assert.ok(Object.prototype.hasOwnProperty.call(S.GROUP_OF, id), `Schritt ${id} hat keine Gruppe`);
  }
});

test('the example is hidden on an update and shown on a first install', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: false };
  assert.equal(S.modules(MODULE_MANIFEST, MODULE_SELECTION, shell).example, null);
  shell.shared = { firstInstall: true };
  assert.equal(S.modules(MODULE_MANIFEST, MODULE_SELECTION, shell).example.id, '89');
});

test('the example is on by default and off when a required service is off', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  const example = S.modules(MODULE_MANIFEST, SELECTION, shell).example;
  assert.equal(example.on, true);
  assert.equal(example.available, true);
  const apsystemsOff = { source: 'node', steps: Object.assign({}, SELECTION.steps, { 81: false }) };
  assert.equal(S.modules(MODULE_MANIFEST, apsystemsOff, shell).example.available, false);
});

test('ein Dienst 89 mit service_id ist ein Dienst, nicht das Beispiel', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  const m = S.modules(MANIFEST_UPDATE, SELECTION, shell);
  assert.equal(m.example, null, 'das Beispiel ist nur der Schritt 89 ohne service_id');
  const modbus = m.services[m.services.length - 1];
  assert.equal(modbus.id, '89');
  assert.equal(modbus.name, 'modbus');
  assert.equal(modbus.icon, 'device');
  assert.equal(modbus.on, false);
  assert.equal(modbus.known, false);
  assert.equal(m.count.total, 9);
});

test('das Beispiel hat sein Symbol, der Schritt 89 ohne service_id', () => {
  const { S, shell } = load();
  shell.shared = { firstInstall: true };
  assert.equal(S.modules(MODULE_MANIFEST, MODULE_SELECTION, shell).example.icon, 'play');
});

test('every module id has name, short, on and off in both catalogs', () => {
  for (const lang of ['de', 'en']) {
    const catalog = realCatalog(lang);
    for (const id of MODULE_IDS) {
      for (const part of ['name', 'short', 'on', 'off']) {
        const key = `configure.module.${id}.${part}`;
        assert.ok(catalog[key], `${lang}.json fehlt ${key}`);
      }
    }
    for (const part of ['on', 'off']) {
      assert.ok(catalog[`configure.module.default.${part}`], `${lang}.json fehlt configure.module.default.${part}`);
    }
  }
});
