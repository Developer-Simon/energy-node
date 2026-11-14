// Tests for storage.page.js: the scenario from GET /api/v1/storage/status,
// the fallback note per place, and the setup flow rules (drive choice,
// format confirmation, data and backup choice). Same jsdom approach as
// backup.page.test.mjs, run by `npm test` from dashboard/.
import { afterEach, test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n, catalog } from './helpers/i18n.mjs';
import { attachStores, fakeModalStore } from './helpers/notify-stores.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const scriptSource = fs.readFileSync(
  path.join(here, '..', 'internal', 'webui', 'static', 'js', 'storage.page.js'),
  'utf8',
);
const de = catalog('de');

const reply = (status, body) => ({ status, body });

const SSD = { kname: 'sdb', name: 'Samsung T7', uuid: 'abc-123', fstype: 'ext4', transport: 'usb', size: 500 * 1024 ** 3, used: 0, ready: true, needs_format: false, contents: '' };
const STICK = { kname: 'sdc', name: 'SanDisk Ultra', uuid: '', fstype: 'exfat', transport: 'usb', size: 64 * 1024 ** 3, used: 12 * 1024 ** 3, ready: false, needs_format: true, contents: 'storage.contents.foreign' };

// statusBody builds GET /api/v1/storage/status. mode is the watcher mode,
// uuid the configured drive ("" when none is set up).
function statusBody({ mode = 'none', uuid = '', fallback = 'system', since = null, job = {} } = {}) {
  return {
    config: { uuid, name: uuid ? 'Samsung T7' : '', fallback, use_data: true, use_backups: true },
    status: { mode, since, reason: '' },
    system: { size: 32 * 1024 ** 3, used: 6 * 1024 ** 3 },
    drive: { size: uuid ? SSD.size : 0, used: 0, parts: [] },
    job: { phase: '', err: '', done: false, ...job },
  };
}

function session(roles = ['storage_manage']) {
  return reply(200, { csrf_token: 'tok', roles });
}

// routes: "METHOD /path" or "/path" -> reply, or a function returning one.
// Every call is recorded in calls for the assertions.
function router(routes, calls = []) {
  const fetchImpl = async (url, options = {}) => {
    const method = options.method || 'GET';
    const body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
    calls.push({ url, method, headers: options.headers || {}, body });
    const handler = routes[`${method} ${url}`] || routes[url];
    if (!handler) throw new Error(`unexpected fetch ${method} ${url}`);
    const result = typeof handler === 'function' ? handler() : handler;
    return { ok: result.status < 400, status: result.status, json: async () => result.body };
  };
  return { fetchImpl, calls };
}

// Pages created by a test. A failing assertion can leave the drive poll
// running, which would keep node waiting, so every page closes its flow.
const created = [];
afterEach(() => {
  for (const page of created.splice(0)) page.closeFlow(true);
});

function createStoragePage({ fetchImpl } = {}) {
  const dom = new JSDOM(
    '<!doctype html><html><body><div data-flow="storage"><div class="bs-flow-stage"></div><div class="bs-flow" tabindex="-1"></div></div></body></html>',
    { runScripts: 'outside-only', url: 'https://dashboard.local/' },
  );
  const context = dom.getInternalVMContext();
  const factories = {};
  dom.window.Alpine = { data: (name, fn) => { factories[name] = fn; } };
  // Reduced motion: go() switches steps without animating, jsdom has no animate().
  dom.window.matchMedia = () => ({ matches: true });
  dom.window.fetch = fetchImpl || (async () => { throw new Error('fetch should not be called'); });
  installI18n(dom.window);
  vm.runInContext(scriptSource, context);
  const page = factories.storagePage();
  const stores = attachStores(page, { modal: fakeModalStore({ answer: true }) });
  page.$nextTick = (fn) => fn();
  // The settings shell owns activeSettingsPage, the tests switch it by hand.
  page.$watch = (key, fn) => { page.watchers = { ...page.watchers, [key]: fn }; };
  created.push(page);
  return { page, stores, window: dom.window };
}

// Standard routes: a session with the storage right, a status and a drive list.
function baseRoutes(status, drives = [SSD, STICK]) {
  return {
    '/api/v1/auth/session': session(),
    '/api/v1/storage/status': reply(200, status),
    '/api/v1/storage/drives': reply(200, drives),
  };
}

