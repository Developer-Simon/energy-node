import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { MANIFEST, DIAGNOSE } from './helpers/fixtures.mjs';

const plain = (value) => JSON.parse(JSON.stringify(value));

// stubLive ersetzt LiveLog.open: der Test loest die Ereignisse von Hand aus.
function stubLive(window) {
  const live = { opens: [], closes: 0 };
  window.LiveLog.open = (options) => {
    live.opens.push(options);
    return { close: () => { live.closes += 1; } };
  };
  return live;
}

async function mount(options = {}) {
  const mounted = mountScreen('screen-diagnose.js', 'screenDiagnose', Object.assign({
    scripts: ['services.js', 'download.js', 'live-log.js'],
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
  mounted.live = stubLive(mounted.window);
  await mounted.screen.init();
  return mounted;
}

const LIVE_UNITS = { unit_logs: true };
const DASH = 'energy-node-dashboard.service';
const UPD = 'energy-node-updater.service';
const AT = 1761900000000;
const line = (unit, msg, extra = {}) => Object.assign({ unit, at: AT, prio: 6, msg }, extra);
const feed = (mounted) => mounted.live.opens[0];

async function mountLive(options = {}) {
  return mount(Object.assign({}, options, {
    shell: Object.assign({ bootstrap: LIVE_UNITS }, options.shell),
  }));
}

// attachLog legt das Log-Element an, wie es der x-ref in der Vorlage liefert.
// Die Messwerte sind fest: 1000 px Inhalt, 200 px sichtbar.
function attachLog(mounted) {
  const el = mounted.window.document.createElement('div');
  Object.defineProperties(el, {
    scrollHeight: { value: 1000, writable: true, configurable: true },
    clientHeight: { value: 200, writable: true, configurable: true },
    scrollTop: { value: 0, writable: true, configurable: true },
  });
  mounted.screen.$refs = { liveLog: el };
  return el;
}

// Der Oeffnen-Weg der Shell laeuft in der App ueber /api/open-url, wenn der
// Wirt das kann. Hier dieselbe Weiterleitung, ohne die ganze app.js zu laden.
function useHostOpen(mounted) {
  mounted.shell.openExternal = (url) => mounted.window.Api.post('/api/open-url', { url });
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
  assert.deepEqual(plain(fail.retry), { stepId: '83', label: 'Schritt 10 · Shelly erneut ausführen' });
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

// Shelly-Wake-Webhook: die Pruefungen kommen nur mit Opt-in (Schritt 35).
const MANIFEST_WEBHOOK = Object.assign({}, MANIFEST, {
  steps: MANIFEST.steps.concat([{ id: '35', optional: true, default: false, requires: '83' }]),
});
const withWebhook = (firewall, listening) => Object.assign({}, DIAGNOSE, {
  checks: DIAGNOSE.checks.concat([
    { name: 'shelly webhook firewall 8082', ok: firewall, detail: firewall ? 'allowed' : 'missing', retry_step_id: '35', group: 'system', subject: 'shelly-webhook-firewall:8082' },
    { name: 'shelly webhook listener 8082', ok: listening, detail: listening ? 'listening' : 'not listening', group: 'system', subject: 'shelly-webhook-listener:8082', severity: 'warn' },
  ]),
});

test('der Shelly-Webhook steht mit Port auf der System-Karte, getrennt von den Pflichtports', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withWebhook(true, true), 'GET /api/manifest': MANIFEST_WEBHOOK } });
  const system = screen.rightCards[0];
  const names = rows(system);
  assert.ok(names.some(([name, value]) => name === 'Ports 443 · 1883 · 8080' && value === 'offen'));
  assert.ok(names.some(([name, value]) => name === 'Shelly-Webhook-Port 8082' && value === 'freigegeben'));
  assert.ok(names.some(([name, value]) => name === 'Shelly-Wake-Webhook' && value === 'lauscht'));
  assert.deepEqual(plain(screen.tally), { ok: 16, warn: 0, bad: 1 });
});

