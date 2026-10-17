// Cytoscape element and style builders for the device map. Pure functions:
// no DOM, no Cytoscape instance. devicemap.page.js and, from PR6 on, the
// energy tab's plant view build their graphs from these. Spec:
// .docs/superpowers/specs/2026-10-07-devicemap-energie-datenfluss-design.md
(() => {
  const relationKey = (a, b) => [a, b].sort().join('::');

  // taxi/unbundled-bezier ship in the vendored cytoscape.min.js already, no new dependency.
  const EDGE_STYLES = {
    straight: {'curve-style': 'straight'},
    elbow: {'curve-style': 'taxi', 'taxi-direction': 'auto', 'taxi-turn': '50%', 'taxi-turn-min-distance': 5},
    curved: {'curve-style': 'unbundled-bezier', 'control-point-distances': [40], 'control-point-weights': [0.5]},
  };

  // Wiring edges only. Sub-device and memberships come first, then
  // deviceMap.edges, so their overrideId, membership and subDevice data win the
  // pair-dedup over discovery relations.
  const wiringElements = ({devices, deviceMap, groups, nodeIds, energy = new Map()}) => {
    const elements = [];
    const seenEdges = new Set();
    const index = window.DeviceMapWiring.propsIndex(deviceMap.edge_props);
    // overrideId is only set for manually created relations (settings.RelationOverride) -
    // those are the only ones removeSelectedRelation() is allowed to delete.
    const addEdge = (childId, parentId, overrideId, membership, subDevice) => {
      if (!nodeIds.has(childId) || !nodeIds.has(parentId) || childId === parentId) return;
      const key = relationKey(childId, parentId);
      if (seenEdges.has(key)) return;
      seenEdges.add(key);
      const data = {id: `edge-${key}`, source: parentId, target: childId};
      if (overrideId) data.overrideId = overrideId;
      if (membership) data.membership = membership;
      if (subDevice) {
        // A sub-device is measured by its device, so its edge is always
        // energy flowing down. Only the channel is saved for it.
        const saved = index.get(key);
        data.subDevice = true;
        data.connType = 'energy';
        data.direction = 'fixed_down';
        data.heuristic = false;
        if (saved && Number.isInteger(saved.channel)) data.channel = saved.channel;
      } else {
        const props = window.DeviceMapWiring.resolve({parent: parentId, child: childId, membership}, index, energy);
        data.connType = props.type;
        data.direction = props.direction;
        data.heuristic = props.heuristic;
      }
      const classes = ['devicemap-wiring', `devicemap-conn-${data.connType}`];
      if (data.heuristic) classes.push('devicemap-conn-detected');
      elements.push({data, classes: classes.join(' ')});
    };
    for (const {parent, child} of window.DeviceMapSubDevices.pairs(deviceMap, nodeIds)) {
      addEdge(child, parent, null, null, true);
    }
    // Memberships come from energy.json, not from device-map.json (one
    // source of truth). They render as wiring, so edgeFlow() sums a
    // group like any other subtree.
    for (const {parent, child} of window.DeviceMapModel.membershipPairs(groups)) {
      addEdge(child, parent, null, {group: parent.slice(window.DeviceMapModel.GROUP_PREFIX.length), member: child});
    }
    // deviceMap.edges (RelationOverride, carries the real overrideId)
    // must be processed before device.relations: the registry merges
    // overrides into each device's relations list too (so the graph and
    // other UIs see them as normal relations), but DeviceRelation has no
    // overrideId field there. addEdge()'s pair-dedup means whichever
    // pass runs first "wins" the overrideId - if device.relations ran
    // first, every override-derived edge would silently lose its
    // overrideId and render as an unremovable via_device edge.
    for (const edge of deviceMap.edges || []) {
      addEdge(edge.child_id, edge.parent_id, edge.id);
    }
    for (const device of devices) {
      for (const relation of device.relations || []) {
        if (relation.kind === 'parent') addEdge(device.id, relation.id);
        else if (relation.kind === 'child') addEdge(relation.id, device.id);
      }
    }
    return elements;
  };

  const pairsOf = edgeElements => edgeElements
    .map(element => ({parent: element.data.source, child: element.data.target}));

  // `cut` holds node ids whose outgoing energy edges carry no flow (switched
  // off outputs and nodes without power). Pairs under a cut parent get no flow.
  const flowElements = ({pairs, energy, view, edgeProps, cut}) => {
    const model = window.DeviceMapModel;
    const wiring = window.DeviceMapWiring;
    const index = wiring.propsIndex(edgeProps);
    const energyPairs = wiring.energyPairs(pairs, index, energy);
    const children = model.childrenIndex(energyPairs);
    const dirOf = (parent, child) => (energyPairs.find(pair => pair.parent === parent && pair.child === child) || {}).direction || 'auto';
    const byPower = view.width_by_power;
    const flows = [];
    for (const {parent, child} of energyPairs) {
      if (cut && cut.has(parent)) continue;
      const flow = model.edgeFlow(child, energy, children, {dirOf, parentId: parent});
      if (!flow) continue;
      const reverse = flow.value < 0;
      const abs = Math.abs(flow.value);
      flows.push({
        group: 'edges',
        classes: 'devicemap-flow',
        data: {
          id: `flow-${parent}::${child}`,
          source: reverse ? child : parent,
          target: reverse ? parent : child,
          label: model.flowLabel(flow),
          color: model.flowColorToken(flow, energy.get(child)),
          width: model.flowWidth(abs, byPower),
          speed: model.speedBucket(abs),
          reverse,
        },
      });
    }
    return flows;
  };

  // Balance, rule, service and stub nodes. Positions come from the saved map
  // (or the page's placement), unknown ones are left to the layout.
  const virtualElements = ({kinds, positions, colorOf}) => kinds.map(({id, kind, name}) => {
    const element = {
      data: {id, name, svg: window.DeviceMapNodeSvg.virtualDataUri(kind, colorOf)},
      classes: `devicemap-virtual devicemap-${kind}`,
    };
    const position = positions.get(id);
    if (position) element.position = {x: position.x, y: position.y};
    return element;
  });

  // Data flow edges. data.id is prefixed so it never collides with a wiring or
  // flow edge id, flowId keeps the original id for focus and popovers.
  const dataEdgeElements = edges => edges.map(edge => ({
    group: 'edges',
    data: {id: `data-${edge.id}`, source: edge.from, target: edge.to, flowId: edge.id},
    classes: `devicemap-data devicemap-data-${edge.cat}`,
  }));

  const flowOffset = (nowMs, speed) => {
    const period = window.DeviceMapModel.SPEED_SECONDS[speed] || window.DeviceMapModel.SPEED_SECONDS.mid;
    return -(((nowMs / 1000) % period) / period) * window.DeviceMapModel.DASH_CYCLE;
  };

  // One requestAnimationFrame loop for all flows (spec "Bewegung"). It stops
  // itself when nothing should move and is restarted by its owner.
  const createFlowAnimator = ({getCy, shouldAnimate, requestFrame, cancelFrame}) => {
    let frame = null;
    const step = now => {
      if (!shouldAnimate()) { frame = null; return; }
      const cy = getCy();
      if (cy) {
        cy.batch(() => cy.edges('.devicemap-flow').forEach(edge => {
          edge.style('line-dash-offset', flowOffset(now, edge.data('speed')));
        }));
      }
      frame = requestFrame(step);
    };
    return {
      start() { if (!frame) frame = requestFrame(step); },
      stop() { if (frame) cancelFrame(frame); frame = null; },
    };
  };

  // `theme` holds the resolved colours (line, accent, labelStrong, panel) as
  // plain values. `color(token)` resolves a token at draw time, for the
  // per-edge colours that vary per flow.
  const style = ({view, theme, color, reducedMotion}) => [
    {selector: 'node', style: {
      label: '', width: window.DeviceMapNodeSvg.SIZE, height: window.DeviceMapNodeSvg.SIZE,
      shape: 'ellipse', 'background-opacity': 0, 'border-width': 0,
      'background-image': 'data(svg)', 'background-fit': 'contain', 'background-clip': 'none',
      'background-image-smoothing': 'yes',
      'outline-width': 0, 'outline-color': theme.accent, 'outline-offset': 3,
    }},
    {selector: 'node.devicemap-connect-source', style: {'outline-width': 3}},
    // Snap-preview (see onNodeDrag()): an unselectable, non-interactive
    // placeholder at the grid cell the dragged node will land on.
    // 'events: no' keeps it from stealing taps/drags from whatever is
    // underneath it.
    {selector: 'node.devicemap-ghost', style: {
      label: '', 'background-opacity': 0, 'background-image': 'none',
      'border-width': 2, 'border-style': 'dashed', 'border-color': theme.accent,
      events: 'no',
    }},
    {selector: 'edge', style: {
      width: 2, 'line-color': theme.line, 'target-arrow-color': theme.line,
      'target-arrow-shape': 'triangle', 'arrow-scale': 0.9,
      ...(EDGE_STYLES[view.edge_style] || EDGE_STYLES.straight),
    }},
    {selector: 'edge.devicemap-wiring', style: wiringEdgeStyle(view, theme)},
    {selector: 'edge.devicemap-wiring.devicemap-conn-data', style: {'line-style': 'dashed', 'line-dash-pattern': [6, 4]}},
    {selector: 'edge.devicemap-wiring.devicemap-conn-info', style: {width: 1, 'line-color': theme.faint, 'target-arrow-color': theme.faint}},
    {selector: 'edge.devicemap-wiring.devicemap-conn-detected', style: {'line-opacity': 0.7}},
    ...(view.layers.wiring ? [] : [{selector: 'edge.devicemap-wiring:not(.devicemap-conn-energy)', style: {display: 'none'}}]),
    // Dead edges: the source is cut (off output or no power), so no flow runs
    // over them.
    {selector: 'edge.devicemap-wiring.devicemap-dead', style: {'line-style': 'dotted', 'line-dash-pattern': [2, 4], 'line-color': theme.faint, 'target-arrow-shape': 'none'}},
    // Switch ports: a filled dot at the source of every switched wiring edge,
    // in the color of the channel's state.
    {selector: 'edge.devicemap-port-on', style: {'source-arrow-shape': 'circle', 'source-arrow-fill': 'filled', 'source-arrow-color': theme.ok, 'source-distance-from-node': 0, 'arrow-scale': 0.9}},
    {selector: 'edge.devicemap-port-off', style: {'source-arrow-shape': 'circle', 'source-arrow-fill': 'filled', 'source-arrow-color': theme.bad, 'source-distance-from-node': 0, 'arrow-scale': 0.9}},
    {selector: 'edge.devicemap-port-unknown', style: {'source-arrow-shape': 'circle', 'source-arrow-fill': 'filled', 'source-arrow-color': theme.faint, 'source-distance-from-node': 0, 'arrow-scale': 0.9}},
    // Quiet nodes stack the normal and the quiet image. Cytoscape interpolates
    // the list opacities, the cross-fade relies on that. The list comes from a
    // function because Cytoscape leaves a data() mapper inside a list as the
    // literal URL "data(svg)", which draws no image at all.
    {selector: 'node[svgQuiet]', style: {'background-image': node => [node.data('svg'), node.data('svgQuiet')], 'background-image-opacity': [1, 0]}},
    {selector: 'node[svgQuiet][?quiet]', style: {'background-image-opacity': [0, 1]}},
    {selector: 'edge.devicemap-flow', style: {
      ...(EDGE_STYLES[view.edge_style] || EDGE_STYLES.straight),
      'line-color': edge => color(edge.data('color')),
      width: 'data(width)',
      'line-style': reducedMotion ? 'solid' : 'dashed',
      'line-dash-pattern': window.DeviceMapModel.DASH_PATTERN,
      'line-cap': 'round',
      'target-arrow-shape': reducedMotion ? 'triangle' : 'none',
      'target-arrow-color': edge => color(edge.data('color')),
      'arrow-scale': 0.7,
      events: 'no',
      'font-size': '10px', 'font-weight': 600, 'font-family': 'system-ui, sans-serif', color: theme.labelStrong,
      'text-background-color': theme.panel, 'text-background-opacity': 1, 'text-background-shape': 'round-rectangle', 'text-background-padding': '3px',
      'text-border-width': 1.2, 'text-border-opacity': 1, 'text-border-color': edge => color(edge.data('color')),
    }},
    {selector: 'edge.devicemap-flow[!reverse]', style: {'target-label': 'data(label)', 'target-text-offset': 32}},
    {selector: 'edge.devicemap-flow[?reverse]', style: {'source-label': 'data(label)', 'source-text-offset': 32}},
    ...(view.edge_style === 'curved' ? [{selector: 'edge.devicemap-flow[?reverse]', style: {'control-point-distances': [-40]}}] : []),
    {selector: 'node, edge', style: {'transition-property': 'opacity', 'transition-duration': '200ms', 'transition-timing-function': 'ease-out'}},
    {selector: '.devicemap-dimmed', style: {opacity: 0.18}},
    {selector: 'node.devicemap-focused', style: {'outline-width': 2.5}},
    {selector: 'edge.devicemap-selected-edge', style: {width: 3, 'line-color': theme.accent, 'target-arrow-color': theme.accent}},
    {selector: 'edge.devicemap-data', style: {
      'curve-style': 'unbundled-bezier', 'control-point-distances': [36], 'control-point-weights': [0.5],
      width: 1.5, 'line-style': 'dashed', 'line-dash-pattern': [5, 4],
      'line-color': theme.info, 'target-arrow-shape': 'triangle', 'target-arrow-color': theme.info, 'arrow-scale': 0.8,
      'overlay-opacity': 0, 'overlay-padding': 7,
    }},
    {selector: 'edge.devicemap-data-role', style: {'line-color': theme.faint, 'target-arrow-color': theme.faint}},
    {selector: 'edge.devicemap-data-stub', style: {'line-color': theme.warn, 'target-arrow-color': theme.warn}},
    {selector: 'edge.devicemap-data.devicemap-hover, edge.devicemap-data.devicemap-focused', style: {'line-color': theme.accent, 'target-arrow-color': theme.accent}},
    {selector: 'edge.devicemap-data-role.devicemap-hover, edge.devicemap-data-role.devicemap-focused', style: {'line-color': theme.subtle, 'target-arrow-color': theme.subtle}},
    {selector: 'node.devicemap-stub', style: {width: window.DeviceMapNodeSvg.STUB_SIZE, height: window.DeviceMapNodeSvg.STUB_SIZE}},
    {selector: 'node.devicemap-virtual', style: {
      'transition-property': reducedMotion ? 'opacity' : 'opacity, width, height',
      'transition-duration': '200ms', 'transition-timing-function': 'ease-out',
    }},
    {selector: '.devicemap-hidden', style: {opacity: 0, events: 'no'}},
    {selector: 'node.devicemap-virtual.devicemap-hidden', style: {width: window.DeviceMapNodeSvg.SIZE * 0.94, height: window.DeviceMapNodeSvg.SIZE * 0.94}},
  ];

  // Draft layer rules: wiring on = normal line, wiring off + energy on =
  // dotted track under the flows, both off = invisible and untappable. The
  // dotted track stays tappable: the flow edge on top ignores events, so a
  // tap on an energy line lands here and opens the wiring card. The wide
  // invisible overlay makes the thin line easy to hit.
  const wiringEdgeStyle = (view, theme) => {
    const {wiring, energy} = view.layers;
    if (wiring) return {display: 'element', 'line-style': 'solid', opacity: 1, 'overlay-opacity': 0, 'overlay-padding': 7};
    if (energy) return {display: 'element', 'line-style': 'dotted', 'line-dash-pattern': [2, 4], 'target-arrow-shape': 'none', opacity: 0.8, 'line-color': theme.line, 'overlay-opacity': 0, 'overlay-padding': 7};
    return {display: 'none'};
  };

  window.DeviceMapGraph = {EDGE_STYLES, relationKey, wiringElements, pairsOf, flowElements, virtualElements, dataEdgeElements, flowOffset, createFlowAnimator, style, wiringEdgeStyle};
})();
