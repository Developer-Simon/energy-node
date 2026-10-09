// Pure logic of the data flow and balance layers: which virtual node and
// edge is visible, the role edges into the balance, the focus set across
// layers, where new virtual nodes go and what the detail popover shows.
// No DOM and no Cytoscape. Spec:
// .docs/superpowers/specs/2026-10-07-devicemap-energie-datenfluss-design.md
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);

  const BALANCE_ID = 'balance';
  const STUB_PREFIX = 'stub:';
  const ruleId = id => `rule:${encodeURIComponent(id)}`;

  const nodeKind = id => {
    if (id === BALANCE_ID) return 'balance';
    if (id.startsWith('group:')) return 'group';
    if (id.startsWith('rule:')) return 'rule';
    if (id.startsWith('service:')) return 'service';
    if (id.startsWith(STUB_PREFIX)) return 'stub';
    return 'device';
  };
  const isVirtualId = id => nodeKind(id) !== 'device';
  const isPersistedVirtual = id => ['balance', 'group', 'rule', 'service'].includes(nodeKind(id));

  const nodeVisible = (kind, layers) => {
    if (kind === 'balance') return Boolean(layers.balance);
    if (kind === 'rule' || kind === 'service' || kind === 'stub') return Boolean(layers.data);
    return true;
  };
  // Spec "Ebene -> Sichtbarkeit": an edge between balance and rule needs both
  // layers. Set per element, so focus dimming can never reveal a hidden one.
  const edgeVisible = (edge, layers) => {
    if (edge.cat === 'role') return Boolean(layers.balance);
    if (edge.from === BALANCE_ID || edge.to === BALANCE_ID) return Boolean(layers.data && layers.balance);
    return Boolean(layers.data);
  };

  const refId = ref => (ref && (ref.virtual_id || ref.device_id)) || '';

  const flowEdges = (graph, nodeIds) => ((graph && graph.edges) || [])
    .map(edge => ({id: edge.id, cat: edge.cat, from: refId(edge.from), to: refId(edge.to), fromEntity: edge.from.entity_id || '', toEntity: edge.to.entity_id || '',
      title: edge.title, titleKey: edge.title_key || '', details: edge.details || {}, link: edge.link || {}}))
    .filter(edge => nodeIds.has(edge.from) && nodeIds.has(edge.to));

  const stubs = (graph, nodeIds) => {
    const nodes = [];
    const edges = [];
    let invalid = 0;
    const perAnchor = new Map();
    for (const item of (graph && graph.unresolved) || []) {
      const anchor = refId(item.target);
      if (!anchor) { if (item.reason === 'config_invalid') invalid += 1; continue; }
      if (!nodeIds.has(anchor)) continue;
      const id = STUB_PREFIX + item.id;
      const index = perAnchor.get(anchor) || 0;
      perAnchor.set(anchor, index + 1);
      nodes.push({id, unresolved: item, anchor, index});
      const output = item.direction === 'output';
      edges.push({id: `stub-edge:${item.id}`, cat: 'stub', from: output ? anchor : id, to: output ? id : anchor, unresolved: item});
    }
    return {nodes, edges, invalid};
  };

  const roleEdges = snapshot => {
    const byDevice = new Map();
    for (const entity of (snapshot && snapshot.entities) || []) {
      const role = entity.role && entity.role.role;
      if (!role || !entity.device_id || entity.device_id === window.DeviceMapModel.OWN_ENERGY_DEVICE_ID) continue;
      let entry = byDevice.get(entity.device_id);
      if (!entry) {
        entry = {id: `role:${entity.device_id}`, cat: 'role', from: entity.device_id, to: BALANCE_ID, roles: [], entities: [], heuristic: false};
        byDevice.set(entity.device_id, entry);
      }
      if (!entry.roles.includes(role)) entry.roles.push(role);
      entry.entities.push(entity.entity_id);
      if (entity.role.source === 'heuristic') entry.heuristic = true;
    }
    return [...byDevice.values()];
  };

  // Draft related(): data edges are walked transitively forward and backward,
  // wiring only to the direct neighbours and only with wiring or energy on.
  const relatedFocus = (id, {wiringPairs, dataEdges, layers}) => {
    const nodes = new Set([id]);
    const edges = new Set();
    const visible = (dataEdges || []).filter(edge => edgeVisible(edge, layers));
    for (const forward of [true, false]) {
      const queue = [id];
      const seen = new Set([id]);
      while (queue.length) {
        const current = queue.shift();
        for (const edge of visible) {
          const [a, b] = forward ? [edge.from, edge.to] : [edge.to, edge.from];
          if (a !== current) continue;
          edges.add(edge.id);
          nodes.add(b);
          if (!seen.has(b)) { seen.add(b); queue.push(b); }
        }
      }
    }
    if (layers.wiring || layers.energy) {
      for (const {parent, child} of wiringPairs || []) {
        if (parent !== id && child !== id) continue;
        nodes.add(parent);
        nodes.add(child);
        edges.add(window.DeviceMapGraph.relationKey(parent, child));
      }
    }
    return {nodes, edges};
  };

  const STEP_X = 140;
  const STEP_Y = 120;
  const mean = values => values.reduce((sum, value) => sum + value, 0) / values.length;

  const placeBalance = ({rolePositions, allPositions, snap}) => {
    const all = allPositions || [];
    const x = all.length ? Math.max(...all.map(p => p.x)) + STEP_X : 0;
    const anchor = rolePositions && rolePositions.length ? rolePositions : all;
    const y = anchor.length ? (Math.min(...anchor.map(p => p.y)) + Math.max(...anchor.map(p => p.y))) / 2 : 0;
    return {x: snap(x), y: snap(y)};
  };

  const placeFlowNodes = ({nodes, allPositions, snap}) => {
    const all = allPositions || [];
    const minX = all.length ? Math.min(...all.map(p => p.x)) : 0;
    const maxX = all.length ? Math.max(...all.map(p => p.x)) : 0;
    const maxY = all.length ? Math.max(...all.map(p => p.y)) : 0;
    const columns = Math.max(1, Math.min(8, Math.round((maxX - minX) / STEP_X) + 1));
    const sorted = [...(nodes || [])].sort((a, b) => {
      const ax = a.inputXs.length ? mean(a.inputXs) : Infinity;
      const bx = b.inputXs.length ? mean(b.inputXs) : Infinity;
      return ax - bx || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
    });
    return sorted.map((node, index) => ({
      id: node.id,
      x: snap(minX + (index % columns) * STEP_X),
      y: snap(maxY + STEP_Y + Math.floor(index / columns) * STEP_Y),
    }));
  };

  // Freigabe 2: left above the target for inputs, right below the source for
  // outputs, several stubs of one anchor 26 px apart.
  const stubPosition = (anchor, direction, index) => (direction === 'output'
    ? {x: anchor.x + 64 + index * 26, y: anchor.y + 48 + index * 26}
    : {x: anchor.x - 64 - index * 26, y: anchor.y - 48 - index * 26});

  const titleOf = edge => {
    if (edge.titleKey) {
      const text = t(edge.titleKey);
      if (text && text !== edge.titleKey) return text;
    }
    return edge.title || '';
  };

  const edgeView = (edge, labelOf) => {
    const d = edge.details || {};
    const rows = [];
    const push = (key, value) => { if (value !== undefined && value !== '') rows.push([key, value]); };
    if (edge.cat === 'service') {
      push('devicemap.pop.row.source', [labelOf(edge.from), edge.fromEntity].filter(Boolean).join(', '));
      push('devicemap.pop.row.topic', d.topic);
      push('devicemap.pop.row.json_key', d.json_key);
      push('devicemap.pop.row.unit', d.unit);
      push('devicemap.pop.row.target', [labelOf(edge.to), d.field].filter(Boolean).join(', '));
      const output = edge.toEntity && !edge.fromEntity;
      return {eyebrowKey: output ? 'devicemap.pop.eyebrow.service_output' : 'devicemap.pop.eyebrow.service_input', title: titleOf(edge), rows, linkKey: 'devicemap.pop.link.config', link: edge.link};
    }
    if (edge.cat === 'automation') {
      push('devicemap.pop.row.kind', d.kind ? t(`devicemap.flow.kind.${d.kind}`) : '');
      push('devicemap.pop.row.source', edge.fromEntity ? `${labelOf(edge.from)}, ${edge.fromEntity}` : '');
      push('devicemap.pop.row.topic', d.topic);
      push('devicemap.pop.row.json_key', d.json_key);
      push('devicemap.pop.row.value', d.value);
      push('devicemap.pop.row.target', edge.toEntity ? `${labelOf(edge.to)}, ${edge.toEntity}` : '');
      return {eyebrowKey: 'devicemap.pop.eyebrow.automation', title: titleOf(edge), rows, linkKey: 'devicemap.pop.link.automations', link: edge.link, part: d.part, index: d.index};
    }
    if (edge.cat === 'role') {
      push('devicemap.pop.row.device', labelOf(edge.from));
      push('devicemap.pop.row.entities', (edge.entities || []).join(', '));
      push('devicemap.pop.row.origin', t(edge.heuristic ? 'devicemap.panel.source.heuristic' : 'devicemap.panel.source.override'));
      const roles = (edge.roles || []).map(role => (role.startsWith('custom:') ? labelOf(role) : t(`energy.role_label.${role}`))).join(', ');
      return {eyebrowKey: 'devicemap.pop.eyebrow.role', title: t('devicemap.pop.role_title', {roles}), rows, linkKey: 'devicemap.pop.link.energy', link: {tab: 'energy', entities: edge.entities}};
    }
    const u = edge.unresolved || {};
    push('devicemap.pop.row.topic', u.topic);
    push('devicemap.pop.row.reason', t(`devicemap.stub.reason.${u.reason}`));
    push('devicemap.pop.row.config', u.config);
    return {eyebrowKey: 'devicemap.pop.eyebrow.stub', title: titleOf({title: u.title, titleKey: u.title_key}) || t('devicemap.stub.name'), rows,
      linkKey: u.link && u.link.tab === 'automations' ? 'devicemap.pop.link.automations' : 'devicemap.pop.link.config', link: u.link || {}};
  };

  const deviceFlows = (deviceId, edges, labelOf) => (edges || [])
    .filter(edge => edge.cat !== 'role' && edge.cat !== 'stub' && (edge.from === deviceId || edge.to === deviceId))
    .map(edge => ({
      title: titleOf(edge),
      text: edge.to === deviceId ? t('devicemap.panel.flow_from', {name: labelOf(edge.from)}) : t('devicemap.panel.flow_to', {name: labelOf(edge.to)}),
    }));

  window.DeviceMapDataflow = {
    BALANCE_ID, STUB_PREFIX, ruleId, nodeKind, isVirtualId, isPersistedVirtual, nodeVisible, edgeVisible,
    refId, flowEdges, stubs, roleEdges, relatedFocus, placeBalance, placeFlowNodes, stubPosition, titleOf, edgeView, deviceFlows,
  };
})();
