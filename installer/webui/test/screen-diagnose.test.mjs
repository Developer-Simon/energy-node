import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { MANIFEST, DIAGNOSE } from './helpers/fixtures.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));

async function mount(options = {}) {
  const mounted = mountScreen('screen-diagnose.js', 'screenDiagnose', Object.assign({
    scripts: ['services.js', 'download.js'],
    catalog: realCatalog('de'),
    responses: Object.assign({
      'GET /api/diagnose': DIAGNOSE,
      'GET /api/manifest': MANIFEST,
      'POST /api/run': { run_id: 'run-8', seq: 2 },
    }, options.responses),
  }, options.errors ? { errors: options.errors } : {}, {
    shell: Object.assign({
      entry: 'diagnose', screen: 'diagnose', connected: true,
      shared: { target: { host: 'energy-node.local', user: 'pi' } },
    }, options.shell),
  }));
  mounted.window.setInterval = () => 1;
  mounted.window.clearInterval = () => {};
  await mounted.screen.init();
  return mounted;
}

const withCheck = (name, changes) => Object.assign({}, DIAGNOSE, {
  checks: DIAGNOSE.checks.map((check) => (check.name === name ? Object.assign({}, check, changes) : check)),
});
const rows = (card) => card.parts.filter((part) => part.type === 'r').map((part) => [part.name, part.value]);

test('Zaehler und Kopfleiste', async () => {
  const { screen, shell } = await mount();
  assert.deepEqual(plain(screen.tally), { ok: 14, warn: 0, bad: 1 });
  assert.equal(shell.bar.lead, '· energy-node.local · Paket 1.4.2');
  assert.equal(shell.bar.status, 'geprüft vor 0 s');
  assert.equal(shell.bar.action.label, 'Erneut prüfen');
});

test('die Dienste-Karte zeigt Units ohne .service und den Ausfall mit Reparatur', async () => {
  const { screen } = await mount();
  const [card] = screen.leftCards;
  assert.equal(card.heading, 'Dienste');
  assert.deepEqual(plain(card.parts.map((part) => part.type === 'r' ? [part.name, part.value, part.dot] : [part.type, part.chip, part.title])), [
    ['apsystems-ez1', 'aktiv', 'd'], ['automation', 'aktiv', 'd'], ['battery-soc', 'aktiv', 'd'],
    ['shelly-rpc', 'fehlgeschlagen', 'd bad'],
    ['fail', 'failed', 'Der Dienst läuft nicht'],
    ['trucki-http', 'aktiv', 'd'], ['tuya', 'aktiv', 'd'],
  ]);
  const fail = card.parts[4];
  assert.equal(fail.text, 'shelly-rpc.service ist nicht aktiv. Eine Reparatur wiederholt den Schritt, der ihn installiert und startet.');
  assert.deepEqual(plain(fail.retry), { stepId: '83', label: 'Schritt 6 · Shelly erneut ausführen' });
  assert.equal(card.parts[3].valueCls, 'r-v bad');
});

test('System- und Konfigurations-Karte mit Klartextnamen und zusammengefassten Ports', async () => {
  const { screen } = await mount();
  const [system, config] = screen.rightCards;
  assert.equal(system.heading, 'System');
  assert.deepEqual(plain(rows(system)), [
    ['Caddy', 'aktiv'], ['Dashboard', 'aktiv'], ['Mosquitto', 'aktiv'], ['Tailscale-Dienst', 'aktiv'],
    ['Ports 443 · 1883 · 8080', 'offen'], ['Tailscale-Anmeldung', 'angemeldet'],
  ]);
  assert.equal(config.heading, 'Konfiguration');
  assert.deepEqual(plain(rows(config)), [['config.json', 'vorhanden']]);
});

