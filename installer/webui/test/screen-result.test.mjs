import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { MANIFEST, SELECTION } from './helpers/fixtures.mjs';

// Helper to convert vm-realm objects to plain literals for comparison
const plain = (value) => JSON.parse(JSON.stringify(value));

const T0 = new Date(2026, 8, 13, 14, 18, 3).getTime();
const ACTIVE = {
  'apsystems-ez1.service': 'active', 'battery-soc.service': 'active', 'shelly-rpc.service': 'active',
  'trucki-http.service': 'active', 'tuya.service': 'active', 'automation.service': 'active',
  'energy-node-dashboard.service': 'active', 'mosquitto.service': 'active',
};
const catalog = realCatalog('de');

async function mount({ outcome = {}, shared = {}, responses = {}, errors = {}, shell = {} } = {}) {
  const mounted = mountScreen('screen-result.js', 'screenResult', {
    scripts: ['services.js', 'run-model.js', 'download.js'],
    catalog,
    responses: Object.assign({ 'GET /api/diagnose': { bundle_version: 'v1.4.2', units: ACTIVE, ports: {}, checks: [] } }, responses),
    errors,
    shell: Object.assign({ screen: 'result', connected: true, entry: 'install' }, shell, {
      shared: Object.assign({ target: { host: 'energy-node.local', user: 'pi' }, manifest: MANIFEST, selection: SELECTION }, shared),
    }),
  });
  const s = mounted.shell;
  s.shared.lastRun = Object.assign({
    ok: true, code: '', stepId: '', mode: 'install', only: '', startedAt: T0, finishedAt: T0 + 492000,
    loginUrl: '', loginPending: false, steps: {}, lastLines: [], logText: '14:18:03  ##STEP 10 begin\n',
    groups: mounted.window.Services.runGroups(MANIFEST, s.shared.selection, s),
  }, outcome);
  await mounted.screen.init();
  return mounted;
}

test('ein gelungener Lauf zeigt die Texte der Vorlage', async () => {
  const { screen } = await mount();
  assert.equal(screen.heading, 'Der Node läuft');
  assert.equal(screen.summary, '7 von 7 gewählten Diensten aktiv · 8:12 gebraucht · Paket 1.4.2 (armv6)');
  assert.deepEqual(plain(screen.urls.map((u) => [u.url, u.hint, u.lead])), [
    ['https://energy-node.local', 'Über Caddy, nötig für die Admin-Anmeldung', true],
    ['http://energy-node.local:8080', 'Direkt, ohne Verschlüsselung', false],
  ]);
  assert.equal(screen.primaryUrl, 'https://energy-node.local');
  assert.equal(screen.todoText, 'Diese drei Schritte kann das Programm nicht übernehmen.');
  assert.deepEqual(plain(screen.todos.map((item) => [item.n, item.title])), [
    [1, 'Schlüsselablauf in der Tailscale-Konsole abschalten'],
    [2, 'Caddys lokalem Zertifikat vertrauen'],
    [3, 'Geräte und die Brücke im Dashboard eintragen'],
  ]);
});

test('ohne Diagnose steht die kurze Zusammenfassung', async () => {
  const { screen } = await mount({ errors: { 'GET /api/diagnose': { code: 'DIAGNOSE_FAILED', status: 500 } } });
  assert.equal(screen.summary, '8:12 gebraucht · Paket 1.4.2 (armv6)');
});

test('ein ausgefallener Dienst zaehlt nicht als aktiv', async () => {
  const units = Object.assign({}, ACTIVE, { 'shelly-rpc.service': 'failed' });
  const { screen } = await mount({ responses: { 'GET /api/diagnose': { units, ports: {}, checks: [] } } });
  assert.equal(screen.summary, '6 von 7 gewählten Diensten aktiv · 8:12 gebraucht · Paket 1.4.2 (armv6)');
});

test('eine offene Tailscale-Anmeldung wird der erste Punkt', async () => {
  const { screen } = await mount({ outcome: { loginPending: true, loginUrl: 'https://login.tailscale.com/a/4f2c8ab19de3' } });
  assert.equal(screen.todos[0].title, 'Tailscale-Anmeldung abschließen');
  assert.equal(screen.todos[0].text, 'https://login.tailscale.com/a/4f2c8ab19de3');
  assert.equal(screen.todoText, 'Diese vier Schritte kann das Programm nicht übernehmen.');
});

