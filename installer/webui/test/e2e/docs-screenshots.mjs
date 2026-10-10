#!/usr/bin/env node
// Nimmt die Installer-Screenshots fuer docs/installer.md neu auf, gegen den
// Testwirt (cmd/fakehost) im Szenario vorlage-update, englisch:
//
//   cd installer/webui
//   node test/e2e/docs-screenshots.mjs                    # alle Bilder
//   node test/e2e/docs-screenshots.mjs installer-changelog
//   node test/e2e/docs-screenshots.mjs --out /tmp/shots   # woanders hin
//
// Geschrieben werden installer-update.png (die Vorschau),
// installer-changelog.png (Was ist neu, Ansicht Highlights) und
// installer-run.png (die Ausfuehrung einer Erstinstallation, vom zweiten
// Testwirt im Szenario vorlage angehalten bei Schritt 85). Die uebrigen
// Installer-Bilder haben noch kein Skript.
import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import { startFakehost, SECRETS } from './fakehost.mjs';

const WEBUI_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const en = JSON.parse(fs.readFileSync(path.join(WEBUI_DIR, 'catalogs', 'en.json'), 'utf8'));
const REPO_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../..');

const args = { out: path.join(REPO_DIR, 'docs/images'), names: [] };
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === '--out') args.out = path.resolve(argv[++i]);
  else if (argv[i].startsWith('--')) throw new Error(`unbekannte Option: ${argv[i]}`);
  else args.names.push(argv[i]);
}
const want = (name) => !args.names.length || args.names.includes(name);

await mkdir(args.out, { recursive: true });
const browser = await chromium.launch({ args: ['--no-sandbox'] });
const host = await startFakehost(['--lang', 'en', '--trusted', '--scenario', 'vorlage-update']);

async function shot(page, name) {
  await page.screenshot({ path: path.join(args.out, `${name}.png`) });
  console.log('geschrieben:', name);
}

// Ueber den Einstieg Update und die Verbindung in die Vorschau. Die Felder
// werden ueber ihre Ids gefunden, damit das Skript nicht an der Sprache haengt.
async function preview() {
  const context = await browser.newContext({ viewport: { width: 1080, height: 900 } });
  const page = await context.newPage();
  await page.goto(host.url);
  await page.locator('.app:not([x-cloak]) .bar-title:not(:empty)').waitFor();
  await page.locator('.mode-i').nth(1).click();
  await page.locator('input[aria-labelledby="connect-address"]').fill('energy-node.local');
  await page.locator('input[aria-labelledby="connect-user"]').fill('pi');
  await page.locator('input[aria-labelledby="connect-password"]').fill('raspberry-7731');
  await page.locator('.app[data-screen="connect"] button.primary').click();
  await page.locator('.app[data-screen="preview"] .whatsnew').waitFor({ timeout: 30000 });
  await page.waitForTimeout(1000);
  return { context, page };
}

// Erstinstallation bis in die Ausfuehrung; der Wirt haelt bei Schritt 85 an,
// damit Tailscale, Dashboard und die ersten Dienste fertig, die Rest offen sind.
async function run() {
  const runHost = await startFakehost(['--lang', 'en', '--trusted', '--hold-step', '85']);
  const context = await browser.newContext({ viewport: { width: 1080, height: 1000 } });
  const page = await context.newPage();
  await page.goto(runHost.url);
  await page.locator('.app:not([x-cloak]) .bar-title:not(:empty)').waitFor();
  await page.getByLabel(en['field.address'], { exact: true }).fill('energy-node.local');
  await page.getByLabel(en['field.user'], { exact: true }).fill('pi');
  await page.getByLabel(en['field.password'], { exact: true }).fill(SECRETS.login);
  await page.getByRole('button', { name: en['action.connect'], exact: true }).click();
  await page.getByRole('button', { name: en['action.next'], exact: true }).click();
  await page.getByLabel(en['field.mqtt_password'], { exact: true }).fill(SECRETS.mqtt);
  await page.getByLabel(en['field.admin_password'], { exact: true }).fill(SECRETS.admin);
  await page.getByRole('button', { name: en['action.start_install'] }).click();
  await page.locator('.app[data-screen="run"] .stp.now').waitFor({ timeout: 30000 });
  await page.waitForTimeout(1500);
  return { context, page, stop: () => runHost.stop() };
}

try {
  if (want('installer-run')) {
    const { context, page, stop } = await run();
    await shot(page, 'installer-run');
    await context.close();
    await stop();
  }
  if (want('installer-update')) {
    const { context, page } = await preview();
    await shot(page, 'installer-update');
    await context.close();
  }
  if (want('installer-changelog')) {
    const { context, page } = await preview();
    await page.locator('.whatsnew button').click();
    await page.locator('.app[data-screen="changelog"] .cl-comp').first().waitFor();
    await page.waitForTimeout(800);
    await shot(page, 'installer-changelog');
    await context.close();
  }
} finally {
  await host.stop();
  await browser.close();
}
