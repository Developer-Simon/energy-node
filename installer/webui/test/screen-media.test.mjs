import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { mountScreen, realCatalog } from './helpers/mount.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));
const DOCS_URL = 'https://developer-simon.github.io/energy-node/installer.html';
const DISCOVER = 'appstream://org.raspberrypi.rpi-imager';
const KEY = 'energy-node-installer.media';

const IMAGE = { name: 'Raspberry Pi OS Lite', arch: 'arm64', download_size: 480 * 1024 * 1024, extract_size: 1200 * 1024 * 1024, cached: false, release_date: '2026-10-01' };
const STATUS = {
  supported: true, platform: 'linux',
  imager: { found: true, kind: 'flatpak', version: '2.0.1', too_old: false },
  links: [{ kind: 'discover', url: DISCOVER }, { kind: 'website', url: 'https://www.raspberrypi.com/software/' }],
};
const DEFAULTS = { hostname: 'energy-node', user: 'node', timezone: 'Europe/Berlin', keyboard: 'de', wifi_country: 'DE', timezones: ['Europe/Berlin', 'Europe/Wien'], keyboards: ['de', 'us'] };
const CATALOG = {
  devices: [
    { id: 'pi5', group: 'pi', experimental: true, image: IMAGE },
    { id: 'pi4', group: 'pi', experimental: true, image: IMAGE },
    { id: 'pi3', group: 'pi', experimental: false, image: IMAGE },
    { id: 'pi1', group: 'pi', experimental: false, image: null },
  ],
  offline: false,
  defaults: DEFAULTS,
};
const NAME_FREE = { valid: true, taken: false, suggestion: '' };

// mountMedia: wie mountScreen, mit dem Medium-Stand der Shell (shared.media).
function mountMedia(file, factory, options = {}) {
  const shell = { screen: options.screen, shared: { media: { device: null, settings: null, drive: null, confirm: '' } } };
  return mountScreen(file, factory, Object.assign({ scripts: ['media-model.js'], catalog: realCatalog('de'), shell }, options));
}

function device(responses = {}, options = {}) {
  const mounted = mountMedia('screen-media.js', 'screenMediaDevice', {
    screen: 'media-device',
    responses: Object.assign({ 'GET /api/media/status': STATUS, 'GET /api/media/catalog': CATALOG }, responses),
    ...options,
  });
  mounted.shell.openExternal = (url) => mounted.opened.push(url);
  return mounted;
}

function settings(responses = {}, options = {}) {
  return mountMedia('screen-media.js', 'screenMediaSettings', {
    screen: 'media-settings',
    responses: Object.assign({ 'GET /api/media/catalog': CATALOG, 'GET /api/media/name': NAME_FREE, 'GET /api/media/wifi-networks': [] }, responses),
    ...options,
  });
}

// Eine Fassung, die alle Pflichtfelder fuellt.
function fill(screen) {
  screen.form.hostname = 'werkstatt';
  screen.form.user = 'node';
}

test('ohne Imager erscheint die Fehlerkarte mit Discover-Link', async () => {
  const status = Object.assign({}, STATUS, { imager: { found: false, kind: '', version: '', too_old: false } });
  const { screen, shell, opened } = device({ 'GET /api/media/status': status });
  await screen.init();

  assert.equal(screen.state, 'missing');
  assert.equal(screen.imagerKey, 'missing');
  screen.openLink(screen.links[0]);
  assert.deepEqual(opened, [DISCOVER]);
  assert.equal(shell.screen, 'media-device', 'die Karte bleibt auf dem Bildschirm');
});

test('ein zu alter Imager zeigt die Karte mit dem Hinweis auf die Version', async () => {
  const status = Object.assign({}, STATUS, { imager: { found: true, kind: 'flatpak', version: '1.9.0', too_old: true } });
  const { screen } = device({ 'GET /api/media/status': status });
  await screen.init();
  assert.equal(screen.state, 'missing');
  assert.equal(screen.imagerKey, 'old');
});

test('nicht Linux zeigt nicht verfuegbar und oeffnet die Anleitung', async () => {
  const status = Object.assign({}, STATUS, { supported: false, platform: 'windows' });
  const { screen, opened } = device({ 'GET /api/media/status': status });
  await screen.init();

  assert.equal(screen.state, 'unsupported');
  screen.openDocs();
  assert.deepEqual(opened, [DOCS_URL]);
});

test('nur fehlende Tests sind markiert', async () => {
  const { screen } = device();
  await screen.init();

  const pi1 = screen.devices.find((item) => item.id === 'pi1');
  const pi4 = screen.devices.find((item) => item.id === 'pi4');
  assert.equal(screen.tagFor(pi1), '');
  assert.equal(screen.tagFor(pi4), 'experimentell');
});

test('ohne Image bleibt Weiter gesperrt und der Hinweis erscheint', async () => {
  const { screen, shell } = device();
  await screen.init();

  screen.pick('pi1');
  assert.equal(screen.canContinue, false);
  assert.equal(screen.summary.offline, true);
  screen.next();
  assert.equal(shell.screen, 'media-device');
  assert.equal(shell.shared.media.device, null);
});

test('Weiter merkt sich das Geraet und geht zu den Einstellungen', async () => {
  const { screen, shell } = device();
  await screen.init();

  screen.pick('pi3');
  assert.equal(screen.summary.name, 'Raspberry Pi OS Lite');
  screen.next();
  assert.equal(shell.shared.media.device, 'pi3');
  assert.equal(shell.screen, 'media-settings');
});

test('die Liste erinnert sich an das zuletzt gewaehlte Geraet', async () => {
  const { screen, shell } = device();
  shell.shared.media.device = 'pi5';
  await screen.init();
  assert.equal(screen.picked, 'pi5');
});

test('Einstellungen kommen vom Rechner und aus dem Gedaechtnis', async () => {
  const { screen, window } = settings({ 'GET /api/media/name': NAME_FREE });
  window.localStorage.setItem(KEY, JSON.stringify({ hostname: 'werkstatt' }));
  await screen.init();

  assert.equal(screen.form.hostname, 'werkstatt');
  assert.equal(screen.form.timezone, 'Europe/Berlin');
  assert.equal(screen.form.keyboard, 'de');
  assert.equal(screen.form.emergency, false);
});

