import { test } from 'node:test';
import assert from 'node:assert/strict';
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
    responses: Object.assign({ 'GET /api/media/catalog': CATALOG, 'GET /api/media/name': NAME_FREE }, responses),
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

test('ein Netz in Reichweite wird per Klick der Netzwerkname', async () => {
  const nets = Object.assign({}, DEFAULTS, { wifi_ssid: 'Haus', wifi_networks: [
    { ssid: 'Haus', signal: 80, band_24: true, band_5: false },
    { ssid: 'Nachbar', signal: 30, band_24: true, band_5: false },
  ] });
  const { screen } = settings({ 'GET /api/media/catalog': Object.assign({}, CATALOG, { defaults: nets }) });
  await screen.init();
  assert.deepEqual(screen.nearby.map((n) => n.ssid), ['Nachbar']);
  screen.pickNetwork('Nachbar');
  assert.equal(screen.form.ssid, 'Nachbar');
  assert.deepEqual(screen.nearby.map((n) => n.ssid), ['Haus']);
});
