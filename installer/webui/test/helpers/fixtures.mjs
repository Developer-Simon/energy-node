// Die Daten der Vorlagen (cmd/fakehost --scenario vorlage) fuer die
// Bildschirmtests. Eine eigene Datei: importierte ein Test sie aus einem
// anderen Testmodul, registrierte node --test deren Tests ein zweites Mal.

// Das Manifest der Vorlage.
export const MANIFEST = {
  bundle_version: 'v1.4.2', arch: 'armv6', target_user: 'energynode', target_base: '/home/energynode',
  components: {
    bootstrap: 'v1.0.5', dashboard: '1.4.2', services: '3.7.1',
    energy_node_common: '1.4.2', battery_soc_core: '0.9.3',
    tinytuya: '1.16.0', 'paho-mqtt': '2.1.0',
  },
  steps: [
    { id: '10' }, { id: '20' }, { id: '30' }, { id: '40', optional: true, default: true }, { id: '50' }, { id: '60' },
    { id: '70', optional: true, default: true },
    { id: '81', service_id: 'apsystems', unit: 'apsystems-ez1.service', kind: 'device', optional: true, default: true },
    { id: '82', service_id: 'battery_soc', unit: 'battery-soc.service', kind: 'device', optional: true },
    { id: '83', service_id: 'shelly', unit: 'shelly-rpc.service', kind: 'device', optional: true, default: true },
    { id: '84', service_id: 'trucki', unit: 'trucki-http.service', kind: 'device', optional: true, default: true },
    { id: '85', service_id: 'tuya', unit: 'tuya.service', kind: 'device', optional: true, default: true },
    { id: '88', service_id: 'automation', unit: 'automation.service', kind: 'service', optional: true, default: true },
  ],
};
export const SELECTION = {
  source: 'manifest-default',
  steps: { 40: true, 70: true, 81: true, 82: false, 83: true, 84: true, 85: true, 88: true },
};

// Die Daten der Vorlage "Aktualisieren" (cmd/fakehost --scenario vorlage-update).
export const MANIFEST_UPDATE = Object.assign({}, MANIFEST, {
  steps: MANIFEST.steps.concat([{ id: '89', service_id: 'modbus', unit: 'modbus.service', kind: 'service', optional: true }]),
});
export const SELECTION_NODE = { source: 'node', steps: Object.assign({}, SELECTION.steps) };
export const PLAN_UPDATE = {
  bundle_version: 'v1.4.2',
  steps: [
    { id: '10', state: 'done' }, { id: '20', state: 'done' }, { id: '30', state: 'done' },
    { id: '40', state: 'done', optional: true, selected: true },
    { id: '50', state: 'pending' }, { id: '60', state: 'pending' },
    { id: '70', state: 'done', optional: true, selected: true },
    { id: '81', state: 'done', optional: true, selected: true, unit: 'apsystems-ez1.service' },
    { id: '82', state: 'deselected', optional: true, unit: 'battery-soc.service' },
    { id: '83', state: 'done', optional: true, selected: true, unit: 'shelly-rpc.service' },
    { id: '84', state: 'done', optional: true, selected: true, unit: 'trucki-http.service' },
    { id: '85', state: 'pending', optional: true, selected: true, unit: 'tuya.service' },
    { id: '88', state: 'pending', optional: true, selected: true, unit: 'automation.service' },
    { id: '89', state: 'deselected', optional: true, unit: 'modbus.service' },
  ],
  components: {
    bootstrap: { from: 'v1.0.4', to: 'v1.0.5' },
    dashboard: { from: '1.4.1', to: '1.4.2' },
    services: { from: '3.6.0', to: '3.7.1' },
    energy_node_common: { from: '1.4.0', to: '1.4.2' },
    battery_soc_core: { from: '0.9.3', to: '0.9.3' },
    tinytuya: { from: '1.15.1', to: '1.16.0' },
    'paho-mqtt': { from: '2.1.0', to: '2.1.0' },
  },
};

// Die Pruefliste des Testwirts (cmd/fakehost): Shelly ausgefallen, sonst gruen.
export const DIAGNOSE = {
  bundle_version: 'v1.4.2',
  units: {},
  ports: { 443: true, 1883: true, 8080: true },
  checks: [
    { name: 'unit apsystems-ez1.service', ok: true, detail: 'active', retry_step_id: '81', group: 'services', subject: 'apsystems-ez1.service' },
    { name: 'unit automation.service', ok: true, detail: 'active', retry_step_id: '88', group: 'services', subject: 'automation.service' },
    { name: 'unit battery-soc.service', ok: true, detail: 'active', retry_step_id: '82', group: 'services', subject: 'battery-soc.service' },
    { name: 'unit caddy.service', ok: true, detail: 'active', retry_step_id: '70', group: 'system', subject: 'caddy.service' },
    { name: 'unit energy-node-dashboard.service', ok: true, detail: 'active', retry_step_id: '60', group: 'system', subject: 'energy-node-dashboard.service' },
    { name: 'unit mosquitto.service', ok: true, detail: 'active', retry_step_id: '20', group: 'system', subject: 'mosquitto.service' },
    { name: 'unit shelly-rpc.service', ok: false, detail: 'failed', retry_step_id: '83', group: 'services', subject: 'shelly-rpc.service' },
    { name: 'unit tailscaled.service', ok: true, detail: 'active', retry_step_id: '40', group: 'system', subject: 'tailscaled.service' },
    { name: 'unit trucki-http.service', ok: true, detail: 'active', retry_step_id: '84', group: 'services', subject: 'trucki-http.service' },
    { name: 'unit tuya.service', ok: true, detail: 'active', retry_step_id: '85', group: 'services', subject: 'tuya.service' },
    { name: 'port 443', ok: true, detail: 'open', retry_step_id: '70', group: 'system', subject: '443' },
    { name: 'port 1883', ok: true, detail: 'open', retry_step_id: '20', group: 'system', subject: '1883' },
    { name: 'port 8080', ok: true, detail: 'open', retry_step_id: '60', group: 'system', subject: '8080' },
    { name: 'config.json', ok: true, detail: 'present=true', retry_step_id: '60', group: 'config', subject: 'config.json' },
    { name: 'tailscale login', ok: true, detail: 'angemeldet=true', retry_step_id: '40', group: 'system', subject: 'tailscale' },
  ],
};
