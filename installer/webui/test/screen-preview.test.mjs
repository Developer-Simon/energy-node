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
    scripts: ['services.js', 'changelog-model.js'],
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

test('Was sich aendert: je Komponente und je Dienst von/nach, unveraenderte Abhaengigkeiten zusammengefasst', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.components.map((row) => [row.label, row.em, row.changed, row.from, row.to])), [
    ['Bootstrap', '', true, '1.0.4', '1.0.5'],
    ['Tuya', '', true, '1.0.0', '1.0.1'],
    ['Automation', '', false, '1.0.0', '1.0.0'],
    ['Wheel', 'energy_node_common', true, '1.4.0', '1.4.2'],
    ['Wheel', 'battery_soc_core', false, '0.9.3', '0.9.3'],
    ['Abhängigkeit', 'tinytuya', true, '1.15.1', '1.16.0'],
    ['übrige Abhängigkeiten', '', false, '', 'unverändert'],
  ]);
});

test('eine Komponente ohne Vorzustand ist neu', async () => {
  const plan = Object.assign({}, PLAN_UPDATE, { components: { bootstrap: { from: null, to: 'v1.5.0' } } });
  const { screen } = await mount({ responses: { 'GET /api/plan': plan } });
  assert.deepEqual(plain(screen.components.map((row) => [row.changed, row.from, row.to])), [[true, 'neu', '1.5.0']]);
  assert.equal(screen.fromVersion, '', 'ohne dashboard kein "von"');
});

test('ohne Dienstversionen im Plan bleibt die Sammelzeile der Dienste', async () => {
  const steps = PLAN_UPDATE.steps.map((step) => { const copy = Object.assign({}, step); delete copy.from; delete copy.to; return copy; });
  const { screen } = await mount({ responses: { 'GET /api/plan': Object.assign({}, PLAN_UPDATE, { steps }) } });
  assert.deepEqual(plain(screen.components.slice(0, 2).map((row) => [row.label, row.from, row.to])), [
    ['Bootstrap', '1.0.4', '1.0.5'],
    ['Dienste (Repo)', '3.6.0', '3.7.1'],
  ]);
});

test('ein Dienst ohne installierte Version ist neu, bei unbekanntem Stand unbekannt', async () => {
  const steps = PLAN_UPDATE.steps.map((step) => {
    if (step.id === '85') { return Object.assign({}, step, { from: undefined, restart: 'first' }); }
    if (step.id === '88') { return Object.assign({}, step, { from: undefined, restart: 'unknown' }); }
    return step;
  });
  const { screen } = await mount({ responses: { 'GET /api/plan': Object.assign({}, PLAN_UPDATE, { steps }) } });
  const services = plain(screen.components.filter((row) => row.key.startsWith('service-')).map((row) => [row.label, row.from, row.to]));
  assert.deepEqual(services, [['Tuya', 'neu', '1.0.1'], ['Automation', 'unbekannt', '1.0.0']]);
});

test('Startet neu und Bleibt stehen folgen dem Plan (A17)', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.restart), [
    { unit: 'energy-node-dashboard.service', reason: 'neues Paket' },
    { unit: 'tuya.service', reason: 'neue Version' },
  ]);
  assert.equal(screen.keepNames, 'Systempakete · MQTT-Broker · Firewall · Tailscale · HTTPS über Caddy');
});

test('nur Dienste mit Aenderung stehen unter Neustarts, "Alle neu starten" nimmt alle dazu', async () => {
  const { screen } = await mount();
  const units = plain(screen.restart).map((row) => row.unit);
  assert.ok(units.includes('tuya.service'), 'geaenderter Dienst fehlt');
  assert.ok(!units.includes('automation.service'), 'unveraenderter Dienst darf nicht neu starten');
  screen.restartAll = true;
  const all = plain(screen.restart);
  assert.deepEqual(all.find((row) => row.unit === 'automation.service'), { unit: 'automation.service', reason: 'auf Wunsch' }, 'mit "alle" muss auch automation dabei sein');
  assert.equal(all.find((row) => row.unit === 'tuya.service').reason, 'neue Version', 'ein echter Grund bleibt auch mit "alle" stehen');
});

test('jeder Neustartgrund aus restart_rule hat einen Text', async () => {
  const reasons = ['version', 'library', 'first', 'unknown'];
  const steps = PLAN_UPDATE.steps.map((step) => step.id === '85' ? Object.assign({}, step, { restart: 'library' }) : step);
  const { screen } = await mount({ responses: { 'GET /api/plan': Object.assign({}, PLAN_UPDATE, { steps }) } });
  assert.equal(plain(screen.restart).find((row) => row.unit === 'tuya.service').reason, 'gemeinsame Bibliothek geändert');
  for (const reason of reasons) {
    assert.notEqual(screen.shell.t('preview.restart.reason.' + reason), 'preview.restart.reason.' + reason, reason);
  }
});

