import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { MANIFEST, SELECTION } from './helpers/fixtures.mjs';

async function mount(options = {}) {
  const mounted = mountScreen('screen-configure.js', 'screenConfigure', Object.assign({
    scripts: ['services.js'],
    catalog: realCatalog('de'),
    responses: {
      'GET /api/manifest': MANIFEST,
      'GET /api/selection': SELECTION,
      'PUT /api/selection': (body) => ({ source: 'node', steps: body.steps }),
      'POST /api/run': { run_id: 'run-1', seq: 3 },
    },
  }, options, { shell: Object.assign({ screen: 'configure', connected: true }, options.shell) }));
  await mounted.screen.init();
  return mounted;
}

function fillSecrets(screen) {
  screen.mqttPassword = 'mqtt-geheim';
  screen.adminPassword = 'admin-geheim';
}

test('init nimmt Vorgaben aus dem Manifest und die Auswahl vom Wirt', async () => {
  const { screen, shell } = await mount();
  assert.equal(screen.mqttUser, 'energynode');
  assert.equal(screen.targetUser, 'energynode');
  assert.equal(screen.targetBase, '/home/energynode');
  assert.equal(screen.steps['82'], false);
  assert.equal(shell.shared.manifest, MANIFEST);
  assert.equal(shell.shared.selection, SELECTION);
});

test('was die Shell schon kennt, wird nicht noch einmal geholt', async () => {
  const { calls } = await mount({ shell: { shared: { manifest: MANIFEST, selection: SELECTION } } });
  assert.deepEqual(calls, []);
});

test('der Geraete-Schalter schaltet alle, ein Chip einen', async () => {
  const { screen } = await mount();
  const devices = () => screen.rows.find((row) => row.kind === 'devices');
  screen.toggle(devices());
  assert.deepEqual(['81', '82', '83', '84', '85'].map((id) => screen.steps[id]), [false, false, false, false, false]);
  screen.toggleChip(devices().chips[1]);
  assert.equal(screen.steps['82'], true);
  assert.equal(devices().on, true);
});

test('ohne beide Passwoerter startet nichts', async () => {
  const { screen } = await mount();
  assert.equal(screen.canStart, false);
  screen.mqttPassword = 'x';
  assert.equal(screen.canStart, false);
  screen.adminPassword = 'y';
  assert.equal(screen.canStart, true);
});

test('start speichert die Auswahl, startet den Lauf und vergisst die Passwoerter', async () => {
  const { screen, shell, calls } = await mount();
  fillSecrets(screen);
  screen.toggle(screen.rows.find((row) => row.name === 'Tailscale'));
  await screen.start();

  assert.deepEqual(calls.map((call) => call.key), ['GET /api/manifest', 'GET /api/selection', 'PUT /api/selection', 'POST /api/run']);
  assert.equal(calls[2].body.steps['40'], false);
  assert.deepEqual(calls[3].body, {
    mode: 'install', mqtt_user: 'energynode', mqtt_password: 'mqtt-geheim', admin_password: 'admin-geheim',
    target_user: 'energynode', target_base: '/home/energynode',
  });
  assert.equal(shell.screen, 'run');
  assert.equal(shell.mutating, true);
  assert.equal(JSON.stringify(shell.shared).includes('geheim'), false, 'kein Passwort in shared');
  assert.equal(screen.mqttPassword, '');
  assert.equal(screen.adminPassword, '');
});

test('ein abgewiesener Lauf bleibt auf dem Bildschirm und zeigt den Fehler', async () => {
  const { screen, shell } = await mount({ errors: { 'POST /api/run': { code: 'RUN_IN_PROGRESS', status: 409 } } });
  fillSecrets(screen);
  await screen.start();
  assert.equal(shell.screen, 'configure');
  assert.equal(shell.error.code, 'RUN_IN_PROGRESS');
  assert.equal(screen.busy, false);
});

test('im Nur-Dienste-Modus speichert apply und kehrt in die Vorschau zurueck', async () => {
  const { screen, shell, calls } = await mount({ shell: { entry: 'redeploy', shared: { servicesOnly: true } } });
  assert.equal(screen.servicesOnly, true);
  assert.equal(screen.canApply, true);
  screen.toggleChip(screen.rows.find((row) => row.kind === 'devices').chips[1]);
  await screen.apply();
  assert.equal(calls.at(-1).key, 'PUT /api/selection');
  assert.equal(calls.at(-1).body.steps['82'], true);
  assert.equal(shell.shared.selection.steps['82'], true);
  assert.equal(shell.shared.servicesOnly, false);
  assert.equal(shell.screen, 'preview');
  assert.equal(shell.dir, 'back');
});

test('Zurueck fuehrt je nach Modus in die Vorpruefung oder die Vorschau', async () => {
  const install = await mount();
  install.screen.back();
  assert.equal(install.shell.screen, 'precheck');

  const redeploy = await mount({ shell: { shared: { servicesOnly: true } } });
  redeploy.screen.back();
  assert.equal(redeploy.shell.screen, 'preview');
  assert.equal(redeploy.shell.shared.servicesOnly, false);
});
