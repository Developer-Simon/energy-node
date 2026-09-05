#!/usr/bin/env node
// Screenshot-Helfer fuer Sichtpruefungen am mit run-local-dashboard.sh --keep
// gestarteten Dashboard. Playwright ist echte devDependency (siehe
// package.json) und die Chromium-Revision liegt im ueblichen
// ~/.cache/ms-playwright - kein Ad-hoc-Aufbau mehr pro Lauf noetig.
//
// Anmeldung: ueber HTTP ist nur der Gast-Zugang moeglich (siehe login.html /
// GuestOnly), daher klickt dieses Skript "Als Gast fortfahren", wenn die
// Login-Seite erscheint - fuer Sichtpruefungen an Uebersicht/Kacheln reicht
// das, ein echtes Admin-Login ist hier nicht das Ziel.
//
// Beispiele:
//   node test/smoke/screenshot.mjs
//   node test/smoke/screenshot.mjs --url http://localhost:18100/geraete --out /tmp/geraete.png
import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

function parseArgs(argv) {
  // width/height wie in measure-client-cost.mjs: dieselben Flag-Namen fuer
  // denselben Zweck (Viewport-Groesse), statt einer eigenen Konvention hier.
  const args = { url: 'http://localhost:18100/', out: 'test/smoke/.run/screenshot.png', wait: null, width: 1280, height: 900 };
  for (let i = 0; i < argv.length; i++) {
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    const value = inlineValue ?? argv[++i];
    if (flag === '--url') args.url = value;
    else if (flag === '--out') args.out = value;
    else if (flag === '--wait') args.wait = value;
    else if (flag === '--width') args.width = Number(value);
    else if (flag === '--height') args.height = Number(value);
    else throw new Error(`unbekannte Option: ${flag}`);
  }
  return args;
}

const args = parseArgs(process.argv.slice(2));
const browser = await chromium.launch({ args: ['--no-sandbox'] });
try {
  const page = await browser.newPage({ viewport: { width: args.width, height: args.height } });
  await page.goto(args.url, { waitUntil: 'networkidle' });

  const guestButton = page.locator('#guest-login');
  if (await guestButton.isVisible().catch(() => false)) {
    await guestButton.click();
    await page.waitForLoadState('networkidle');
  }
  if (args.wait) await page.waitForSelector(args.wait, { timeout: 15000 });
  await page.waitForTimeout(500);

  await mkdir(path.dirname(args.out), { recursive: true });
  await page.screenshot({ path: args.out, fullPage: true });
  console.log(`Screenshot geschrieben: ${args.out}`);
} finally {
  await browser.close();
}
