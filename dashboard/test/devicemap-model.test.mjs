import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';
import { installI18n } from './helpers/i18n.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, '..', 'internal', 'webui', 'static', 'js', 'devicemap-model.js'), 'utf8');

function loadModel() {
  const dom = new JSDOM('<!doctype html><html><head></head><body></body></html>', { runScripts: 'outside-only' });
  installI18n(dom.window);
  vm.runInContext(source, dom.getInternalVMContext());
  return dom.window.DeviceMapModel;
}
const plain = value => JSON.parse(JSON.stringify(value));
const entity = (device_id, entity_id, role, value, source = 'override', unit = 'W') => ({ device_id, entity_id, value, unit, role: { role, source } });

test('deviceEnergy groups roles per device and signs power toward the device', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [
    entity('pv', 'pv1', 'pv', 1300), entity('pv', 'pv2', 'pv', 1100),
    entity('bat', 'bat_p', 'battery', -400),
    entity('soc', 'soc_1', 'battery_soc', 64, 'override', '%'),
    entity('bkw', 'bkw_p', 'pv', 600, 'heuristic'),
  ] });
  assert.deepEqual(plain(energy.get('pv')), { roles: ['pv'], heuristic: false, power: -2400, soc: null, byRole: { pv: 2400 }, primaryRole: 'pv', meta: { pv: { color: 'flow-pv', icon: 'cat:energy-node:sun' } } });
  assert.equal(energy.get('bat').power, -400, 'discharging battery pushes power away from the device');
  assert.equal(energy.get('soc').power, null, 'battery_soc is not a power role');
  assert.equal(energy.get('soc').soc, 64);
  assert.equal(energy.get('soc').primaryRole, 'battery_soc');
  assert.equal(energy.get('bkw').heuristic, true);
});

test('deviceEnergy ignores entities without a known role and tolerates a missing snapshot', () => {
  const model = loadModel();
  assert.equal(model.deviceEnergy(null).size, 0);
  assert.equal(model.deviceEnergy({ entities: [entity('x', 'x1', '', 5), entity('y', 'y1', 'nonsense', 5)] }).size, 0);
});

test('primaryRole is the power role with the largest magnitude', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [entity('d', 'a', 'load', 100), entity('d', 'b', 'wallbox', 1100), entity('d', 'c', 'battery_soc', 50, 'override', '%')] });
  assert.equal(energy.get('d').primaryRole, 'wallbox');
  assert.deepEqual(plain(energy.get('d').roles), ['load', 'wallbox', 'battery_soc']);
});

test('nodeValueText follows the draft wording per role', () => {
  const model = loadModel();
  const one = (role, value, unit = 'W') => model.deviceEnergy({ entities: [entity('d', 'e', role, value, 'override', unit)] }).get('d');
  assert.equal(model.nodeValueText({}, one('pv', 2400)), '2,4 kW');
  assert.equal(model.nodeValueText({}, one('load', 800)), '800 W');
  assert.equal(model.nodeValueText({}, one('grid', 1000)), '1,0 kW Bezug');
  assert.equal(model.nodeValueText({}, one('grid', -300)), '300 W Einspeisung');
  assert.equal(model.nodeValueText({}, one('battery', 757)), '757 W lädt');
  assert.equal(model.nodeValueText({}, one('battery', -200)), '200 W entlädt');
  assert.equal(model.nodeValueText({}, one('battery_soc', 64, '%')), '64 %');
});

test('nodeValueText falls back to the first numeric reading, then to "keine Messwerte"', () => {
  const model = loadModel();
  const device = { entities: [{ value: 'on' }, { value: '52.8', unit_of_measurement: 'V' }] };
  assert.equal(model.nodeValueText(device, undefined), '52,8 V');
  assert.equal(model.nodeValueText({ entities: [] }, undefined), 'keine Messwerte');
});

test('deviceHealth counts only entities that publish availability', () => {
  const model = loadModel();
  assert.equal(model.deviceHealth({ entities: [{ has_availability: false }] }), 'unknown');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: true }, { has_availability: false }] }), 'ok');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: true }, { has_availability: true, available: false }] }), 'degraded');
  assert.equal(model.deviceHealth({ entities: [{ has_availability: true, available: false }] }), 'down');
});

test('ringSpec describes segments, dashing and icon like draft variant C', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [entity('d', 'a', 'battery', 800), entity('d', 'b', 'battery_soc', 60, 'heuristic', '%')] });
  assert.deepEqual(plain(model.ringSpec(energy.get('d'), 'ok', true)), { segments: ['flow-battery', 'flow-battery'], dashed: true, icon: 'cat:energy-node:battery', iconColor: 'flow-battery', health: 'ok', iconMarkup: '' });
  assert.deepEqual(plain(model.ringSpec(undefined, 'unknown', true)), { segments: [], dashed: false, icon: 'cat:energy-node:sensor', iconColor: 'text-muted', health: 'unknown', iconMarkup: '' });
  assert.equal(model.ringSpec(undefined, 'unknown', false).icon, 'box');
});

