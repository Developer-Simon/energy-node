import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadScripts } from './helpers/load.mjs';

const { window } = loadScripts(['media-model.js']);
const M = window.MediaModel;

// plain() macht aus vm-Objekten gewoehnliche Literale, damit deepEqual
// nicht an der Realm-Grenze scheitert.
const plain = (value) => JSON.parse(JSON.stringify(value));

const DEFAULTS = { hostname: 'energy-node', user: 'node', timezone: 'Europe/Berlin', keyboard: 'de', wifi_country: 'DE', timezones: [], keyboards: [] };

function valid(overrides = {}) {
  return Object.assign(M.defaultSettings(DEFAULTS, 'pi'), overrides);
}

test('validHostname folgt der Regel des Go-Codes', () => {
  assert.equal(M.validHostname('energy-node'), true);
  assert.equal(M.validHostname('Energy'), false);
  assert.equal(M.validHostname('-energy'), false);
  assert.equal(M.validHostname('energy-'), false);
  assert.equal(M.validHostname(''), false);
  assert.equal(M.validHostname('a'.repeat(63)), true);
  assert.equal(M.validHostname('a'.repeat(64)), false);
  assert.equal(M.validHostname(undefined), false);
});

test('validUser lehnt root und ungueltige Namen ab', () => {
  assert.equal(M.validUser('pi'), true);
  assert.equal(M.validUser('_dienst'), true);
  assert.equal(M.validUser('root'), false);
  assert.equal(M.validUser('Node'), false);
  assert.equal(M.validUser('1node'), false);
  assert.equal(M.validUser(''), false);
});

test('groups fasst die Geraete je Gruppe zusammen, in Eingabereihenfolge', () => {
  const devices = [
    { id: 'pi5', group: 'pi' },
    { id: 'x86', group: 'debian' },
    { id: 'pi1', group: 'pi' },
  ];
  assert.deepEqual(plain(M.groups(devices)), [
    { id: 'pi', devices: [{ id: 'pi5', group: 'pi' }, { id: 'pi1', group: 'pi' }] },
    { id: 'debian', devices: [{ id: 'x86', group: 'debian' }] },
  ]);
  assert.deepEqual(plain(M.groups([])), []);
});

test('defaultSettings uebernimmt die Vorgaben und lässt Passwoerter und Schalter aus', () => {
  assert.deepEqual(plain(M.defaultSettings(DEFAULTS, 'pi')), {
    hostname: 'energy-node', user: 'node', emergency: false, password: '', password2: '',
    timezone: 'Europe/Berlin', keyboard: 'de', wifiOn: false, ssid: '', wifiPassword: '', country: 'DE',
  });
});

test('settingsComplete verlangt gueltige Namen', () => {
  assert.equal(M.settingsComplete(valid()), true);
  assert.equal(M.settingsComplete(valid({ hostname: 'Energy' })), false);
  assert.equal(M.settingsComplete(valid({ user: 'root' })), false);
});

test('settingsComplete prueft das Notfall-Passwort nur bei eingeschaltetem Schalter', () => {
  assert.equal(M.settingsComplete(valid({ emergency: true, password: 'kurz', password2: 'kurz' })), false);
  assert.equal(M.settingsComplete(valid({ emergency: true, password: 'langes-passwort', password2: 'langes-passwort' })), true);
  assert.equal(M.settingsComplete(valid({ emergency: true, password: 'langes-passwort', password2: 'anders-lang' })), false);
  assert.equal(M.settingsComplete(valid({ emergency: false, password: 'kurz' })), true);
});

test('settingsComplete prueft das WLAN nur bei eingeschaltetem Schalter', () => {
  assert.equal(M.settingsComplete(valid({ wifiOn: true, ssid: '', country: 'DE' })), false);
  assert.equal(M.settingsComplete(valid({ wifiOn: true, ssid: 'Haus', country: 'de' })), false);
  assert.equal(M.settingsComplete(valid({ wifiOn: true, ssid: 'Haus', country: 'DE' })), true);
  assert.equal(M.settingsComplete(valid({ wifiOn: false, ssid: '', country: 'x' })), true);
});

