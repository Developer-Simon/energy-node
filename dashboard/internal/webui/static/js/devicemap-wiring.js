// Connection type and energy direction of a wiring edge (decisions 6 to 9,
// plan 2026-10-11-devicemap-bilanzknoten-automation-verbindungstypen). Pure:
// no DOM, no Cytoscape. A saved entry in device-map.json edge_props wins,
// otherwise the heuristic decides and the edge is marked as detected.
(() => {
  const TYPES = Object.freeze(['energy', 'data', 'info']);
  const DIRECTIONS = Object.freeze(['auto', 'reversed', 'fixed_down', 'fixed_up']);
  const key = (a, b) => window.DeviceMapGraph.relationKey(a, b);

  const propsIndex = edgeProps => new Map((edgeProps || []).map(prop => [key(prop.parent_id, prop.child_id), prop]));

  const hasPowerRole = (id, energy) => {
    const model = window.DeviceMapModel;
    if (id === model.OWN_ENERGY_DEVICE_ID) return false;
    const entry = energy.get(id);
    return Boolean(entry && entry.power != null);
  };

  const resolve = ({parent, child, membership}, index, energy) => {
    if (membership) return {type: 'energy', direction: 'auto', heuristic: false};
    // A sub-device edge is always energy flowing down, whatever is saved, as
    // wiringElements() draws it.
    if (window.DeviceMapSubDevices && window.DeviceMapSubDevices.isSubId(child)) return {type: 'energy', direction: 'fixed_down', heuristic: false};
    const saved = index.get(key(parent, child));
    if (saved) {
      // A saved direction refers to the orientation it was saved with. When
      // the stored pair is the other way round, fixed directions flip.
      let direction = saved.type === 'energy' ? (saved.direction || 'auto') : 'auto';
      if (saved.parent_id !== parent) direction = {fixed_down: 'fixed_up', fixed_up: 'fixed_down'}[direction] || direction;
      return {type: saved.type, direction, heuristic: false};
    }
    const model = window.DeviceMapModel;
    const own = model.OWN_ENERGY_DEVICE_ID;
    const groupEnd = parent !== own && child !== own && (model.isGroupId(parent) || model.isGroupId(child));
    const energyEdge = groupEnd || (hasPowerRole(parent, energy) && hasPowerRole(child, energy));
    return {type: energyEdge ? 'energy' : 'data', direction: 'auto', heuristic: true};
  };

  // Positive = toward the child, as edgeFlow() reports it.
  const applyDirection = (value, direction) => {
    if (direction === 'reversed') return -value;
    if (direction === 'fixed_down') return Math.abs(value);
    if (direction === 'fixed_up') return -Math.abs(value);
    return value;
  };

  const energyPairs = (pairs, index, energy) => (pairs || [])
    .map(pair => ({pair, props: resolve(pair, index, energy)}))
    .filter(({props}) => props.type === 'energy')
    .map(({pair, props}) => ({parent: pair.parent, child: pair.child, direction: props.direction}));

  // Channel only means something on an energy edge, so it is kept there and
  // dropped for every other type.
  const setProp = (edgeProps, {parent, child, type, direction, channel}) => {
    const next = (edgeProps || []).filter(prop => key(prop.parent_id, prop.child_id) !== key(parent, child));
    const entry = {parent_id: parent, child_id: child, type};
    if (type === 'energy') {
      entry.direction = DIRECTIONS.includes(direction) ? direction : 'auto';
      if (Number.isInteger(channel) && channel >= 1 && channel <= 16) entry.channel = channel;
    }
    return [...next, entry];
  };

  const prune = (edgeProps, livePairKeys) => (edgeProps || []).filter(prop => livePairKeys.has(key(prop.parent_id, prop.child_id)));

  // What edgeFlow() needs over the energy edges only: the children index and
  // the direction of each energy pair. Data and info edges carry no flow.
  const flowContext = (pairs, edgeProps, energy) => {
    const energyOnly = energyPairs(pairs, propsIndex(edgeProps), energy);
    const children = window.DeviceMapModel.childrenIndex(energyOnly);
    const dirOf = (parent, child) => (energyOnly.find(pair => pair.parent === parent && pair.child === child) || {}).direction || 'auto';
    return {children, dirOf};
  };

  window.DeviceMapWiring = {TYPES, DIRECTIONS, propsIndex, resolve, applyDirection, energyPairs, setProp, prune, flowContext};
})();
