// Switch outputs of the device map: detection, assignment and the area that
// loses power when an output is off. See the spec "Erkennung" and
// "Stromloser Bereich" in .docs/superpowers/specs/2026-10-14-devicemap-schaltzustand-design.md
// and the plan 2026-10-16-devicemap-schaltzustand. Pure: no DOM, no Cytoscape.
(() => {
  const model = () => window.DeviceMapModel;
  const POWER_UNITS = Object.freeze(['W', 'kW']);

  // Switches the server delivered are already filtered to commandable
  // entities without entity_category. Detection counts them per device.
  const hasPowerEntity = device => Boolean(device && (device.entities || []).some(entity => POWER_UNITS.includes(entity.unit_of_measurement)));

  const groupByDevice = switches => {
    const groups = new Map();
    for (const item of switches || []) {
      if (!groups.has(item.device_id)) groups.set(item.device_id, []);
      groups.get(item.device_id).push(item);
    }
    return groups;
  };

  // Map<deviceId, {entityId, name, state, source}>. A device has at most one
  // output. An override entry always wins over detection, and an override
  // that points to a missing entity leaves the device without an output.
  const resolveOutputs = ({switches, switchOutputs, devices}) => {
    const deviceById = new Map((devices || []).map(device => [device.id, device]));
    const result = new Map();
    for (const [deviceId, list] of groupByDevice(switches)) {
      const own = (switchOutputs || []).filter(entry => entry.device_id === deviceId);
      const outputOverride = own.find(entry => entry.function === 'output');
      const noneIds = new Set(own.filter(entry => entry.function === 'none').map(entry => entry.entity_id));
      let chosen = null;
      let source = null;
      if (outputOverride) {
        chosen = list.find(item => item.entity_id === outputOverride.entity_id) || null;
        source = 'override';
      } else if (list.length === 1 && !noneIds.has(list[0].entity_id) && hasPowerEntity(deviceById.get(deviceId))) {
        chosen = list[0];
        source = 'heuristic';
      }
      if (chosen) result.set(deviceId, {entityId: chosen.entity_id, name: chosen.name, state: chosen.state, source});
    }
    return result;
  };

  const functionOf = entry => (entry ? entry.function : undefined);

  // One row per switch of the device. fn is the function the draft gives it,
  // source tells where that comes from. A row whose override entry differs
  // from the saved one is a draft change.
  const rows = ({deviceId, switches, switchOutputs, savedSwitchOutputs, devices}) => {
    const current = resolveOutputs({switches, switchOutputs, devices}).get(deviceId);
    const entryFor = (list, entityId) => (list || []).find(entry => entry.device_id === deviceId && entry.entity_id === entityId);
    return (switches || []).filter(item => item.device_id === deviceId).map(item => {
      const override = entryFor(switchOutputs, item.entity_id);
      const saved = entryFor(savedSwitchOutputs, item.entity_id);
      const fn = current && current.entityId === item.entity_id ? 'output' : 'none';
      let source;
      if (functionOf(override) !== functionOf(saved)) source = 'draft';
      else if (override) source = 'override';
      else source = fn === 'output' ? 'heuristic' : 'none';
      return {entityId: item.entity_id, name: item.name, state: item.state, fn, source};
    });
  };

  // Returns a new list. Assigning output removes the previous output of the
  // same device, so a device never has two.
  const setFunction = (switchOutputs, {deviceId, entityId, fn}) => {
    const kept = (switchOutputs || []).filter(entry => {
      if (entry.entity_id === entityId) return false;
      return !(fn === 'output' && entry.device_id === deviceId && entry.function === 'output');
    });
    return [...kept, {entity_id: entityId, device_id: deviceId, function: fn}];
  };

  // Wiring edges that carry energy between two devices, as {parent, child}.
  // Memberships and edges touching a group are not power paths.
  const pathPairs = edgeElements => (edgeElements || [])
    .map(element => element.data)
    .filter(data => data && data.connType === 'energy' && !data.membership)
    .filter(data => !model().isGroupId(data.source) && !model().isGroupId(data.target))
    .map(data => ({parent: data.source, child: data.target}));

  // Nodes that lose power: below an off output, and not reachable from any
  // root without passing through an off output. Map<nodeId, {plug, hops}>,
  // where plug is the nearest off output.
  const deadArea = ({pairs, offIds}) => {
    const off = new Set(offIds || []);
    const children = new Map();
    const hasParent = new Set();
    const nodes = new Set();
    for (const {parent, child} of pairs || []) {
      if (!children.has(parent)) children.set(parent, []);
      children.get(parent).push(child);
      hasParent.add(child);
      nodes.add(parent);
      nodes.add(child);
    }

    // Reached from the roots without ever leaving an off node.
    const roots = [...nodes].filter(node => !hasParent.has(node));
    const powered = new Set(roots);
    const queue = [...roots];
    while (queue.length) {
      const node = queue.shift();
      if (off.has(node)) continue;
      for (const child of children.get(node) || []) {
        if (powered.has(child)) continue;
        powered.add(child);
        queue.push(child);
      }
    }

    // From every off node downward: the nearest plug wins.
    const dead = new Map();
    for (const plug of off) {
      const hops = new Map([[plug, 0]]);
      const walk = [plug];
      while (walk.length) {
        const node = walk.shift();
        for (const child of children.get(node) || []) {
          if (hops.has(child)) continue;
          hops.set(child, hops.get(node) + 1);
          walk.push(child);
        }
      }
      for (const [node, distance] of hops) {
        if (node === plug || off.has(node) || powered.has(node)) continue;
        const best = dead.get(node);
        if (!best || distance < best.hops) dead.set(node, {plug, hops: distance});
      }
    }
    return dead;
  };

  // Everything the map needs about switch outputs in one call. leafOff lists
  // off outputs with nothing below them, they get a hollow dot instead of a
  // start point on an edge.
  const status = ({switches, switchOutputs, devices, pairs}) => {
    const outputs = resolveOutputs({switches, switchOutputs, devices});
    const off = new Set([...outputs].filter(([, output]) => output.state === 'off').map(([deviceId]) => deviceId));
    const dead = deadArea({pairs, offIds: off});
    const parents = new Set((pairs || []).map(pair => pair.parent));
    const leafOff = new Set([...off].filter(deviceId => !parents.has(deviceId)));
    return {outputs, off, dead, leafOff};
  };

  window.DeviceMapSwitch = {resolveOutputs, rows, setFunction, pathPairs, deadArea, status};
})();