test('zeigt Einrichten ohne Laufwerk', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  assert.equal(page.csrfToken, 'tok');
  assert.equal(page.scenario(), 'none');
  await page.openSetup();
  assert.equal(page.flow, 'setup');
  assert.equal(page.step, 'drives');
  assert.equal(page.drives.length, 2);
  assert.equal(page.drive, null, 'no drive is preselected');
  page.closeFlow();
});

test('zeigt Warnung und Text je Ausweichort, wenn das Laufwerk fehlt', async () => {
  const since = '2026-11-10T14:02:00Z';
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'fallback_system', uuid: 'abc-123', since })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  assert.equal(page.scenario(), 'missing');
  assert.equal(page.statusLabel(), de['storage.status.missing']);
  const [, systemRest] = de['storage.missing.note.system'].split('{since}');
  assert.ok(page.missingNote().includes(systemRest.trim()), page.missingNote());

  page.apply(statusBody({ mode: 'fallback_ram', uuid: 'abc-123', fallback: 'ram', since }));
  const [, ramRest] = de['storage.missing.note.ram'].split('{since}');
  assert.ok(page.missingNote().includes(ramRest.trim()), page.missingNote());
  assert.notEqual(page.missingNote(), '');
});

test('verlangt die Bestätigung nur beim Formatieren', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openSetup();
  page.drive = page.drives.find((d) => !d.needsFormat);
  assert.deepEqual([...page.stepList()], ['drives', 'driveuse'], 'a ready drive has no format step');
  page.flowPrimary();
  assert.equal(page.step, 'driveuse');
  assert.equal(page.primaryEnabled(), true, 'no confirmation for a ready drive');

  page.closeFlow();
  await page.openSetup();
  page.drive = page.drives.find((d) => d.needsFormat);
  assert.deepEqual([...page.stepList()], ['drives', 'format', 'driveuse']);
  page.flowPrimary();
  assert.equal(page.step, 'format');
  assert.equal(page.primaryEnabled(), false, 'formatting needs the confirmation');
  page.eraseOk = true;
  assert.equal(page.primaryEnabled(), true);
  page.closeFlow();
});

test('sperrt Weiter ohne Auswahl', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openSetup();
  assert.equal(page.primaryLabel(), de['storage.flow.next']);
  assert.equal(page.primaryEnabled(), false);
  page.drive = page.drives[0];
  assert.equal(page.primaryEnabled(), true);
  page.closeFlow();
});

test('sperrt Einrichten, wenn weder Daten noch Backups gewählt sind', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openSetup();
  page.drive = page.drives.find((d) => !d.needsFormat);
  page.flowPrimary();
  assert.equal(page.step, 'driveuse');
  assert.equal(page.primaryLabel(), de['storage.use.submit']);
  page.use = { data: false, backups: false };
  assert.equal(page.primaryEnabled(), false);
  page.use = { data: false, backups: true };
  assert.equal(page.primaryEnabled(), true);
  page.closeFlow();
});

test('zeigt Wird geprüft, solange der Wächter noch nicht läuft, ohne Warnung', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none', uuid: 'abc-123' })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  assert.equal(page.scenario(), 'pending');
  assert.equal(page.statusLabel(), de['storage.status.pending']);
  assert.notEqual(page.statusLabel(), de['storage.status.missing']);
  assert.equal(page.pendingNote(), de['storage.pending.note']);
  page.apply(statusBody({ mode: 'drive', uuid: 'abc-123' }));
  assert.equal(page.scenario(), 'active');
});

test('Ausweichort ohne Zeitpunkt zeigt keinen leeren Platzhalter', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'fallback_ram', uuid: 'abc-123', since: null })));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  assert.equal(page.scenario(), 'missing');
  assert.equal(page.missingNote(), de['storage.missing.note.ram.plain']);
  assert.ok(!page.missingNote().includes('{since}'));
  assert.ok(!/Seit\s*(gehen|liegen)/.test(page.missingNote()), page.missingNote());
});

// Ein zweites, bereites Laufwerk, das nicht das eingerichtete ist.
const OTHER = { kname: 'sdd', name: 'Kingston DT', uuid: 'def-456', fstype: 'ext4', transport: 'usb', size: 128 * 1024 ** 3, used: 0, ready: true, needs_format: false, contents: '' };

