#!/usr/bin/env node
// Prüft am laufenden Smoke-Dashboard, ob eine Seite breiter als das Fenster
// wird. Je Tab und Breite einmal geschlossen und einmal mit jedem sichtbaren
// Hilfe-Knopf fokussiert, weil ein offener Tooltip die Seite ebenfalls
// verbreitern kann. Meldet die äußersten Elemente, die über den rechten Rand
// ragen. Exit 1 bei Überstand.
//
//   node test/smoke/mobile-overflow.mjs --url http://localhost:18200 --widths 400,1280 overview settings energy devicemap stats
import { chromium } from 'playwright';

function parseArgs(argv) {
  const args = { url: 'http://localhost:18100', widths: [400], password: 'smoketest1234', tabs: [] };
  for (let i = 0; i < argv.length; i++) {
    if (!argv[i].startsWith('--')) { args.tabs.push(argv[i]); continue; }
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    const value = inlineValue ?? argv[++i];
    if (flag === '--url') args.url = value.replace(/\/$/, '');
    else if (flag === '--widths') args.widths = value.split(',').map(Number);
    else if (flag === '--password') args.password = value;
    else throw new Error(`unbekannte Option: ${flag}`);
  }
  if (!args.tabs.length) args.tabs = ['overview', 'settings', 'energy', 'devicemap', 'stats'];
  return args;
}

const measure = page => page.evaluate(() => {
  const viewport = document.documentElement.clientWidth;
  const culprits = [];
  for (const el of document.querySelectorAll('body *')) {
    const box = el.getBoundingClientRect();
    if (!box.width || box.right <= viewport + 1) continue;
    // Die Tab-Leiste scrollt in sich, ihre Knöpfe verbreitern die Seite nicht.
    if (el.closest('[role="tablist"]')) continue;
    const parent = el.parentElement && el.parentElement.getBoundingClientRect();
    if (parent && parent.right > viewport + 1) continue;
    culprits.push(`${el.tagName.toLowerCase()}.${[...el.classList].join('.')} rechts=${Math.round(box.right)}`);
  }
  return { overflow: document.documentElement.scrollWidth - viewport, culprits: culprits.slice(0, 6) };
});

const args = parseArgs(process.argv.slice(2));
const browser = await chromium.launch({ args: ['--no-sandbox'] });
let failed = false;
const report = (width, label, result) => {
  if (result.overflow > 0) {
    failed = true;
    console.log(`  FEHL ${width}px ${label}: ${result.overflow}px (${result.culprits.join(', ')})`);
  } else {
    console.log(`  OK   ${width}px ${label}`);
  }
};
try {
  for (const width of args.widths) {
    const ctx = await browser.newContext({ extraHTTPHeaders: { 'X-Forwarded-Proto': 'https' }, viewport: { width, height: 860 } });
    await ctx.request.post(`${args.url}/api/v1/auth/login`, { data: { username: 'admin', password: args.password } });
    const page = await ctx.newPage();
    await page.goto(`${args.url}/`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(2000);
    for (const tab of args.tabs) {
      const found = await page.evaluate(name => {
        const el = name === 'overview' ? document.querySelector('button.tab.is-page') : document.querySelector(`#tab-${name}`);
        if (el) el.click();
        return Boolean(el);
      }, tab);
      if (!found) { console.log(`  --   ${width}px ${tab}: kein Tab`); continue; }
      await page.waitForTimeout(3000);
      report(width, tab, await measure(page));
      const triggers = page.locator('.field-help-trigger:visible');
      const count = await triggers.count();
      for (let i = 0; i < count; i++) {
        await triggers.nth(i).focus();
        await page.waitForTimeout(200);
        const open = await measure(page);
        if (open.overflow > 0) report(width, `${tab} Hilfe ${i + 1}`, open);
      }
      await page.evaluate(() => document.activeElement && document.activeElement.blur());
    }
    await ctx.close();
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
