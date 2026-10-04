import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountScreen, realCatalog } from './helpers/mount.mjs';

// Objekte aus dem jsdom-Kontext haben andere Prototypen als die des Tests.
const plain = (value) => JSON.parse(JSON.stringify(value));

const entry = (text, extra = {}) => ({ text, breaking: false, ...extra });
const release = (version, groups) => ({ version, date: '2026-09-21', groups });
const group = (type, label, entries) => ({ type, label, entries });

const CHANGELOG = {
  bundle_version: 'v0.7.5',
  installed: { dashboard: 'v0.7.3', 'service:shelly': 'v0.4.1' },
  document: {
    schema_version: 1,
    components: [
      { id: 'dashboard', label: 'Dashboard', kind: 'app', version: 'v0.7.5', releases: [
        release('v0.7.5', [
          group('feat', 'Features', [entry('add a versions page', { scope: 'dashboard', pr: 51 })]),
          group('fix', 'Fixes', [entry('stop duplicate mounts', { scope: 'dashboard' })]),
        ]),
        release('v0.7.4', [group('feat', 'Features', [entry('drop the legacy node block', { scope: 'dashboard', breaking: true, pr: 14 })])]),
        release('v0.7.3', [group('fix', 'Fixes', [entry('already installed')])]),
      ] },
      { id: 'service:shelly', label: 'Shelly', kind: 'service', version: 'v0.4.2', releases: [
        release('v0.4.2', [group('fix', 'Fixes', [entry('retry the rpc call', { scope: 'shelly' })])]),
      ] },
    ],
  },
};

async function mount(options = {}) {
  const mounted = mountScreen('screen-changelog.js', 'screenChangelog', Object.assign({
    scripts: ['changelog-model.js'],
    catalog: realCatalog('de'),
    responses: Object.assign({ 'GET /api/changelog': CHANGELOG }, options.responses),
  }, options.errors ? { errors: options.errors } : {}, {
    shell: Object.assign({ entry: 'redeploy', screen: 'changelog', connected: true }, options.shell),
  }));
  await mounted.screen.init();
  return mounted;
}

test('init loads /api/changelog and slices it against the installed versions', async () => {
  const { screen, calls } = await mount();
  assert.deepEqual(calls.map((call) => call.key), ['GET /api/changelog']);
  assert.deepEqual(plain(screen.rows.map((row) => [row.id, row.from, row.to])), [
    ['dashboard', 'v0.7.3', 'v0.7.5'], ['service:shelly', 'v0.4.1', 'v0.4.2'],
  ]);
  assert.equal(screen.loading, false);
  assert.equal(screen.missing, false);
});

test('the version bar shows the installed dashboard against the package, without the v', async () => {
  const { screen } = await mount();
  assert.equal(screen.installedVersion, '0.7.3');
  assert.equal(screen.packageVersion, '0.7.5');
});

test('a first install has no installed dashboard, so the bar shows only the package', async () => {
  const { screen } = await mount({ responses: { 'GET /api/changelog': { ...CHANGELOG, installed: {} } } });
  assert.equal(screen.installedVersion, '');
  assert.equal(screen.packageVersion, '0.7.5');
  assert.deepEqual(plain(screen.rows.map((row) => row.releases.map((r) => r.version))), [['v0.7.5'], ['v0.4.2']], 'newest release only');
});

test('the count line is in the catalog language, pluralised, and skips zeros', async () => {
  const { screen } = await mount();
  // Der Breaking-Eintrag ist zugleich eine Neuerung: er zaehlt in beiden Zahlen.
  assert.equal(screen.countLine, '2 Neuerungen · 2 Korrekturen · 1 Breaking Change');
});

test('breaking changes are collected regardless of the active filter', async () => {
  const { screen } = await mount();
  screen.scope = 'shelly';
  assert.deepEqual(plain(screen.breaking.map((item) => item.text)), ['drop the legacy node block']);
  assert.equal(screen.visible.length, 1, 'the filter still cuts the component list');
});

test('toggleKind selects and deselects, and filters the visible components', async () => {
  const { screen } = await mount();
  screen.toggleKind('service');
  assert.deepEqual(plain(screen.visible.map((row) => row.id)), ['service:shelly']);
  screen.toggleKind('service');
  assert.equal(screen.visible.length, 2);
});

test('scope and text filters combine, and nothingMatches reports an empty result', async () => {
  const { screen } = await mount();
  screen.scope = 'dashboard';
  screen.query = 'no such thing';
  assert.equal(screen.visible.length, 0);
  assert.equal(screen.nothingMatches, true);
  screen.query = '';
  assert.equal(screen.nothingMatches, false);
});

test('kinds and scopes feed the filter controls', async () => {
  const { screen } = await mount();
  assert.deepEqual(plain(screen.kinds), ['app', 'service']);
  assert.deepEqual(plain(screen.scopes), ['dashboard', 'shelly']);
});