test('Webhook im Dashboard aus: ein Hinweis mit eigenem Text, kein Fehler', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withWebhook(true, false), 'GET /api/manifest': MANIFEST_WEBHOOK } });
  assert.deepEqual(plain(screen.tally), { ok: 15, warn: 1, bad: 1 });
  const system = screen.rightCards[0];
  const listener = system.parts.find((part) => part.name === 'Shelly-Wake-Webhook');
  assert.equal(listener.value, 'im Dashboard aus');
  assert.equal(listener.dot, 'd warn');
  const box = system.parts[system.parts.indexOf(listener) + 1];
  assert.equal(box.type, 'warnbox');
  assert.match(box.text, /webhook_enabled/);
  assert.match(box.text, /8082/);
});

test('fehlende Firewall-Regel: Fehler mit Reparatur ueber Schritt 35 unter der Firewall-Station', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withWebhook(false, false), 'GET /api/manifest': MANIFEST_WEBHOOK } });
  const system = screen.rightCards[0];
  const rule = system.parts.find((part) => part.name === 'Shelly-Webhook-Port 8082');
  assert.equal(rule.value, 'nicht freigegeben');
  assert.equal(rule.dot, 'd bad');
  const fail = system.parts[system.parts.indexOf(rule) + 1];
  assert.equal(fail.type, 'fail');
  assert.equal(fail.title, 'Die Firewall-Freigabe fehlt');
  assert.deepEqual(plain(fail.retry), { stepId: '35', label: 'Schritt 3 · Shelly-Wake-Webhook in der Firewall erneut ausführen' });
});

// Versionen und Geraete: reine Information aus installed-manifest.json und
// den *_devices.json, sie zaehlen nicht in der Bilanz.
const DIAGNOSE_INFO = Object.assign({}, DIAGNOSE, {
  units: { 'battery-soc.service': 'not-installed' },
  checks: DIAGNOSE.checks.filter((check) => check.name !== 'unit battery-soc.service'),
  versions: {
    components: { dashboard: 'v1.4.2', bootstrap: 'v1.0.5', services: 'v3.7.1', energy_node_common: 'v1.4.2', battery_soc_core: 'v0.9.3', tinytuya: '1.16.0', 'paho-mqtt': '2.1.0' },
    services: { 'shelly-rpc.service': 'v0.4.3', 'tuya.service': 'v0.4.2' },
  },
  devices: {
    'shelly-rpc.service': [{ id: 'plug', name: 'Plug S+' }, { id: 'em3', name: '3EM Hauptzähler' }],
    'tuya.service': [],
    'trucki-http.service': null,
    'apsystems-ez1.service': [{ id: 'ez1', name: 'EZ1 Dach' }],
  },
});

test('die Dienste-Karte zeigt Version, Geraete und nicht installierte Dienste', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': DIAGNOSE_INFO } });
  const [card] = screen.leftCards;
  assert.deepEqual(plain(card.parts.filter((part) => part.type !== 'fail').map((part) => part.type === 'r' ? [part.name, part.value, part.dot] : [part.type, part.text])), [
    ['apsystems-ez1', 'aktiv', 'd'], ['devs', '1 Gerät: EZ1 Dach'],
    ['automation', 'aktiv', 'd'],
    ['shelly-rpc · 0.4.3', 'fehlgeschlagen', 'd bad'], ['devs', '2 Geräte: Plug S+, 3EM Hauptzähler'],
    ['trucki-http', 'aktiv', 'd'], ['devs', 'Gerätedatei nicht lesbar'],
    ['tuya · 0.4.2', 'aktiv', 'd'], ['devs', 'keine Geräte eingerichtet'],
    ['battery-soc', 'nicht installiert', 'd off'],
  ]);
  assert.deepEqual(plain(screen.tally), { ok: 13, warn: 0, bad: 1 }, 'Information zaehlt nicht mit');
});

