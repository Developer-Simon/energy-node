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

test('die Ausfuehrung hat die sieben Stationen der Vorlage', () => {
  const { S, shell } = load();
  const groups = S.runGroups(MANIFEST, SELECTION, shell);
  assert.deepEqual(plain(groups.map((group) => group.label)), [
    'Systempakete', 'MQTT-Broker', 'Firewall', 'Tailscale', 'Python-Pakete', 'Dienste und Dashboard', 'HTTPS über Caddy',
  ]);
  const services = groups[5];
  assert.deepEqual(plain(services.ids), ['60', '88', '81', '82', '83', '84', '85']);
  assert.deepEqual(plain(services.subs.map((sub) => [sub.name, sub.on])), [
    ['MQTT-Brücke', true], ['Automation', true], ['APsystems', true], ['Batterie-SoC', false],
    ['Shelly', true], ['Trucki', true], ['Tuya', true], ['Dashboard', true],
  ]);
  assert.equal(services.subs[0].unit, 'energy-node-dashboard.service');
  assert.equal(services.subs[2].unit, 'apsystems-ez1.service');
  assert.equal(services.subs.filter((sub) => sub.on).length, 7);
});

test('groupOf und stepLabel liefern Nummer und Namen fuer die Diagnose', () => {
  const { S, shell } = load();
  const groups = S.runGroups(MANIFEST, SELECTION, shell);
  assert.equal(S.groupOf(groups, '50').number, 5);
  assert.equal(S.groupOf(groups, '83').number, 6);
  assert.equal(S.groupOf(groups, '99'), null);
  assert.equal(S.stepLabel(MANIFEST, '83', shell), 'Shelly');
  assert.equal(S.stepLabel(MANIFEST, '50', shell), 'Python-Pakete');
});

test('Pflichtschritte sind immer gewaehlt, optionale nur mit true', () => {
  const { S } = load();
  assert.equal(S.isSelected({ id: '10' }, { steps: {} }), true);
  assert.equal(S.isSelected({ id: '40', optional: true, default: true }, { steps: {} }), false);
  assert.equal(S.isSelected({ id: '40', optional: true }, { steps: { 40: true } }), true);
});

// Schritt 35 (Firewall-Freigabe fuer den Shelly-Wake-Webhook) ist Opt-in:
// Manifest-Vorgabe aus, eigener Schalter in der Konfiguration, in der
// Ausfuehrung aber Teil der Station "Firewall".
const WEBHOOK_STEP = { id: '35', optional: true, default: false };
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
  const opted = S.toggles(manifest, { steps: { 35: true } }, shell).find((r) => r.ids[0] === '35');
  assert.equal(opted.on, true);
});

test('in der Ausfuehrung laeuft der Shelly-Webhook unter der Firewall', () => {
  const { S, shell } = load();
  const manifest = withWebhookStep();
  const groups = S.runGroups(manifest, SELECTION, shell);
  assert.deepEqual(plain(groups.map((group) => group.label)), [
    'Systempakete', 'MQTT-Broker', 'Firewall', 'Tailscale', 'Python-Pakete', 'Dienste und Dashboard', 'HTTPS über Caddy',
  ]);
  assert.deepEqual(plain(groups[2].ids), ['30', '35']);
  assert.equal(S.groupOf(groups, '35').number, 3);
  assert.equal(S.stepLabel(manifest, '35', shell), 'Shelly-Wake-Webhook in der Firewall');
});
