// Pure helpers for the device map's energy view: which roles a device
// carries, what its node shows and which nodes belong to a focus. No DOM and
// no Cytoscape here, devicemap.page.js does the drawing. Spec:
// .docs/superpowers/specs/2026-10-07-devicemap-energie-datenfluss-design.md
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const num = (value, decimals) => window.I18n.formatNumber(value, decimals);

  const DEFAULT_LAYERS = Object.freeze({wiring: true, energy: true, balance: false, data: false});
  // No layer is pending any more. Kept as an empty list so the page's
  // isLayerPending() still works for a future layer.
  const PENDING_LAYERS = Object.freeze([]);

  // toward(v): signed power toward the device, consumption positive. The
  // snapshot has already applied scale/invert; each role's own sign is
  // documented in energy.go semanticsFor(). null = not a power role.
  const ROLE_META = Object.freeze({
    pv: {color: 'flow-pv', icon: 'pv', toward: v => -Math.abs(v)},
    battery: {color: 'flow-battery', icon: 'battery', toward: v => v},
    battery_charge: {color: 'flow-battery', icon: 'battery', toward: v => Math.abs(v)},
    battery_discharge: {color: 'flow-battery', icon: 'battery', toward: v => -Math.abs(v)},
    grid: {color: 'flow-grid', icon: 'grid', toward: v => v},
    grid_import: {color: 'flow-grid', icon: 'grid', toward: v => Math.abs(v)},
    grid_export: {color: 'flow-grid', icon: 'grid', toward: v => -Math.abs(v)},
    load: {color: 'flow-load', icon: 'load', toward: v => Math.abs(v)},
    wallbox: {color: 'flow-wallbox', icon: 'wallbox', toward: v => Math.abs(v)},
    heat_pump: {color: 'flow-heatpump', icon: 'heat_pump', toward: v => Math.abs(v)},
    battery_soc: {color: 'flow-battery', icon: 'soc', toward: null},
  });

  const BASE_TOWARD = Object.freeze({
    consumer: v => Math.abs(v),
    producer: v => -Math.abs(v),
    storage: v => v,
  });

  const roleMeta = (role, categories) => {
    if (ROLE_META[role]) return ROLE_META[role];
    if (!role || !role.startsWith('custom:')) return null;
    const def = (categories || {})[role.slice('custom:'.length)];
    if (!def) return null;
    return {color: `flow-${String(def.color || 'cat_1').replace('_', '-')}`, icon: `cat:${def.icon}`, toward: BASE_TOWARD[def.base] || BASE_TOWARD.consumer};
  };

  const POWER_ROLES = Object.freeze(['pv', 'battery', 'battery_charge', 'battery_discharge', 'grid', 'grid_import', 'grid_export', 'load', 'wallbox', 'heat_pump']);
  // The dashboard's own balance device, read back from discovery. The energy
  // page hides it from the role editor, the panel does the same.
  const OWN_ENERGY_DEVICE_ID = 'energy_node';

  const isEligibleUnit = unit => unit === 'W' || unit === 'kW' || unit === '%';

  const roleLabel = role => (role ? t(`energy.role_label.${role}`) : t('energy.roles.no_role'));

  const roleOptions = (unit, categories = {}) => {
    if (unit === '%') return ['', 'battery_soc'].map(value => ({value, label: roleLabel(value), group: 'standard'}));
    const standard = ['', ...POWER_ROLES].map(value => ({value, label: roleLabel(value), group: 'standard'}));
    const custom = Object.entries(categories)
      .sort(([, a], [, b]) => window.I18n.compare(a.label, b.label))
      .map(([id, def]) => ({value: `custom:${id}`, label: def.label, group: 'custom'}));
    return [...standard, ...custom];
  };

  const normalizeAssignment = value => ({
    role: (value && value.role) || '',
    scale: Number(value && value.scale) || 1,
    invert: Boolean(value && value.invert),
    capacity_kwh: Number(value && value.capacity_kwh) || 0,
  });

  const snapshotEntity = (snapshot, entityId) =>
    ((snapshot && snapshot.entities) || []).find(entity => entity.entity_id === entityId) || null;

  // What the server holds for one entity, before any draft: a saved
  // override wins, then a heuristic role from the live snapshot, then none.
  const baseAssignment = (entityId, saved, snapshot) => {
    if (saved && Object.prototype.hasOwnProperty.call(saved, entityId)) {
      return {...normalizeAssignment(saved[entityId]), source: 'override'};
    }
    const live = snapshotEntity(snapshot, entityId);
    if (live && live.role && live.role.source === 'heuristic') {
      return {...normalizeAssignment(live.role), source: 'heuristic'};
    }
    return {...normalizeAssignment(null), source: 'none'};
  };

  const isDraftChange = (base, draft) => {
    const next = normalizeAssignment(draft);
    if (draft && draft.pin && base.source === 'heuristic') return true;
    return next.role !== base.role || next.scale !== base.scale
      || next.invert !== base.invert || next.capacity_kwh !== base.capacity_kwh;
  };

  const valueText = entity => {
    const raw = entity.value;
    if (raw === '' || raw == null || !Number.isFinite(Number(raw))) return '';
    return `${window.I18n.formatValue(String(raw), entity.unit_of_measurement || '')} ${entity.unit_of_measurement || ''}`.trim();
  };

  const panelRows = ({device, snapshot, saved, drafts}) => {
    const rows = [];
    const others = [];
    for (const entity of (device && device.entities) || []) {
      const id = entity.unique_id;
      const name = entity.name || entity.object_id || id;
      if (!isEligibleUnit(entity.unit_of_measurement)) {
        others.push({id, name, valueText: valueText(entity)});
        continue;
      }
      const base = baseAssignment(id, saved, snapshot);
      const draft = drafts && drafts[id];
      const dirty = Boolean(draft) && isDraftChange(base, draft);
      const assignment = dirty ? normalizeAssignment(draft) : {role: base.role, scale: base.scale, invert: base.invert, capacity_kwh: base.capacity_kwh};
      let source = base.source;
      if (dirty) source = 'draft';
      else if (!base.role && base.source !== 'override') source = 'none';
      rows.push({id, name, unit: entity.unit_of_measurement, valueText: valueText(entity), assignment, source, dirty,
        canPin: !dirty && base.source === 'heuristic' && Boolean(base.role)});
    }
    return {rows, others};
  };

  const rawFromDevice = (devices, entityId) => {
    for (const device of devices || []) {
      const entity = (device.entities || []).find(candidate => candidate.unique_id === entityId);
      if (!entity) continue;
      const value = Number(entity.value);
      if (!Number.isFinite(value)) return null;
      return {deviceId: device.id, raw: entity.unit_of_measurement === 'kW' ? value * 1000 : value, unit: entity.unit_of_measurement};
    }
    return null;
  };

  // Preview of unsaved panel drafts: the snapshot as it would look after
  // saving. The raw value is recovered from the live entity (undoing its
  // server-side scale and sign), so a live update keeps the preview current.
  // Like energy.Aggregate, the raw value is inverted first and scaled after.
  const applyDrafts = (snapshot, drafts, devices) => {
    const ids = Object.keys(drafts || {});
    if (!snapshot || ids.length === 0) return snapshot;
    const entities = ((snapshot.entities) || []).filter(entity => !ids.includes(entity.entity_id));
    for (const id of ids) {
      const draft = normalizeAssignment(drafts[id]);
      if (!draft.role) continue;
      const live = snapshotEntity(snapshot, id);
      let raw = null;
      let deviceId = live && live.device_id;
      let unit = live ? live.unit : '';
      if (live && live.role) {
        const scale = Number(live.role.scale) || 1;
        raw = live.role.role === 'battery_soc' ? Number(live.value) : (live.role.invert ? -1 : 1) * Number(live.value) / scale;
      } else {
        const fromDevice = rawFromDevice(devices, id);
        if (!fromDevice) continue;
        raw = fromDevice.raw;
        deviceId = fromDevice.deviceId;
        unit = fromDevice.unit === '%' ? '%' : 'W';
      }
      const value = draft.role === 'battery_soc' ? raw : (draft.invert ? -raw : raw) * draft.scale;
      entities.push({...(live || {}), entity_id: id, device_id: deviceId, unit, value,
        role: {role: draft.role, scale: draft.scale, invert: draft.invert, capacity_kwh: draft.capacity_kwh, source: 'override'}});
    }
    return {...snapshot, entities};
  };

  const assignmentPayload = draft => {
    const value = normalizeAssignment(draft);
    if (!value.role) return {role: ''};
    if (value.role === 'battery_soc') return {role: 'battery_soc', capacity_kwh: value.capacity_kwh};
    return {role: value.role, scale: value.scale, invert: value.invert};
  };

  // Draft wording: whole watts below 1 kW, one decimal above.
  const formatPower = watts => {
    const abs = Math.abs(watts);
    return abs >= 1000 ? `${num(abs / 1000, 1)} kW` : `${num(Math.round(abs), 0)} W`;
  };

  const primaryRole = entry => {
    let best = null;
    let bestAbs = -1;
    for (const role of entry.roles) {
      if (!entry.meta[role].toward) continue;
      const abs = Math.abs(entry.byRole[role] || 0);
      if (abs > bestAbs) { best = role; bestAbs = abs; }
    }
    return best || entry.roles[0] || null;
  };

  const deviceEnergy = snapshot => {
    const byDevice = new Map();
    for (const entity of (snapshot && snapshot.entities) || []) {
      const role = entity.role && entity.role.role;
      const meta = role && roleMeta(role, snapshot && snapshot.categories);
      if (!meta) continue;
      let entry = byDevice.get(entity.device_id);
      if (!entry) {
        entry = {roles: [], heuristic: false, power: null, soc: null, byRole: {}, primaryRole: null, meta: {}};
        byDevice.set(entity.device_id, entry);
      }
      if (!entry.roles.includes(role)) entry.roles.push(role);
      entry.meta[role] = meta;
      if (entity.role.source === 'heuristic') entry.heuristic = true;
      const value = Number(entity.value) || 0;
      if (meta.toward) {
        entry.power = (entry.power || 0) + meta.toward(value);
        entry.byRole[role] = (entry.byRole[role] || 0) + value;
      } else {
        entry.soc = value;
      }
    }
    for (const entry of byDevice.values()) entry.primaryRole = primaryRole(entry);
    return byDevice;
  };

  // Same rule as the former statusClass(): only entities that publish an
  // availability topic count, everything else would make devices look degraded.
  const deviceHealth = device => {
    const tracked = ((device && device.entities) || []).filter(entity => entity.has_availability);
    if (tracked.length === 0) return 'unknown';
    const available = tracked.filter(entity => entity.available).length;
    if (available === tracked.length) return 'ok';
    if (available === 0) return 'down';
    return 'degraded';
  };

  const firstReading = device => {
    const entity = ((device && device.entities) || []).find(candidate =>
      candidate.unit_of_measurement && candidate.value !== '' && candidate.value != null && Number.isFinite(Number(candidate.value)));
    return entity ? `${window.I18n.formatValue(String(entity.value), entity.unit_of_measurement)} ${entity.unit_of_measurement}` : '';
  };

  const nodeValueText = (device, entry) => {
    const role = entry && entry.primaryRole;
    if (!role) return firstReading(device) || t('devicemap.value.no_values');
    if (role === 'battery_soc') return `${num(entry.soc, 0)} %`;
    const total = entry.byRole[role] || 0;
    const value = formatPower(total);
    if (role === 'grid') return t(total >= 0 ? 'devicemap.value.grid_import' : 'devicemap.value.grid_export', {value});
    if (role === 'grid_import') return t('devicemap.value.grid_import', {value});
    if (role === 'grid_export') return t('devicemap.value.grid_export', {value});
    if (role === 'battery') return t(total >= 0 ? 'devicemap.value.battery_charging' : 'devicemap.value.battery_discharging', {value});
    return value;
  };

  const ringSpec = (entry, health, hasEntities, iconMarkup = {}) => {
    const role = entry && entry.primaryRole;
    const meta = role ? entry.meta[role] : null;
    const spec = {
      segments: entry ? entry.roles.map(r => entry.meta[r].color) : [],
      dashed: Boolean(entry && entry.heuristic),
      icon: meta ? meta.icon : (hasEntities ? 'sensor' : 'box'),
      iconColor: meta ? meta.color : 'text-muted',
      health,
    };
    if (spec.icon.startsWith('cat:')) spec.iconMarkup = iconMarkup[spec.icon.slice(4)] || '';
    return spec;
  };

  const relatedIds = (id, pairs) => {
    const set = new Set([id]);
    for (const {parent, child} of pairs || []) {
      if (parent === id) set.add(child);
      if (child === id) set.add(parent);
    }
    return set;
  };

  const MIN_FLOW_W = 1;
  const SPEED_SECONDS = Object.freeze({slow: 2.4, mid: 1.4, fast: 0.8});
  const DASH_PATTERN = Object.freeze([7, 9]);
  const DASH_CYCLE = 16;

  const childrenIndex = pairs => {
    const index = new Map();
    for (const {parent, child} of pairs || []) {
      if (!index.has(parent)) index.set(parent, []);
      index.get(parent).push(child);
    }
    return index;
  };

  // Spec: an edge parent -> child carries the child's own measurement,
  // otherwise the signed sum of its subtree. visited guards against cycles
  // that manual relations can create.
  const edgeFlow = (childId, energy, children, visited = new Set()) => {
    if (visited.has(childId)) return null;
    visited.add(childId);
    const entry = energy.get(childId);
    if (entry && entry.power != null) {
      return Math.abs(entry.power) < MIN_FLOW_W ? null : {value: entry.power, sum: false};
    }
    let total = null;
    for (const next of children.get(childId) || []) {
      const flow = edgeFlow(next, energy, children, visited);
      if (flow) total = (total || 0) + flow.value;
    }
    return total == null || Math.abs(total) < MIN_FLOW_W ? null : {value: total, sum: true};
  };

  const speedBucket = abs => (abs < 300 ? 'slow' : abs < 1200 ? 'mid' : 'fast');
  const flowWidth = (abs, byPower) => (byPower ? Number((1.8 + 3.6 * Math.min(1, abs / 2500)).toFixed(2)) : 2.6);
  const flowLabel = flow => `${flow.sum ? 'Σ ' : ''}${flow.value < 0 ? '↑' : '↓'} ${formatPower(flow.value)}`;
  const flowColorToken = (flow, entry) => (flow.sum || !entry || !entry.primaryRole ? 'flow-rest' : entry.meta[entry.primaryRole].color);

  const GROUP_PREFIX = 'group:';
  const isGroupId = id => typeof id === 'string' && id.startsWith(GROUP_PREFIX);

  const membershipPairs = groups => {
    const pairs = [];
    for (const [id, group] of Object.entries(groups || {})) {
      const members = (group && group.members) || {};
      for (const device of members.devices || []) pairs.push({parent: GROUP_PREFIX + id, child: device});
      for (const child of members.groups || []) pairs.push({parent: GROUP_PREFIX + id, child: GROUP_PREFIX + child});
    }
    return pairs;
  };

  const groupValueText = flow => (flow ? t('devicemap.group.value', {value: formatPower(flow.value)}) : t('devicemap.group.no_value'));

  const TRANSLIT = {ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss'};
  const slugId = (label, existing) => {
    const base = String(label || '').toLowerCase()
      .replace(/[äöüß]/g, char => TRANSLIT[char])
      .replace(/[^a-z0-9]+/g, '_')
      .replace(/^_+|_+$/g, '') || 'gruppe';
    const taken = new Set(existing || []);
    if (!taken.has(base)) return base;
    for (let n = 2; ; n += 1) if (!taken.has(`${base}_${n}`)) return `${base}_${n}`;
  };

  const PLACE_STEP_X = 140;
  const PLACE_STEP_Y = 110;
  const PLACE_CLEARANCE = 60;

  // Spec "Automatische Einordnung": a group sits centred above its members,
  // one row above the highest one, and moves right while the spot is taken.
  // Without members it goes right of the existing arrangement.
  const placeGroup = ({memberPositions, allPositions, snap}) => {
    const all = allPositions || [];
    let x = 0;
    let y = 0;
    if (memberPositions && memberPositions.length) {
      x = memberPositions.reduce((sum, p) => sum + p.x, 0) / memberPositions.length;
      y = Math.min(...memberPositions.map(p => p.y)) - PLACE_STEP_Y;
    } else if (all.length) {
      x = Math.max(...all.map(p => p.x)) + PLACE_STEP_X;
      y = Math.min(...all.map(p => p.y));
    }
    const taken = (px, py) => all.some(p => Math.abs(p.x - px) < PLACE_CLEARANCE && Math.abs(p.y - py) < PLACE_CLEARANCE);
    let spot = {x: snap(x), y: snap(y)};
    while (taken(spot.x, spot.y)) spot = {x: snap(spot.x + PLACE_STEP_X), y: spot.y};
    return spot;
  };

  window.DeviceMapModel = {
    DEFAULT_LAYERS, PENDING_LAYERS, ROLE_META, SPEED_SECONDS, DASH_PATTERN, DASH_CYCLE,
    formatPower, deviceEnergy, deviceHealth, nodeValueText, ringSpec, relatedIds,
    childrenIndex, edgeFlow, speedBucket, flowWidth, flowLabel, flowColorToken,
    POWER_ROLES, OWN_ENERGY_DEVICE_ID, isEligibleUnit, roleOptions, baseAssignment, isDraftChange,
    panelRows, applyDrafts, assignmentPayload,
    roleMeta, GROUP_PREFIX, isGroupId, membershipPairs, groupValueText, slugId, placeGroup,
  };
})();