test('ein vergebener Name bietet einen Vorschlag an', async () => {
  const taken = { valid: true, taken: true, suggestion: 'energy-node-2' };
  const { screen } = settings({ 'GET /api/media/name': taken });
  await screen.init();

  screen.form.hostname = 'energy-node';
  await screen.checkName();
  assert.equal(screen.nameState, 'taken');
  assert.equal(screen.suggestion, 'energy-node-2');

  await screen.useSuggestion();
  assert.equal(screen.form.hostname, 'energy-node-2');
});

test('ein ungueltiger Name wird lokal abgelehnt, ohne Anfrage', async () => {
  const { screen, calls } = settings();
  await screen.init();
  const before = calls.filter((call) => call.key === 'GET /api/media/name').length;

  screen.form.hostname = 'Energy';
  await screen.checkName();
  assert.equal(screen.nameState, 'invalid');
  assert.equal(calls.filter((call) => call.key === 'GET /api/media/name').length, before);
});

test('ein freier Name meldet sich als gueltig', async () => {
  const { screen } = settings();
  await screen.init();
  screen.form.hostname = 'werkstatt';
  await screen.checkName();
  assert.equal(screen.nameState, 'ok');
});

test('Weiter bleibt gesperrt, solange der Name ungueltig ist', async () => {
  const { screen, shell } = settings();
  await screen.init();
  screen.form.hostname = 'Werkstatt';
  assert.equal(screen.canContinue, false);
  screen.next();
  assert.equal(shell.screen, 'media-settings');
});

test('Passwoerter landen nicht im Gedaechtnis', async () => {
  const { screen, shell, window } = settings();
  await screen.init();
  fill(screen);
  screen.form.emergency = true;
  screen.form.password = 'geheim-geheim';
  screen.form.password2 = 'geheim-geheim';
  screen.form.wifiOn = true;
  screen.form.ssid = 'Haus';
  screen.form.wifiPassword = 'wlan-geheim';
  screen.form.country = 'DE';

  screen.next();
  assert.equal(shell.screen, 'media-drive');
  assert.ok(!window.localStorage.getItem(KEY).includes('geheim'));
  assert.equal(shell.shared.media.settings.password, 'geheim-geheim', 'im Speicher der Sitzung bleibt es');
});

test('Zurueck fuehrt zum Geraet zurueck', async () => {
  const { screen, shell } = settings();
  await screen.init();
  screen.back();
  assert.equal(shell.screen, 'media-device');
  assert.equal(shell.dir, 'back');
});

test('die Namensprobe wartet beim Tippen 400 ms', async () => {
  const { screen, calls } = settings();
  await screen.init();
  const count = () => calls.filter((call) => call.key === 'GET /api/media/name').length;
  const before = count();

  screen.form.hostname = 'werkstat';
  screen.onNameInput();
  assert.equal(count(), before, 'sofort noch keine Anfrage');
  await new Promise((resolve) => setTimeout(resolve, 450));
  assert.equal(count(), before + 1);
  assert.equal(screen.nameState, 'ok');
});

test('die Vorgaben fuer Zeitzone und Tastatur stammen aus dem Katalog', async () => {
  const { screen } = settings();
  await screen.init();
  assert.deepEqual(plain(screen.timezones), ['Europe/Berlin', 'Europe/Wien']);
  assert.deepEqual(plain(screen.keyboards), ['de', 'us']);
});

// Laufwerke: die Antworten je Abruf, Stand der Vorgaben aus dem Katalog.
const SD = { path: '/dev/sdb', model: 'SanDisk Ultra', size: 32 * 1024 * 1024 * 1024, transport: 'usb', removable: true, partitions: [{ name: 'bootfs', label: 'bootfs', fstype: 'vfat', used: 100 * 1024 * 1024 }] };
const SSD_DRIVE = { path: '/dev/sda', model: 'Samsung SSD 870', size: 500 * 1024 * 1024 * 1024, transport: 'sata', removable: false, partitions: [] };
const TINY = { path: '/dev/sdd', model: 'Winzig', size: 1024 * 1024 * 1024, transport: 'usb', removable: true, partitions: [] };

function drive(drives, responses = {}, options = {}) {
  const mounted = mountMedia('screen-media.js', 'screenMediaDrive', {
    screen: 'media-drive',
    responses: Object.assign({ 'GET /api/media/catalog': CATALOG, 'GET /api/media/drives': drives }, responses),
    ...options,
  });
  mounted.shell.shared.media.device = 'pi3';
  return mounted;
}

// urlsOf zeichnet die vollen Adressen der Abrufe auf, mit Query.
function urlsOf(window) {
  const urls = [];
  const original = window.fetch;
  window.fetch = (url, init) => {
    urls.push(String(url));
    return original(url, init);
  };
  return urls;
}

test('interne Laufwerke erst mit dem Schalter', async (t) => {
  const { screen, window } = drive([SD]);
  t.after(() => screen.destroy());
  const urls = urlsOf(window);
  await screen.init();
  assert.ok(urls.some((url) => url.endsWith('/api/media/drives')), 'der erste Abruf ohne internal');
  assert.ok(!urls.some((url) => url.includes('internal=1')));

  await screen.setInternal(true);
  assert.ok(urls.some((url) => url.endsWith('/api/media/drives?internal=1')));
});

test('ohne Schalter bleiben interne Laufwerke aus der Liste', async (t) => {
  const { screen } = drive([SD, SSD_DRIVE]);
  t.after(() => screen.destroy());
  await screen.init();
  assert.deepEqual(plain(screen.visible.map((item) => item.path)), ['/dev/sdb']);
  await screen.setInternal(true);
  assert.deepEqual(plain(screen.visible.map((item) => item.path)), ['/dev/sdb', '/dev/sda']);
});

test('ein internes Laufwerk braucht den abgetippten Namen', async (t) => {
  const { screen, shell } = drive([SSD_DRIVE]);
  t.after(() => screen.destroy());
  await screen.init();
  await screen.setInternal(true);
  screen.pick('/dev/sda');
  assert.equal(screen.selected, '/dev/sda');

  screen.checked = true;
  assert.equal(screen.canErase, false, 'das Haekchen allein reicht nicht');
  screen.typed = 'Samsung SSD 870';
  assert.equal(screen.canErase, true);

  screen.typed = 'samsung ssd 870';
  assert.equal(screen.canErase, false);
  assert.equal(shell.shared.media.drive.path, '/dev/sda');
});