test('bietet Laufwerk wechseln bei aktivem und Laufwerk ersetzen bei fehlendem Laufwerk', async () => {
  const active = createStoragePage({ fetchImpl: router(baseRoutes(statusBody({ mode: 'drive', uuid: 'abc-123' }))).fetchImpl });
  await active.page.init();
  assert.equal(active.page.scenario(), 'active');
  assert.equal(active.page.replaceFlow(), 'switch');

  const missing = createStoragePage({ fetchImpl: router(baseRoutes(statusBody({ mode: 'fallback_system', uuid: 'abc-123', since: null }))).fetchImpl });
  await missing.page.init();
  assert.equal(missing.page.scenario(), 'missing');
  assert.equal(missing.page.replaceFlow(), 'replace');

  const none = createStoragePage({ fetchImpl: router(baseRoutes(statusBody({ mode: 'none' }))).fetchImpl });
  await none.page.init();
  assert.equal(none.page.replaceFlow(), '', 'no button without a drive');

  const pending = createStoragePage({ fetchImpl: router(baseRoutes(statusBody({ mode: 'none', uuid: 'abc-123' }))).fetchImpl });
  await pending.page.init();
  assert.equal(pending.page.replaceFlow(), '', 'no button before the watcher runs');
});

test('Laufwerk wechseln ruft den Ersetzen-Weg auf, nicht die Einrichtung', async () => {
  const calls = [];
  const { fetchImpl } = router({
    ...baseRoutes(statusBody({ mode: 'drive', uuid: 'abc-123' }), [SSD, OTHER]),
    'POST /api/v1/storage/replace': reply(202, { phase: '', done: false }),
    'POST /api/v1/storage/setup': reply(202, { phase: '', done: false }),
  }, calls);
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openReplace();
  assert.equal(page.flow, 'switch');
  assert.equal(page.step, 'drives');
  page.drive = page.drives.find((d) => d.uuid === 'def-456');
  page.flowPrimary();
  assert.equal(page.step, 'driveuse');
  assert.equal(page.primaryLabel(), de['storage.switch.submit']);
  assert.equal(page.driveUseNote(), de['storage.switch.note']);
  page.flowPrimary();
  await new Promise((resolve) => setTimeout(resolve, 0));

  const posted = calls.filter((c) => c.method === 'POST').map((c) => c.url);
  assert.deepEqual(posted, ['/api/v1/storage/replace']);
  assert.equal(page.step, 'run');
  assert.deepEqual(calls.find((c) => c.method === 'POST').body, {
    kname: 'sdd', uuid: 'def-456', format: false, use_data: true, use_backups: true, fallback: 'system',
  });
});

test('ausgehängtes Laufwerk ersetzen zeigt den Hinweis zu Ersatzdaten und verlorenen Backups', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'fallback_ram', uuid: 'abc-123', fallback: 'ram', since: null }), [OTHER]));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openReplace();
  assert.equal(page.flow, 'replace');
  assert.equal(page.flowTitle(), de['storage.flow.title.replace']);
  page.drive = page.drives[0];
  page.flowPrimary();
  assert.equal(page.primaryLabel(), de['storage.replace.submit']);
  assert.equal(page.driveUseNote(), de['storage.replace.note']);
  assert.ok(page.driveUseNote().includes('Backups'), page.driveUseNote());
  page.closeFlow(true);
});

test('blendet das eingerichtete Laufwerk in der Auswahl aus', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'drive', uuid: 'abc-123' }), [SSD, OTHER, STICK]));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openReplace();
  assert.deepEqual(page.drives.map((d) => d.uuid), ['def-456', ''], 'the set up drive is not offered');
  page.closeFlow(true);
});

test('zeigt die Warnung zu verlorenen Backups im Ablauf', async () => {
  const { fetchImpl } = router({
    ...baseRoutes(statusBody({ mode: 'drive', uuid: 'abc-123' }), [OTHER]),
    'POST /api/v1/storage/replace': reply(202, { phase: 'copy', done: false, warn: 'storage_backups_lost' }),
    '/api/v1/storage/status': reply(200, statusBody({ mode: 'drive', uuid: 'abc-123', job: { phase: 'copy', warn: 'storage_backups_lost' } })),
  });
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openReplace();
  page.drive = page.drives[0];
  page.flowPrimary();
  page.flowPrimary();
  await new Promise((resolve) => setTimeout(resolve, 0));
  await page.pollJob();
  assert.equal(page.step, 'run');
  assert.equal(page.warnText(), de['error.storage_backups_lost']);
  page.closeFlow(true);
});