test('toRequest liefert wifi null ohne WLAN und ein leeres Passwort ohne Notfall-Passwort', () => {
  const plainRequest = plain(M.toRequest(valid({ password: 'geheim-geheim', password2: 'geheim-geheim' })));
  assert.equal(plainRequest.wifi, null);
  assert.equal(plainRequest.password, '', 'ohne Schalter geht kein Passwort mit');
  assert.deepEqual(plainRequest, {
    hostname: 'energy-node', user: 'node', password: '', timezone: 'Europe/Berlin', keyboard: 'de', wifi: null,
  });

  const withAll = plain(M.toRequest(valid({
    emergency: true, password: 'langes-passwort', password2: 'langes-passwort',
    wifiOn: true, ssid: 'Haus', wifiPassword: 'wlan-geheim', country: 'DE',
  })));
  assert.equal(withAll.password, 'langes-passwort');
  assert.deepEqual(withAll.wifi, { ssid: 'Haus', password: 'wlan-geheim', country: 'DE' });
});

test('remembered enthaelt keine Passwoerter', () => {
  const remembered = plain(M.remembered(valid({
    emergency: true, password: 'geheim-geheim', password2: 'geheim-geheim',
    wifiOn: true, ssid: 'Haus', wifiPassword: 'wlan-geheim', country: 'DE',
  })));
  assert.deepEqual(remembered, {
    hostname: 'energy-node', user: 'node', timezone: 'Europe/Berlin', keyboard: 'de',
    wifiOn: true, ssid: 'Haus',
  });
  assert.ok(!JSON.stringify(remembered).includes('geheim'));
});

const CARD = { path: '/dev/sdb', model: 'SanDisk Ultra', removable: true };
const SSD = { path: '/dev/sda', model: 'Samsung SSD 870', removable: false };

test('confirmationFor verlangt beim Wechselmedium ein Ja, bei internen den Namen', () => {
  assert.equal(M.confirmationFor(CARD), 'yes');
  assert.equal(M.confirmationFor(SSD), 'Samsung SSD 870');
});

test('confirmed prueft beim Wechselmedium das Haekchen', () => {
  assert.equal(M.confirmed(CARD, true, ''), true);
  assert.equal(M.confirmed(CARD, false, ''), false);
});

test('confirmed verlangt bei internen Laufwerken den genauen Namen', () => {
  assert.equal(M.confirmed(SSD, true, 'samsung ssd 870'), false);
  assert.equal(M.confirmed(SSD, true, ''), false, 'ein Haekchen allein reicht nicht');
  assert.equal(M.confirmed(SSD, false, 'Samsung SSD 870'), true);
  assert.equal(M.confirmed(SSD, false, ' Samsung SSD 870 '), true, 'Leerzeichen am Rand zaehlen nicht');
  assert.equal(M.confirmed({ path: '/dev/sdc', model: '', removable: false }, false, ''), false, 'ohne Modellname gibt es keine Bestaetigung');
  assert.equal(M.confirmed(null, true, 'x'), false);
});

test('remaining rechnet die Restzeit aus Rest und Rate', () => {
  assert.equal(M.remaining({ bytes_total: 100, bytes_done: 40, rate: 20 }), 3);
  assert.equal(M.remaining({ bytes_total: 100, bytes_done: 100, rate: 20 }), 0);
  assert.equal(M.remaining({ bytes_total: 100, bytes_done: 120, rate: 20 }), 0, 'nie negativ');
  assert.equal(M.remaining({ bytes_total: 100, bytes_done: 40, rate: 0 }), null, 'ohne Rate keine Zeit');
  assert.equal(M.remaining(null), null);
});

test('stepRows nennt jeden Schritt mit seinem Textschluessel', () => {
  const job = { steps: [{ id: 'download', state: 'done' }, { id: 'write', state: 'running' }] };
  assert.deepEqual(plain(M.stepRows(job)), [
    { id: 'download', state: 'done', labelKey: 'media.step.download' },
    { id: 'write', state: 'running', labelKey: 'media.step.write' },
  ]);
  assert.deepEqual(plain(M.stepRows({})), []);
});

test('defaultSettings gibt den erkannten WLAN-Namen vor, der Schalter bleibt aus', () => {
  const settings = plain(M.defaultSettings(Object.assign({}, DEFAULTS, { wifi_ssid: 'Werkstatt' }), 'pi'));
  assert.equal(settings.ssid, 'Werkstatt');
  assert.equal(settings.wifiOn, false);
  assert.equal(settings.wifiPassword, '');
});

