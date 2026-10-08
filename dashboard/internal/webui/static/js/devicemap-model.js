// Pure helpers for the device map's energy view: which roles a device
// carries, what its node shows and which nodes belong to a focus. No DOM and
// no Cytoscape here, devicemap.page.js does the drawing. Spec:
// .docs/superpowers/specs/2026-10-07-devicemap-energie-datenfluss-design.md
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const num = (value, decimals) => window.I18n.formatNumber(value, decimals);

  const DEFAULT_LAYERS = Object.freeze({wiring: true, energy: true, balance: false, data: false});
  // Layers whose content arrives with spec phases 4 and 5: shown, disabled.
  const PENDING_LAYERS = Object.freeze(['balance', 'data']);

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

  // Draft wording: whole watts below 1 kW, one decimal above.
  const formatPower = watts => {
    const abs = Math.abs(watts);
    return abs >= 1000 ? `${num(abs / 1000, 1)} kW` : `${num(Math.round(abs), 0)} W`;
  };

  const primaryRole = entry => {
    let best = null;
    let bestAbs = -1;
    for (const role of entry.roles) {
      if (!ROLE_META[role].toward) continue;
      const abs = Math.abs(entry.byRole[role] || 0);
      if (abs > bestAbs) { best = role; bestAbs = abs; }
    }
    return best || entry.roles[0] || null;
  };

  const deviceEnergy = snapshot => {
    const byDevice = new Map();
    for (const entity of (snapshot && snapshot.entities) || []) {
      const role = entity.role && entity.role.role;
      const meta = role && ROLE_META[role];
      if (!meta) continue;
      let entry = byDevice.get(entity.device_id);
      if (!entry) {
        entry = {roles: [], heuristic: false, power: null, soc: null, byRole: {}, primaryRole: null};
        byDevice.set(entity.device_id, entry);
      }
      if (!entry.roles.includes(role)) entry.roles.push(role);
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

  const ringSpec = (entry, health, hasEntities) => {
    const role = entry && entry.primaryRole;
    return {
      segments: entry ? entry.roles.map(r => ROLE_META[r].color) : [],
      dashed: Boolean(entry && entry.heuristic),
      icon: role ? ROLE_META[role].icon : (hasEntities ? 'sensor' : 'box'),
      iconColor: role ? ROLE_META[role].color : 'text-muted',
      health,
    };
  };

  const relatedIds = (id, pairs) => {
    const set = new Set([id]);
    for (const {parent, child} of pairs || []) {
      if (parent === id) set.add(child);
      if (child === id) set.add(parent);
    }
    return set;
  };

  window.DeviceMapModel = {
    DEFAULT_LAYERS, PENDING_LAYERS, ROLE_META,
    formatPower, deviceEnergy, deviceHealth, nodeValueText, ringSpec, relatedIds,
  };
})();
