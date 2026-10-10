import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';
import { realCatalog } from './helpers/mount.mjs';
import { MANIFEST, SELECTION } from './helpers/fixtures.mjs';

function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

function load(lang = 'de') {
  const { window } = loadScripts(['i18n.js', 'services.js']);
  window.I18n.catalog = realCatalog(lang);
  const shell = { t: (key, params) => window.I18n.t(key, params) };
  return { S: window.Services, shell };
}

test('die Schalter folgen der Ordnung der Vorlage', () => {
  const { S, shell } = load();
  const rows = S.toggles(MANIFEST, SELECTION, shell);
  assert.deepEqual(plain(rows.map((row) => [row.kind, row.name, row.on])), [
    ['service', 'Automation', true],
    ['devices', 'Geräte-Dienste', true],
    ['system', 'Tailscale', true],
    ['system', 'HTTPS über Caddy', true],
  ]);
  assert.equal(rows[0].hint, 'Regeln, Zeitpläne und Schwellwerte auf dem Gerät. Fehlt der Dienst, blendet das Dashboard den Automationen-Tab aus.');
  assert.deepEqual(plain(rows[1].ids), ['81', '82', '83', '84', '85']);
  assert.deepEqual(plain(rows[1].chips.map((chip) => [chip.name, chip.on])), [
    ['APsystems', true], ['Batterie-SoC', false], ['Shelly', true], ['Trucki', true], ['Tuya', true],
  ]);
});

test('ein neuer Dienst ohne Katalogtext zeigt seine service_id und ist aus', () => {
  const { S, shell } = load();
  const manifest = Object.assign({}, MANIFEST, {
    steps: MANIFEST.steps.concat([{ id: '89', service_id: 'modbus', unit: 'modbus.service', kind: 'service', optional: true }]),
  });
  const selection = { source: 'node', steps: SELECTION.steps };
  const modbus = S.toggles(manifest, selection, shell).find((row) => row.ids[0] === '89');
  assert.equal(modbus.name, 'modbus');
  assert.equal(modbus.hint, '');
  assert.equal(modbus.on, false, 'ein unbekannter Schluessel faellt nie auf default zurueck');
  assert.equal(modbus.known, false);
});

test('ohne Geraete-Dienste gibt es keine Geraete-Zeile', () => {
  const { S, shell } = load();
  const manifest = { steps: MANIFEST.steps.filter((step) => step.kind !== 'device') };
  assert.ok(!S.toggles(manifest, SELECTION, shell).some((row) => row.kind === 'devices'));
});

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
  const row = S.toggles(manifest, { steps: Object.assign({}, SELECTION.steps, { 35: false }) }, shell)
    .find((r) => r.ids[0] === '35');
  assert.equal(row.kind, 'system');
  assert.equal(row.name, 'Shelly-Wake-Webhook in der Firewall');
  assert.match(row.hint, /8082/);
  assert.equal(row.on, false);
  // Eine Auswahl von vor dem Schritt (Update) nennt 35 nicht: bleibt aus.
  const fromNode = S.toggles(manifest, { source: 'node', steps: SELECTION.steps }, shell).find((r) => r.ids[0] === '35');
  assert.equal(fromNode.on, false);
  assert.equal(fromNode.known, false);
  const opted = S.toggles(manifest, { steps: { 35: true, 83: true } }, shell).find((r) => r.ids[0] === '35');
  assert.equal(opted.on, true);
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

test('der Shelly-Webhook erscheint erst, wenn der Shelly-Dienst an ist', () => {
  const { S, shell } = load();
  const manifest = withWebhookStep();
  const ids = (steps) => S.toggles(manifest, { steps }, shell).map((row) => row.ids[0]);
  assert.ok(ids({ 83: true }).includes('35'));
  assert.ok(!ids({ 83: false }).includes('35'), 'ohne Shelly kein Webhook-Schalter');
  assert.ok(!ids({}).includes('35'), 'ein nicht genannter Shelly-Dienst ist aus');
  const noShelly = Object.assign({}, manifest, { steps: manifest.steps.filter((step) => step.id !== '83') });
  assert.ok(!S.toggles(noShelly, { steps: { 35: true } }, shell).some((row) => row.ids[0] === '35'),
    'ein Bundle ohne Shelly-Dienst zeigt den Schalter nie');
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