test('role icons come from the device icon catalogue', () => {
  const model = loadModel();
  const markup = {'energy-node:sun': '<circle r="4"/>', 'energy-node:battery-level': '<rect/>'};
  const pv = model.ringSpec({primaryRole: 'pv', roles: ['pv'], meta: {pv: model.roleMeta('pv')}}, 'ok', true, markup);
  assert.equal(pv.icon, 'cat:energy-node:sun');
  assert.equal(pv.iconMarkup, '<circle r="4"/>');
  const soc = model.ringSpec({primaryRole: 'battery_soc', roles: ['battery_soc'], meta: {battery_soc: model.roleMeta('battery_soc')}}, 'ok', true, markup);
  assert.equal(soc.iconMarkup, '<rect/>');
  const bare = model.ringSpec(null, 'ok', false, markup);
  assert.equal(bare.icon, 'box');
});

test('relatedIds returns the node with its direct parents and children', () => {
  const model = loadModel();
  const pairs = [{ parent: 'netz', child: 'uv' }, { parent: 'uv', child: 'wallbox' }, { parent: 'netz', child: 'pv' }];
  assert.deepEqual([...model.relatedIds('uv', pairs)].sort(), ['netz', 'uv', 'wallbox']);
});

test('edgeFlow uses the child measurement, otherwise the signed subtree sum', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [
    entity('wallbox', 'w', 'wallbox', 1100), entity('bkw', 'b', 'pv', 600), entity('bat', 'p', 'battery', -300),
  ] });
  const children = model.childrenIndex([{ parent: 'netz', child: 'uv' }, { parent: 'uv', child: 'wallbox' }, { parent: 'uv', child: 'bkw' }, { parent: 'netz', child: 'bat' }]);
  assert.deepEqual(plain(model.edgeFlow('wallbox', energy, children)), { value: 1100, sum: false });
  assert.deepEqual(plain(model.edgeFlow('uv', energy, children)), { value: 500, sum: true });
  assert.deepEqual(plain(model.edgeFlow('bat', energy, children)), { value: -300, sum: false });
  assert.equal(model.edgeFlow('bms', energy, children), null, 'no data, no flow');
});

test('edgeFlow terminates on relation cycles and ignores flows below one watt', () => {
  const model = loadModel();
  const energy = model.deviceEnergy({ entities: [entity('c', 'x', 'load', 0.4)] });
  const children = model.childrenIndex([{ parent: 'a', child: 'b' }, { parent: 'b', child: 'a' }, { parent: 'b', child: 'c' }]);
  assert.equal(model.edgeFlow('a', energy, children), null);
});

test('speed buckets, widths, labels and colours follow the draft', () => {
  const model = loadModel();
  assert.equal(model.speedBucket(299), 'slow');
  assert.equal(model.speedBucket(300), 'mid');
  assert.equal(model.speedBucket(1199), 'mid');
  assert.equal(model.speedBucket(1200), 'fast');
  assert.equal(model.flowWidth(5000, false), 2.6);
  assert.equal(model.flowWidth(0, true), 1.8);
  assert.equal(model.flowWidth(2500, true), 5.4);
  assert.equal(model.flowLabel({ value: 1100, sum: false }), '↓ 1,1 kW');
  assert.equal(model.flowLabel({ value: -532, sum: true }), 'Σ ↑ 532 W');
  const energy = model.deviceEnergy({ entities: [entity('pv', 'p', 'pv', 100)] });
  assert.equal(model.flowColorToken({ value: -100, sum: false }, energy.get('pv')), 'flow-pv');
  assert.equal(model.flowColorToken({ value: 100, sum: true }, undefined), 'flow-rest');
});

test('baseAssignment prefers the saved override, then the heuristic, then none', () => {
  const model = loadModel();
  const snapshot = {entities: [{entity_id: 'h', device_id: 'd', value: 5, role: {role: 'pv', scale: 1, source: 'heuristic'}}]};
  assert.equal(model.baseAssignment('o', {o: {role: 'load', scale: 2}}, snapshot).source, 'override');
  assert.equal(model.baseAssignment('o', {o: {role: 'load', scale: 2}}, snapshot).scale, 2);
  assert.equal(model.baseAssignment('h', {}, snapshot).role, 'pv');
  assert.equal(model.baseAssignment('h', {}, snapshot).source, 'heuristic');
  const none = model.baseAssignment('x', {x: {role: ''}}, snapshot);
  assert.equal(none.role, '');
  assert.equal(none.source, 'override');
  assert.equal(model.baseAssignment('y', {}, snapshot).source, 'none');
});

