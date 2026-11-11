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
