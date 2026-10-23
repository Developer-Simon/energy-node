// device-picker.js: the grouping and search of the shared device picker and
// the promise contract of its Alpine mixin.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'device-picker.js'), 'utf8');

// Values built inside the jsdom realm, compared after a JSON round trip.
const plain = value => JSON.parse(JSON.stringify(value));

function load() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  dom.window.eval(source);
  return dom.window.DevicePicker;
}

const items = [
  { id: 'b', kind: 'device', name: 'Wallbox', section: 'Shelly' },
  { id: 'a', kind: 'device', name: 'Ärmel Steckdose', section: 'Shelly' },
  { id: 'c', kind: 'device', name: 'Wechselrichter', section: 'APsystems' },
  { id: 'd', kind: 'device', name: 'Bastelkiste', section: '' },
  { id: 'group:keller', kind: 'group', name: 'Keller', section: '' },
];

test('groups come first, then manufacturers alphabetically, devices without one last', () => {
  const sections = load().sections(items);
  assert.deepEqual(plain(sections.map(section => section.label)), ['Gruppen', 'APsystems', 'Shelly', 'Sonstige']);
  assert.deepEqual(plain(sections[2].items.map(item => item.id)), ['a', 'b']);
});

test('search matches names and manufacturers, ignoring case and accents', () => {
  const picker = load();
  assert.deepEqual(plain(picker.sections(items, 'armel').flatMap(section => section.items.map(item => item.id))), ['a']);
  assert.deepEqual(plain(picker.sections(items, 'SHELLY').flatMap(section => section.items.map(item => item.id))), ['a', 'b']);
  assert.deepEqual(plain(picker.sections(items, 'nichts')), []);
});

test('the mixin resolves with the picked ids on confirm and with null on cancel', async () => {
  const component = load().mixin();
  const first = component.openPicker({ title: 'T', items });
  component.togglePick('a');
  component.togglePick('c');
  component.togglePick('a');
  assert.equal(component.pickerConfirmLabel(), '1 übernehmen');
  component.closePicker(true);
  assert.deepEqual(plain(await first), ['c']);
  assert.equal(component.picker, null);

  const second = component.openPicker({ title: 'T', items });
  component.togglePick('b');
  component.closePicker(false);
  assert.equal(await second, null);
});

test('confirming without a selection resolves with null', async () => {
  const component = load().mixin();
  const pending = component.openPicker({ title: 'T', items });
  component.closePicker(true);
  assert.equal(await pending, null);
});

const roleItems = [
  { id: 'g', kind: 'group', name: 'Keller', section: '' },
  { id: 'w', kind: 'device', name: 'Wallbox Garage', section: 'go-e', filter: 'wallbox' },
  { id: 'p', kind: 'device', name: 'Dach Wechselrichter', section: 'APsystems', filter: 'pv' },
  { id: 'x', kind: 'device', name: 'Bastelkiste', section: '' },
];

test('sections filters by role and keeps groups first', () => {
  const picker = load();
  const ids = sections => plain(sections.flatMap(section => section.items.map(item => item.id)));
  assert.deepEqual(ids(picker.sections(roleItems, '', 'wallbox')), ['g', 'w']);
  assert.deepEqual(ids(picker.sections(roleItems, '', '')), ['g', 'p', 'w', 'x']);
});

test('single mode replaces the selection and resolves one id', async () => {
  const component = load().mixin();
  const pending = component.openPicker({ title: 'T', items, mode: 'single' });
  component.togglePick('a');
  component.togglePick('c');
  assert.deepEqual(plain(component.picker.selected), ['c']);
  component.closePicker(true);
  assert.equal(await pending, 'c');
});

test('multi mode stays the default', async () => {
  const component = load().mixin();
  const pending = component.openPicker({ title: 'T', items });
  component.togglePick('a');
  component.togglePick('c');
  component.closePicker(true);
  assert.deepEqual(plain(await pending), ['a', 'c']);
});

test('preselected ids are picked on open', () => {
  const component = load().mixin();
  component.openPicker({ title: 'T', items, selected: ['a'] });
  assert.equal(component.isPicked('a'), true);
  assert.equal(component.isPicked('b'), false);
  component.closePicker(false);
});
