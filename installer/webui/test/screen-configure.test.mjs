import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { mountScreen, realCatalog, fakeShell } from './helpers/mount.mjs';
import { loadScripts } from './helpers/load.mjs';
import { MANIFEST, SELECTION, MODULE_MANIFEST, MODULE_SELECTION } from './helpers/fixtures.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

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

test('ohne beide Passwoerter startet nichts', async () => {
  const { screen } = await mount();
  assert.equal(screen.canStart, false);
  screen.mqttPassword = 'x';
  assert.equal(screen.canStart, false);
  screen.adminPassword = 'y';
  assert.equal(screen.canStart, true);
});

test('Weiter reicht die Eingaben an das Dashboard weiter, ohne etwas zu schicken', async () => {
  const { screen, shell, calls } = await mount();
  fillSecrets(screen);
  screen.steps['40'] = false;
  screen.start();

  assert.deepEqual(calls.map((call) => call.key), ['GET /api/manifest', 'GET /api/selection']);
  assert.equal(shell.screen, 'dashboard');
  assert.equal(shell.installInput.steps['40'], false);
  assert.equal(shell.installInput.mqttPassword, 'mqtt-geheim');
  assert.equal(shell.installInput.adminPassword, 'admin-geheim');
  assert.equal(shell.installInput.targetBase, '/home/energynode');
  assert.equal(shell.mutating, false);
  assert.equal(JSON.stringify(shell.shared).includes('geheim'), false, 'kein Passwort in shared');
});

test('canStart bleibt wahr bei leerem Hotspot-Passwort', async () => {
  const { screen } = await mount();
  fillSecrets(screen);
  screen.hotspotPassword = '';
  assert.equal(screen.canStart, true);
});

test('canStart ist falsch bei 7 Zeichen im Hotspot-Passwort', async () => {
  const { screen } = await mount();
  fillSecrets(screen);
  screen.hotspotPassword = 'abcdefg';
  assert.equal(screen.canStart, false);
});

test('canStart ist wahr bei 8 Zeichen mit Leerzeichen im Hotspot-Passwort', async () => {
  const { screen } = await mount();
  fillSecrets(screen);
  screen.hotspotPassword = 'ab cd ef';
  assert.equal(screen.canStart, true);
});

test('canStart ist wahr bei 64 Hex-Ziffern und falsch bei 64 Nicht-Hex-Zeichen', async () => {
  const { screen } = await mount();
  fillSecrets(screen);
  screen.hotspotPassword = 'a'.repeat(64);
  assert.equal(screen.canStart, true);
  screen.hotspotPassword = 'z'.repeat(64);
  assert.equal(screen.canStart, false);
});

test('start gibt hotspotPassword in installInput weiter', async () => {
  const { screen, shell } = await mount();
  fillSecrets(screen);
  screen.hotspotPassword = 'hotspot-geheim';
  screen.start();
  assert.equal(shell.installInput.hotspotPassword, 'hotspot-geheim');
});

test('ohne beide Passwoerter geht Weiter nicht weiter', async () => {
  const { screen, shell } = await mount();
  screen.mqttPassword = 'x';
  screen.start();
  assert.equal(shell.screen, 'configure');
  assert.ok(!shell.installInput);
});

test('Zurueck vom Dashboard stellt Auswahl und Passwoerter wieder her', async () => {
  const kept = { steps: Object.assign({}, SELECTION.steps, { 40: false }), mqttPassword: 'mqtt-geheim', adminPassword: 'admin-geheim' };
  const { screen } = await mount({ shell: { installInput: kept } });
  assert.equal(screen.steps['40'], false);
  assert.equal(screen.mqttPassword, 'mqtt-geheim');
  assert.equal(screen.adminPassword, 'admin-geheim');
});

test('Zurueck vom Dashboard stellt das Hotspot-Passwort wieder her', async () => {
  const kept = { steps: Object.assign({}, SELECTION.steps), mqttPassword: 'mqtt-geheim', adminPassword: 'admin-geheim', hotspotPassword: 'hotspot-geheim' };
  const { screen } = await mount({ shell: { installInput: kept } });
  assert.equal(screen.hotspotPassword, 'hotspot-geheim');
});

test('Zurueck aus der Konfiguration verwirft die gemerkten Eingaben', async () => {
  const { screen, shell } = await mount({ shell: { installInput: { steps: {} } } });
  screen.back();
  assert.equal(shell.installInput, null);
});

