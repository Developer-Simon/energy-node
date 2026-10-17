// Switch channels of the device map: detection, the saved energy roles and
// the area that loses power when a channel is off. See the spec "detection"
// and "area without power" in
// .docs/superpowers/specs/2026-10-14-devicemap-schaltzustand-design.md and the
// plan 2026-10-16-devicemap-schaltzustand. Pure: no DOM, no Cytoscape.
(() => {
  const model = () => window.DeviceMapModel;
  const pairKey = (parent, child) => window.DeviceMapGraph.relationKey(parent, child);
  const POWER_UNITS = Object.freeze(['W', 'kW']);

  // Switches the server delivered are already filtered to commandable
  // entities without entity_category. Detection counts them per device.
  const hasPowerEntity = device => Boolean(device && (device.entities || []).some(entity => POWER_UNITS.includes(entity.unit_of_measurement)));

  const hasOwn = (assignments, entityId) => Boolean(assignments) && Object.prototype.hasOwnProperty.call(assignments, entityId);

  const groupByDevice = switches => {
    const groups = new Map();
    for (const item of switches || []) {
      if (!groups.has(item.device_id)) groups.set(item.device_id, []);
      groups.get(item.device_id).push(item);
    }
    return groups;
  };

  const channelOf = entry => (Number.isInteger(entry.channel) && entry.channel >= 1 && entry.channel <= 16 ? entry.channel : 1);

  // Map<deviceId, Array<{entityId, name, state, channel, source}>>, sorted by
  // channel. A saved energy entry for any switch of a device replaces the
  // detection for all its switches: only the switches with role "switch"
  // count, an empty role means no channel. Without any saved entry, a device
  // with exactly one switch and a power entity gets that switch on channel 1.
  const resolveSwitches = ({switches, assignments, devices}) => {
    const deviceById = new Map((devices || []).map(device => [device.id, device]));
    const result = new Map();
    for (const [deviceId, list] of groupByDevice(switches)) {
      const hasSavedEntry = list.some(item => hasOwn(assignments, item.entity_id));
      let entries;
      if (hasSavedEntry) {
        entries = list
          .filter(item => hasOwn(assignments, item.entity_id) && assignments[item.entity_id].role === 'switch')
          .map(item => ({entityId: item.entity_id, name: item.name, state: item.state, channel: channelOf(assignments[item.entity_id]), source: 'override'}));
      } else if (list.length === 1 && hasPowerEntity(deviceById.get(deviceId))) {
        const [item] = list;
        entries = [{entityId: item.entity_id, name: item.name, state: item.state, channel: 1, source: 'heuristic'}];
      } else {
        entries = [];
      }
      if (entries.length) result.set(deviceId, entries.sort((a, b) => a.channel - b.channel));
    }
    return result;
  };

  // Channel of the energy edge parent to child, 1 without a saved entry.
  // Edge props are stored for energy edges only, the key is orientation free.
  const edgeChannel = (edgeProps, parent, child) => {
    const key = pairKey(parent, child);
    const prop = (edgeProps || []).find(item => pairKey(item.parent_id, item.child_id) === key);
    return prop && Number.isInteger(prop.channel) && prop.channel >= 1 && prop.channel <= 16 ? prop.channel : 1;
  };

  // Wiring edges that carry energy between two devices, as {parent, child,
  // channel}. Memberships and edges touching a group are not power paths.
  const pathPairs = (edgeElements, edgeProps) => (edgeElements || [])
    .map(element => element.data)
    .filter(data => data && data.connType === 'energy' && !data.membership)
    .filter(data => !model().isGroupId(data.source) && !model().isGroupId(data.target))
    .map(data => ({parent: data.source, child: data.target, channel: edgeChannel(edgeProps, data.source, data.target)}));

  // Nodes that lose power: below a cut edge, and not reachable from any root
  // without passing a cut edge. Map<nodeId, {plug, hops}>, where plug is the
  // parent of the nearest cut edge. A node that is itself the parent of a cut
  // edge is a switch output that is off, so it is never dead.
  const deadArea = ({pairs, cutKeys}) => {
    const cut = new Set(cutKeys || []);
    const children = new Map();
    const hasParent = new Set();
    const nodes = new Set();
    const cutParents = new Set();
    for (const {parent, child} of pairs || []) {
      if (!children.has(parent)) children.set(parent, []);
      children.get(parent).push(child);
      hasParent.add(child);
      nodes.add(parent);
      nodes.add(child);
      if (cut.has(pairKey(parent, child))) cutParents.add(parent);
    }

    // Reached from the roots without ever crossing a cut edge.
    const roots = [...nodes].filter(node => !hasParent.has(node));
    const powered = new Set(roots);
    const queue = [...roots];
    while (queue.length) {
      const node = queue.shift();
      for (const child of children.get(node) || []) {
        if (cut.has(pairKey(node, child)) || powered.has(child)) continue;
        powered.add(child);
        queue.push(child);
      }
    }

    // From the child of every cut edge downward: the nearest plug wins.
    const dead = new Map();
    for (const {parent, child} of pairs || []) {
      if (!cut.has(pairKey(parent, child))) continue;
      const hops = new Map([[child, 1]]);
      const walk = [child];
      while (walk.length) {
        const node = walk.shift();
        for (const next of children.get(node) || []) {
          if (hops.has(next)) continue;
          hops.set(next, hops.get(node) + 1);
          walk.push(next);
        }
      }
      for (const [node, distance] of hops) {
        if (cutParents.has(node) || powered.has(node)) continue;
        const best = dead.get(node);
        if (!best || distance < best.hops) dead.set(node, {plug: parent, hops: distance});
      }
    }
    return dead;
  };

  // Everything the map needs about switch channels in one call.
  // edges: every pair whose parent device has a switch on the pair's channel,
  // with that switch's state. off: devices whose switches are all off. dead:
  // see deadArea, fed with the off edges. leaf: switch devices without a
  // switched edge, with the state of their first switch, they get a hollow
  // dot instead of a start point on an edge.
  const status = ({switches, assignments, devices, pairs}) => {
    const byDevice = resolveSwitches({switches, assignments, devices});
    const edges = new Map();
    for (const {parent, child, channel} of pairs || []) {
      const entry = (byDevice.get(parent) || []).find(item => item.channel === channel);
      if (!entry) continue;
      edges.set(pairKey(parent, child), {plug: parent, entityId: entry.entityId, name: entry.name, channel, state: entry.state});
    }
    const off = new Set([...byDevice].filter(([, entries]) => entries.every(entry => entry.state === 'off')).map(([deviceId]) => deviceId));
    const cutKeys = [...edges].filter(([, edge]) => edge.state === 'off').map(([key]) => key);
    const dead = deadArea({pairs, cutKeys});
    const plugs = new Set([...edges.values()].map(edge => edge.plug));
    const leaf = new Map([...byDevice].filter(([deviceId]) => !plugs.has(deviceId)).map(([deviceId, entries]) => [deviceId, entries[0].state]));
    return {byDevice, edges, off, dead, leaf};
  };

  window.DeviceMapSwitch = {resolveSwitches, edgeChannel, pathPairs, deadArea, status};
})();