test('start sendet restart_all nur, wenn der Schalter an ist', async () => {
  const { screen, calls } = await mount();
  await screen.start();
  assert.equal(calls.find((call) => call.key === 'POST /api/run').body.restart_all, undefined);
  screen.restartAll = true;
  await screen.start();
  assert.equal(calls.filter((call) => call.key === 'POST /api/run').pop().body.restart_all, true);
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

test('im Dashboard-Wirt fuehrt Abbrechen zurueck ins Dashboard, und der Hinweis darauf entfaellt', async () => {
  const { screen, shell, calls } = await mount({ shell: { bootstrap: { host: 'dashboard', needs_connection: false, entry_points: ['redeploy', 'diagnose'] } } });
  assert.equal(screen.showReuse, false);
  const before = calls.length;
  await screen.cancel();
  assert.equal(shell.backToDashboardCalled, 1);
  assert.equal(calls.slice(before).some((call) => call.key === 'GET /api/plan'), false, 'die Vorschau wird nicht mehr neu geladen - das wiederholte nur denselben Fehler (z.B. MANIFEST_UNREADABLE ohne bereitliegendes Bundle)');
});

test('ein gescheiterter Plan landet im Banner und sperrt Aktualisieren', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/plan': { code: 'PLAN_FAILED', detail: 'plan.sh: exit 2', status: 500 } } });
  assert.equal(shell.error.code, 'PLAN_FAILED');
  assert.equal(screen.canStart, false);
});

test('die Wheel-Liste entspricht dem Bundle-Bau', async () => {
  const { window } = await mount();
  const script = fs.readFileSync(path.join(here, '..', '..', '..', 'scripts', 'build', 'lib', 'wheels.sh'), 'utf8');
  const match = /for lib in ([a-z0-9_ ]+); do/.exec(script);
  assert.ok(match, 'build_local_wheels in scripts/build/lib/wheels.sh no longer lists its libraries in one for loop');
  assert.deepEqual([...window.PreviewModel.WHEEL_COMPONENTS], match[1].trim().split(/\s+/));
});

// --- "Was ist neu": Zusammenfassung ueber der Vorschau -------------------------
const CHANGELOG_VIEW = {
  bundle_version: 'v1.4.2',
  installed: { dashboard: 'v1.4.0' },
  document: { schema_version: 1, components: [
    { id: 'dashboard', label: 'Dashboard', kind: 'app', version: 'v1.4.2', releases: [
      { version: 'v1.4.2', date: '2026-09-21', groups: [
        { type: 'feat', label: 'Features', entries: [{ text: 'a', breaking: false }] },
        { type: 'fix', label: 'Fixes', entries: [{ text: 'b', breaking: false }, { text: 'c', breaking: true }] },
      ] },
    ] },
  ] },
};

test('die Vorschau zeigt eine Zaehlzeile, wenn das Paket einen Changelog mitbringt', async () => {
  const { screen } = await mount({ responses: { 'GET /api/changelog': CHANGELOG_VIEW } });
  assert.equal(screen.whatsNew, true);
  assert.equal(screen.whatsNewLine, '1 Neuerung · 2 Korrekturen · 1 Breaking Change');
});

test('ohne Changelog (404) bleibt die Vorschau ohne Zaehlzeile und ohne Fehler', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/changelog': { status: 404, code: 'NO_CHANGELOG' } } });
  assert.equal(screen.whatsNew, false);
  assert.equal(screen.whatsNewLine, '');
  assert.equal(shell.error, null);
});

test('die Zaehlzeile faellt weg, wenn nichts neuer ist als das Installierte', async () => {
  const upToDate = { ...CHANGELOG_VIEW, installed: { dashboard: 'v1.4.2' } };
  const { screen } = await mount({ responses: { 'GET /api/changelog': upToDate } });
  assert.equal(screen.whatsNew, false);
});

test('openChangelog merkt sich die Vorschau als Rueckweg und wechselt den Bildschirm', async () => {
  const { screen, shell } = await mount({ responses: { 'GET /api/changelog': CHANGELOG_VIEW } });
  screen.openChangelog();
  assert.equal(shell.screen, 'changelog');
  assert.equal(shell.shared.changelogFrom, 'preview');
});