test('die Versionskarte nennt Paket, Bootstrap und Wheels, der Rest ist gezaehlt', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': DIAGNOSE_INFO } });
  const card = screen.rightCards.find((c) => c.key === 'versions');
  assert.equal(card.heading, 'Installierte Versionen');
  assert.deepEqual(plain(rows(card)), [
    ['Paket', '1.4.2'], ['Bootstrap', '1.0.5'],
    ['Wheel energy_node_common', '1.4.2'], ['Wheel battery_soc_core', '0.9.3'],
    ['übrige Abhängigkeiten', '2'],
  ]);
  assert.ok(card.parts.every((part) => part.dot === 'd off'));
});

test('ohne installiertes Manifest gibt es keine Versionskarte und die Sammelversion bleibt ohne Dienstversionen', async () => {
  const bare = await mount();
  assert.equal(bare.screen.rightCards.some((c) => c.key === 'versions'), false);
  const view = Object.assign({}, DIAGNOSE, { versions: { components: { services: 'v3.7.1' }, services: {} } });
  const { screen } = await mount({ responses: { 'GET /api/diagnose': view } });
  assert.deepEqual(plain(rows(screen.rightCards.find((c) => c.key === 'versions'))), [['Dienste (Repo)', '3.7.1']]);
});

test('der Bericht nimmt Versionen, Geraete und nicht installierte Dienste mit', async () => {
  const { screen, window } = await mount({ responses: { 'GET /api/diagnose': DIAGNOSE_INFO } });
  const saved = [];
  window.Download.text = (name, content) => saved.push({ name, content });
  screen.save();
  const text = saved[0].content;
  assert.ok(text.includes('INFO  unit battery-soc.service  not-installed\n'), text);
  assert.ok(text.includes('INFO  service shelly-rpc.service  v0.4.3\n'), text);
  assert.ok(text.includes('INFO  component bootstrap  v1.0.5\n'), text);
  assert.ok(text.includes('INFO  devices shelly-rpc.service  plug,em3\n'), text);
  assert.ok(text.includes('INFO  devices trucki-http.service  unreadable\n'), text);
  assert.ok(text.includes('INFO  devices tuya.service  -\n'), text);
});

// Ausstehender Neustart (Schritt 15): ein Hinweis auf der System-Karte,
// ohne Reparatur, bis der Node neu gestartet ist.
test('ein ausstehender Neustart ist ein Hinweis ohne Reparatur', async () => {
  const withReboot = Object.assign({}, DIAGNOSE, {
    checks: DIAGNOSE.checks.concat([
      { name: 'reboot required', ok: false, detail: 'required', group: 'system', subject: 'reboot', severity: 'warn' },
    ]),
  });
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withReboot } });
  assert.deepEqual(plain(screen.tally), { ok: 14, warn: 1, bad: 1 });
  const system = screen.rightCards[0];
  const reboot = system.parts.find((part) => part.name === 'Neustart');
  assert.ok(reboot, 'Zeile Neustart fehlt');
  assert.equal(reboot.value, 'ausstehend');
  assert.equal(reboot.dot, 'd warn');
  const box = system.parts[system.parts.indexOf(reboot) + 1];
  assert.equal(box.type, 'warnbox');
  assert.equal(box.text, 'Ein Update braucht einen Neustart des Node. Starte ihn neu, wenn es passt, zum Beispiel mit sudo reboot.');
  assert.ok(!system.parts.some((part) => part.type === 'fail' && part.key === 'fail-reboot required'));
});

// Ausstehende Systempakete: eine Infozeile mit Liste auf der System-Karte,
// keine Pruefung und kein Hinweis.
const withUpdates = (updates) => Object.assign({}, DIAGNOSE, { system_updates: updates });

