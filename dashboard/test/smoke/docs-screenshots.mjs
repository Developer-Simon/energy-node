#!/usr/bin/env node
// Nimmt die Dashboard-Screenshots fuer docs/ neu auf (docs/images/dashboard*.png).
// Erwartet ein mit run-local-dashboard.sh --keep gestartetes Dashboard; die
// Anmeldung laeuft als Admin ueber die API mit dem Header, den das Smoke-Skript
// auch nutzt (X-Forwarded-Proto: https).
//
// Zwei Laeufe, weil --simulate-installed und --simulate-package sich
// ausschliessen:
//   A) run-local-dashboard.sh --keep --preset docs-screenshots --simulate-package
//      node test/smoke/docs-screenshots.mjs
//      node test/smoke/docs-screenshots.mjs --history     # ca. 12 min Aufzeichnung
//   B) run-local-dashboard.sh --keep --preset docs-screenshots --simulate-installed
//      node test/smoke/docs-screenshots.mjs dashboard-settings-versions
//
// Ohne Namen werden alle Bilder ausser dashboard-history und
// dashboard-settings-versions geschrieben. Namen als Argumente begrenzen den
// Lauf auf genau diese Bilder.
//
// Der Verlauf wird im Browser aufgezeichnet (IndexedDB, history-recorder.js).
// --history haelt deshalb eine Seite --record-minutes lang offen und
// fotografiert danach im selben Kontext den Verlauf-Tab.
import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');

function parseArgs(argv) {
  const args = {
    url: 'http://localhost:18100',
    out: path.join(REPO_DIR, 'docs/images'),
    password: 'smoketest1234',
    history: false,
    recordMinutes: 12,
    names: [],
  };
  for (let i = 0; i < argv.length; i++) {
    if (!argv[i].startsWith('--')) { args.names.push(argv[i]); continue; }
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    if (flag === '--history') { args.history = true; continue; }
    const value = inlineValue ?? argv[++i];
    if (flag === '--url') args.url = value.replace(/\/$/, '');
    else if (flag === '--out') args.out = path.resolve(value);
    else if (flag === '--password') args.password = value;
    else if (flag === '--record-minutes') args.recordMinutes = Number(value);
    else throw new Error(`unbekannte Option: ${flag}`);
  }
  return args;
}

const args = parseArgs(process.argv.slice(2));
const OPT_IN = ['dashboard-history', 'dashboard-settings-versions'];
const want = (name) => (args.names.length ? args.names.includes(name) : !OPT_IN.includes(name));

await mkdir(args.out, { recursive: true });
const browser = await chromium.launch({ args: ['--no-sandbox'] });