test('panelRows splits eligible entities from the rest and marks drafts and pins', () => {
  const model = loadModel();
  const device = {id: 'bkw', entities: [
    {unique_id: 'p', name: 'Leistung', unit_of_measurement: 'W', value: '600'},
    {unique_id: 'v', name: 'Spannung', unit_of_measurement: 'V', value: '230'},
  ]};
  const snapshot = {entities: [{entity_id: 'p', device_id: 'bkw', value: 600, role: {role: 'pv', scale: 1, source: 'heuristic'}}]};
  let view = model.panelRows({device, snapshot, saved: {}, drafts: {}});
  assert.equal(view.rows.length, 1);
  assert.equal(view.rows[0].source, 'heuristic');
  assert.equal(view.rows[0].canPin, true);
  assert.equal(view.others.length, 1);
  view = model.panelRows({device, snapshot, saved: {}, drafts: {p: {role: 'pv', scale: 1, invert: false, capacity_kwh: 0, pin: true}}});
  assert.equal(view.rows[0].source, 'draft');
  assert.equal(view.rows[0].dirty, true);
});

test('isDraftChange ignores an unchanged draft but counts a pin on a heuristic role', () => {
  const model = loadModel();
  const base = {role: 'pv', scale: 1, invert: false, capacity_kwh: 0, source: 'heuristic'};
  assert.equal(model.isDraftChange(base, {role: 'pv', scale: 1, invert: false, capacity_kwh: 0}), false);
  assert.equal(model.isDraftChange(base, {role: 'pv', scale: 1, invert: false, capacity_kwh: 0, pin: true}), true);
  assert.equal(model.isDraftChange(base, {role: 'pv', scale: 2, invert: false, capacity_kwh: 0}), true);
});

test('applyDrafts recomputes from the raw value and follows a live update', () => {
  const model = loadModel();
  const devices = [{id: 'd', entities: [{unique_id: 'p', unit_of_measurement: 'kW', value: '1.5'}]}];
  const live = {entities: [{entity_id: 'p', device_id: 'd', value: 3000, unit: 'W', role: {role: 'load', scale: 2, invert: false, source: 'override'}}]};
  const preview = model.applyDrafts(live, {p: {role: 'wallbox', scale: 1, invert: true, capacity_kwh: 0}}, devices);
  const entityPreview = preview.entities.find(item => item.entity_id === 'p');
  assert.equal(entityPreview.role.role, 'wallbox');
  assert.equal(entityPreview.value, -1500, 'raw = 3000 / 2, then inverted');
  assert.equal(live.entities[0].role.role, 'load', 'the input snapshot is untouched');
  const later = {entities: [{...live.entities[0], value: 4000}]};
  assert.equal(model.applyDrafts(later, {p: {role: 'wallbox', scale: 1, invert: true, capacity_kwh: 0}}, devices).entities[0].value, -2000);
});

test('applyDrafts adds an entity that had no role yet and drops one set to no role', () => {
  const model = loadModel();
  const devices = [{id: 'd', entities: [{unique_id: 'n', unit_of_measurement: 'W', value: '200'}]}];
  const snapshot = {entities: [{entity_id: 'r', device_id: 'd', value: 50, role: {role: 'pv', scale: 1, source: 'heuristic'}}]};
  const preview = model.applyDrafts(snapshot, {n: {role: 'heat_pump', scale: 1, invert: false, capacity_kwh: 0}, r: {role: '', scale: 1, invert: false, capacity_kwh: 0}}, devices);
  assert.deepEqual(preview.entities.map(item => item.entity_id), ['n']);
  assert.equal(preview.entities[0].value, 200);
  assert.equal(preview.entities[0].device_id, 'd');
});

test('assignmentPayload keeps only the fields of the role kind', () => {
  const model = loadModel();
  assert.deepEqual({...model.assignmentPayload({role: 'battery_soc', scale: 3, invert: true, capacity_kwh: 10})}, {role: 'battery_soc', capacity_kwh: 10});
  assert.deepEqual({...model.assignmentPayload({role: '', scale: 3, invert: true, capacity_kwh: 0})}, {role: ''});
  assert.deepEqual({...model.assignmentPayload({role: 'pv', scale: 1.5, invert: true, capacity_kwh: 0, pin: true})}, {role: 'pv', scale: 1.5, invert: true});
});

