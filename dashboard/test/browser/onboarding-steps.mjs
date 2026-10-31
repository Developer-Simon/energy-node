#!/usr/bin/env node
// Geht an der laufenden Demo (demo/demo.sh) jeden Einrichtungsschritt einzeln
// durch und prueft, ob das Spotlight auf dem Ziel liegt. Je Breite und Schritt:
// der Schritt ist aktiv, das Spotlight ist an und nicht verloren, und sein
// Rechteck (Ring) ist das Zielrechteck mit dem Rand der Feder (8 px je Seite).
// Die Toleranz betraegt 1 px. Exit 1 bei Abweichung.
//
//   node dashboard/test/browser/onboarding-steps.mjs --url https://localhost:18200 --widths 1280,390 [id ...]
import { chromium } from 'playwright';

const PAD = 8;
const TOLERANCE = 1;

function parseArgs(argv) {
  const args = { url: 'https://localhost:18200', widths: [1280, 390], password: 'smoketest1234', ids: [] };
  for (let i = 0; i < argv.length; i++) {
    if (!argv[i].startsWith('--')) { args.ids.push(argv[i]); continue; }
    const [flag, inlineValue] = argv[i].split(/=(.*)/s);
    const value = inlineValue ?? argv[++i];
    if (flag === '--url') args.url = value.replace(/\/$/, '');
    else if (flag === '--widths') args.widths = value.split(',').map(Number);
    else if (flag === '--password') args.password = value;
    else throw new Error(`unbekannte Option: ${flag}`);
  }
  return args;
}

// Liest Schrittliste, Lauf und Spotlight-Rechteck aus der Seite.
const readState = (page, id) => page.evaluate((id) => {
  const store = Alpine.store('onboarding');
  const root = document.querySelector('.ob-spot');
  const ring = root.querySelector('.ob-spot-ring');
  const step = store.step;
  const box = r => ({ x: Math.round(r.left), y: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height) });
  const targetEl = step && step.target ? window.obOnboarding.resolveTarget(step.target) : null;
  const visible = document.querySelectorAll(`[data-tour="${id}"]`);
  const visibleCount = [...visible].filter(el => window.obSpotlight.visibleRect(el)).length;
  return {
    active: Boolean(store.active && step && step.id === id),
    onSpot: root.classList.contains('is-on'),
    spot: box(ring.getBoundingClientRect()),
    target: targetEl && window.obSpotlight.visibleRect(targetEl) ? box(targetEl.getBoundingClientRect()) : null,
    visibleTourCount: visibleCount,
    hasTourAttr: visible.length > 0,
  };
}, id);

const settled = async (page, id) => {
  // Warten, bis der Schritt aktiv ist und die Feder nicht mehr wandert.
  const started = Date.now();
  let last = '';
  let calm = 0;
  while (Date.now() - started < 6000) {
    const s = await readState(page, id).catch(() => null);
    const key = s ? JSON.stringify([s.active, s.onSpot, s.spot]) : '';
    calm = key === last ? calm + 1 : 0;
    last = key;
    if (s && s.active && s.onSpot && calm >= 6) return s;
    await page.waitForTimeout(100);
  }
  return readState(page, id).catch(() => null);
};

const diff = (spot, target) => {
  const want = { x: target.x - PAD, y: target.y - PAD, w: target.w + PAD * 2, h: target.h + PAD * 2 };
  return ['x', 'y', 'w', 'h'].filter(k => Math.abs(spot[k] - want[k]) > TOLERANCE)
    .map(k => `${k} ${spot[k]} statt ${want[k]}`);
};

const args = parseArgs(process.argv.slice(2));
const browser = await chromium.launch({ args: ['--no-sandbox'] });
let failed = false;
try {
  for (const width of args.widths) {
    const height = width < 600 ? 844 : 860;
    const ctx = await browser.newContext({
      ignoreHTTPSErrors: true,
      viewport: { width, height },
      isMobile: width < 600,
      hasTouch: width < 600,
    });
    await ctx.request.post(`${args.url}/api/v1/auth/login`, { data: { username: 'admin', password: args.password } });
    const page = await ctx.newPage();
    page.on('pageerror', error => console.log(`  SEITENFEHLER ${width}px: ${error.message}`));
    await page.goto(`${args.url}/`, { waitUntil: 'networkidle' });
    await page.waitForFunction(() => window.Alpine?.store('onboarding') && window.obSteps, null, { timeout: 15000 });
    const all = await page.evaluate(() => window.obSteps.setup.map(step => step.id));
    const ids = args.ids.length ? all.filter(id => args.ids.includes(id)) : all;

    for (const id of ids) {
      // Lauf "change" mit genau diesem Schritt. "setup" ab dem Schritt liefe weiter.
      await page.evaluate((id) => Alpine.store('onboarding').start('change', id), id);
      const state = await settled(page, id);
      if (!state) {
        failed = true;
        console.log(`  FEHL ${width}px ${id}: Zustand nicht lesbar`);
        continue;
      }
      const problems = [];
      if (!state.active) problems.push('Schritt nicht aktiv');
      if (!state.onSpot) problems.push('Spotlight aus');
      if (!state.target) problems.push('kein sichtbares Ziel');
      if (state.hasTourAttr && state.visibleTourCount !== 1) problems.push(`${state.visibleTourCount} sichtbare Ziele`);
      if (state.target && state.onSpot) problems.push(...diff(state.spot, state.target));
      if (problems.length) {
        failed = true;
        console.log(`  FEHL ${width}px ${id}: ${problems.join(', ')}`);
      } else {
        console.log(`  OK   ${width}px ${id}`);
      }
    }
    await ctx.close();
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