test('ausstehende Systempakete stehen als Info mit Liste auf der System-Karte', async () => {
  const { screen } = await mount({ responses: { 'GET /api/diagnose': withUpdates({
    count: 2, checked_at: '2026-10-04T06:12:00+00:00',
    packages: [{ name: 'libssl3', from: '3.0.11', to: '3.0.13' }, { name: 'openssl', from: '3.0.11', to: '3.0.13' }],
  }) } });
  assert.deepEqual(plain(screen.tally), { ok: 14, warn: 0, bad: 1 });
  const system = screen.rightCards[0];
  const row = system.parts.find((part) => part.name === 'Systempakete');
  assert.ok(row, 'Zeile Systempakete fehlt');
  assert.equal(row.value, '2 Updates · Stand 04.10.');
  assert.equal(row.dot, 'd off');
  const list = system.parts[system.parts.indexOf(row) + 1];
  assert.equal(list.type, 'devs');
  assert.equal(list.text, 'libssl3 3.0.11 → 3.0.13, openssl 3.0.11 → 3.0.13');
  const act = system.parts[system.parts.indexOf(row) + 2];
  assert.equal(act.type, 'act');
  assert.equal(act.label, 'Jetzt neu abrufen');
});

test('Jetzt neu abrufen holt die Paketlisten frisch und ersetzt die Zeile', async () => {
  const { screen, calls } = await mount({ responses: {
    'GET /api/diagnose': withUpdates({ count: 2, checked_at: '2026-10-01T06:12:00+00:00', packages: [] }),
    'POST /api/system-updates/refresh': { count: 1, checked_at: '2026-10-05T17:40:00+00:00', packages: [{ name: 'tzdata', from: '2024a-0', to: '2024b-0' }] },
  } });
  const pending = screen.refreshUpdates();
  let system = screen.rightCards[0];
  let act = system.parts.find((part) => part.type === 'act');
  assert.equal(act.label, 'Ruft ab …');
  assert.equal(act.busy, true);
  await pending;
  assert.equal(calls.filter((call) => call.key === 'POST /api/system-updates/refresh').length, 1);
  system = screen.rightCards[0];
  assert.equal(system.parts.find((part) => part.name === 'Systempakete').value, '1 Update · Stand 05.10.');
  assert.equal(system.parts.find((part) => part.key === 'devs-system-updates').text, 'tzdata 2024a-0 → 2024b-0');
  act = system.parts.find((part) => part.type === 'act');
  assert.equal(act.label, 'Jetzt neu abrufen');
  assert.equal(act.busy, false);
});

test('ein gescheiterter Abruf nennt den Fehler an der Zeile und behaelt den alten Stand', async () => {
  const { screen, shell } = await mount({
    responses: { 'GET /api/diagnose': withUpdates({ count: 2, checked_at: '2026-10-01T06:12:00+00:00', packages: [] }) },
    errors: { 'POST /api/system-updates/refresh': { code: 'APT_UPDATE_FAILED', status: 502, detail: 'E: Failed to fetch' } },
  });
  await screen.refreshUpdates();
  assert.equal(shell.error, null, 'kein Banner, der Rest der Diagnose stimmt ja');
  const system = screen.rightCards[0];
  assert.equal(system.parts.find((part) => part.name === 'Systempakete').value, '2 Updates · Stand 01.10.');
  const note = system.parts.find((part) => part.key === 'system-updates-error');
  assert.equal(note.type, 'devs');
  assert.equal(note.text, 'Die Paketlisten ließen sich nicht abrufen. E: Failed to fetch');
});

test('ohne ausstehende Systempakete keine Liste, ohne Bericht keine Zeile', async () => {
  const none = await mount({ responses: { 'GET /api/diagnose': withUpdates({ count: 0, checked_at: '', packages: [] }) } });
  const system = none.screen.rightCards[0];
  const row = system.parts.find((part) => part.name === 'Systempakete');
  assert.equal(row.value, 'aktuell');
  const next = system.parts[system.parts.indexOf(row) + 1];
  assert.ok(!next || next.type !== 'devs');
  const bare = await mount();
  assert.ok(!bare.screen.rightCards[0].parts.some((part) => part.name === 'Systempakete'));
});

