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