test('ein Wechselmedium braucht nur das Haekchen', async (t) => {
  const { screen, shell } = drive([SD]);
  t.after(() => screen.destroy());
  await screen.init();
  screen.pick('/dev/sdb');
  assert.equal(screen.canErase, false);
  screen.checked = true;
  assert.equal(screen.canErase, true);
  assert.equal(shell.shared.media.drive.path, '/dev/sdb');
});

test('ein zu kleines Laufwerk laesst sich nicht waehlen', async (t) => {
  const { screen } = drive([TINY, SD]);
  t.after(() => screen.destroy());
  await screen.init();
  assert.equal(screen.fits(TINY), false);
  screen.pick('/dev/sdd');
  assert.equal(screen.selected, null);
});

test('ein abgezogenes Laufwerk hebt die Auswahl auf', async (t) => {
  const mounted = drive([SD, TINY]);
  const { screen, state } = mounted;
  t.after(() => screen.destroy());
  await screen.init();
  screen.pick('/dev/sdb');
  screen.checked = true;

  state.responses['GET /api/media/drives'] = [TINY];
  await screen.load();
  assert.equal(screen.selected, null);
  assert.equal(screen.checked, false);
  assert.equal(mounted.shell.shared.media.drive, null);
});

test('Weiter schreibt das Laufwerk in den Schritt und geht zum Schreiben', async (t) => {
  const { screen, shell } = drive([SD]);
  t.after(() => screen.destroy());
  await screen.init();
  screen.pick('/dev/sdb');
  screen.checked = true;
  screen.erase();
  assert.equal(shell.shared.media.confirm, 'yes');
  assert.equal(shell.shared.media.target, 'card');
  assert.equal(shell.screen, 'media-write');
});

test('jedes Laufwerk traegt seine Etikette', async (t) => {
  const { screen } = drive([SD, SSD_DRIVE]);
  t.after(() => screen.destroy());
  await screen.init();
  await screen.setInternal(true);
  assert.equal(screen.labelFor(SD), 'Wechselmedium');
  assert.equal(screen.labelFor(SSD_DRIVE), 'intern');
});

test('die Einstiegsschalter sind auf allen Medium-Bildschirmen gestaltet', async () => {
  const { default: fs } = await import('node:fs');
  const { default: path } = await import('node:path');
  const { fileURLToPath } = await import('node:url');
  const here = path.dirname(fileURLToPath(import.meta.url));
  const css = fs.readFileSync(path.join(here, '..', 'static', 'css', 'screens.css'), 'utf8');
  for (const rule of ['.mode{', '.mode-i{', '.mode-i.on{']) {
    assert.ok(css.includes(`.app[data-screen^="media-"] ${rule}`), `Regel fehlt: ${rule}`);
  }
});

// Schreiben und Fertig. Die Zeitgeber fuellt ein Test selbst: der Bildschirm
// startet sie in init(), fire() ruft sie auf, und destroy() muss sie alle
// wieder entfernen (timers.active() bleibt dann bei 0).
function fakeTimers(window) {
  const timers = new Map();
  let next = 1;
  window.setInterval = (fn, ms) => {
    const id = next++;
    timers.set(id, { fn, ms });
    return id;
  };
  window.clearInterval = (id) => {
    timers.delete(id);
  };
  return {
    active: () => timers.size,
    intervals: () => [...timers.values()].map((item) => item.ms),
    fire: (ms) => {
      for (const item of [...timers.values()]) {
        if (item.ms === ms) {
          item.fn();
        }
      }
    },
  };
}

const TARGET = { host: 'werkstatt.local', user: 'node', key_path: '/home/dev/.energy-node/id_ed25519' };
const SETTINGS = { hostname: 'werkstatt', user: 'node', emergency: false, password: '', password2: '', timezone: 'Europe/Berlin', keyboard: 'de', wifiOn: false, ssid: '', wifiPassword: '', country: 'DE' };
const CARD = { path: '/dev/sdb', model: 'SanDisk Ultra', size: 32 * 1024 * 1024 * 1024, transport: 'usb', removable: true, partitions: [] };

function job(overrides = {}) {
  return Object.assign({
    state: 'running',
    steps: [{ id: 'download', state: 'done' }, { id: 'write', state: 'active' }, { id: 'verify', state: 'pending' }],
    percent: 40, bytes_done: 40, bytes_total: 100, rate: 20, error: null, log: ['Lade Image'], target: null,
  }, overrides);
}

function writer(responses = {}) {
  const saved = [];
  const mounted = mountMedia('screen-media.js', 'screenMediaWrite', {
    screen: 'media-write',
    responses: Object.assign({ 'POST /api/media/write': {}, 'GET /api/media/job': job(), 'POST /api/media/cancel': {} }, responses),
  });
  mounted.shell.shared.media.device = 'pi3';
  mounted.shell.shared.media.drive = Object.assign({}, CARD);
  mounted.shell.shared.media.settings = Object.assign({}, SETTINGS);
  mounted.shell.shared.media.confirm = 'yes';
  mounted.shell.saveFile = (name, content) => saved.push({ name, content });
  return Object.assign(mounted, { timers: fakeTimers(mounted.window), saved });
}

function done(responses = {}) {
  const setups = [];
  const mounted = mountMedia('screen-media.js', 'screenMediaDone', {
    screen: 'media-done',
    responses: Object.assign({ 'GET /api/media/reachable': { reachable: false, key_matches: false } }, responses),
  });
  mounted.shell.shared.media.target = Object.assign({}, TARGET);
  mounted.shell.shared.media.settings = Object.assign({}, SETTINGS);
  mounted.shell.shared.media.drive = Object.assign({}, CARD);
  mounted.shell.openSetup = (target) => setups.push(target);
  return Object.assign(mounted, { timers: fakeTimers(mounted.window), setups });
}

test('Schreiben schickt die Bestaetigung und sperrt den Umschalter', async (t) => {
  const { screen, shell, calls, timers } = writer();
  t.after(() => screen.destroy());
  await screen.init();

  assert.equal(calls[0].key, 'POST /api/media/write');
  assert.equal(calls[0].body.confirm, 'yes');
  assert.equal(calls[0].body.drive, '/dev/sdb');
  assert.equal(calls[0].body.settings.wifi, null);
  assert.equal(shell.mutating, true);
  assert.deepEqual(timers.intervals(), [500], 'die Jobabfrage laeuft alle 500 ms');
});

