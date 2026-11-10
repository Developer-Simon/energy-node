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
const galleryPages = ["dashboard/overview-cards.html", "dashboard/automations-templates.html"];
pages.push(...galleryPages);
const widths = [390, 1024, 1440];
const failures = [];
const fail = (msg) => { failures.push(msg); console.error("FAIL:", msg); };

const browser = await chromium.launch();

// The gallery of a page with gallery: true: tiles, the detail sheet, the
// lightbox inside it, the filter and the next button.
async function checkGallery(ctx, page, path, width, scheme) {
  const tag = `${path} @${width} ${scheme}`;
  const sheetOpen = "dialog.gallery-sheet[open]";
  await page.goto(base + path, { waitUntil: "networkidle" });
  const tiles = await page.locator(".gallery-tile").count();
  const items = await page.locator("section.gallery-item").count();
  if (!tiles || tiles !== items) fail(`${tag}: ${tiles} tiles for ${items} items`);
  const tocCategory = await page.evaluate(() => [...document.querySelectorAll("#toc a")].some((a) => a.textContent.trim() === "Category"));
  if (tocCategory) fail(`${tag}: the table of contents lists a Category field`);

  // Open the first tile, check the sheet, close with Escape.
  await page.locator(".gallery-tile").first().click();
  await page.waitForSelector(sheetOpen);
  const id = await page.evaluate((s) => document.querySelector(s + " h2")?.id ?? null, sheetOpen);
  if (!id) fail(`${tag}: the sheet has no h2`);
  const hash = await page.evaluate(() => location.hash);
  if (id && hash !== "#" + id) fail(`${tag}: hash is ${hash} after opening, expected #${id}`);
  if (width === 390) {
    // The sheet slides up from the bottom, so measure after the transition.
    await page.evaluate((s) => Promise.all(document.querySelector(s).getAnimations().map((a) => a.finished)), sheetOpen);
    const box = await page.evaluate((s) => {
      const r = document.querySelector(s).getBoundingClientRect();
      return { bottom: r.bottom, height: innerHeight, scroll: document.documentElement.scrollWidth, width: innerWidth };
    }, sheetOpen);
    if (Math.abs(box.bottom - box.height) > 1) fail(`${tag}: sheet bottom ${box.bottom} is not at the window bottom ${box.height}`);
    if (box.scroll > box.width) fail(`${tag}: page scrolls horizontally with the sheet open`);
  }
  await page.keyboard.press("Escape");
  const closed = await page.waitForFunction((s) => !document.querySelector(s), sheetOpen, { timeout: 3000 }).then(() => true, () => false);
  if (!closed) fail(`${tag}: Escape did not close the sheet`);
  // The close handler runs in a task after the open attribute is gone.
  const cleared = await page.waitForFunction(() => location.hash === "", null, { timeout: 3000 }).then(() => true, () => false);
  if (!cleared) fail(`${tag}: hash is ${await page.evaluate(() => location.hash)} after closing`);
  const focusedTile = await page.evaluate(() => document.activeElement.classList.contains("gallery-tile"));
  if (!focusedTile) fail(`${tag}: focus did not return to the tile`);

  // An image in the sheet opens the lightbox, the sheet stays open.
  await page.locator(".gallery-tile").first().click();
  await page.waitForSelector(sheetOpen);
  await page.locator(sheetOpen + " img").first().click();
  const lightbox = await page.waitForSelector("dialog.lightbox[open]", { timeout: 3000 }).then(() => true, () => false);
  if (!lightbox) fail(`${tag}: image in the sheet did not open the lightbox`);
  const stillOpen = await page.evaluate((s) => !!document.querySelector(s), sheetOpen);
  if (!stillOpen) fail(`${tag}: the lightbox closed the sheet`);
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  await page.waitForFunction((s) => !document.querySelector(s), sheetOpen, { timeout: 3000 }).catch(() => fail(`${tag}: second Escape did not close the sheet`));

  // Filter: a second filter button, the first visible tile, next goes to the same category.
  const filterButtons = page.locator(".gallery-filter button");
  const nFilter = await filterButtons.count();
  if (nFilter < 3) {
    fail(`${tag}: ${nFilter} filter buttons, expected All plus at least two categories`);
  } else {
    const category = (await filterButtons.nth(1).textContent()).trim();
    await filterButtons.nth(1).click();
    const pressed = await filterButtons.nth(1).getAttribute("aria-pressed");
    if (pressed !== "true") fail(`${tag}: filter ${category} is not aria-pressed after the click`);
    const wrongTiles = await page.locator(".gallery-tile:not([hidden]) .gallery-tile__meta").evaluateAll(
      (els, c) => els.filter((e) => !e.textContent.trim().startsWith(c)).length, category);
    if (wrongTiles) fail(`${tag}: ${wrongTiles} visible tiles outside the ${category} filter`);
    await page.locator(".gallery-tile:not([hidden])").first().click();
    await page.waitForSelector(sheetOpen);
    await page.locator(".gallery-sheet__next").click();
    const opened = await page.evaluate((s) => {
      const h = [...document.querySelectorAll(s + " h3")].find((x) => x.textContent.trim() === "Category");
      return h && h.nextElementSibling ? h.nextElementSibling.textContent.trim() : null;
    }, sheetOpen);
    if (opened !== category) fail(`${tag}: next under the ${category} filter opened a ${opened} item`);
    await page.keyboard.press("Escape");
  }

  // Deep link: a fresh page opens the item named in the hash.
  if (path.startsWith("dashboard/overview-cards")) {
    const fresh = await ctx.newPage();
    await fresh.goto(base + path + "#energy-energy-flow", { waitUntil: "networkidle" });
    const heading = await fresh.evaluate((s) => document.querySelector(s + " h2")?.textContent.trim() ?? null, sheetOpen);
    if (heading !== "Energy: energy flow") fail(`${tag}: #energy-energy-flow opened ${heading}`);
    await fresh.close();
  }
}

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
    for (const path of galleryPages) await checkGallery(ctx, page, path, width, scheme);
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
    ["dashboard.html#mqtt", "dashboard/settings-mqtt.html#mqtt"],
    ["device-services.html#shelly", "services/shelly.html"],
    ["knowledge/data-flow.html", "developing/data-flow.html"],
  ]) {
    await page.goto(base + from, { waitUntil: "networkidle" });
    await page.waitForTimeout(300);
    if (!page.url().endsWith(to)) fail(`${from} ended at ${page.url()}, expected …${to}`);
  }
  await ctx.close();
}

// ha/companion.html loads without scrolling the window.
{
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 800 } });
  const page = await ctx.newPage();
  await page.goto(base + "ha/companion.html", { waitUntil: "networkidle" });
  const scrollY = await page.evaluate(() => window.scrollY);
  if (scrollY !== 0) fail(`ha/companion.html loads scrolled by ${scrollY}px`);
  const sidebarScroll = await page.evaluate(() => document.querySelector(".sidebar").scrollTop);
  await ctx.close();
}

await browser.close();
if (failures.length) process.exit(1);
console.log(`PASS: visual check, screenshots in ${out}`);