test('Erneut prüfen stößt die Prüfung an und zeigt den neuen Zustand', async () => {
  const calls = [];
  const { fetchImpl } = router({
    ...baseRoutes(statusBody({ mode: 'fallback_system', uuid: 'abc-123', since: null })),
    'POST /api/v1/storage/recheck': reply(200, statusBody({ mode: 'drive', uuid: 'abc-123' })),
  }, calls);
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  assert.equal(page.scenario(), 'missing');

  await page.retry();
  assert.deepEqual(calls.filter((c) => c.method === 'POST').map((c) => c.url), ['/api/v1/storage/recheck']);
  assert.equal(page.scenario(), 'active');
  assert.equal(page.checking, false);
});

test('beim Öffnen der Speicherseite wird der Zustand neu geprüft', async () => {
  const calls = [];
  const { fetchImpl } = router({
    ...baseRoutes(statusBody({ mode: 'drive', uuid: 'abc-123' })),
    'POST /api/v1/storage/recheck': reply(200, statusBody({ mode: 'fallback_ram', uuid: 'abc-123', fallback: 'ram', since: null })),
  }, calls);
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  assert.equal(page.scenario(), 'active');

  page.watchers.activeSettingsPage('general');
  assert.equal(calls.filter((c) => c.method === 'POST').length, 0, 'other pages do not trigger it');
  page.watchers.activeSettingsPage('storage');
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(page.scenario(), 'missing');
});

// Ein Stick mit zwei Partitionen und die ganze Platte davor, dazu eine
// Einzelplatte. Die ganze Platte kommt zuerst, wie der Watcher sie liefert.
const DISK_SIZE = 64 * 1024 ** 3;
const STICK_WHOLE = { kname: 'sda', name: 'SanDisk Ultra', uuid: '', fstype: '', transport: 'usb', size: DISK_SIZE, used: 0, ready: false, needs_format: true, contents: 'storage.contents.foreign', disk: 'sda', disk_size: DISK_SIZE, label: '', whole: true };
const STICK_INSTALL = { kname: 'sda1', name: 'SanDisk Ultra', uuid: 'part-1', fstype: 'vfat', transport: 'usb', size: 4.2 * 1024 ** 3, used: 0, ready: true, needs_format: false, contents: '', disk: 'sda', disk_size: DISK_SIZE, label: 'INSTALL', whole: false };
const STICK_EFI = { kname: 'sda2', name: 'SanDisk Ultra', uuid: 'part-2', fstype: 'vfat', transport: 'usb', size: 512 * 1024 ** 2, used: 0, ready: true, needs_format: false, contents: '', disk: 'sda', disk_size: DISK_SIZE, label: 'EFI', whole: false };
const SINGLE = { ...SSD, disk: 'sdb', disk_size: SSD.size, label: '', whole: false };
const GROUPED = [STICK_WHOLE, STICK_INSTALL, STICK_EFI, SINGLE];

test('gruppiert die Laufwerke nach ihrer Platte, die ganze Platte zuerst', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();

  const groups = page.driveGroups;
  assert.equal(groups.length, 2, 'one group per disk');
  assert.equal(groups[0].key, 'sda');
  assert.deepEqual([...groups[0].items.map((item) => item.kname)], ['sda', 'sda1', 'sda2']);
  assert.equal(groups[0].items[0].whole, true, 'the whole disk comes first');
  assert.equal(groups[0].title, 'SanDisk Ultra');
  assert.ok(groups[0].detail.includes('USB'), groups[0].detail);
  assert.equal(groups[1].key, 'sdb');
  assert.equal(groups[1].items.length, 1, 'a single drive stays a group of one');
  page.closeFlow(true);
});

test('zeigt bei Partitionen mit Label das Label vor der Größe', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();

  const [, install, efi] = page.driveGroups[0].items;
  assert.match(install.detail, /^INSTALL, \d/, install.detail);
  assert.match(efi.detail, /^EFI, \d/, efi.detail);
  assert.ok(page.driveGroups[1].items[0].detail.includes('USB'), 'a drive without a label keeps transport and size');
  page.closeFlow(true);
});

test('listet bei der ganzen Platte die Partitionen, die verloren gehen', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();

  assert.deepEqual([...page.drives[0].lost], ['INSTALL', 'EFI']);
  page.closeFlow(true);
});

test('wählt eine Partition oder die ganze Platte', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();

  await page.openSetup();
  page.drive = page.drives.find((d) => d.kname === 'sda2');
  page.flowPrimary();
  assert.equal(page.drive.kname, 'sda2');
  assert.equal(page.step, 'driveuse', 'a partition with a filesystem needs no format');
  page.closeFlow(true);

  await page.openSetup();
  page.drive = page.drives.find((d) => d.kname === 'sda' && d.whole);
  assert.equal(page.drive.whole, true);
  assert.equal(page.drive.needsFormat, true, 'the whole disk is formatted');
  page.closeFlow(true);
});

