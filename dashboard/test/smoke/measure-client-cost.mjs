#!/usr/bin/env node
// Misst, was ein offener Uebersichts-Tab im Leerlauf kostet - gegen ein mit
// run-local-dashboard.sh --keep gestartetes Dashboard.
//
// Warum CDP und nicht performance.now(): die Kosten des outerHTML-Tauschs
// liegen in Layout und Style-Neuberechnung, also ausserhalb von JavaScript.
// Chrome DevTools Protocol "Performance.getMetrics" liefert genau diese
// Zaehler kumulativ; die Differenz zweier Abrufe ist die Zeit dazwischen.
//
// Anmeldung wie in screenshot.mjs: ueber HTTP ist nur der Gast-Zugang
// moeglich (login.html / GuestOnly).
//
//   dashboard/test/smoke/run-local-dashboard.sh --keep --preset energie-simulate &
//   node dashboard/test/smoke/measure-client-cost.mjs --seconds 30
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

function parseArgs(argv) {
  const args = { url: 'http://localhost:18100/', seconds: 30, out: null, width: 1280, height: 900, swapTarget: 'overview-live' };
  for (let i = 0; i < argv.length; i++) {
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    const value = inlineValue ?? argv[++i];
    if (flag === '--url') args.url = value;
    else if (flag === '--seconds') args.seconds = Number(value);
    else if (flag === '--out') args.out = value;
    else if (flag === '--width') args.width = Number(value);
    else if (flag === '--height') args.height = Number(value);
    else if (flag === '--swap-target') args.swapTarget = value;
    else throw new Error(`unbekannte Option: ${flag}`);
  }
  return args;
}

// Die sechs Zaehler, mit denen die Spec argumentiert. Nodes und LayoutCount
// sind Zaehlerstaende, keine Zeiten - fuer sie ist der Endwert die Aussage,
// nicht die Differenz.
const DURATIONS = ['TaskDuration', 'ScriptDuration', 'LayoutDuration', 'RecalcStyleDuration'];
const GAUGES = ['Nodes', 'LayoutCount'];

const toMap = metrics => Object.fromEntries(metrics.map(m => [m.name, m.value]));

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
  // Das Fragment, dessen Tausche gezaehlt werden - "overview-live" fuer die
  // Uebersicht, "devices-live" fuer den Geraete-Tab (--url ...?panel=devices).
  await page.waitForSelector(`#${args.swapTarget}`, { timeout: 15000 });

  // Der Zaehler haengt am document, nicht an einer Variablen im Skript:
  // htmx feuert afterSwap am ersetzten Element, das nach dem Tausch ein
  // anderes Objekt ist.
  await page.evaluate(target => {
    window.__swapCount = 0;
    document.addEventListener('htmx:afterSwap', event => {
      if (event.target?.id === target) window.__swapCount++;
    });
  }, args.swapTarget);

  const client = await page.context().newCDPSession(page);
  await client.send('Performance.enable');
  const before = toMap((await client.send('Performance.getMetrics')).metrics);
  await page.waitForTimeout(args.seconds * 1000);
  const after = toMap((await client.send('Performance.getMetrics')).metrics);
  const swaps = await page.evaluate(() => window.__swapCount);

  const wall = (after.Timestamp - before.Timestamp) * 1000;
  const result = { url: args.url, seconds: args.seconds, swapTarget: args.swapTarget, wallMs: Math.round(wall), swaps, durations: {}, gauges: {} };
  for (const name of DURATIONS) {
    const ms = ((after[name] ?? 0) - (before[name] ?? 0)) * 1000;
    result.durations[name] = { ms: Math.round(ms), percentCPU: Number((100 * ms / wall).toFixed(1)) };
  }
  for (const name of GAUGES) result.gauges[name] = after[name] ?? 0;

  console.log(`Messfenster ${result.wallMs} ms, ${swaps} Tausch(e) auf #${args.swapTarget}`);
  for (const [name, value] of Object.entries(result.durations)) {
    console.log(`${name.padEnd(22)} ${String(value.ms).padStart(6)} ms  (${value.percentCPU} % CPU)`);
  }
  for (const [name, value] of Object.entries(result.gauges)) {
    console.log(`${name.padEnd(22)} ${String(value).padStart(6)}`);
  }
  if (args.out) {
    await mkdir(path.dirname(args.out), { recursive: true });
    await writeFile(args.out, JSON.stringify(result, null, 2));
    console.log(`JSON geschrieben: ${args.out}`);
  }
} finally {
  await browser.close();
}