test('Fehler beim Schreiben landet im Banner', async (t) => {
  const failed = job({ state: 'failed', error: { code: 'MEDIA_WRITE_FAILED', detail: '' } });
  const { screen, shell, timers } = writer({ 'GET /api/media/job': failed });
  t.after(() => screen.destroy());
  await screen.init();
  await screen.poll();

  assert.equal(shell.error.code, 'MEDIA_WRITE_FAILED');
  assert.equal(shell.mutating, false);
  assert.equal(screen.failed, true);
  assert.equal(timers.active(), 0);
});

test('Restzeit und Schritte kommen aus dem Auftrag', async (t) => {
  const { screen } = writer();
  t.after(() => screen.destroy());
  await screen.init();

  assert.match(screen.remainingText, /00:03/, 'noch 60 Byte bei 20 Byte je Sekunde');
  assert.equal(screen.percent, 40);
  assert.deepEqual(plain(screen.rows.map((row) => row.state)), ['done', 'active', 'pending']);
});

test('ein Erfolg uebergibt das Ziel und geht zu Fertig', async (t) => {
  const { screen, shell, timers } = writer({ 'GET /api/media/job': job({ state: 'done', target: TARGET }) });
  t.after(() => screen.destroy());
  await screen.init();
  await screen.poll();

  assert.equal(shell.screen, 'media-done');
  assert.deepEqual(plain(shell.shared.media.target), TARGET);
  assert.equal(shell.mutating, false);
  assert.equal(timers.active(), 0);
});

test('Abbrechen schickt POST /api/media/cancel', async (t) => {
  const { screen, calls } = writer();
  t.after(() => screen.destroy());
  await screen.init();
  await screen.cancel();
  assert.equal(calls.at(-1).key, 'POST /api/media/cancel');
});

test('Protokoll speichern gibt die Zeilen an saveFile', async (t) => {
  const { screen, saved } = writer({ 'GET /api/media/job': job({ log: ['eins', 'zwei'] }) });
  t.after(() => screen.destroy());
  await screen.init();
  screen.save();
  assert.deepEqual(saved, [{ name: 'energy-node-media.log', content: 'eins\nzwei' }]);
});

test('Wiederholen startet neu und raeumt den Banner', async (t) => {
  const { screen, shell, calls, state } = writer({ 'GET /api/media/job': job({ state: 'failed', error: { code: 'MEDIA_WRITE_FAILED', detail: '' } }) });
  t.after(() => screen.destroy());
  await screen.init();
  await screen.poll();
  assert.equal(shell.error.code, 'MEDIA_WRITE_FAILED');

  state.responses['GET /api/media/job'] = job();
  await screen.retry();
  assert.equal(shell.error, null);
  assert.equal(calls.filter((call) => call.key === 'POST /api/media/write').length, 2);
});

test('Zurueck nach einem Fehler geht zum Laufwerk', async (t) => {
  const { screen, shell, timers } = writer({ 'GET /api/media/job': job({ state: 'failed', error: { code: 'MEDIA_WRITE_FAILED', detail: '' } }) });
  t.after(() => screen.destroy());
  await screen.init();
  await screen.poll();
  screen.back();
  assert.equal(shell.screen, 'media-drive');
  assert.equal(shell.dir, 'back');
  assert.equal(timers.active(), 0);
});

test('Fertig: der Fund bei passendem Schluessel, die Warnung bei fremdem', async (t) => {
  const { screen, state, setups } = done({ 'GET /api/media/reachable': { reachable: true, key_matches: false } });
  t.after(() => screen.destroy());
  await screen.init();
  assert.equal(screen.foreign, true);
  assert.equal(screen.found, false);

  state.responses['GET /api/media/reachable'] = { reachable: true, key_matches: true };
  await screen.check();
  assert.equal(screen.found, true);
  assert.equal(screen.foreign, false);

  screen.toSetup();
  assert.deepEqual(plain(setups[0]), { host: 'werkstatt.local', user: 'node', keyPath: '/home/dev/.energy-node/id_ed25519' });
});

test('Weiter zum Einrichten ist auch vor dem Fund klickbar', async (t) => {
  const { screen, setups } = done();
  t.after(() => screen.destroy());
  await screen.init();
  assert.equal(screen.found, false);
  screen.toSetup();
  assert.equal(setups.length, 1);
});

test('nach zehn Minuten erscheint die Karte, und die IP-Adresse ersetzt den Namen', async (t) => {
  const { screen, window } = done();
  t.after(() => screen.destroy());
  await screen.init();
  const urls = urlsOf(window);

  screen.tick();
  assert.equal(screen.slow, false, 'vor zehn Minuten keine Karte');
  screen.startedAt -= 10 * 60 * 1000;
  screen.tick();
  assert.equal(screen.slow, true);

  screen.ip = '192.168.1.50';
  await screen.check();
  assert.ok(urls.at(-1).endsWith('/api/media/reachable?host=192.168.1.50'), urls.at(-1));
});

test('Weiteres Medium geht zur Geraeteauswahl und behaelt die Einstellungen', async (t) => {
  const { screen, shell } = done();
  t.after(() => screen.destroy());
  await screen.init();
  screen.another();
  assert.equal(shell.screen, 'media-device');
  assert.deepEqual(plain(shell.shared.media.settings), SETTINGS);
  assert.equal(shell.shared.media.drive, null);
});

test('Fertig hat Uhr und Abfrage und gibt beides beim Verlassen frei', async (t) => {
  const { screen, state, timers, calls } = done();
  t.after(() => screen.destroy());
  await screen.init();
  assert.deepEqual(timers.intervals(), [1000, 5000]);

  const before = calls.filter((call) => call.key === 'GET /api/media/reachable').length;
  timers.fire(5000);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls.filter((call) => call.key === 'GET /api/media/reachable').length, before + 1);

  screen.toSetup();
  assert.equal(timers.active(), 0);
  assert.ok(state);
});

const DETECTED = Object.assign({}, DEFAULTS, { wifi_ssid: 'Werkstatt' });

test('das WLAN des Rechners wird vorgeschlagen, Schalter und Passwort bleiben leer', async () => {
  const { screen } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: DETECTED }) });
  await screen.init();
  assert.equal(screen.form.ssid, 'Werkstatt');
  assert.equal(screen.form.wifiOn, false);
  assert.equal(screen.form.wifiPassword, '');
});

