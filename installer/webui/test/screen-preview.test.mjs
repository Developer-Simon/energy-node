import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { mountScreen, realCatalog, fakeShell } from './helpers/mount.mjs';
import { loadScripts } from './helpers/load.mjs';
import { MANIFEST_UPDATE, SELECTION_NODE, PLAN_UPDATE, PLAN_APPLY } from './helpers/fixtures.mjs';

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
    ['Service common library', '', true, '1.4.0', '1.4.2'],
    ['Battery SoC Core', '', false, '0.9.3', '0.9.3'],
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

test('der Funktionsumfang: Dienste in der Reihenfolge der Vorlage, der neue Dienst aus und als neu markiert', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.mods.services.map((mod) => [mod.id, mod.name, mod.on, mod.isNew])), [
    ['88', 'Automation', true, false],
    ['81', 'APsystems', true, false],
    ['82', 'Batterie-SoC', false, false],
    ['83', 'Shelly', true, false],
    ['84', 'Trucki', true, false],
    ['85', 'Tuya', true, false],
    ['40', 'Tailscale', true, false],
    ['70', 'HTTPS', true, false],
    ['89', 'modbus', false, true],
  ]);
  assert.deepEqual(plain(screen.mods.tuning), [], 'ohne Systemschritte keine Feinabstimmung');
  assert.equal(screen.mods.services[8].icon, 'device');
});