test('leert die Auswahl, wenn das gewählte Laufwerk abgesteckt wird', async () => {
  let current = GROUPED;
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    '/api/v1/storage/status': reply(200, statusBody({ mode: 'none' })),
    '/api/v1/storage/drives': () => reply(200, current),
  });
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();

  page.drive = page.drives.find((d) => d.kname === 'sda2');
  current = GROUPED.filter((d) => d.kname !== 'sda2');
  await page.loadDrives();
  assert.equal(page.drive, null, 'an unplugged partition is no longer chosen');
  page.closeFlow(true);
});

test('eine Gruppe kennt ihre ganze Platte und ihre Partitionen', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();

  const [stick, single] = page.driveGroups;
  assert.equal(stick.whole.kname, 'sda');
  assert.deepEqual([...stick.parts.map((p) => p.kname)], ['sda1', 'sda2']);
  assert.equal(stick.count, 2);
  assert.equal(single.whole, null, 'a single drive has no switch');
  assert.equal(single.count, 1);
  page.closeFlow(true);
});

test('Gruppenmodus: ganze Platte wählen und wieder auf Partition zurückschalten', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();
  const [stick] = page.driveGroups;

  assert.equal(page.isWhole(stick), false);
  page.setGroupMode(stick, 'whole');
  assert.equal(page.isWhole(stick), true);
  assert.equal(page.drive.kname, 'sda');
  assert.equal(page.drive.needsFormat, true);

  page.setGroupMode(stick, 'part');
  assert.equal(page.isWhole(stick), false);
  assert.equal(page.drive, null, 'leaving the whole disk clears its choice');
  page.closeFlow(true);
});

test('pickPart wirkt nur im Partitionsmodus', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();
  const [stick] = page.driveGroups;
  const efi = stick.parts.find((p) => p.kname === 'sda2');

  page.setGroupMode(stick, 'whole');
  page.pickPart(stick, efi);
  assert.equal(page.drive.kname, 'sda', 'pickPart does nothing while the whole disk is chosen');

  page.setGroupMode(stick, 'part');
  assert.equal(page.drive, null);
  page.pickPart(stick, efi);
  assert.equal(page.drive.kname, 'sda2');

  page.setGroupMode(stick, 'whole');
  assert.equal(page.drive.kname, 'sda');
  page.setGroupMode(stick, 'part');
  assert.equal(page.drive, null, 'whole to part clears the whole disk choice, not the partition');
  page.closeFlow(true);
});

test('chooseDrive setzt den Ganze-Stick-Modus einer anderen Gruppe zurück', async () => {
  const { fetchImpl } = router(baseRoutes(statusBody({ mode: 'none' }), GROUPED));
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();
  const [stick, single] = page.driveGroups;

  page.setGroupMode(stick, 'whole');
  assert.equal(page.isWhole(stick), true);
  page.chooseDrive(single.items[0]);
  assert.equal(page.isWhole(stick), false, 'the other group is back to part');
  assert.equal(page.drive.kname, 'sdb');
  page.closeFlow(true);
});

test('Gruppenmodus wird beim Öffnen und beim Abstecken zurückgesetzt', async () => {
  let current = GROUPED;
  const { fetchImpl } = router({
    '/api/v1/auth/session': session(),
    '/api/v1/storage/status': reply(200, statusBody({ mode: 'none' })),
    '/api/v1/storage/drives': () => reply(200, current),
  });
  const { page } = createStoragePage({ fetchImpl });
  await page.init();
  await page.openSetup();
  const [stick] = page.driveGroups;

  page.setGroupMode(stick, 'whole');
  await page.openSetup();
  assert.deepEqual({ ...page.groupMode }, {}, 'opening the flow resets the mode');
  assert.equal(page.drive, null);

  await page.loadDrives();
  page.setGroupMode(page.driveGroups[0], 'whole');
  current = GROUPED.filter((d) => d.kname !== 'sda');
  await page.loadDrives();
  assert.equal(page.drive, null, 'the unplugged whole disk is no longer chosen');
  assert.deepEqual({ ...page.groupMode }, {}, 'unplugging clears the mode');
  page.closeFlow(true);
});
