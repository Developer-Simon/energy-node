// Visual check of the built documentation site. Not part of CI.
//
// Usage: node scripts/docs/visual-check.mjs [base-url]
//   base-url defaults to http://localhost:4000/energy-node/
// Serve _site first (see scripts/docs/build-local.sh). Writes screenshots to
// .cache/docs-visual/ and exits 1 on the first broken expectation.
import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";

const require = createRequire(new URL("../../dashboard/package.json", import.meta.url));
const { chromium } = require("playwright");

const base = process.argv[2] || "http://localhost:4000/energy-node/";
const out = new URL("../../.cache/docs-visual/", import.meta.url).pathname;
mkdirSync(out, { recursive: true });

const pages = ["", "dashboard/settings.html", "developing/data-flow.html", "operating/configuration.html"];
const widths = [390, 1024, 1440];
const failures = [];
const fail = (msg) => { failures.push(msg); console.error("FAIL:", msg); };

const browser = await chromium.launch();

for (const scheme of ["light", "dark"]) {
  for (const width of widths) {
    const ctx = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: scheme });
    const page = await ctx.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    for (const path of pages) {
      await page.goto(base + path, { waitUntil: "networkidle" });
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      if (overflow > 0) fail(`${path || "index"} @${width} ${scheme}: page scrolls horizontally by ${overflow}px`);
      const name = `${(path || "index").replace(/\W+/g, "_")}-${width}-${scheme}.png`;
      await page.screenshot({ path: out + name, fullPage: false });
    }
    if (errors.length) fail(`@${width} ${scheme}: page errors: ${errors.join(" | ")}`);
    await ctx.close();
  }
}

// Mermaid survives toggling the theme twice.
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(base + "developing/data-flow.html", { waitUntil: "networkidle" });
  for (const mode of ["dark", "light", "dark"]) {
    await page.evaluate((m) => window.EnergyDocs.setTheme(m), mode);
    await page.waitForTimeout(800);
    const svgs = await page.locator("pre.mermaid svg").count();
    const raw = await page.locator("pre.mermaid:not(:has(svg))").count();
    if (!svgs || raw) fail(`mermaid after switching to ${mode}: ${svgs} diagrams, ${raw} unrendered`);
  }
  await ctx.close();
}

// Blocked localStorage: the toggle still works, no exception.
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "light" });
  await ctx.addInitScript(() => {
    Object.defineProperty(window, "localStorage", { get() { throw new Error("blocked"); } });
  });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base, { waitUntil: "networkidle" });
  await page.click("#theme-toggle");
  const theme = await page.evaluate(() => document.documentElement.dataset.theme);
  if (theme !== "dark") fail(`toggle with blocked storage left theme at ${theme}`);
  if (errors.length) fail(`blocked storage threw: ${errors.join(" | ")}`);
  await ctx.close();
}

// Stored dark choice applies before first paint (no light flash).
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: "light" });
  await ctx.addInitScript(() => { try { localStorage.setItem("en-docs-theme", "dark"); } catch (e) {} });
  const page = await ctx.newPage();
  await page.goto(base, { waitUntil: "commit" });
  const early = await page.evaluate(() => document.documentElement.dataset.theme);
  if (early !== "dark") fail(`stored dark theme not applied before load, got ${early}`);
  await ctx.close();
}

// Old deep links land on the moved section.
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  for (const [from, to] of [
    ["dashboard.html#mqtt", "dashboard/settings.html#mqtt"],
    ["device-services.html#shelly", "services/shelly.html"],
    ["knowledge/data-flow.html", "developing/data-flow.html"],
  ]) {
    await page.goto(base + from, { waitUntil: "networkidle" });
    await page.waitForTimeout(300);
    if (!page.url().endsWith(to)) fail(`${from} ended at ${page.url()}, expected …${to}`);
  }
  await ctx.close();
}

await browser.close();
if (failures.length) process.exit(1);
console.log(`PASS: visual check, screenshots in ${out}`);