test('die Feinabstimmung zeigt nur die verfuegbaren Module', async () => {
  const { screen } = await mount({ responses: { 'GET /api/manifest': MANIFEST_15 } });
  assert.deepEqual(plain(screen.mods.tuning.map((mod) => [mod.id, mod.on, mod.isNew])), [['15', true, true]]);
  const unmet = Object.assign({}, MANIFEST_15, { steps: MANIFEST_15.steps.concat([{ id: '35', optional: true, default: false, requires: '99' }]) });
  const { screen: other } = await mount({ responses: { 'GET /api/manifest': unmet } });
  assert.deepEqual(plain(other.mods.tuning.map((mod) => mod.id)), ['15'], 'ein Schalter mit fehlender Voraussetzung fehlt');
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

test('ohne apply_steps im Plan gibt es keine Auswahl anwenden', async () => {
  const { screen } = await mount();
  assert.equal(screen.canApplySelection, false);
});

test('Auswahl anwenden: Neustarts mit Grund, und der Lauf kennt die Schritte', async () => {
  const { screen, shell, calls } = await mount({ responses: { 'GET /api/plan': PLAN_APPLY } });
  assert.equal(screen.canApplySelection, true);
  assert.deepEqual(plain(screen.restart), [
    { unit: 'energy-node-dashboard.service', reason: 'übernimmt die Auswahl' },
    { unit: 'apsystems-ez1.service', reason: 'wird gestartet' },
  ]);

  await screen.applySelection();
  assert.deepEqual(plain(calls.at(-1)), { key: 'POST /api/run', body: { mode: 'apply-selection' } });
  assert.equal(shell.screen, 'run');
  assert.equal(shell.shared.run.mode, 'apply-selection');
  assert.deepEqual(plain(shell.shared.run.steps), ['65', '81']);
});

test('waehrend des Laufs bleibt Auswahl anwenden angeboten, mit denselben Neustarten', async () => {
  const { screen } = await mount({ responses: { 'GET /api/plan': PLAN_APPLY } });
  screen.busy = true;
  assert.equal(screen.applyOffered, true);
  assert.equal(screen.canApplySelection, false);
  assert.deepEqual(plain(screen.restart), [
    { unit: 'energy-node-dashboard.service', reason: 'übernimmt die Auswahl' },
    { unit: 'apsystems-ez1.service', reason: 'wird gestartet' },
  ]);
});

test('ein abgewaehlter Dienst mit apply_steps wird gestoppt', async () => {
  const { screen, shell } = await mount({ responses: { 'GET /api/plan': PLAN_APPLY } });
  shell.shared.selection = { source: 'node', steps: Object.assign({}, SELECTION_NODE.steps, { 81: false }) };
  assert.equal(plain(screen.restart).find((row) => row.unit === 'apsystems-ez1.service').reason, 'wird gestoppt');
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

test('die Zaehlzeile zaehlt nur die Highlights, wenn der Changelog welche markiert', async () => {
  const flagged = JSON.parse(JSON.stringify(CHANGELOG_VIEW));
  const [feat, fix] = flagged.document.components[0].releases[0].groups;
  feat.entries[0].highlight = true;
  fix.entries[0].highlight = false;
  fix.entries[1].highlight = true;
  const { screen } = await mount({ responses: { 'GET /api/changelog': flagged } });
  assert.equal(screen.whatsNewLine, '1 Neuerung · 1 Korrektur · 1 Breaking Change');
});

// Schritt 15: die Vorschau zaehlt die Systempakete nicht von sich aus (auf
// einem Pi 1 dauert schon die Simulation), erst auf Abfrage.
const MANIFEST_15 = Object.assign({}, MANIFEST_UPDATE, {
  steps: [MANIFEST_UPDATE.steps[0], { id: '15', optional: true, default: true }].concat(MANIFEST_UPDATE.steps.slice(1)),
});

test('die Zeile Systempakete aktualisieren zaehlt die Updates erst auf Abfrage', async () => {
  const { screen, calls } = await mount({ responses: {
    'GET /api/manifest': MANIFEST_15,
    'GET /api/system-updates': { count: 12, checked_at: '2026-10-04T06:12:00+00:00', packages: [] },
  } });
  assert.ok(!calls.some((call) => call.key === 'GET /api/system-updates'), 'die Vorschau fragt nicht von sich aus');
  let line = screen.updatesLine;
  assert.ok(line, 'Zeile fuer Schritt 15 fehlt');
  assert.equal(line.check, true);
  assert.equal(line.detail, '');

  const pending = screen.checkUpdates();
  line = screen.updatesLine;
  assert.equal(line.check, false);
  assert.equal(line.detail, 'prüft …');
  await pending;
  line = screen.updatesLine;
  assert.equal(line.detail, '12 Updates · Stand 04.10.');
  assert.equal(line.check, false);
});

test('ohne Systempakete (Schritt 15) im Manifest gibt es keine Zeile', async () => {
  const { screen, calls } = await mount();
  assert.equal(screen.updatesLine, null);
  await screen.checkUpdates();
  assert.equal(calls.filter((call) => call.key === 'GET /api/system-updates').length, 1, 'nur auf Abfrage');
});

test('eine unbekannte Zahl oder ein Fehler heisst nicht ermittelbar, erneut pruefbar', async () => {
  const unknown = await mount({ responses: { 'GET /api/manifest': MANIFEST_15, 'GET /api/system-updates': null } });
  await unknown.screen.checkUpdates();
  let line = unknown.screen.updatesLine;
  assert.equal(line.detail, 'nicht ermittelbar');
  assert.equal(line.check, true);

  const failed = await mount({ responses: { 'GET /api/manifest': MANIFEST_15 }, errors: { 'GET /api/system-updates': { code: 'SYSTEM_UPDATES_FAILED', status: 502 } } });
  await failed.screen.checkUpdates();
  line = failed.screen.updatesLine;
  assert.equal(line.detail, 'nicht ermittelbar');
  assert.equal(failed.shell.error, null, 'kein Banner fuer eine Zusatzinfo');
});

// Das echte Template, von Alpine gebunden (wie screen-configure.test.mjs): die
// Go-Platzhalter fallen weg, die Icons kommen aus icons.html.
function previewMarkup() {
  const templates = path.join(here, '..', 'templates');
  const icons = fs.readFileSync(path.join(templates, 'icons.html'), 'utf8');
  const parts = {};
  for (const match of icons.matchAll(/\{\{define "([^"]+)"\}\}([\s\S]*?)\{\{end\}\}/g)) {
    parts[match[1]] = match[2];
  }
  const source = fs.readFileSync(path.join(templates, 'screen-preview.html'), 'utf8');
  const html = source.replace(/\{\{template "([^"]+)"\}\}/g, (_, name) => parts[name] || '');
  return html.slice(html.indexOf('<div class="screen"'), html.lastIndexOf('</template>'));
}

async function mountPreviewTemplate(manifest, plan = PLAN_UPDATE) {
  const alpine = fs.readFileSync(path.join(here, '..', 'static', 'js-deps', 'alpine.min.js'), 'utf8');
  const dom = loadScripts(['i18n.js', 'format.js', 'api.js', 'services.js', 'changelog-model.js', 'screen-preview.js'], {
    html: `<!doctype html><html><body><div class="app" x-data="harness">${previewMarkup()}</div></body></html>`,
  });
  const { window } = dom;
  window.Api.configure({ basePath: '', token: 'tok' });
  window.I18n.lang = 'de';
  window.I18n.catalog = realCatalog('de');
  const answers = {
    'GET /api/manifest': manifest,
    'GET /api/selection': structuredClone(SELECTION_NODE),
    'GET /api/plan': plan,
  };
  window.fetch = async (url) => {
    const key = `GET ${String(url).split('?')[0]}`;
    if (!(key in answers)) {
      throw new Error(`no canned response for ${key}`);
    }
    return { ok: true, status: 200, json: async () => answers[key] };
  };
  const shell = fakeShell(window, { entry: 'redeploy', screen: 'preview', connected: true });
  window.Installer = { shell };
  window.document.addEventListener('alpine:init', () => {
    window.Alpine.data('harness', () => ({ t: (key, params) => shell.t(key, params) }));
    window.Alpine.data('screenPreview', window.Screens.screenPreview);
  });
  vm.runInContext(alpine, dom.getInternalVMContext(), { filename: 'alpine.min.js' });
  await new Promise((resolve) => setTimeout(resolve, 20));
  return { document: window.document };
}

test('die Vorschau zeigt die Module nur lesend: Divs ohne Schalterrolle, der neue Dienst mit Tag', async () => {
  const { document } = await mountPreviewTemplate(MANIFEST_15);
  const mods = [...document.querySelectorAll('.mods .mod')];
  assert.equal(mods.length, 10, '9 Dienste und die Systempakete');
  for (const mod of mods) {
    assert.equal(mod.tagName, 'DIV');
    assert.equal(mod.hasAttribute('role'), false);
    assert.equal(mod.hasAttribute('aria-checked'), false);
    assert.ok(mod.querySelector('.mod-svg'), 'jedes Modul hat ein Symbol');
  }
  assert.equal(document.querySelectorAll('.mods [role="switch"]').length, 0, 'die Module sind keine Schalter (der Schalter "Alle neu starten" steht daneben)');
  const modbus = mods.find((mod) => mod.querySelector('.mod-long').textContent === 'modbus');
  assert.ok(modbus, 'der Dienst modbus steht in der Liste');
  assert.equal(modbus.classList.contains('on'), false);
  assert.equal(modbus.querySelector('.svc-tag').textContent, 'neu im Paket');
  assert.equal(document.querySelectorAll('.svc-tag').length, 2, 'modbus und die Systempakete sind neu');
  assert.equal(document.querySelector('.mod-hint .svc-check').textContent, 'Updates prüfen');
  assert.ok(document.querySelector('.svc-edit'), 'Dienste aendern bleibt');
});

test('ohne Systempakete zeigt die Vorschau keine Zeile unter der Liste', async () => {
  const { document } = await mountPreviewTemplate(MANIFEST_UPDATE);
  assert.equal(document.querySelector('.mod-hint'), null);
  assert.equal(document.querySelectorAll('.mod').length, 9);
});

test('die Fusszeile: ohne apply_steps Aktualisieren, mit apply_steps Auswahl anwenden', async () => {
  const { document } = await mountPreviewTemplate(MANIFEST_UPDATE);
  assert.equal(document.querySelector('.foot .primary').textContent, 'Aktualisieren');

  const { document: applying } = await mountPreviewTemplate(MANIFEST_UPDATE, PLAN_APPLY);
  const buttons = applying.querySelectorAll('.foot .primary');
  assert.equal(buttons.length, 1, 'genau ein primaerer Knopf');
  assert.equal(buttons[0].textContent, 'Auswahl anwenden');
});