test('im Nur-Dienste-Modus speichert apply und kehrt in die Vorschau zurueck', async () => {
  const { screen, shell, calls } = await mountModules({ shell: { entry: 'redeploy', shared: { servicesOnly: true } } });
  assert.equal(screen.servicesOnly, true);
  assert.equal(screen.canApply, true);
  screen.toggleModule(moduleOf(screen, '82'));
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

test('ein allgemeines Bundle ohne Ziel nimmt den SSH-Benutzer und sein Heimatverzeichnis', async () => {
  const generic = Object.assign({}, MANIFEST);
  delete generic.target_user;
  delete generic.target_base;
  const { screen } = await mount({
    responses: {
      'GET /api/manifest': generic,
      'GET /api/selection': SELECTION,
      'PUT /api/selection': (body) => ({ source: 'node', steps: body.steps }),
      'POST /api/run': { run_id: 'run-1', seq: 3 },
    },
    shell: { shared: { target: { host: 'knoten', user: 'orgelbau' } } },
  });
  assert.equal(screen.targetUser, 'orgelbau');
  assert.equal(screen.targetBase, '/home/orgelbau');
  assert.equal(screen.mqttUser, 'orgelbau');
});

test('der Shelly-Webhook folgt dem Shelly-Modul: nicht verfuegbar ohne Shelly, danach nicht ungefragt an', async () => {
  const { screen } = await mountModules();
  assert.equal(moduleOf(screen, '35').on, true, 'Ausgangslage: an');
  screen.toggleModule(moduleOf(screen, '83'));
  assert.equal(screen.steps['35'], false, 'ohne Shelly faellt der Webhook weg');
  assert.equal(moduleOf(screen, '35').available, false);

  screen.toggleModule(moduleOf(screen, '83'));
  assert.equal(moduleOf(screen, '35').available, true, 'wieder verfuegbar');
  assert.equal(screen.steps['35'], false, 'aber nicht ungefragt an');
});

test('eine gespeicherte Auswahl mit Webhook, aber ohne Shelly wird beim Laden bereinigt', async () => {
  const steps = MANIFEST.steps.concat([{ id: '35', optional: true, default: false, requires: '83' }]);
  const { screen } = await mount({ responses: {
    'GET /api/manifest': Object.assign({}, MANIFEST, { steps }),
    'GET /api/selection': { source: 'node', steps: Object.assign({}, SELECTION.steps, { 35: true, 83: false }) },
  } });
  assert.equal(screen.steps['35'], false);
});

test('init records firstInstall from the first selection only', async () => {
  const { screen, shell } = await mount();
  assert.equal(shell.shared.firstInstall, true, 'erste Antwort manifest-default');
  shell.shared.selection = { source: 'pending', steps: {} };
  await screen.init();
  assert.equal(shell.shared.firstInstall, true, 'ein spaeteres Laden aendert es nicht');
});

// Module-Ansicht: das Manifest mit den Systemschritten 15, 17, 35, 41 und 89.
async function mountModules(options = {}) {
  return mount(Object.assign({ responses: {
    'GET /api/manifest': MODULE_MANIFEST,
    'GET /api/selection': MODULE_SELECTION,
    'PUT /api/selection': (body) => ({ source: 'node', steps: body.steps }),
    'POST /api/run': { run_id: 'run-1', seq: 3 },
  } }, options));
}

function moduleOf(screen, id) {
  const all = screen.mods.services.concat(screen.mods.tuning);
  return all.find((mod) => mod.id === id);
}

const DE = realCatalog('de');

test('ein Tipp auf ein Modul schaltet den Schritt, Feinabstimmung ohne Voraussetzung faellt weg', async () => {
  const { screen } = await mountModules();
  screen.toggleModule(moduleOf(screen, '83'));
  assert.equal(screen.steps['83'], false);
  assert.equal(screen.steps['35'], false, 'Webhook braucht Shelly');
  assert.equal(moduleOf(screen, '35').available, false);
});

test('der Hinweis beginnt bei Automation und Journal und folgt dem letzten Tipp', async () => {
  const { screen } = await mountModules();
  assert.equal(screen.hintText.services, DE['configure.hint.service.automation']);
  assert.equal(screen.hintText.tuning, DE['configure.hint.step.17']);
  screen.showHint(moduleOf(screen, '40'));
  assert.equal(screen.hintText.services, DE['configure.hint.step.40']);
  assert.equal(screen.hintText.tuning, DE['configure.hint.step.17'], 'Feinabstimmung unveraendert');
});

test('ohne Erstinstallation gibt es kein Beispiel, aber Feinabstimmung und Zaehler', async () => {
  const { screen } = await mountModules({ shell: { shared: { servicesOnly: true, firstInstall: false } } });
  assert.equal(screen.mods.example, null);
  assert.ok(screen.mods.tuning.length > 0);
  assert.equal(screen.mods.count.on, 7);
  assert.equal(screen.mods.count.total, 8);
});

test('der Beispielschalter ist gesperrt, wenn ein Dienst fehlt, und bleibt danach aus', async () => {
  const { screen, shell } = await mountModules();
  assert.equal(screen.mods.example.available, true);
  assert.equal(shell.t('configure.example.needs'), DE['configure.example.needs']);

  screen.toggleModule(moduleOf(screen, '81'));
  assert.equal(screen.steps['89'], false, 'APsystems aus nimmt das Beispiel weg');
  assert.equal(screen.mods.example.available, false);

  screen.toggleModule(moduleOf(screen, '81'));
  assert.equal(screen.steps['89'], false, 'nach dem Wiedereinschalten bleibt es aus');
  assert.equal(screen.mods.example.available, true);
});

// Das echte Template, von Alpine gebunden: die Go-Platzhalter fallen weg, die
// Icons kommen aus icons.html. Alpine startet erst, wenn die Komponente
// registriert ist (deferLoadingAlpine), damit x-data sie findet.
function configureMarkup() {
  const templates = path.join(here, '..', 'templates');
  const icons = fs.readFileSync(path.join(templates, 'icons.html'), 'utf8');
  const parts = {};
  for (const match of icons.matchAll(/\{\{define "([^"]+)"\}\}([\s\S]*?)\{\{end\}\}/g)) {
    parts[match[1]] = match[2];
  }
  const source = fs.readFileSync(path.join(templates, 'screen-configure.html'), 'utf8');
  const html = source.replace(/\{\{template "([^"]+)"\}\}/g, (_, name) => parts[name] || '');
  return html.slice(html.indexOf('<div class="screen"'), html.lastIndexOf('</template>'));
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 10));

async function mountTemplate(shared = {}) {
  const alpine = fs.readFileSync(path.join(here, '..', 'static', 'js-deps', 'alpine.min.js'), 'utf8');
  const dom = loadScripts(['i18n.js', 'format.js', 'api.js', 'services.js', 'screen-configure.js'], {
    html: `<!doctype html><html><body><div class="app" x-data="harness">${configureMarkup()}</div></body></html>`,
  });
  const { window } = dom;
  window.I18n.lang = 'de';
  window.I18n.catalog = realCatalog('de');
  window.fetch = async () => { throw new Error('the template test fetches nothing'); };
  const shell = fakeShell(window, { shared: Object.assign({
    manifest: MODULE_MANIFEST, selection: MODULE_SELECTION, firstInstall: true,
  }, shared) });
  window.Installer = { shell };

  // Wie app.js: die Komponenten erst im Ereignis alpine:init registrieren.
  window.document.addEventListener('alpine:init', () => {
    window.Alpine.data('harness', () => ({ t: (key, params) => shell.t(key, params) }));
    window.Alpine.data('screenConfigure', window.Screens.screenConfigure);
  });
  vm.runInContext(alpine, dom.getInternalVMContext(), { filename: 'alpine.min.js' });
  await settle();
  return { window, document: window.document, shell };
}

test('jedes Modul ist ein Schalter mit eigener Beschreibung, der Hinweis ist verborgen', async () => {
  const { document, window } = await mountTemplate();
  const mods = [...document.querySelectorAll('.mod')];
  assert.equal(mods.length, 12, '8 Dienste und 4 Feinabstimmungen');
  for (const mod of mods) {
    assert.equal(mod.getAttribute('role'), 'switch');
    assert.equal(mod.getAttribute('aria-checked'), String(mod.classList.contains('on')));
    const target = document.getElementById(mod.getAttribute('aria-describedby'));
    assert.ok(target, 'aria-describedby zeigt auf ein Element');
    assert.equal(mod.querySelector('.sr-only'), target, 'die Beschreibung steckt im Modul');
    assert.ok(target.textContent.length > 0, 'mit Langtext');
  }
  assert.equal(document.getElementById('mod-hint-83').textContent, window.I18n.t('configure.hint.service.shelly'));
  const hints = [...document.querySelectorAll('.mod-hint')];
  assert.equal(hints.length, 2);
  assert.ok(hints.every((hint) => hint.getAttribute('aria-hidden') === 'true'));
});

test('ein Klick schaltet das Modul, und aria-checked folgt', async () => {
  const { document } = await mountTemplate();
  const shelly = document.querySelector('.mod[aria-describedby="mod-hint-83"]');
  assert.equal(shelly.getAttribute('aria-checked'), 'true');
  shelly.click();
  await settle();
  assert.equal(shelly.getAttribute('aria-checked'), 'false');
  assert.equal(shelly.classList.contains('on'), false);
});

test('im Nur-Dienste-Modus gibt es kein Beispiel, aber Feinabstimmung und Zaehler', async () => {
  const { document } = await mountTemplate({ servicesOnly: true, firstInstall: false });
  assert.equal(document.querySelector('label.ex'), null);
  assert.equal(document.querySelectorAll('.mods').length, 2);
  assert.equal(document.querySelector('.mods-count').textContent.trim(), '7 von 8 an');
});