test('custom categories get their palette colour, icon and the sign of their base', () => {
  const model = loadModel();
  const categories = {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_3', icon: 'mdi:home'}, bkw: {label: 'BKW', base: 'producer', color: 'cat_1', icon: 'mdi:solar-panel'}};
  assert.equal(model.roleMeta('custom:werkstatt', categories).color, 'flow-cat-3');
  assert.equal(model.roleMeta('custom:bkw', categories).toward(600), -600);
  assert.equal(model.roleMeta('custom:fehlt', categories), null);
  const energy = model.deviceEnergy({categories, entities: [{entity_id: 'w', device_id: 'w', value: 350, role: {role: 'custom:werkstatt', source: 'override'}}]});
  const entry = energy.get('w');
  assert.equal(entry.power, 350);
  const spec = model.ringSpec(entry, 'ok', true, {'mdi:home': '<path d="M1 1"/>'});
  assert.deepEqual([...spec.segments], ['flow-cat-3']);
  assert.equal(spec.iconMarkup, '<path d="M1 1"/>');
});

test('roleOptions lists custom categories after the standard roles, only for power', () => {
  const model = loadModel();
  const categories = {werkstatt: {label: 'Werkstatt', base: 'consumer', color: 'cat_1', icon: 'mdi:home'}};
  const options = model.roleOptions('W', categories);
  assert.equal(options[options.length - 1].value, 'custom:werkstatt');
  assert.equal(options[options.length - 1].group, 'custom');
  assert.equal(model.roleOptions('%', categories).some(option => option.group === 'custom'), false);
});

test('membershipPairs turns groups into parent/child pairs', () => {
  const model = loadModel();
  const pairs = model.membershipPairs({garage: {members: {devices: ['wb'], groups: ['bank']}}, bank: {members: {devices: ['saege'], groups: []}}});
  assert.deepEqual(JSON.parse(JSON.stringify(pairs)).sort((a, b) => a.child.localeCompare(b.child)), [
    {parent: 'group:garage', child: 'group:bank'},
    {parent: 'group:bank', child: 'saege'},
    {parent: 'group:garage', child: 'wb'},
  ]);
});

test('slugId transliterates and avoids collisions', () => {
  const model = loadModel();
  assert.equal(model.slugId('UV Garage', []), 'uv_garage');
  assert.equal(model.slugId('Küche & Bad', []), 'kueche_bad');
  assert.equal(model.slugId('UV Garage', ['uv_garage']), 'uv_garage_2');
  assert.equal(model.slugId('!!!', []), 'gruppe');
});

test('placeGroup centres above its members and moves right when taken', () => {
  const model = loadModel();
  const snap = value => value;
  const members = [{x: 400, y: 400}, {x: 600, y: 420}];
  assert.deepEqual({...model.placeGroup({memberPositions: members, allPositions: members, snap})}, {x: 500, y: 290});
  const taken = [...members, {x: 500, y: 290}];
  assert.deepEqual({...model.placeGroup({memberPositions: members, allPositions: taken, snap})}, {x: 640, y: 290});
  assert.deepEqual({...model.placeGroup({memberPositions: [], allPositions: members, snap})}, {x: 740, y: 400});
});

test('placeGroup treats a whole grid cell as taken, so it never lands beside a neighbour label', () => {
  const model = loadModel();
  const snap = value => value;
  const member = {x: 0, y: 200};
  const neighbour = {x: 90, y: 90};
  assert.deepEqual({...model.placeGroup({memberPositions: [member], allPositions: [member, neighbour], snap})}, {x: 280, y: 90});
});

test('placeDevices fills rows below the arrangement and needs an anchor', () => {
  const model = loadModel();
  const snap = value => value;
  const anchor = [{x: 0, y: 0}, {x: 280, y: 100}];
  assert.deepEqual(model.placeDevices({ids: ['a', 'b', 'c', 'd'], allPositions: anchor, snap}).map(spot => ({...spot})), [
    {id: 'a', x: 0, y: 210}, {id: 'b', x: 140, y: 210}, {id: 'c', x: 280, y: 210}, {id: 'd', x: 0, y: 320},
  ]);
  assert.equal(model.placeDevices({ids: ['a'], allPositions: [], snap}).length, 0);
});

test('helpTipPosition keeps a tooltip inside the panel, below its trigger or above when there is no room', () => {
  const model = loadModel();
  const panel = {left: 1000, top: 100, width: 352, height: 600};
  const tip = {width: 240, height: 60};
  // trigger near the left edge: the tip is pushed right instead of leaving the panel
  assert.deepEqual({...model.helpTipPosition({panel, trigger: {left: 1100, top: 200, width: 22, height: 22}, tip})}, {left: 12, top: 128});
  // trigger near the right edge
  assert.deepEqual({...model.helpTipPosition({panel, trigger: {left: 1320, top: 200, width: 22, height: 22}, tip})}, {left: 100, top: 128});
  // centred when there is room
  assert.deepEqual({...model.helpTipPosition({panel: {...panel, width: 600}, trigger: {left: 1289, top: 200, width: 22, height: 22}, tip})}, {left: 180, top: 128});
  // near the bottom it opens above
  assert.deepEqual({...model.helpTipPosition({panel, trigger: {left: 1100, top: 660, width: 22, height: 22}, tip})}, {left: 12, top: 494});
});