test('countryFor nimmt das Land der Zeitzone, sonst das der Sprache', () => {
  const d = Object.assign({}, DEFAULTS, { wifi_country: 'GB', zone_countries: { 'Europe/Berlin': 'DE', 'Europe/Zurich': 'CH' } });
  assert.equal(M.countryFor('Europe/Zurich', d), 'CH');
  assert.equal(M.countryFor('UTC', d), 'GB');
  assert.equal(M.countryFor('UTC', {}), '');
  assert.equal(M.defaultSettings(d, 'pi').country, 'DE');
});

test('remembered merkt sich kein WLAN-Land', () => {
  assert.equal('country' in M.remembered(valid()), false);
});

test('fiveGhzOnly warnt nur beim erkannten 5-GHz-Netz und alten Geraeten', () => {
  const d = Object.assign({}, DEFAULTS, { wifi_ssid: 'Haus', wifi_freq_mhz: 5500 });
  assert.equal(M.fiveGhzOnly('pi3', 'Haus', d), true);
  assert.equal(M.fiveGhzOnly('pi4', 'Haus', d), false);
  assert.equal(M.fiveGhzOnly('pi3', 'Anderes', d), false);
  assert.equal(M.fiveGhzOnly('pi3', 'Haus', Object.assign({}, d, { wifi_freq_mhz: 2437 })), false);
});

const NETS = [
  { ssid: 'Haus', signal: 80, band_24: true, band_5: true },
  { ssid: 'Haus 5G', signal: 60, band_24: false, band_5: true },
  { ssid: 'Nachbar', signal: 30, band_24: true, band_5: false },
];

test('fiveGhzOnly nutzt die Baender der Netze in Reichweite', () => {
  const d = Object.assign({}, DEFAULTS, { wifi_networks: NETS });
  assert.equal(M.fiveGhzOnly('pi3', 'Haus 5G', d), true);
  assert.equal(M.fiveGhzOnly('pi3', 'Haus', d), false);
  assert.equal(M.fiveGhzOnly('pi5', 'Haus 5G', d), false);
  assert.equal(M.fiveGhzOnly('pi3', '', d), false);
});

test('networkRows zeigt sechs Netze, das gewaehlte immer, mit Balken und 5-GHz-Kennung', () => {
  const nets = [];
  for (let i = 0; i < 8; i++) {
    nets.push({ ssid: 'N' + i, signal: 90 - i * 10, band_24: i !== 1, band_5: i === 1 });
  }
  const d = Object.assign({}, DEFAULTS, { wifi_networks: nets });
  const rows = plain(M.networkRows(d, 'N7'));
  assert.deepEqual(rows.map((r) => r.ssid), ['N0', 'N1', 'N2', 'N3', 'N4', 'N7']);
  assert.equal(rows[1].only5, true);
  assert.equal(rows[0].level, 3);
  assert.equal(rows[5].level, 1);
  assert.deepEqual(plain(M.networkRows({}, 'x')), []);
});

test('mergeNetworks laesst bekannte Netze an ihrem Platz', () => {
  const old = [{ ssid: 'A', signal: 10 }, { ssid: 'B', signal: 20 }, { ssid: 'Weg', signal: 30 }];
  const fresh = [{ ssid: 'C', signal: 90 }, { ssid: 'B', signal: 80 }, { ssid: 'A', signal: 70 }];
  assert.deepEqual(plain(M.mergeNetworks(old, fresh)), [
    { ssid: 'A', signal: 70 }, { ssid: 'B', signal: 80 }, { ssid: 'C', signal: 90 },
  ]);
});

test('siblingFor findet dasselbe Netz auf 2,4 GHz', () => {
  const d = Object.assign({}, DEFAULTS, { wifi_networks: [
    { ssid: 'WelcomeHome 5 GHz', signal: 70, band_24: false, band_5: true },
    { ssid: 'WelcomeHome', signal: 60, band_24: true, band_5: false },
    { ssid: 'Haus_5G', signal: 50, band_24: false, band_5: true },
    { ssid: 'Haus-2.4GHz', signal: 40, band_24: true, band_5: false },
    { ssid: 'Nur5 5G', signal: 30, band_24: false, band_5: true },
  ] });
  assert.equal(M.siblingFor('WelcomeHome 5 GHz', d), 'WelcomeHome');
  assert.equal(M.siblingFor('Haus_5G', d), 'Haus-2.4GHz');
  assert.equal(M.siblingFor('Nur5 5G', d), '');
  assert.equal(M.siblingFor('', d), '');
});