test('labels come from the catalog; an unknown kind or group type falls back gracefully', async () => {
  const { screen } = await mount();
  assert.equal(screen.kindLabel('service'), 'Dienste');
  assert.equal(screen.kindLabel('gadget'), 'Weitere');
  assert.equal(screen.groupLabel({ type: 'feat', label: 'Features' }), 'Neuerungen');
  assert.equal(screen.groupLabel({ type: 'wibble', label: 'Wibble' }), 'Wibble');
});

test('every commit type the generator can produce has a group label in both catalogs', () => {
  // scripts/generate_changelog.sh LABELS: feat fix refactor perf docs test style chore dev build ci other
  for (const lang of ['de', 'en']) {
    const catalog = realCatalog(lang);
    for (const type of ['feat', 'fix', 'refactor', 'perf', 'docs', 'test', 'style', 'chore', 'dev', 'build', 'ci', 'other']) {
      assert.ok(catalog[`changelog.group.${type}`], `${lang}.json: changelog.group.${type}`);
    }
    for (const kind of ['app', 'shared', 'service', 'library', 'tool', 'integration', 'other']) {
      assert.ok(catalog[`changelog.kind.${kind}`], `${lang}.json: changelog.kind.${kind}`);
    }
  }
});

test('a package without a changelog (404) shows the "missing" state, not an error banner', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/changelog': { status: 404, code: 'NO_CHANGELOG' } } });
  assert.equal(screen.missing, true);
  assert.equal(shell.error, null);
  assert.equal(screen.rows.length, 0);
});

test('a host that cannot serve one (501) is the same state', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/changelog': { status: 501, code: 'NOT_SUPPORTED' } } });
  assert.equal(screen.missing, true);
  assert.equal(shell.error, null);
});

test('any other failure goes to the shell error banner', async () => {
  const { screen, shell } = await mount({ errors: { 'GET /api/changelog': { status: 500, code: 'CHANGELOG_FAILED', detail: 'ssh broke' } } });
  assert.equal(screen.missing, false);
  assert.deepEqual(plain(shell.error), { code: 'CHANGELOG_FAILED', detail: 'ssh broke' });
});

test('nothing newer than the installed version is an empty state, not a missing one', async () => {
  const { screen } = await mount({ responses: { 'GET /api/changelog': { ...CHANGELOG, installed: { dashboard: 'v0.7.5', 'service:shelly': 'v0.4.2' } } } });
  assert.equal(screen.hasRows, false);
  assert.equal(screen.missing, false);
});

test('back returns to the screen that opened it, the preview by default', async () => {
  const opened = await mount({ shell: { shared: { changelogFrom: 'configure' } } });
  opened.screen.back();
  assert.equal(opened.shell.screen, 'configure');
  assert.equal(opened.shell.dir, 'back');

  const plainMount = await mount();
  plainMount.screen.back();
  assert.equal(plainMount.shell.screen, 'preview');
});

// Highlights / Alles: changelog.json flags every entry. Shelly's only entry is
// a detail, so in the highlights view Shelly is left out and counted instead.
const FLAGGED = JSON.parse(JSON.stringify(CHANGELOG));
FLAGGED.document.components[0].releases[0].groups[0].entries[0].highlight = true;
FLAGGED.document.components[0].releases[0].groups[1].entries[0].highlight = false;
FLAGGED.document.components[0].releases[1].groups[0].entries[0].highlight = true;
FLAGGED.document.components[1].releases[0].groups[0].entries[0].highlight = false;
const mountFlagged = () => mount({ responses: { 'GET /api/changelog': FLAGGED } });
const visibleTexts = (screen) => plain(screen.visible.map((row) => [row.id, row.releases.flatMap((r) => r.groups.flatMap((g) => g.entries.map((e) => e.text)))]));

test('a flagged changelog opens on the highlights and can switch to everything', async () => {
  const { screen } = await mountFlagged();
  assert.equal(screen.canSwitch, true);
  assert.equal(screen.mode, 'highlights');
  assert.deepEqual(visibleTexts(screen), [['dashboard', ['add a versions page', 'drop the legacy node block']]]);
  assert.equal(screen.detailsOnly, 1, 'Shelly has details only');
  assert.equal(screen.countLine, '2 Neuerungen · 1 Breaking Change');
  screen.setMode('all');
  assert.deepEqual(visibleTexts(screen), [
    ['dashboard', ['add a versions page', 'stop duplicate mounts', 'drop the legacy node block']],
    ['service:shelly', ['retry the rpc call']],
  ]);
  assert.equal(screen.detailsOnly, 0);
  assert.equal(screen.countLine, '2 Neuerungen · 2 Korrekturen · 1 Breaking Change');
});

test('the filters work inside the highlights view too', async () => {
  const { screen } = await mountFlagged();
  screen.query = 'versions';
  assert.deepEqual(visibleTexts(screen), [['dashboard', ['add a versions page']]]);
  screen.query = 'retry';
  assert.equal(screen.nothingMatches, true, 'a detail does not match in the highlights view');
});

test('a changelog without flags has nothing to switch and shows everything', async () => {
  const { screen } = await mount();
  assert.equal(screen.canSwitch, false);
  assert.equal(screen.visible.length, 2);
  assert.equal(screen.detailsOnly, 0);
});