async function session(width, height) {
  const ctx = await browser.newContext({ extraHTTPHeaders: { 'X-Forwarded-Proto': 'https' }, viewport: { width, height } });
  await ctx.request.post(`${args.url}/api/v1/auth/login`, { data: { username: 'admin', password: args.password } });
  await ctx.addCookies([{ name: 'lang', value: 'en', url: args.url }]);
  const page = await ctx.newPage();
  await page.goto(`${args.url}/`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(2500);
  return { ctx, page };
}

async function tab(page, name) {
  if (name === 'overview') await page.locator('button.tab.is-page').first().click();
  else await page.locator('#tab-' + name).first().click();
  await page.waitForTimeout(3000);
}

// Die Bilder beginnen an der Tab-Leiste, nicht am Seitenkopf. Das Polster
// unten erlaubt das Scrollen auch auf kurzen Seiten.
async function scrollToTabs(page) {
  await page.evaluate(() => {
    document.body.style.paddingBottom = '1200px';
    const nav = document.querySelector('#tab-devices');
    window.scrollTo(0, nav.getBoundingClientRect().top + window.scrollY - 12);
  });
  await page.waitForTimeout(600);
}

async function shot(page, name) {
  await page.screenshot({ path: path.join(args.out, `${name}.png`) });
  console.log('geschrieben:', name);
}

async function history() {
  const s = await session(1110, 818);
  console.log(`zeichne den Verlauf ${args.recordMinutes} min auf ...`);
  await s.page.waitForTimeout(args.recordMinutes * 60 * 1000);
  await tab(s.page, 'history');
  await s.page.getByRole('button', { name: '1 h', exact: true }).first().click().catch(() => {});
  await s.page.waitForTimeout(3000);
  await scrollToTabs(s.page);
  await shot(s.page, 'dashboard-history');
  await s.ctx.close();
}

async function all() {
  let s;
  if (want('dashboard')) {
    s = await session(1920, 997);
    await shot(s.page, 'dashboard');
    await s.ctx.close();
  }
  if (want('dashboard-update-available')) {
    s = await session(1280, 650);
    await shot(s.page, 'dashboard-update-available');
    await s.ctx.close();
  }

  const tabs = [
    ['dashboard-overview', 'overview', 996],
    ['dashboard-devices', 'devices', 553],
    ['dashboard-config', 'config', 997],
    ['dashboard-device-map', 'devicemap', 928],
    ['dashboard-diagnosis', 'diagnostics', 1175],
  ];
  for (const [name, id, height] of tabs) {
    if (!want(name)) continue;
    s = await session(1110, height);
    await tab(s.page, id);
    if (id === 'config') {
      // Die Konfigurationskarten brauchen einen Moment, bis alle Dienste
      // geantwortet haben; danach einmal alle Karten auffrischen.
      await s.page.waitForTimeout(20000);
      for (const b of await s.page.getByRole('button', { name: 'Refresh', exact: true }).all()) await b.click().catch(() => {});
      await s.page.waitForTimeout(3000);
    }
    if (id === 'devices' && await s.page.locator('.device-density-toggle.active').count()) {
      await s.page.locator('.device-density-toggle').first().click();
      await s.page.waitForTimeout(1500);
    }
    if (id === 'devicemap') await frameMap(s.page, 0.9);
    await scrollToTabs(s.page);
    await shot(s.page, name);
    await s.ctx.close();
  }

  if (want('dashboard-devices-detailed')) {
    s = await session(1110, 997);
    await tab(s.page, 'devices');
    if (!await s.page.locator('.device-density-toggle.active').count()) await s.page.locator('.device-density-toggle').first().click();
    await s.page.waitForTimeout(1500);
    await scrollToTabs(s.page);
    await shot(s.page, 'dashboard-devices-detailed');
    await s.ctx.close();
  }
  if (want('dashboard-layout-editor')) {
    s = await session(1110, 1000);
    await s.page.getByRole('button', { name: 'Edit', exact: true }).first().click();
    await s.page.waitForTimeout(2000);
    await s.page.getByRole('button', { name: /^\s*Block\s*$/ }).first().click();
    await s.page.waitForTimeout(1500);
    await scrollToTabs(s.page);
    await shot(s.page, 'dashboard-layout-editor');
    await s.ctx.close();
  }
  if (want('dashboard-automation')) {
    s = await session(1110, 819);
    await tab(s.page, 'automations');
    await s.page.locator('[x-bind\\:aria-expanded^="Boolean(expanded"]').first().click();
    await s.page.waitForTimeout(1500);
    await scrollToTabs(s.page);
    await shot(s.page, 'dashboard-automation');
    await s.ctx.close();
  }

  await energyShowcase();
  await deviceMapShowcase();

  const subpages = [
    ['dashboard-settings-general', 'settings-general', 819],
    ['dashboard-settings-display', 'settings-display', 769],
    ['dashboard-settings-history', 'settings-history', 997],
    ['dashboard-settings-tinytuya', 'settings-tiny-tuya', 691],
    ['dashboard-settings-mqtt', 'settings-mqtt', 988],
    ['dashboard-settings-tailscale', 'settings-tailscale', 988],
    ['dashboard-settings-system', 'settings-system', 997],
    // Die Aenderungen des Dashboards aufgeklappt, damit der Schalter
    // Highlights/Alles etwas zu zeigen hat.
    ['dashboard-settings-versions', 'settings-versions', 1200, async (page) => {
      await page.locator('#settings-versions .version-item button').first().click();
      await page.waitForTimeout(1500);
    }],
  ];
  for (const [name, id, height, prepare] of subpages) {
    if (!want(name)) continue;
    s = await session(1110, height);
    await tab(s.page, 'settings');
    await s.page.locator(`#settings-panel [role="tab"][aria-controls="${id}"]`).click();
    await s.page.waitForTimeout(2000);
    if (prepare) await prepare(s.page);
    await scrollToTabs(s.page);
    await shot(s.page, name);
    await s.ctx.close();
  }

  if (want('dashboard-update-preview')) {
    s = await session(1280, 600);
    await s.page.goto(`${args.url}/redeploy/`, { waitUntil: 'networkidle' });
    await s.page.waitForTimeout(8000);
    await shot(s.page, 'dashboard-update-preview');
    await s.ctx.close();
  }

  // Mint zuletzt, damit das Dashboard danach wieder im Standardschema steht.
  const themes = [
    ['dashboard-theme-blue', 'Power blue'],
    ['dashboard-theme-yellow', 'Signal yellow'],
    ['dashboard-theme-light', 'Daylight'],
    ['dashboard-theme-default-mint', 'Mint'],
  ];
  for (const [name, label] of themes) {
    if (!want(name)) continue;
    s = await session(1920, 997);
    await tab(s.page, 'settings');
    await s.page.locator('#settings-panel [role="tab"][aria-controls="settings-display"]').click();
    await s.page.waitForTimeout(1000);
    await s.page.locator('#settings-display').getByText(label, { exact: true }).first().click();
    await s.page.locator('#settings-display').getByRole('button', { name: 'Save', exact: true }).first().click();
    await s.page.waitForTimeout(2500);
    await s.page.goto(`${args.url}/`, { waitUntil: 'networkidle' });
    await s.page.waitForTimeout(2500);
    await shot(s.page, name);
    await s.ctx.close();
  }
}

// Die ganze Anordnung der Device Map ins Bild holen und dann zur Ecke unten
// links hin verkleinern: oben liegt die Werkzeugleiste ueber der Flaeche,
// rechts bei offenem Panel das Panel. ratio < 1 laesst dafuer Platz.
async function frameMap(page, ratio) {
  await page.evaluate(r => {
    const container = document.querySelector('#devicemap-panel [x-ref="canvas"]');
    const cy = container._cyreg.cy;
    cy.fit(undefined, 40);
    cy.zoom({ level: cy.zoom() * r, renderedPosition: { x: 24, y: container.clientHeight - 24 } });
  }, ratio);
  await page.waitForTimeout(800);
}

// Einzelbilder fuer docs/dashboard/energy.md: je Einstellung ein Ausschnitt
// statt einer ganzen Seite. Das Seed docs-screenshots bringt dafuer zwei
// Gruppen (Garage einzeln, Workshop als Ganzes) und eine eigene Kategorie mit.
async function element(locator, name) {
  await locator.screenshot({ path: path.join(args.out, `${name}.png`) });
  console.log('geschrieben:', name);
}

async function energyShowcase() {
  const names = ['dashboard-energy-plant', 'dashboard-energy-interpretation', 'dashboard-energy-roles', 'dashboard-energy-groups', 'dashboard-energy-categories'];
  if (!names.some(want)) return;
  const s = await session(1110, 1400);
  await tab(s.page, 'energy');
  if (want('dashboard-energy-plant')) await element(s.page.locator('.energy-plant'), 'dashboard-energy-plant');
  for (const section of ['interpretation', 'roles', 'groups', 'categories']) {
    const name = `dashboard-energy-${section}`;
    if (!want(name)) continue;
    const toggle = s.page.locator(`#energy-acc-${section}-toggle`);
    if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click();
    await s.page.waitForTimeout(800);
    const box = await s.page.locator(`[data-energy-section="${section}"]`).boundingBox();
    // Die Rollentabelle ist lang, das Bild zeigt die Kacheln und die ersten Zeilen.
    const height = section === 'roles' ? Math.min(box.height, 640) : box.height;
    await s.page.evaluate(top => window.scrollTo(0, top), box.y + await s.page.evaluate(() => window.scrollY) - 8);
    await s.page.waitForTimeout(400);
    const fresh = await s.page.locator(`[data-energy-section="${section}"]`).boundingBox();
    await s.page.screenshot({ path: path.join(args.out, `${name}.png`), clip: { x: fresh.x, y: fresh.y, width: fresh.width, height } });
    console.log('geschrieben:', name);
  }
  await s.ctx.close();
}

async function deviceMapShowcase() {
  const names = ['dashboard-device-map-device-panel', 'dashboard-device-map-group-panel', 'dashboard-device-map-picker',
    'dashboard-device-map-view-menu', 'dashboard-device-map-create-group', 'dashboard-device-map-category',
    'dashboard-device-map-balance', 'dashboard-device-map-dataflow'];
  if (!names.some(want)) return;
  const s = await session(1110, 928);
  await tab(s.page, 'devicemap');
  const map = (fn, arg) => s.page.evaluate(fn, arg);
  const stage = s.page.locator('#devicemap-panel .devicemap-stage');
  await scrollToTabs(s.page);
  if (want('dashboard-device-map-device-panel')) {
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).openPanel('wallbox'));
    await s.page.waitForTimeout(1200);
    await frameMap(s.page, 0.75);
    await element(stage, 'dashboard-device-map-device-panel');
  }
  if (want('dashboard-device-map-group-panel') || want('dashboard-device-map-picker')) {
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).openPanel('group:workshop'));
    await s.page.waitForTimeout(1200);
    await frameMap(s.page, 0.75);
    if (want('dashboard-device-map-group-panel')) await element(stage, 'dashboard-device-map-group-panel');
    if (want('dashboard-device-map-picker')) {
      await s.page.locator('.devicemap-panel[data-open="true"]').getByRole('button', { name: 'Add members' }).click();
      await s.page.waitForTimeout(800);
      await element(s.page.locator('#devicemap-panel dialog.device-picker'), 'dashboard-device-map-picker');
      await s.page.keyboard.press('Escape');
      await s.page.waitForTimeout(400);
    }
  }
  await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).closePanel());
  await s.page.waitForTimeout(600);
  if (want('dashboard-device-map-view-menu')) {
    await s.page.locator('#devicemap-view-menu-toggle').click();
    await s.page.waitForTimeout(600);
    const toolbar = await s.page.locator('#devicemap-panel .devicemap-toolbar').boundingBox();
    const menu = await s.page.locator('#devicemap-view-menu').boundingBox();
    const x = Math.min(toolbar.x, menu.x) - 8;
    const y = toolbar.y - 8;
    await s.page.screenshot({ path: path.join(args.out, 'dashboard-device-map-view-menu.png'),
      clip: { x, y, width: Math.max(toolbar.x + toolbar.width, menu.x + menu.width) + 8 - x, height: menu.y + menu.height + 8 - y } });
    console.log('geschrieben: dashboard-device-map-view-menu');
    await s.page.locator('#devicemap-view-menu-toggle').click();
    await s.page.waitForTimeout(400);
  }
  if (want('dashboard-device-map-create-group')) {
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).openGroupDialog());
    await s.page.waitForTimeout(500);
    await s.page.locator('#devicemap-panel dialog[open] input').fill('Garden');
    await element(s.page.locator('#devicemap-panel dialog[open]'), 'dashboard-device-map-create-group');
    await s.page.keyboard.press('Escape');
    await s.page.waitForTimeout(400);
  }
  if (want('dashboard-device-map-category')) {
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).openCategoryDialog());
    await s.page.waitForTimeout(500);
    await s.page.locator('#devicemap-panel dialog[open] input').first().fill('Pool pump');
    await element(s.page.locator('#devicemap-panel dialog[open]'), 'dashboard-device-map-category');
    await s.page.keyboard.press('Escape');
    await s.page.waitForTimeout(400);
  }
  // Die Ebenen zuletzt: sie platzieren neue Knoten, gespeichert wird nur,
  // damit das Bild ohne den Hinweis auf ungespeicherte Positionen auskommt.
  for (const [name, layers] of [['dashboard-device-map-balance', ['balance']], ['dashboard-device-map-dataflow', ['data']]]) {
    if (!want(name)) continue;
    for (const [layer, label] of [['balance', 'Energy balance'], ['data', 'Data flow']]) {
      const on = await map(id => window.Alpine.$data(document.querySelector('#devicemap-panel')).view.layers[id], layer);
      if (on === layers.includes(layer)) continue;
      await s.page.locator('#devicemap-panel .devicemap-toolbar').getByRole('button', { name: label }).click();
      await s.page.waitForTimeout(2500);
    }
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).save());
    await s.page.waitForTimeout(1000);
    await frameMap(s.page, 0.92);
    await element(stage, name);
  }
  await s.ctx.close();
}

try {
  if (args.history) await history();
  else await all();
} finally {
  await browser.close();
}