test('ein gemerkter WLAN-Name gewinnt vor dem erkannten', async () => {
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: DETECTED }) });
  window.localStorage.setItem(KEY, JSON.stringify({ ssid: 'Haus' }));
  await screen.init();
  assert.equal(screen.form.ssid, 'Haus');
});

test('ein leerer gemerkter WLAN-Name laesst den erkannten stehen', async () => {
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: DETECTED }) });
  window.localStorage.setItem(KEY, JSON.stringify({ ssid: '' }));
  await screen.init();
  assert.equal(screen.form.ssid, 'Werkstatt');
});

test('die Erkennung speichert kein WLAN-Passwort', async () => {
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: DETECTED }) });
  await screen.init();
  fill(screen);
  screen.next();
  const stored = window.localStorage.getItem(KEY);
  assert.ok(!stored.includes('wifiPassword'));
  assert.equal(JSON.parse(stored).ssid, 'Werkstatt');
});

const ZONED = Object.assign({}, DEFAULTS, { wifi_country: 'GB', zone_countries: { 'Europe/Berlin': 'DE', 'Europe/Wien': 'AT' } });

test('das WLAN-Land kommt aus der Zeitzone, nicht aus einem gemerkten Wert', async () => {
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: ZONED }) });
  window.localStorage.setItem(KEY, JSON.stringify({ timezone: 'Europe/Wien', country: 'GB' }));
  await screen.init();
  assert.equal(screen.form.country, 'AT');
  assert.equal(screen.countryKnown, true);
});

test('ohne Land aus Zeitzone und Sprache fragt das Formular danach', async () => {
  const bare = Object.assign({}, DEFAULTS, { wifi_country: '', zone_countries: {} });
  const { screen } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: bare }) });
  await screen.init();
  assert.equal(screen.countryKnown, false);
});

test('das WLAN-Passwort wird mit dem des Rechners verglichen', async () => {
  const answers = { 'POST /api/media/wifi-check': (body) => ({ result: body.password === 'richtig-123' ? 'match' : 'mismatch' }) };
  const { screen, calls } = settings(answers);
  await screen.init();
  screen.form.wifiOn = true;
  screen.form.ssid = 'Werkstatt';
  screen.form.wifiPassword = 'richtig-123';
  await screen.checkWifi();
  assert.equal(screen.wifiState, 'match');
  screen.form.wifiPassword = 'falsch-123';
  await screen.checkWifi();
  assert.equal(screen.wifiState, 'mismatch');
  const call = calls.find((c) => c.key === 'POST /api/media/wifi-check');
  assert.deepEqual(call.body, { ssid: 'Werkstatt', password: 'richtig-123' });
});

test('ohne WLAN-Schalter oder Passwort fragt der Abgleich nicht', async () => {
  const { screen, calls } = settings({ 'POST /api/media/wifi-check': { result: 'match' } });
  await screen.init();
  screen.form.ssid = 'Werkstatt';
  screen.form.wifiPassword = 'richtig-123';
  await screen.checkWifi();
  screen.form.wifiOn = true;
  screen.form.wifiPassword = '';
  await screen.checkWifi();
  assert.equal(screen.wifiState, '');
  assert.equal(calls.filter((c) => c.key === 'POST /api/media/wifi-check').length, 0);
});

test('beim erkannten 5-GHz-Netz und einem Pi 3 erscheint die Warnung', async () => {
  const five = Object.assign({}, DEFAULTS, { wifi_ssid: 'Haus', wifi_freq_mhz: 5500 });
  const { screen, shell } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: five }) });
  shell.shared.media.device = 'pi3';
  await screen.init();
  assert.equal(screen.fiveGhzOnly, true);
  shell.shared.media.device = 'pi5';
  assert.equal(screen.fiveGhzOnly, false);
});

const NEAR = Object.assign({}, DEFAULTS, { wifi_ssid: 'Haus 5 GHz', wifi_freq_mhz: 5500, wifi_networks: [
  { ssid: 'Haus 5 GHz', signal: 80, band_24: false, band_5: true },
  { ssid: 'Nachbar', signal: 30, band_24: true, band_5: false },
] });

test('ein Netz aus der Liste wird per Klick gewaehlt, die Reihenfolge bleibt', async () => {
  const { screen } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: NEAR }) });
  await screen.init();
  assert.equal(screen.manual, false);
  screen.pickNetwork('Nachbar');
  assert.equal(screen.form.ssid, 'Nachbar');
  assert.deepEqual(screen.rows.map((r) => r.ssid), ['Haus 5 GHz', 'Nachbar']);
});

test('Anderes Netz leert einen Namen aus der Liste und zeigt das Feld', async () => {
  const { screen } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: NEAR }) });
  await screen.init();
  assert.equal(screen.showInput, false);
  screen.pickOther();
  assert.equal(screen.manual, true);
  assert.equal(screen.form.ssid, '');
  assert.equal(screen.showInput, true);
});

test('ein gemerkter Name ausserhalb der Liste ist ein getipptes Netz', async () => {
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: NEAR }) });
  window.localStorage.setItem(KEY, JSON.stringify({ ssid: 'Versteckt' }));
  await screen.init();
  assert.equal(screen.manual, true);
  assert.equal(screen.form.ssid, 'Versteckt');
});

test('der Suchlauf haengt neue Netze hinten an und waehlt ein getipptes aus der Liste', async () => {
  const scanned = [
    { ssid: 'Versteckt', signal: 90, band_24: true, band_5: false },
    { ssid: 'Haus 5 GHz', signal: 80, band_24: false, band_5: true },
    { ssid: 'Haus', signal: 75, band_24: true, band_5: false },
  ];
  const { screen, window } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: NEAR }), 'GET /api/media/wifi-networks': scanned });
  window.localStorage.setItem(KEY, JSON.stringify({ ssid: 'Versteckt' }));
  await screen.init();
  await screen.loadNetworks();
  assert.equal(screen.scanning, false);
  assert.deepEqual(screen.rows.map((r) => r.ssid), ['Haus 5 GHz', 'Versteckt', 'Haus']);
  assert.equal(screen.manual, false);
});

test('beim 5-GHz-Netz und einem Pi 3 bietet die Warnung das 2,4-GHz-Netz an', async () => {
  const twin = Object.assign({}, NEAR, { wifi_networks: NEAR.wifi_networks.concat([{ ssid: 'Haus', signal: 75, band_24: true, band_5: false }]) });
  const { screen, shell } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: twin }) });
  shell.shared.media.device = 'pi3';
  await screen.init();
  screen.form.ssid = 'Haus 5 GHz';
  assert.equal(screen.sibling, 'Haus');
  screen.pickNetwork(screen.sibling);
  assert.equal(screen.form.ssid, 'Haus');
  assert.equal(screen.sibling, '');
});

