#!/usr/bin/env node
// Prueft am laufenden Dashboard (run-local-dashboard.sh --keep --preset
// verlauf-kachel), dass eine Verlaufskachel den Live-Tausch von
// #overview-live als derselbe DOM-Knoten ueberlebt (hx-preserve) und dass
// eine geaenderte Sicht einen neuen Knoten erzeugt.
//   node test/smoke/history-view-tile.mjs [--url http://localhost:18100/]
import { chromium } from 'playwright';

const urlArg = process.argv.indexOf('--url');
const url = (urlArg > 0 ? process.argv[urlArg + 1] : 'http://127.0.0.1:18100').replace(/\/$/, '');
const fail = message => { console.error(`FEHLER: ${message}`); process.exitCode = 1; };

const browser = await chromium.launch({ args: ['--no-sandbox'] });
try {
  const ctx = await browser.newContext({ extraHTTPHeaders: { 'X-Forwarded-Proto': 'https' } });
  await ctx.request.post(`${url}/api/v1/auth/login`, { data: { username: 'admin', password: 'smoketest1234' } });
  const page = await ctx.newPage({ viewport: { width: 1280, height: 900 } });
  await page.goto(url, { waitUntil: 'networkidle' });
  await page.waitForSelector('.history-view-card', { timeout: 15000 });
  if (await page.locator('.history-view-card').count() !== 2) fail('erwartet zwei Verlaufskacheln');

  // Marker an die Knoten haengen, dann zwei echte Fragment-Tausche ausloesen.
  await page.evaluate(() => document.querySelectorAll('.history-view-card').forEach((node, index) => { node.__marker = `m${index}`; }));
  for (let round = 0; round < 2; round++) {
    await page.evaluate(() => new Promise(resolve => {
      document.addEventListener('htmx:afterSettle', () => resolve(), { once: true });
      window.dispatchEvent(new Event('layout-saved'));
    }));
  }
  const kept = await page.evaluate(() => [...document.querySelectorAll('.history-view-card')].map(node => node.__marker || null));
  if (kept.join(',') !== 'm0,m1') fail(`Knoten nach zwei Tauschen nicht erhalten: ${kept}`);
  else console.log('    ok: beide Kacheln sind nach zwei Tauschen dieselben Knoten');

  // Die Sicht aendern (24 h statt 1 h) und Layout neu laden lassen.
  const changed = await page.evaluate(async () => {
    const base = document.documentElement.dataset.basePath || '';
    const current = await (await fetch(`${base}/api/v1/settings`)).json();
    current.history_views[0].range_hours = current.history_views[0].range_hours === 24 ? 6 : 24;
    const csrf = document.querySelector('meta[name="csrf-token"]')?.content || '';
    const response = await fetch(`${base}/api/v1/settings`, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(current) });
    return response.ok;
  });
  if (!changed) {
    fail('Settings schreiben fehlgeschlagen (Anmeldung als Admin erforderlich)');
  } else {
    await page.evaluate(() => new Promise(resolve => {
      document.addEventListener('htmx:afterSettle', () => resolve(), { once: true });
      window.dispatchEvent(new Event('layout-saved'));
    }));
    const after = await page.evaluate(() => [...document.querySelectorAll('.history-view-card')].map(node => node.__marker || null));
    if (after[0] !== null || after[1] !== 'm1') fail(`geaenderte Sicht: erwartet neuer Knoten nur fuer die gebundene Kachel, bekam ${after}`);
    else console.log('    ok: geaenderte Sicht erzeugt einen neuen Knoten, die eigene Kachel bleibt');
  }
  await ctx.close();
} finally {
  await browser.close();
}
