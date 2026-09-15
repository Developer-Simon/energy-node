// Die Daten der Vorlagen (cmd/fakehost --scenario vorlage) fuer die
// Bildschirmtests. Eine eigene Datei: importierte ein Test sie aus einem
// anderen Testmodul, registrierte node --test deren Tests ein zweites Mal.

// Das Manifest der Vorlage.
export const MANIFEST = {
  bundle_version: 'v1.4.2', arch: 'armv6', target_user: 'energynode', target_base: '/home/energynode',
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
    bootstrap: { from: 'v1.4.1', to: 'v1.4.2' },
    dashboard: { from: '1.4.1', to: '1.4.2' },
    services: { from: '3.6.0', to: '3.7.1' },
    energy_node_common: { from: '1.4.0', to: '1.4.2' },
    battery_soc_core: { from: '0.9.3', to: '0.9.3' },
    tinytuya: { from: '1.15.1', to: '1.16.0' },
    'paho-mqtt': { from: '2.1.0', to: '2.1.0' },
  },
};