test('der Abgleich nennt das Netz auf dem anderen Band', async () => {
  const { screen } = settings({ 'POST /api/media/wifi-check': { result: 'match', via: 'Haus' } });
  await screen.init();
  screen.form.wifiOn = true;
  screen.form.ssid = 'Haus 5 GHz';
  screen.form.wifiPassword = 'richtig-123';
  await screen.checkWifi();
  assert.equal(screen.wifiState, 'match');
  assert.equal(screen.wifiVia, 'Haus');
});

test('kennt der Rechner kein Passwort, sagt der Abgleich das', async () => {
  const { screen } = settings({ 'POST /api/media/wifi-check': { result: 'unknown', via: '' } });
  await screen.init();
  screen.form.wifiOn = true;
  screen.form.ssid = 'Fremd';
  screen.form.wifiPassword = 'egal-1234';
  await screen.checkWifi();
  assert.equal(screen.wifiState, 'unknown');
  assert.equal(screen.wifiVia, '');
});

test('die Region oeffnet eine Liste zur Zeit und schliesst sie bei der Wahl', async () => {
  const { screen } = settings();
  await screen.init();
  screen.togglePicker('zone');
  assert.equal(screen.picker, 'zone');
  screen.togglePicker('keyboard');
  assert.equal(screen.picker, 'keyboard');
  screen.pickKeyboard('us');
  assert.equal(screen.form.keyboard, 'us');
  assert.equal(screen.picker, '');
  screen.togglePicker('zone');
  screen.zoneQuery = 'wien';
  screen.pickZone('Europe/Wien');
  assert.equal(screen.form.timezone, 'Europe/Wien');
  assert.equal(screen.picker, '');
  assert.equal(screen.zoneSummary.startsWith('Wien'), true);
});

// Debian: Geraete der Gruppe debian neben den Pi. deb-i386 ist nicht unterstuetzt.
const DEBIAN_CATALOG = Object.assign({}, CATALOG, {
  devices: [
    { id: 'pi3', group: 'pi', experimental: false, image: IMAGE },
    { id: 'deb-amd64', group: 'debian', experimental: false, image: IMAGE },
    { id: 'deb-arm64', group: 'debian', experimental: true, image: IMAGE },
    { id: 'deb-i386', group: 'debian', experimental: false, unsupported: true, image: null },
  ],
});

// mediaTemplate: die Vorlage screen-media.html als DOM, ohne die Platzhalter der Go-Vorlage.
// Alpine laeuft in den Tests nicht, darum prueft der Test die Bedingungen der x-if-Knoten.
function mediaTemplate() {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const html = fs.readFileSync(path.join(here, '..', 'templates', 'screen-media.html'), 'utf8').replace(/\{\{[\s\S]*?\}\}/g, '');
  return new JSDOM(`<!doctype html><body>${html}</body>`).window.document;
}

// templateNodes: alle template-Knoten, die aeusseren vor den inneren.
function templateNodes(doc) {
  const out = [];
  const walk = (root) => {
    root.querySelectorAll('template').forEach((node) => {
      out.push(node);
      walk(node.content);
    });
  };
  walk(doc);
  return out;
}

// guardOf: der innerste x-if-Knoten, der das Element enthaelt.
function guardOf(doc, selector) {
  return templateNodes(doc).filter((node) => node.content.querySelector(selector)).at(-1);
}

function debianSettings(device, responses = {}) {
  const mounted = settings(Object.assign({ 'GET /api/media/catalog': DEBIAN_CATALOG }, responses));
  mounted.shell.shared.media.device = device;
  return mounted;
}

test('Debian: die Geraeteliste hat zwei Gruppen, i386 ist ausgegraut und nicht waehlbar', async () => {
  const { screen } = device({ 'GET /api/media/catalog': DEBIAN_CATALOG });
  await screen.init();
  assert.deepEqual(plain(screen.groups.map((group) => group.id)), ['pi', 'debian']);

  const i386 = screen.devices.find((item) => item.id === 'deb-i386');
  assert.equal(screen.tagFor(i386), 'nicht unterstützt');
  screen.pick('deb-i386');
  assert.equal(screen.picked, '', 'ein Klick oder die Tastatur auf 32-bit wählt nichts');
  screen.pick('deb-amd64');
  screen.pick('deb-i386');
  assert.equal(screen.picked, 'deb-amd64');

  const html = fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'templates', 'screen-media.html'), 'utf8');
  assert.ok(html.includes("'media-opt--off': item.unsupported"), 'die Zeile ist ausgegraut');
  assert.ok(html.includes(':tabindex="item.unsupported ? -1 : 0"'), 'ohne Tastaturfokus');
});

test('Debian: Zusatzzeilen der Geraete und das Etikett des Experiments', async () => {
  const { screen } = device({ 'GET /api/media/catalog': DEBIAN_CATALOG });
  await screen.init();
  const byId = (id) => screen.devices.find((item) => item.id === id);
  assert.equal(screen.hintFor(byId('deb-amd64')), 'z. B. AMD GX-424CC, Intel N100');
  assert.equal(screen.hintFor(byId('deb-arm64')), 'mit UEFI');
  assert.equal(screen.hintFor(byId('deb-i386')), '');
  assert.equal(screen.hintFor(byId('pi3')), '');
  assert.equal(screen.tagFor(byId('deb-arm64')), 'experimentell');
});

test('Debian: Weiter schickt die Gruppe in den Schreibauftrag', async () => {
  const { screen, shell } = debianSettings('deb-amd64');
  await screen.init();
  screen.form.password = 'geheim-geheim';
  screen.form.password2 = 'geheim-geheim';
  screen.next();
  assert.equal(shell.screen, 'media-drive');
  assert.equal(shell.shared.media.group, 'debian');
});

test('Debian: Notfall-Zugang und Rueckfrage an, Sprache der Installation auf Deutsch', async () => {
  const { screen } = debianSettings('deb-amd64');
  await screen.init();
  assert.equal(screen.isDebian, true);
  assert.equal(screen.form.emergency, true);
  assert.equal(screen.form.askBeforeErase, true);
  assert.equal(screen.form.installLang, 'de');
  assert.equal(screen.form.systemLang, 'de');
  assert.equal(screen.canContinue, false, 'ohne Passwort kein Weiter');
  screen.form.password = 'geheim-geheim';
  screen.form.password2 = 'geheim-geheim';
  assert.equal(screen.canContinue, true);
});

