import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';

// Normalization helper to convert objects from jsdom realm to plain realm
const plain = (value) => JSON.parse(JSON.stringify(value));

// Die Daten des Artboards, wie cmd/fakehost --scenario vorlage sie liefert.
const REPORT = {
  os_id: 'debian', os_version_id: '12', os_pretty_name: 'Raspberry Pi OS Lite 12 (bookworm)',
  arch: 'armv6l', python_abi: 'cp311', python_version: '3.11.2',
  disk_free_mb: 12698, disk_total_mb: 30413, sudo_nopasswd: true, internet: true,
  installed: false, installed_bundle_version: '', timezone: 'Etc/UTC',
  arch_ok: true, python_abi_ok: true, disk_ok: true, blocking: [], warnings: ['TIMEZONE_UTC'],
};
const MANIFEST = {
  bundle_version: 'v1.4.2', arch: 'armv6', python_abi: 'cp311', target_user: 'energynode', target_base: '/home/energynode',
  components: {}, has_caddy: true,
  bundle_bytes: 41 * 1024 * 1024, wheel_count: 12, unit_count: 8, template_count: 7,
  steps: [
    { id: '10' }, { id: '20' }, { id: '30' }, { id: '40', optional: true, default: true }, { id: '50' }, { id: '60' },
    { id: '70', optional: true, default: true },
    { id: '81', service_id: 'apsystems', kind: 'device', optional: true, default: true },
    { id: '82', service_id: 'battery_soc', kind: 'device', optional: true },
    { id: '83', service_id: 'shelly', kind: 'device', optional: true, default: true },
    { id: '84', service_id: 'trucki', kind: 'device', optional: true, default: true },
    { id: '85', service_id: 'tuya', kind: 'device', optional: true, default: true },
    { id: '88', service_id: 'automation', kind: 'service', optional: true, default: true },
  ],
};

async function mount(report = REPORT, options = {}) {
  const mounted = mountScreen('screen-precheck.js', 'screenPrecheck', Object.assign({
    catalog: realCatalog(options.lang || 'de'),
    lang: options.lang || 'de',
    responses: { 'GET /api/precheck': report, 'GET /api/manifest': MANIFEST },
    shell: { screen: 'precheck', connected: true },
  }, options));
  await mounted.screen.init();
  return mounted;
}

const withReport = (changes) => Object.assign({}, REPORT, changes);

test('mit den Daten der Vorlage entstehen ihre sieben Zeilen und Texte', async () => {
  const { screen, shell } = await mount();
  assert.equal(shell.shared.manifest.bundle_version, 'v1.4.2');
  assert.equal(screen.summary, 'Sieben Prüfungen, bevor etwas verändert wird.');
  assert.deepEqual(plain(screen.rows.map((row) => [row.title, row.state])), [
    ['Architektur', 'ok'], ['Betriebssystem', 'ok'], ['Freier Speicher', 'ok'], ['Internetzugang', 'ok'],
    ['Erhöhte Rechte', 'ok'], ['Vorhandene Installation', 'ok'], ['Zeitzone', 'warn'],
  ]);
  const [arch, os, disk, internet, sudo, installed, timezone] = screen.rows;
  assert.equal(arch.detail + arch.em, 'armv6l, 32 Bit. Passt zum Paket armv6');
  assert.equal(os.detail, 'Raspberry Pi OS Lite 12 (bookworm) · Python 3.11.2');
  assert.equal(disk.detail, '12,4 GB von 29,7 GB frei');
  assert.equal(internet.detail, 'deb.debian.org erreichbar, nur für die Systempakete nötig');
  assert.equal(sudo.detail, 'sudo ohne Passwortabfrage verfügbar');
  assert.equal(installed.detail, 'keine gefunden, das Gerät wird von Grund auf eingerichtet');
  assert.equal(timezone.detail, 'Etc/UTC, damit weichen Zeitstempel im Verlauf von der Ortszeit ab');
  assert.equal(screen.hint, '1 Hinweis, blockiert nicht');
  assert.equal(screen.canProceed, true);
});

test('die Karte "Was uebertragen wird" zeigt die Zahlen der Vorlage', async () => {
  const { screen } = await mount();
  assert.equal(screen.sumNumber, '1.4.2');
  assert.equal(screen.sumUnit, 'armv6 · 41 MB');
  assert.deepEqual(plain(screen.transfer.map((line) => [line.label, line.value])), [
    ['Dashboard', '1 Binary'], ['Python-Dienste', '7'], ['Wheels', '12'], ['systemd-Units', '8'], ['Konfigurationsvorlagen', '7'],
  ]);
});

