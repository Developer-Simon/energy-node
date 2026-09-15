import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mountScreen, realCatalog } from './helpers/mount.mjs';
import { MANIFEST_UPDATE, SELECTION_NODE, PLAN_UPDATE } from './helpers/fixtures.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

// Helper to convert vm-realm objects to plain literals for comparison
const plain = (value) => JSON.parse(JSON.stringify(value));

async function mount(options = {}) {
  const mounted = mountScreen('screen-preview.js', 'screenPreview', Object.assign({
    scripts: ['services.js'],
    catalog: realCatalog('de'),
    responses: Object.assign({
      'GET /api/manifest': MANIFEST_UPDATE,
      'GET /api/selection': () => structuredClone(SELECTION_NODE),
      'GET /api/plan': PLAN_UPDATE,
      'PUT /api/selection': (body) => ({ source: 'node', steps: body.steps }),
      'POST /api/run': { run_id: 'run-5', seq: 9 },
    }, options.responses),
  }, options.errors ? { errors: options.errors } : {}, {
    shell: Object.assign({ entry: 'redeploy', screen: 'preview', connected: true }, options.shell),
  }));
  await mounted.screen.init();
  return mounted;
}

test('init holt Manifest, Auswahl und Plan und merkt sich die Auswahl beim Einstieg', async () => {
  const { screen, shell } = await mount();
  assert.deepEqual(plain(shell.shared.plan), PLAN_UPDATE);
  assert.deepEqual(plain(shell.shared.selectionAtEntry), SELECTION_NODE);
  shell.shared.selection.steps['89'] = true;
  await screen.load();
  assert.equal(shell.shared.selectionAtEntry.steps['89'], undefined, 'ein zweites Laden ueberschreibt die gemerkte Auswahl nicht');
});

test('die Versionsleiste zeigt von und nach, ohne Installationsdauer (A7)', async () => {
  const { screen } = await mount();
  assert.equal(screen.fromVersion, '1.4.1');
  assert.equal(screen.toVersion, '1.4.2');
  assert.equal(screen.note, 'armv6 · Auswahl unverändert');
});

test('Was sich aendert: je Komponente von/nach, unveraenderte Abhaengigkeiten zusammengefasst', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.components.map((row) => [row.label, row.em, row.changed, row.from, row.to])), [
    ['Dashboard', '', true, '1.4.1', '1.4.2'],
    ['Dienste (Repo)', '', true, '3.6.0', '3.7.1'],
    ['Wheel', 'energy_node_common', true, '1.4.0', '1.4.2'],
    ['Wheel', 'battery_soc_core', false, '0.9.3', '0.9.3'],
    ['Abhängigkeit', 'tinytuya', true, '1.15.1', '1.16.0'],
    ['übrige Abhängigkeiten', '', false, '', 'unverändert'],
  ]);
});

test('eine Komponente ohne Vorzustand ist neu', async () => {
  const plan = Object.assign({}, PLAN_UPDATE, { components: { dashboard: { from: null, to: 'v1.5.0' } } });
  const { screen } = await mount({ responses: { 'GET /api/plan': plan } });
  assert.deepEqual(plain(screen.components.map((row) => [row.changed, row.from, row.to])), [[true, 'neu', '1.5.0']]);
  assert.equal(screen.fromVersion, '', 'ohne bootstrap kein "von"');
});

test('Startet neu und Bleibt stehen folgen dem Plan (A17)', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.restart), ['energy-node-dashboard.service', 'tuya.service', 'automation.service']);
  assert.equal(screen.keepNames, 'Systempakete · MQTT-Broker · Firewall · Tailscale · HTTPS über Caddy');
});

test('der Funktionsumfang zeigt die Auswahl und einen neuen Dienst als aus', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.serviceParts.map((part) => [part.type, part.name || '', part.on, part.isNew || false])), [
    ['svc', 'Automation', true, false],
    ['svc', 'modbus', false, true],
    ['svc', 'Geräte-Dienste', true, false],
    ['chips', '', null, false],
    ['svc', 'Tailscale', true, false],
    ['svc', 'HTTPS über Caddy', true, false],
  ]);
  assert.equal(screen.serviceParts[1].cls, 'svc off');
  assert.deepEqual(plain(screen.serviceParts[3].chips.map((chip) => [chip.name, chip.on])), [
    ['APsystems', true], ['Batterie-SoC', false], ['Shelly', true], ['Trucki', true], ['Tuya', true],
  ]);
});

test('Dienste aendern fuehrt in die Konfiguration im Nur-Dienste-Modus', async () => {
  const { screen, shell } = await mount();
  screen.editServices();
  assert.equal(shell.shared.servicesOnly, true);
  assert.equal(shell.screen, 'configure');
});

test('eine geaenderte Auswahl wird genannt und beim Abbrechen zurueckgeschrieben', async () => {
  const { screen, shell, calls } = await mount();
  shell.shared.selection = { source: 'node', steps: Object.assign({}, SELECTION_NODE.steps, { 89: true }) };
  assert.equal(screen.selectionChanged, true);
  assert.equal(screen.note, 'armv6 · Auswahl geändert');

  await screen.cancel();
  const put = calls.find((call) => call.key === 'PUT /api/selection');
  assert.deepEqual(plain(put.body), { steps: SELECTION_NODE.steps });
  assert.equal(shell.shared.selectionAtEntry, null);
  assert.equal(shell.screen, 'connect');
  assert.equal(shell.dir, 'back');
});

test('Aktualisieren startet den Lauf ohne Geheimnisse und vergisst die gemerkte Auswahl', async () => {
  const { screen, shell, calls } = await mount();
  await screen.start();
  assert.deepEqual(plain(calls.at(-1)), { key: 'POST /api/run', body: { mode: 'redeploy' } });
  assert.equal(shell.screen, 'run');
  assert.equal(shell.shared.run.mode, 'redeploy');
  assert.equal(shell.shared.selectionAtEntry, null);
});

test('im Dashboard-Wirt laedt Abbrechen neu, und der Hinweis auf das Dashboard entfaellt', async () => {
  const { screen, shell, calls } = await mount({ shell: { bootstrap: { host: 'dashboard', needs_connection: false, entry_points: ['redeploy', 'diagnose'] } } });
  assert.equal(screen.showReuse, false);
  const before = calls.length;
  await screen.cancel();
  assert.equal(shell.screen, 'preview');
  assert.ok(calls.slice(before).some((call) => call.key === 'GET /api/plan'), 'die Vorschau wurde neu geladen');
});

test('ein gescheiterter Plan landet im Banner und sperrt Aktualisieren', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/plan': { code: 'PLAN_FAILED', detail: 'plan.sh: exit 2', status: 500 } } });
  assert.equal(shell.error.code, 'PLAN_FAILED');
  assert.equal(screen.canStart, false);
});

test('die Wheel-Liste entspricht dem Bundle-Bau', async () => {
  const { window } = await mount();
  const script = fs.readFileSync(path.join(here, '..', '..', 'scripts', 'build', 'lib', 'wheels.sh'), 'utf8');
  const match = /for lib in ([a-z0-9_ ]+); do/.exec(script);
  assert.ok(match, 'build_local_wheels in scripts/build/lib/wheels.sh no longer lists its libraries in one for loop');
  assert.deepEqual([...window.PreviewModel.WHEEL_COMPONENTS], match[1].trim().split(/\s+/));
});