test('Debian: kein WLAN in der Oberflaeche und auch im Abschicken', async () => {
  const { screen, window } = debianSettings('deb-amd64');
  window.localStorage.setItem(KEY, JSON.stringify({ wifiOn: true, ssid: 'Haus' }));
  await screen.init();
  const doc = mediaTemplate();
  const wifi = guardOf(doc, 'input[x-model="form.wifiOn"]');
  assert.equal(wifi.getAttribute('x-if'), '!isDebian', 'der Schalter erscheint nur bei Pi');
  const hint = templateNodes(doc).find((node) => node.getAttribute('x-if') === 'isDebian');
  assert.ok(hint.content.querySelector('[x-text="t(\'media.settings.debian_network\')"]'), 'der Hinweis zur Netzwerkkarte');
  assert.ok(hint.content.querySelector('.media-warn [x-text="t(\'media.settings.debian_network_title\')"]'), 'die Warnbox zum Netzwerkkabel');

  assert.equal(window.MediaModel.toRequest(screen.form, screen.group).wifi, null);
});

test('Debian: die Warnung erscheint nur bei ausgeschalteter Rueckfrage', async () => {
  const { screen, window } = debianSettings('deb-amd64');
  await screen.init();
  assert.equal(screen.showEraseWarning, false);
  screen.form.askBeforeErase = false;
  assert.equal(screen.showEraseWarning, true);

  const doc = mediaTemplate();
  const warning = guardOf(doc, '[x-text="t(\'media.settings.ask_erase_off\')"]');
  assert.equal(warning.getAttribute('x-if'), 'showEraseWarning');
  assert.ok(warning.content.querySelector('.media-res--warn'), 'in Warnfarbe');
  assert.equal(window.I18n.t('media.settings.ask_erase_off'), 'Das Zielgerät löscht seine interne Platte ohne Rückfrage, sobald es vom Stick startet.');
});

test('Debian x86 64-bit: Sprachauswahl nur bei deutscher Oberflaeche', async () => {
  const { screen } = debianSettings('deb-amd64');
  await screen.init();
  assert.equal(screen.showLangSelect, true);
  assert.deepEqual(plain(screen.installChoices), ['de', 'en']);
  assert.equal(screen.showArmHint, false);

  const english = debianSettings('deb-amd64');
  english.shell.lang = 'en';
  await english.screen.init();
  assert.equal(english.screen.showLangSelect, false);
  assert.equal(english.screen.form.installLang, 'en');
});

test('Debian ARM64: fester Hinweis statt Sprachauswahl, Installation auf Englisch', async () => {
  const { screen } = debianSettings('deb-arm64');
  await screen.init();
  assert.equal(screen.showArmHint, true);
  assert.equal(screen.showLangSelect, false);
  assert.deepEqual(plain(screen.installChoices), []);
  assert.equal(screen.form.installLang, 'en');

  const doc = mediaTemplate();
  const hint = templateNodes(doc).find((node) => node.getAttribute('x-if') === 'showArmHint');
  assert.ok(hint.content.querySelector('[x-text="t(\'media.settings.arm_serial\')"]'));
  const select = guardOf(doc, 'select');
  assert.equal(select.getAttribute('x-if'), 'showLangSelect');
});

test('Pi: keine Debian-Abschnitte, Weiter wie bisher', async () => {
  const { screen, shell } = settings();
  shell.shared.media.device = 'pi3';
  await screen.init();
  assert.equal(screen.isDebian, false);
  assert.equal(screen.showEraseWarning, false);
  assert.equal(screen.showLangSelect, false);
  assert.equal(screen.showArmHint, false);
  assert.equal(screen.form.askBeforeErase, undefined);
  fill(screen);
  assert.equal(screen.canContinue, true);
});

test('Debian schreibt Rueckfrage, Sprachen und kein WLAN in den Auftrag', async (t) => {
  const { screen, calls, shell } = writer();
  t.after(() => screen.destroy());
  shell.shared.media.device = 'deb-amd64';
  shell.shared.media.group = 'debian';
  shell.shared.media.settings = Object.assign({}, SETTINGS, {
    emergency: true, password: 'geheim-geheim', password2: 'geheim-geheim',
    askBeforeErase: false, installLang: 'de', systemLang: 'de', wifiOn: true, ssid: 'Haus',
  });
  await screen.init();

  const body = calls[0].body.settings;
  assert.equal(body.wifi, null);
  assert.equal(body.ask_before_erase, false);
  assert.equal(body.install_lang, 'de');
  assert.equal(body.system_lang, 'de');
});

test('Debian: die Gruppe kommt aus der Geraete-ID, auch wenn der Katalog ausfaellt', async () => {
  const { screen, shell, window } = settings({}, { errors: { 'GET /api/media/catalog': { status: 500, code: 'catalog_failed' } } });
  shell.shared.media.device = 'deb-arm64';
  await screen.init();
  assert.notEqual(shell.error, null, 'der Fehler wird gemeldet');
  assert.equal(screen.group, 'debian');
  screen.form.password = 'geheim-geheim';
  screen.form.password2 = 'geheim-geheim';
  const body = window.MediaModel.toRequest(screen.form, screen.group);
  assert.equal(body.wifi, null);
  assert.equal(body.ask_before_erase, true);
  assert.equal(body.install_lang, 'en');
  assert.equal(body.system_lang, 'de');
});

test('Debian: auch die Laufwerksstufe kennt die Gruppe ohne Katalog', async (t) => {
  const { screen, shell } = drive([SD], {}, { errors: { 'GET /api/media/catalog': { status: 500, code: 'catalog_failed' } } });
  t.after(() => screen.destroy());
  shell.shared.media.device = 'deb-arm64';
  await screen.init();
  assert.equal(screen.group, 'debian');
  assert.equal(screen.isDebian, true);
  assert.equal(screen.showLayout, false, 'kein Pi-Aufteilung');
});