test('eine falsche Architektur blockiert und nennt die Abhilfe', async () => {
  const { screen, shell } = await mount(withReport({ arch: 'aarch64', arch_ok: false, blocking: ['ARCH_MISMATCH'] }));
  const arch = screen.rows[0];
  assert.equal(arch.state, 'bad');
  assert.equal(arch.detail + arch.em, 'aarch64, 64 Bit. Passt nicht zum Paket armv6');
  assert.equal(arch.remedy, realCatalog('de')['fault.ARCH_MISMATCH.remediation']);
  assert.equal(screen.hint, '1 Befund blockiert die Installation');
  assert.equal(screen.canProceed, false);
  screen.proceed();
  assert.equal(shell.screen, 'precheck');
});

test('ein falsches Python-ABI blockiert in der Zeile Betriebssystem', async () => {
  const { screen } = await mount(withReport({ python_abi: 'cp313', python_version: '3.13.1', python_abi_ok: false, blocking: ['PYTHON_ABI_MISMATCH'] }));
  const os = screen.rows[1];
  assert.equal(os.state, 'bad');
  assert.equal(os.detail, 'Raspberry Pi OS Lite 12 (bookworm) · Python 3.13.1 passt nicht zu den Wheels (cp311)');
  assert.equal(screen.canProceed, false);
});

test('Hinweise zaehlen, blockieren aber nicht', async () => {
  const { screen } = await mount(withReport({
    disk_free_mb: 300, disk_ok: false, internet: false, sudo_nopasswd: false,
    warnings: ['DISK_LOW', 'SUDO_PASSWORD_REQUIRED', 'NO_INTERNET', 'TIMEZONE_UTC'],
  }));
  assert.equal(screen.rows.filter((row) => row.state === 'warn').length, 4);
  assert.equal(screen.rows[2].detail, '0,3 GB von 29,7 GB frei, das reicht voraussichtlich nicht');
  assert.equal(screen.hint, '4 Hinweise, blockieren nicht');
  assert.equal(screen.canProceed, true);
});

test('ohne Zeitzone gibt es sechs Pruefungen', async () => {
  const { screen } = await mount(withReport({ timezone: '', warnings: [] }));
  assert.equal(screen.rows.length, 6);
  assert.equal(screen.summary, 'Sechs Prüfungen, bevor etwas verändert wird.');
  assert.equal(screen.hint, 'Alles bereit');
});

test('ein unbekannter Code verschwindet nicht, sondern bekommt eine Zeile', async () => {
  const { screen } = await mount(withReport({ warnings: ['TIMEZONE_UTC', 'SWAP_MISSING'], blocking: ['KERNEL_TOO_OLD'] }));
  const titles = screen.rows.map((row) => row.title);
  assert.ok(titles.includes('warning.SWAP_MISSING'), titles.join(' | '));
  assert.ok(titles.includes('fault.KERNEL_TOO_OLD.message'), titles.join(' | '));
  assert.equal(screen.canProceed, false);
});

test('eine vorhandene Installation wird genannt und blockiert nicht', async () => {
  const { screen } = await mount(withReport({ installed: true, installed_bundle_version: 'v1.4.1' }));
  assert.equal(screen.rows[5].detail, 'Paket 1.4.1 gefunden. Für ein eingerichtetes Gerät ist „Aktualisieren“ der passende Weg');
  assert.equal(screen.canProceed, true);
});

test('auf Englisch folgen Texte und Zahlen der Sprache', async () => {
  const { screen } = await mount(REPORT, { lang: 'en' });
  assert.equal(screen.summary, 'Seven checks before anything is changed.');
  assert.equal(screen.rows[2].detail, '12.4 GB of 29.7 GB free');
});

test('Weiter fuehrt in die Konfiguration, Zurueck in die Verbindung', async () => {
  const { screen, shell } = await mount();
  screen.proceed();
  assert.equal(shell.screen, 'configure');
  screen.back();
  assert.equal(shell.screen, 'connect');
  assert.equal(shell.dir, 'back');
});

test('ein gescheiterter Aufruf landet im Banner', async () => {
  const mounted = mountScreen('screen-precheck.js', 'screenPrecheck', {
    catalog: realCatalog('de'),
    errors: { 'GET /api/precheck': { code: 'PREFLIGHT_FAILED', detail: 'bash: not found', status: 500 } },
    responses: { 'GET /api/manifest': MANIFEST },
  });
  await mounted.screen.init();
  assert.equal(mounted.shell.error.code, 'PREFLIGHT_FAILED');
  assert.equal(mounted.screen.busy, false);
  assert.equal(mounted.screen.canProceed, false);
});