test('der Bericht nennt die ausstehenden Systempakete', async () => {
  const { screen, window } = await mount({ responses: { 'GET /api/diagnose': withUpdates({
    count: 1, checked_at: '2026-10-04T06:12:00+00:00', packages: [{ name: 'libssl3', from: '3.0.11', to: '3.0.13' }],
  }) } });
  const saved = [];
  window.Download.text = (name, content) => saved.push({ name, content });
  screen.save();
  const text = saved[0].content;
  assert.ok(text.includes('INFO  system-updates  1  2026-10-04T06:12:00+00:00\n'), text);
  assert.ok(text.includes('INFO  system-update libssl3  3.0.11 -> 3.0.13\n'), text);
});

// Live-Log-Karte: nur mit bootstrap.unit_logs. Der Stream kommt von LiveLog.open,
// der Test schickt die Ereignisse selbst.
test('live card is absent without unit_logs', async () => {
  const { screen, live } = await mount();
  assert.equal(screen.liveEnabled, false);
  assert.equal(live.opens.length, 0);
});

test('lines of the selected unit are appended as text', async () => {
  const mounted = await mountLive();
  const el = attachLog(mounted);
  feed(mounted).onHello({ units: [DASH, UPD], hostname: 'energy-node' });
  assert.equal(mounted.screen.live.empty, true);
  feed(mounted).onLine(line(DASH, '<b>x</b>', { prio: 3, ident: 'systemd', pid: '1' }));
  assert.equal(mounted.screen.live.empty, false);
  const nodes = el.querySelectorAll('.ll-ln');
  assert.equal(nodes.length, 1);
  assert.ok(nodes[0].textContent.includes('<b>x</b>'), nodes[0].textContent);
  assert.ok(nodes[0].classList.contains('err'));
  assert.ok(nodes[0].classList.contains('sys'));
  assert.ok(!el.innerHTML.includes('<b>'), el.innerHTML);
});

test('switching unit rebuilds from the buffer', async () => {
  const mounted = await mountLive();
  const el = attachLog(mounted);
  const f = feed(mounted);
  f.onLine(line(DASH, 'd1'));
  f.onLine(line(DASH, 'd2'));
  f.onLine(line(DASH, 'd3'));
  f.onLine(line(UPD, 'u1'));
  f.onLine(line(UPD, 'u2'));
  assert.equal(el.querySelectorAll('.ll-ln').length, 3);
  mounted.screen.liveSelect(UPD);
  const nodes = [...el.querySelectorAll('.ll-ln')];
  assert.deepEqual(nodes.map((node) => node.textContent.split(' ').at(-1)), ['u1', 'u2']);
  assert.equal(mounted.screen.live.unit, UPD);
  assert.equal(mounted.screen.live.follow, true);
  // Der Neuaufbau laeuft mit ll-static, das CSS schaltet dann die Zeilenanimation ab.
  assert.equal(el.classList.contains('ll-static'), true);
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.equal(el.classList.contains('ll-static'), false);
});

test('scrolling up pauses and the jump pill resumes', async () => {
  const mounted = await mountLive();
  const { screen } = mounted;
  const el = attachLog(mounted);
  el.scrollTop = 100;
  screen.liveScrolled();
  assert.equal(screen.live.follow, false);
  el.scrollTop = 800;
  screen.liveScrolled();
  assert.equal(screen.live.follow, true);
  el.scrollTop = 100;
  screen.liveScrolled();
  screen.liveJump();
  assert.equal(screen.live.follow, true);
  assert.equal(el.scrollTop, 1000);
});

test('status text for a crash loop', async () => {
  const mounted = await mountLive();
  feed(mounted).onStatus({ unit: DASH, load: 'loaded', active: 'activating', sub: 'auto-restart', result: 'exit-code', exit_status: 1, restarts: 9, pid: 0, since: 0 });
  assert.equal(mounted.screen.liveStatusText, 'activating (auto-restart) · Exit-Code 1 · Neustarts 9');
});