test('Debian: Schritt 3 zeigt zwei Kaesten, die Groessen und den Hinweis zu den Platten', async (t) => {
  const { screen, shell, window } = drive([SD, TINY], { 'GET /api/media/catalog': DEBIAN_CATALOG });
  t.after(() => screen.destroy());
  shell.shared.media.device = 'deb-amd64';
  await screen.init();
  assert.equal(screen.isDebian, true);
  assert.equal(screen.limit, IMAGE.extract_size);
  assert.equal(window.I18n.t('media.debian.split.stick'), 'Auf dem Stick');
  assert.equal(window.I18n.t('media.debian.split.disk'), 'Auf der Platte des Zielgeräts');

  const doc = mediaTemplate();
  const block = guardOf(doc, '.media-split');
  assert.equal(block.getAttribute('x-if'), 'isDebian', 'nur bei Debian');
  assert.equal(block.content.querySelectorAll('.media-split-box').length, 2, 'zwei Kaesten nebeneinander');
  assert.equal(block.content.querySelectorAll('.media-classes li').length, 4, 'die vier Groessenklassen');
  assert.ok(block.content.querySelector('[x-text="t(\'media.debian.drive.recommend\')"]'), 'die Empfehlung');
  assert.equal(window.I18n.t('media.debian.drive.recommend'), 'Vorsichtshalber am Zielgerät alle Laufwerke außer der internen Platte abziehen, zum Beispiel USB-Platten, Speicherkarten oder eine zweite Platte. Die Installation fasst nur die interne Platte an.');
});

test('Debian: ein zu kleiner Stick ist ausgegraut und laesst sich nicht waehlen', async (t) => {
  const { screen, shell } = drive([SD, TINY], { 'GET /api/media/catalog': DEBIAN_CATALOG });
  t.after(() => screen.destroy());
  shell.shared.media.device = 'deb-amd64';
  await screen.init();
  assert.equal(screen.fits(TINY), false);
  screen.pick(TINY.path);
  assert.equal(screen.selected, null);
  const doc = mediaTemplate();
  assert.equal(guardOf(doc, '[x-text="t(\'media.drive.too_small\')"]').getAttribute('x-if'), '!fits(item)', 'der Grund steht an der Zeile');
});

test('Debian: die Warnkarte nennt die Rueckfrage aus Schritt 2, das Haekchen bestaetigt beides', async (t) => {
  const { screen, shell, window } = drive([SD], { 'GET /api/media/catalog': DEBIAN_CATALOG });
  t.after(() => screen.destroy());
  shell.shared.media.device = 'deb-amd64';
  shell.shared.media.settings = { askBeforeErase: true };
  await screen.init();
  screen.pick(SD.path);
  assert.equal(screen.debianWarnKey, 'media.warn.debian_ask');
  assert.equal(window.I18n.t(screen.debianWarnKey), 'Die Installation löscht später die interne Platte des Zielgeräts, nach einer Rückfrage am Gerät.');

  shell.shared.media.settings = { askBeforeErase: false };
  assert.equal(screen.debianWarnKey, 'media.warn.debian_noask');
  assert.equal(window.I18n.t(screen.debianWarnKey), 'Die Installation löscht später die interne Platte des Zielgeräts, ohne Rückfrage.');

  const doc = mediaTemplate();
  const warn = guardOf(doc, '[x-text="t(debianWarnKey)"]');
  assert.equal(warn.getAttribute('x-if'), 'isDebian');
  assert.equal(window.I18n.t('media.warn.confirm_debian'), 'Ich habe verstanden, dass der Stick beschrieben und die interne Platte des Zielgeräts gelöscht wird.');

  screen.checked = true;
  assert.equal(screen.canErase, true, 'der Stick braucht nur das Haekchen');
});

test('Pi: Schritt 3 ohne Debian-Abschnitt, die Aufteilung bleibt', async (t) => {
  const { screen } = drive([SD]);
  t.after(() => screen.destroy());
  await screen.init();
  assert.equal(screen.group, 'pi');
  assert.equal(screen.isDebian, false);
  screen.pick(SD.path);
  assert.equal(screen.showLayout, true);

  const doc = mediaTemplate();
  assert.equal(guardOf(doc, '.media-split').getAttribute('x-if'), 'isDebian');
});

test('Debian: Schreiben zeigt ohne Auftragsschritte die Ersatzschritte mit patch', async (t) => {
  const { screen, shell } = writer({ 'GET /api/media/job': job({ steps: [] }) });
  shell.shared.media.group = 'debian';
  t.after(() => screen.destroy());
  await screen.init();
  assert.deepEqual(plain(screen.rows.map((row) => row.id)), ['download', 'checksum', 'patch', 'rights', 'write', 'verify', 'eject']);
});

test('Debian: die Karte kommt nach 30 Minuten, nicht nach 10', async (t) => {
  const { screen, shell } = done();
  shell.shared.media.device = 'deb-amd64';
  t.after(() => screen.destroy());
  await screen.init();
  assert.equal(screen.isDebian, true);
  screen.startedAt -= 10 * 60 * 1000;
  screen.tick();
  assert.equal(screen.slow, false, 'nach zehn Minuten noch keine Karte');
  screen.startedAt -= 20 * 60 * 1000;
  screen.tick();
  assert.equal(screen.slow, true, 'nach dreissig Minuten die Karte');
});

test('Debian: Schritt 5 hat fuenf Punkte, die Warnung und beide Rueckfrage-Varianten', () => {
  const doc = mediaTemplate();
  const list = guardOf(doc, '[x-text="t(\'media.done.debian.5\')"]');
  assert.equal(list.getAttribute('x-if'), 'isDebian');
  assert.equal(list.content.querySelectorAll('ol > li').length, 5);
  assert.ok(list.content.querySelector('.media-warn[role="note"]'), 'die Warnung ist hervorgehoben');
  assert.ok(list.innerHTML.includes("t(eraseWithoutAsk ? 'media.done.debian.4.noask' : 'media.done.debian.4.ask')"));
  assert.ok(list.innerHTML.includes("t(eraseWithoutAsk ? 'media.done.debian.5.warn_noask' : 'media.done.debian.5.warn')"));

  assert.equal(guardOf(doc, '[x-text="t(\'media.done.pi.1\')"]').getAttribute('x-if'), '!isDebian', 'Pi behaelt seine drei Punkte');
});

test('Die Ursache ohne DHCP steht in der Fehlersuche nur bei Debian', () => {
  const doc = mediaTemplate();
  assert.equal(guardOf(doc, '[x-text="t(\'media.done.slow.dhcp\')"]').getAttribute('x-if'), 'isDebian');
});
