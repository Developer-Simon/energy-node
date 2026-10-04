#!/usr/bin/env node
// Nimmt die Installer-Screenshots fuer docs/installer.md neu auf, gegen den
// Testwirt (cmd/fakehost) im Szenario vorlage-update, englisch:
//
//   cd installer/webui
//   node test/e2e/docs-screenshots.mjs                    # alle Bilder
//   node test/e2e/docs-screenshots.mjs installer-changelog
//   node test/e2e/docs-screenshots.mjs --out /tmp/shots   # woanders hin
//
// Geschrieben werden installer-update.png (die Vorschau) und
// installer-changelog.png (Was ist neu, Ansicht Highlights). Die uebrigen
// Installer-Bilder haben noch kein Skript.
import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { startFakehost } from './fakehost.mjs';

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

try {
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
