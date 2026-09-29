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
    ['dashboard-energy', 'energy', 997],
    ['dashboard-device-map', 'devicemap', 928],
    ['dashboard-diagnosis', 'diagnostics', 952],
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

  const subpages = [
    ['dashboard-settings-general', 'settings-general', 819],
    ['dashboard-settings-display', 'settings-display', 769],
    ['dashboard-settings-history', 'settings-history', 997],
    ['dashboard-settings-tinytuya', 'settings-tiny-tuya', 691],
    ['dashboard-settings-mqtt', 'settings-mqtt', 988],
    ['dashboard-settings-tailscale', 'settings-tailscale', 988],
    ['dashboard-settings-system', 'settings-system', 997],
    ['dashboard-settings-versions', 'settings-versions', 997],
  ];
  for (const [name, id, height] of subpages) {
    if (!want(name)) continue;
    s = await session(1110, height);
    await tab(s.page, 'settings');
    await s.page.locator(`#settings-panel [role="tab"][aria-controls="${id}"]`).click();
    await s.page.waitForTimeout(2000);
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

try {
  if (args.history) await history();
  else await all();
} finally {
  await browser.close();
}