test('ein geschlossener Port wird eine Zeile mit Reparatur', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withCheck('port 443', { ok: false, detail: 'closed' }) } });
  const system = screen.rightCards[0];
  const ports = system.parts.find((part) => part.key === 'ports');
  assert.equal(ports.value, 'geschlossen: 443');
  assert.equal(ports.dot, 'd bad');
  const fail = system.parts[system.parts.indexOf(ports) + 1];
  assert.equal(fail.type, 'fail');
  assert.equal(fail.chip, 'closed');
  assert.equal(fail.title, 'Ein Port ist nicht erreichbar');
  assert.deepEqual(plain(fail.retry), { stepId: '70', label: 'Schritt 7 · HTTPS über Caddy erneut ausführen' });
});

test('ein Hinweis zaehlt als Hinweis und bekommt die gelbe Box statt einer Reparatur', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withCheck('unit mosquitto.service', { ok: false, detail: 'activating', severity: 'warn' }) } });
  assert.deepEqual(plain(screen.tally), { ok: 13, warn: 1, bad: 1 });
  const system = screen.rightCards[0];
  const mosquitto = system.parts.find((part) => part.name === 'Mosquitto');
  assert.equal(mosquitto.dot, 'd warn');
  assert.equal(mosquitto.value, 'startet');
  assert.equal(system.parts[system.parts.indexOf(mosquitto) + 1].type, 'warnbox');
});

test('erneut ausfuehren startet eine Reparatur genau dieses Schritts', async () => {
  const { screen, shell, calls } = await mount();
  const fail = screen.leftCards[0].parts.find((part) => part.type === 'fail');
  await screen.retry(fail);
  assert.deepEqual(calls.at(-1), { key: 'POST /api/run', body: { mode: 'repair', only: '83' } });
  assert.equal(shell.screen, 'run');
  assert.equal(shell.shared.run.only, '83');
});

test('eine Pruefung ohne Reparaturschritt hat keinen Knopf', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withCheck('unit shelly-rpc.service', { retry_step_id: '' }) } });
  assert.equal(screen.leftCards[0].parts.find((part) => part.type === 'fail').retry, null);
});

test('Erneut pruefen in der Kopfleiste laedt neu', async () => {
  const { shell, calls } = await mount();
  await shell.bar.action.run();
  assert.equal(calls.filter((call) => call.key === 'GET /api/diagnose').length, 2);
});

test('Bericht speichern schreibt jede Pruefung als Zeile', async () => {
  const { screen, window } = await mount();
  const saved = [];
  window.Download.text = (name, content) => saved.push({ name, content });
  screen.save();
  assert.match(saved[0].name, /^energy-node-diagnose-\d{8}-\d{6}\.txt$/);
  assert.ok(saved[0].content.includes('FAIL  unit shelly-rpc.service  failed\n'), saved[0].content);
  assert.ok(saved[0].content.includes('OK    port 443  open\n'), saved[0].content);
});

test('Schliessen fuehrt zum ersten anderen Einstieg', async () => {
  const installer = await mount();
  installer.screen.close();
  assert.equal(installer.shell.entry, 'install');

  const dashboard = await mount({ shell: { bootstrap: { host: 'dashboard', needs_connection: false, entry_points: ['redeploy', 'diagnose'] } } });
  dashboard.screen.close();
  assert.equal(dashboard.shell.entry, 'redeploy');
});

test('eine Pruefung aus unbekannter Gruppe erscheint in der System-Karte', async () => {
  const view = Object.assign({}, DIAGNOSE, {
    checks: DIAGNOSE.checks.concat([{ name: 'throttled', ok: false, detail: '0x50000', retry_step_id: '', group: 'power', subject: 'throttled' }]),
  });
  const { screen } = await mount({ responses: { 'GET /api/diagnose': view } });
  const system = screen.rightCards[0];
  assert.deepEqual(plain(rows(system)).at(-1), ['throttled', '0x50000']);
});

test('ein gescheiterter Aufruf landet im Banner', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/diagnose': { code: 'DIAGNOSE_FAILED', detail: 'diagnose.sh: exit 1', status: 500 } } });
  assert.equal(shell.error.code, 'DIAGNOSE_FAILED');
  assert.deepEqual(plain(screen.cards), []);
  assert.equal(shell.bar.status, '');
});
