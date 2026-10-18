// Pure helpers for sub-devices: a single measurement of a device (for example
// the power of a washing machine on a plug) shown on the map as its own node.
// No DOM and no Cytoscape here, devicemap.page.js does the drawing. Spec:
// .docs/superpowers/specs/2026-10-17-devicemap-untergeraete-design.md
(() => {
  const PREFIX = 'sub:';
  const POWER_UNITS = Object.freeze(['W', 'kW']);
  const model = () => window.DeviceMapModel;

  const isSubId = id => typeof id === 'string' && id.startsWith(PREFIX);

  const list = deviceMap => ((deviceMap && deviceMap.sub_devices) || []).filter(Boolean);

  const byEntity = deviceMap => new Map(list(deviceMap).map(sub => [sub.entity_id, sub]));

  const labelFor = (deviceMap, entityId) => {
    const sub = byEntity(deviceMap).get(entityId);
    return sub && sub.label ? sub.label : null;
  };

  // The snapshot is copied, never changed: entities with a sub-device move
  // to the sub-device's ID, every other entity stays as it is.
  const remapSnapshot = (snapshot, deviceMap) => {
    if (!snapshot) return snapshot;
    const subs = byEntity(deviceMap);
    return {
      ...snapshot,
      entities: (snapshot.entities || []).map(entity => (subs.has(entity.entity_id)
        ? {...entity, device_id: subs.get(entity.entity_id).id}
        : entity)),
    };
  };

  // Parent and child edge of each sub-device. Only sub-devices whose device
  // is shown on the map get an edge.
  const pairs = (deviceMap, nodeIds) => list(deviceMap)
    .filter(sub => nodeIds.has(sub.device_id))
    .map(sub => ({parent: sub.device_id, child: sub.id}));

  // A sub-device is orphaned when its device or its entity is gone.
  const isOrphan = (sub, devices) => {
    const device = (devices || []).find(item => item.id === sub.device_id);
    if (!device) return true;
    return !(device.entities || []).some(entity => entity && entity.unique_id === sub.entity_id);
  };

  // Right of the anchor, one step further down for every taken spot in the
  // way. A spot counts as taken within one grid cell, as in placeGroup().
  // snap is the grid size or null when the grid is off.
  const placeNew = ({anchor, taken, snap}) => {
    const {PLACE_STEP_X, PLACE_STEP_Y} = model();
    const round = value => (snap ? Math.round(value / snap) * snap : value);
    // With a grid coarser than the step, a step below the grid size would round
    // back onto the same row forever. Step by at least one grid cell.
    const step = snap ? Math.max(PLACE_STEP_Y, snap) : PLACE_STEP_Y;
    const occupied = (x, y) => (taken || []).some(p => Math.abs(p.x - x) < PLACE_STEP_X && Math.abs(p.y - y) < PLACE_STEP_Y);
    const x = round(anchor.x + PLACE_STEP_X);
    let y = round(anchor.y);
    while (occupied(x, y)) y = round(y + step);
    return {x, y};
  };

  const powerEntities = device => ((device && device.entities) || [])
    .filter(entity => entity && POWER_UNITS.includes(entity.unit_of_measurement));

  window.DeviceMapSubDevices = {
    PREFIX, isSubId, list, byEntity, labelFor, remapSnapshot, pairs, isOrphan, placeNew, powerEntities,
  };
})();