test('ohne Caddy und Tailscale bleiben die direkte Adresse und ein Punkt', async () => {
  const selection = { source: 'node', steps: Object.assign({}, SELECTION.steps, { 40: false, 70: false }) };
  const { screen } = await mount({ shared: { selection } });
  assert.deepEqual(plain(screen.urls.map((u) => [u.url, u.lead])), [['http://energy-node.local:8080', true]]);
  assert.equal(screen.todoText, 'Diesen Schritt kann das Programm nicht übernehmen.');
});

test('ein Update nennt von/nach und die neu gestarteten Dienste, ohne Diagnose zu holen', async () => {
  const { screen, calls } = await mount({
    shell: { entry: 'redeploy' },
    shared: { plan: { bundle_version: 'v1.4.2', steps: [], components: { bootstrap: { from: 'v1.4.1', to: 'v1.4.2' } } } },
    outcome: {
      mode: 'redeploy', finishedAt: T0 + 41000,
      steps: { 10: { state: 'skip' }, 50: { state: 'ok' }, 60: { state: 'ok' }, 85: { state: 'ok' }, 88: { state: 'ok' } },
    },
  });
  assert.equal(screen.summary, 'Paket 1.4.1 → 1.4.2 · 3 Dienste neu gestartet · 0:41 gebraucht');
  assert.deepEqual(plain(calls), []);
});

test('eine Reparatur nennt den Dienst und fuehrt zurueck in die Diagnose', async () => {
  const { screen, shell } = await mount({ shell: { entry: 'diagnose' }, outcome: { mode: 'repair', only: '83', finishedAt: T0 + 12000 } });
  assert.equal(screen.summary, 'Shelly erneut ausgeführt · 0:12 gebraucht');
  screen.openDiagnose();
  assert.equal(shell.screen, 'diagnose');
});

test('ein Fehlschlag zeigt Klartext, Abhilfe und die letzten Zeilen des Schritts', async () => {
  const { screen, calls } = await mount({
    outcome: { ok: false, code: 'PIP_EXTERNALLY_MANAGED', stepId: '50', lastLines: ['error: externally-managed-environment', 'hint: …'] },
  });
  assert.equal(screen.heading, 'Der Lauf ist abgebrochen');
  assert.equal(screen.summary, 'Bei „Python-Pakete“ · nach 8:12');
  assert.equal(screen.fault.message, catalog['fault.PIP_EXTERNALLY_MANAGED.message']);
  assert.equal(screen.fault.remediation, catalog['fault.PIP_EXTERNALLY_MANAGED.remediation']);
  assert.deepEqual(plain(screen.lines), ['error: externally-managed-environment', 'hint: …']);
  assert.deepEqual(plain(calls), [], 'nach einem Fehlschlag keine Diagnose');
});

test('ein unbekannter Code bleibt als unbekannt erkennbar', async () => {
  const { screen } = await mount({ outcome: { ok: false, code: 'SOMETHING_NEW', stepId: '85' } });
  assert.equal(screen.fault.message, 'Unbekannter Fehlercode SOMETHING_NEW.');
  assert.equal(screen.fault.remediation, '');
});

test('ein abgebrochener Lauf ist kein Fehler', async () => {
  const { screen } = await mount({ outcome: { ok: false, code: 'RUN_CANCELLED' } });
  assert.equal(screen.cancelled, true);
  assert.equal(screen.heading, 'Lauf abgebrochen');
  assert.equal(screen.summary, 'Abgebrochen nach 8:12');
});

test('Erneut starten fuehrt an den Anfang des jeweiligen Weges zurueck', async () => {
  for (const [mode, target] of [['install', 'configure'], ['redeploy', 'preview'], ['repair', 'diagnose']]) {
    const { screen, shell } = await mount({ outcome: { ok: false, code: 'UFW_MISSING', stepId: '30', mode } });
    screen.retry();
    assert.equal(shell.screen, target, mode);
    assert.equal(shell.dir, 'back');
  }
});

test('Protokoll speichern und Adressen oeffnen', async () => {
  const { screen, window, opened } = await mount();
  const saved = [];
  window.Download.text = (name, content) => saved.push({ name, content });
  screen.save();
  assert.match(saved[0].name, /^energy-node-protokoll-\d{8}-\d{6}\.txt$/);
  assert.equal(saved[0].content, '14:18:03  ##STEP 10 begin\n');
  screen.openUrl(screen.primaryUrl);
  assert.deepEqual(opened, ['https://energy-node.local']);
});

test('ohne Verbindungsziel ist die Adresse die des Browsers', async () => {
  const { screen } = await mount({ shared: { target: { host: '', user: '' } } });
  assert.equal(screen.primaryUrl, 'https://127.0.0.1');
});
