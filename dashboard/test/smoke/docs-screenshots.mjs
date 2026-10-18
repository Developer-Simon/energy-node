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
//   C) run-local-dashboard.sh --keep --preset docs-screenshots --simulate-package --fake-companion
//      node test/smoke/docs-screenshots.mjs dashboard-statistics
//
// --cards nimmt jede Uebersichtskarte einzeln in ihrer Mindestgroesse auf
// (docs/images/dashboard-card-*.png, fuer docs/dashboard/overview-cards.md).
// Dafuer legt es eine eigene Layout-Seite an, zeichnet --record-minutes lang
// Verlauf auf (Vorgabe hier 16 min, die Batterie-Trajektorie braucht 15) und stellt
// das Layout danach wieder her. Am besten gegen
//   run-local-dashboard.sh --keep --preset docs-screenshots --simulate
// damit Tagesband und Verlauf sich bewegen.
//
// Ohne Namen werden alle Bilder ausser dashboard-history,
// dashboard-settings-versions und dashboard-statistics geschrieben. Namen als Argumente begrenzen den
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
    cards: false,
    recordMinutes: null,
    names: [],
  };
  for (let i = 0; i < argv.length; i++) {
    if (!argv[i].startsWith('--')) { args.names.push(argv[i]); continue; }
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    if (flag === '--history') { args.history = true; continue; }
    if (flag === '--cards') { args.cards = true; continue; }
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
const OPT_IN = ['dashboard-history', 'dashboard-settings-versions', 'dashboard-statistics'];
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
  args.recordMinutes ??= 12;
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
  await deviceModalShowcase();

  if (want('dashboard-statistics')) {
    // Needs the fake companion (--fake-companion): it feeds the hourly values.
    s = await session(1110, 1100);
    await tab(s.page, 'stats');
    await s.page.waitForTimeout(2000);
    await scrollToTabs(s.page);
    await shot(s.page, 'dashboard-statistics');
    await s.ctx.close();
  }

  const subpages = [
    ['dashboard-settings-general', 'settings-general', 819],
    ['dashboard-settings-display', 'settings-display', 769],
    ['dashboard-settings-history', 'settings-history', 1235],
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

// Das Geraete-Modal: breit mit Seitenleiste und Tabs, schmal als Akkordeon.
// Es zeigt das Geraet mit den meisten Entitaeten, damit Messwerte und
// Steuerung beide gefuellt sind.
async function deviceModalShowcase() {
  const names = ['dashboard-device-modal', 'dashboard-device-modal-narrow', 'dashboard-device-modal-manage'];
  if (!names.some(want)) return;
  for (const [name, width, height] of [['dashboard-device-modal', 1280, 900], ['dashboard-device-modal-narrow', 560, 900]]) {
    if (!want(name)) continue;
    const s = await session(width, height);
    await tab(s.page, 'devices');
    await s.page.locator('.compact-card:visible, .device-detail-trigger:visible').first().click();
    await s.page.waitForSelector('dialog#device-modal[open]');
    await s.page.waitForTimeout(2500);
    await element(s.page.locator('dialog#device-modal'), name);
    if (name === 'dashboard-device-modal' && want('dashboard-device-modal-manage')) {
      await s.page.locator('.device-modal-trigger').filter({ hasText: 'Management' }).first().click();
      await s.page.waitForTimeout(1200);
      await element(s.page.locator('dialog#device-modal'), 'dashboard-device-modal-manage');
    }
    await s.ctx.close();
  }
}

async function deviceMapShowcase() {
  const names = ['dashboard-device-map-device-panel', 'dashboard-device-map-group-panel', 'dashboard-device-map-picker',
    'dashboard-device-map-view-menu', 'dashboard-device-map-create-group', 'dashboard-device-map-category',
    'dashboard-device-map-balance', 'dashboard-device-map-dataflow', 'dashboard-device-map-switch-off',
    'dashboard-device-map-switch-channel', 'dashboard-device-map-sub-device'];
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
  // Opens the wiring card of the edge between two nodes and frames both nodes.
  const openWiringCard = (a, b) => map(([a, b]) => {
    const panel = window.Alpine.$data(document.querySelector('#devicemap-panel'));
    const cy = document.querySelector('#devicemap-panel [x-ref="canvas"]')._cyreg.cy;
    const first = cy.getElementById(a);
    const second = cy.getElementById(b);
    const edge = first.edgesWith(second).filter('.devicemap-wiring')[0];
    panel.selectedEdge = {id: edge.id(), source: edge.data('source'), target: edge.data('target'), overrideId: edge.data('overrideId') || null, membership: edge.data('membership') || null};
    cy.edges().removeClass('devicemap-selected-edge');
    edge.addClass('devicemap-selected-edge');
    // The card opens to the right of the edge midpoint, so the labels on the
    // left of the pair stay uncovered.
    cy.fit(first.union(second), 120);
    panel.openWiringPopover(edge.renderedMidpoint());
  }, [a, b]);
  if (want('dashboard-device-map-switch-off')) {
    // Plug Balcony is switched off: the wiring card of its edge to the
    // inverter is open, the view is framed on that pair and its neighbours.
    await openWiringCard('plug_balkon', 'balkon_kraftwerk');
    await s.page.waitForTimeout(1200);
    await element(stage, 'dashboard-device-map-switch-off');
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).clearEdgeSelection());
    await s.page.waitForTimeout(600);
  }
  if (want('dashboard-device-map-switch-channel')) {
    // Channel 2 of Cellar Relay is off: its edge to the dehumidifier is cut,
    // the card shows the switch state and the cause above the link.
    await openWiringCard('relais_keller', 'plug_entfeuchter');
    await s.page.waitForTimeout(1200);
    await element(stage, 'dashboard-device-map-switch-channel');
    await map(() => window.Alpine.$data(document.querySelector('#devicemap-panel')).clearEdgeSelection());
    await s.page.waitForTimeout(600);
  }
  if (want('dashboard-device-map-sub-device')) {
    // The sub-device Washing machine hangs on the power value of the dehumidifier
    // plug: its panel is open, the view is framed on the pair and the arrows are curved (local only, not saved).
    await map(() => {
      const panel = window.Alpine.$data(document.querySelector('#devicemap-panel'));
      const cy = document.querySelector('#devicemap-panel [x-ref="canvas"]')._cyreg.cy;
      window.__prevEdgeStyle = panel.view.edge_style;
      panel.setEdgeStyle('curved');
      panel.openPanel('sub:waschmaschine');
      // Frame the pair in the part of the stage the open panel leaves free.
      const bb = cy.getElementById('sub:waschmaschine').union(cy.getElementById('plug_entfeuchter')).boundingBox();
      const free = cy.width() - 400;
      const zoom = Math.min(free / (bb.w + 200), cy.height() / (bb.h + 260), 1.1);
      cy.zoom(zoom);
      cy.pan({x: (free - (bb.x1 + bb.x2) * zoom) / 2, y: (cy.height() - (bb.y1 + bb.y2) * zoom) / 2});
      panel.unsaved = false;
      panel.placedCount = 0;
    });
    await s.page.waitForTimeout(1200);
    await element(stage, 'dashboard-device-map-sub-device');
    await map(() => {
      const panel = window.Alpine.$data(document.querySelector('#devicemap-panel'));
      panel.closePanel();
      panel.setEdgeStyle(window.__prevEdgeStyle);
      panel.unsaved = false;
    });
    await s.page.waitForTimeout(600);
  }
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
    for (const [layer, label] of [['balance', 'Roles'], ['data', 'Data flow']]) {
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

// Eine Karte je Typ in ihrer Mindestbreite (min_span Spalten) auf einer
// eigenen Layout-Seite. Die Spalten werden auf das Rastermindestmass 18rem
// festgesetzt, Karten mit fills_height auf ihre Mindesthoehe. Karten ohne
// fills_height nehmen ihre Inhaltshoehe, mindestens min_height. Die
// Spannen werden fest gesetzt, sonst dehnt layout-fill.js eine Karte in
// eine Zeilenluecke.
async function cards() {
  args.recordMinutes ??= 16;
  const s = await session(1500, 1200);
  const api = `${args.url}/api/v1/layout`;
  const original = await (await s.ctx.request.get(api)).json();
  const csrf = (await (await s.ctx.request.get(`${args.url}/api/v1/auth/session`)).json()).csrf_token;
  const put = (doc) => s.ctx.request.put(api, { data: { version: doc.version, pages: doc.pages }, headers: { 'X-CSRF-Token': csrf } });
  const types = original.card_types;
  const entries = [
    ['energy_status', {}], ['energy_flow', {}], ['energy_ring', {}], ['energy_schema', {}],
    ['energy_band', {}], ['energy_day', {}], ['energy_board', {}], ['energy_stats', {}],
    ['battery_status', { display: 'column' }],
    ['battery_status:trajectory', { type: 'battery_status', display: 'trajectory' }],
    ['history_view', { title: 'PV and grid', history_range_hours: '1', history_series: ['role:pv', 'role:grid'] }],
    ['diagnostics', {}],
    ['entity_value', { ref: 'bank_a_voltage' }],
    ['entity_group', { title: 'Battery banks', entity_refs: ['bank_a_voltage', 'bank_b_voltage', 'bank_soc'] }],
    ['device', { ref: 'wallbox', display: 'detail' }],
    ['device:compact', { type: 'device', ref: 'wallbox', display: 'compact' }],
  ];
  const items = entries.map(([key, extra]) => ({
    id: `cards-${key.replace(':', '-')}`, type: key.split(':')[0], span: String(types[key].min_span), visible: true, ...extra,
  }));
  const page = { id: 'docs-cards', name: 'Cards', order: original.pages.length, groups: [{ id: 'docs-cards-all', name: 'Cards', items }] };
  const res = await put({ version: original.version, pages: [...original.pages, page] });
  if (!res.ok()) throw new Error(`Layout abgelehnt: ${res.status()} ${await res.text()}`);
  try {
    await s.page.goto(`${args.url}/`, { waitUntil: 'networkidle' });
    console.log(`zeichne den Verlauf ${args.recordMinutes} min auf ...`);
    await s.page.waitForTimeout(args.recordMinutes * 60 * 1000);
    await s.page.locator('button.tab.is-page', { hasText: 'Cards' }).first().click();
    await s.page.waitForTimeout(4000);
    await s.page.addStyleTag({ content: `
      #overview-panel .layout-grid { grid-template-columns: repeat(auto-fill, 18rem) !important; align-items: start !important; }
      #overview-panel .layout-grid-item[data-fills-height="true"] { height: var(--card-min-height); }
      #overview-panel .layout-grid-item-1 { grid-column: span 1 !important; }
      #overview-panel .layout-grid-item-2 { grid-column: span 2 !important; }` });
    await s.page.waitForTimeout(2500);
    for (const [key] of entries) {
      const name = `dashboard-card-${key.replace(/[:_]/g, '-')}`;
      const item = s.page.locator(`[data-layout-item-id="cards-${key.replace(':', '-')}"]`);
      await item.scrollIntoViewIfNeeded();
      await s.page.waitForTimeout(500);
      await element(item, name);
    }
  } finally {
    await put(original);
    await s.ctx.close();
  }
}

try {
  if (args.history) await history();
  else if (args.cards) await cards();
  else await all();
} finally {
  await browser.close();
}