test('status text for a missing unit', async () => {
  const mounted = await mountLive();
  feed(mounted).onStatus({ unit: DASH, load: 'not-found', active: 'inactive', sub: 'dead', result: 'success', exit_status: 0, restarts: 0, pid: 0, since: 0 });
  assert.equal(mounted.screen.liveStatusText, 'nicht installiert');
});

test('an end event shows the error text and the disconnected state', async () => {
  const mounted = await mountLive();
  feed(mounted).onEnd({ code: 'UNIT_LOGS_FAILED', detail: 'Failed to get journal' });
  feed(mounted).onState('ended');
  assert.equal(mounted.screen.live.state, 'ended');
  assert.equal(mounted.screen.live.error, 'Die Dienst-Logs lassen sich nicht lesen. Failed to get journal');
});

test('save appends scrubbed log sections', async () => {
  const mounted = await mountLive({ shell: { shared: { target: { host: '192.168.1.20', user: 'pi' } } } });
  const saved = [];
  mounted.window.Download.text = (name, content) => saved.push({ name, content });
  feed(mounted).onHello({ units: [DASH, UPD], hostname: 'energy-node' });
  feed(mounted).onLine(line(DASH, 'connect 192.168.1.20 refused'));
  mounted.screen.live.stage = 100;
  mounted.screen.save();
  const text = saved[0].content;
  assert.ok(text.includes('== log energy-node-dashboard.service'), text);
  assert.ok(text.includes('<ip-1>'), text);
  assert.ok(!text.includes('192.168.1.20'), text);
});

test('stage 0 saves the plain report', async () => {
  const mounted = await mountLive();
  const saved = [];
  mounted.window.Download.text = (name, content) => saved.push({ name, content });
  feed(mounted).onLine(line(DASH, 'hello'));
  mounted.screen.live.stage = 0;
  mounted.screen.save();
  const text = saved[0].content;
  assert.ok(!text.includes('== log'), text);
  assert.ok(text.includes('FAIL  unit shelly-rpc.service  failed\n'), text);
});

test('copy for issue writes markdown and opens the issue page', async () => {
  const mounted = await mountLive({ shell: { bootstrap: { unit_logs: true, can_open_url: true } }, responses: { 'POST /api/open-url': {} } });
  const { screen, window, calls } = mounted;
  const written = [];
  Object.defineProperty(window.navigator, 'clipboard', { value: { writeText: async (text) => { written.push(text); } }, configurable: true });
  useHostOpen(mounted);
  feed(mounted).onLine(line(DASH, 'hello'));
  screen.live.stage = 100;
  await screen.copyForIssue();
  assert.equal(written.length, 1);
  assert.ok(written[0].startsWith('## energy-node diagnose'), written[0]);
  assert.ok(written[0].includes('<details><summary>energy-node-dashboard.service'), written[0]);
  assert.equal(screen.live.copyState, 'copied');
  assert.deepEqual(calls.at(-1), { key: 'POST /api/open-url', body: { url: window.LiveLog.ISSUE_URL } });
  screen.destroy();
});

test('copy failure shows the hint and opens nothing', async () => {
  const mounted = await mountLive({ shell: { bootstrap: { unit_logs: true, can_open_url: true } }, responses: { 'POST /api/open-url': {} } });
  const { screen, window, calls } = mounted;
  Object.defineProperty(window.navigator, 'clipboard', { value: { writeText: async () => { throw new Error('denied'); } }, configurable: true });
  window.document.execCommand = () => false;
  useHostOpen(mounted);
  await screen.copyForIssue();
  assert.equal(screen.live.copyState, 'failed');
  assert.equal(calls.some((call) => call.key === 'POST /api/open-url'), false);
});

test('destroy closes the stream', async () => {
  const mounted = await mountLive();
  mounted.screen.destroy();
  assert.equal(mounted.live.closes, 1);
});
