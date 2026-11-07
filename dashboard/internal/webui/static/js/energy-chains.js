// Pure helpers for measurement chains: an ordered path of measuring points
// (for example a plug, a storage AC side and its DC side) whose losses the
// energy page shows between the points. No DOM and no Alpine here, the
// energy page does the rendering and the saving. Chains use the shape of
// energy.json: {label, points, counting, direction}. Every function returns
// new objects, the inputs stay untouched.
(() => {
  // i18n-keys: chains.finding.drift, chains.finding.frozen, chains.finding.contradiction, chains.finding.sign, chains.finding.unit
  const EN_DASH = '–';
  const MINUS = '−';
  const POWER_UNITS = Object.freeze(['W', 'kW']);
  const model = () => window.DeviceMapModel;

  const copy = chain => ({...chain, points: [...chain.points]});

  // Chain IDs in a fixed order, so the first match never depends on insertion order.
  const idsOf = chains => Object.keys(chains || {}).sort();

  // A new chain with the entity at position index, counting unchanged.
  const insertPoint = (chain, index, entityId) => {
    const next = copy(chain);
    const at = Math.max(0, Math.min(index, next.points.length));
    next.points.splice(at, 0, entityId);
    return next;
  };

  // Null when fewer than two points would remain. Removing the counting point
  // hands counting to the first remaining point.
  const removePoint = (chain, entityId) => {
    const next = copy(chain);
    next.points = next.points.filter(id => id !== entityId);
    if (next.points.length < 2) return null;
    if (next.counting === entityId) next.counting = next.points[0];
    return next;
  };

  // Only a point of the chain can count, any other entity leaves the chain as it is.
  const setCounting = (chain, entityId) => {
    const next = copy(chain);
    if (next.points.includes(entityId)) next.counting = entityId;
    return next;
  };

  // The chain that lists the entity as a measuring point, or null.
  const chainOf = (chains, entityId) => {
    for (const id of idsOf(chains)) {
      if (chains[id].points.includes(entityId)) return {id, chain: chains[id]};
    }
    return null;
  };

  // A measuring point that does not count: its power already sits in the counting point.
  const isReference = (chains, entityId) => idsOf(chains).some(id => {
    const chain = chains[id];
    return chain.points.includes(entityId) && chain.counting !== entityId;
  });

  // "Plug Balkon → Speicher AC → DC". An entity without a label shows its ID.
  const shortPath = (chain, labelOf) => chain.points
    .map(id => labelOf(id) || id)
    .join(' → ');

  // A loss counts downward: 9 W lost reads "−9 W", a gain reads "+9 W".
  const lossPowerText = loss => {
    const sign = Math.round(Math.abs(loss)) === 0 ? '' : (loss > 0 ? MINUS : '+');
    return `${sign}${model().formatPower(loss)}`;
  };

  const percentText = percent => `${Math.round(percent)} %`;

  // Efficiency in percent, or null when the segment has none.
  const efficiencyPercent = segment => {
    const efficiency = segment && segment.available ? segment.efficiency : null;
    return typeof efficiency === 'number' && Number.isFinite(efficiency) ? efficiency * 100 : null;
  };

  // "−9 W", without a value "–".
  const lossPowerOf = segment => (segment && segment.available ? lossPowerText(Number(segment.loss_w) || 0) : EN_DASH);

  // "−9 W · 97 %", without efficiency "−9 W", without a value "–".
  const lossText = segment => {
    const percent = efficiencyPercent(segment);
    return percent === null ? lossPowerOf(segment) : `${lossPowerOf(segment)} · ${percentText(percent)}`;
  };

  // The chain in which one entity of each list are direct neighbours, with the
  // two points in chain order, or null. Two devices can also share a chain
  // without being neighbours, that is no connection between them.
  const linkBetween = (chains, firstIds, secondIds) => {
    const first = new Set(firstIds);
    const second = new Set(secondIds);
    for (const id of idsOf(chains)) {
      const {points, label} = chains[id];
      for (let index = 0; index + 1 < points.length; index += 1) {
        const [upstream, downstream] = [points[index], points[index + 1]];
        if ((first.has(upstream) && second.has(downstream)) || (first.has(downstream) && second.has(upstream))) {
          return {id, label, upstream, downstream};
        }
      }
    }
    return null;
  };

  // Power entities that no chain lists, in the item form of the device picker.
  // Each entity is {id, name, section, valueText, iconMarkup, unit_of_measurement}.
  const pickerItems = (entities, chains) => {
    const member = new Set(idsOf(chains).flatMap(id => chains[id].points));
    return (entities || [])
      .filter(entity => POWER_UNITS.includes(entity.unit_of_measurement) && !member.has(entity.id))
      .map(entity => ({
        id: entity.id,
        kind: 'device',
        name: entity.name,
        section: entity.section || '',
        valueText: entity.valueText || '',
        iconMarkup: entity.iconMarkup || '',
        memberOf: '',
      }));
  };

  // What saving an edge upstream → downstream needs. Both ends free: a new
  // chain. One end in a chain and the other free: the free one joins next to
  // it. Both ends neighbours in one chain: nothing to do. Anything else is a conflict.
  const edgeChange = ({chains, upstream, downstream, label}) => {
    if (upstream === downstream) return {action: 'conflict'};
    const up = chainOf(chains, upstream);
    const down = chainOf(chains, downstream);

    if (!up && !down) {
      return {action: 'create', body: {label, points: [upstream, downstream], counting: upstream}};
    }

    if (up && !down) {
      const index = up.chain.points.indexOf(upstream);
      return {action: 'update', id: up.id, body: insertPoint(up.chain, index + 1, downstream)};
    }

    if (!up && down) {
      const index = down.chain.points.indexOf(downstream);
      return {action: 'update', id: down.id, body: insertPoint(down.chain, index, upstream)};
    }

    if (up.id !== down.id) return {action: 'conflict'};
    const upIndex = up.chain.points.indexOf(upstream);
    const downIndex = up.chain.points.indexOf(downstream);
    return Math.abs(upIndex - downIndex) === 1 ? {action: 'none'} : {action: 'conflict'};
  };

  // Label of a point of the chain, the raw ID for an entity the chain does not name.
  const pointLabel = (chain, entityId) => {
    const point = (chain.points || []).find(item => item.entity_id === entityId);
    return point && point.label ? point.label : (entityId || '');
  };

  const sinceText = since => {
    const date = new Date(since);
    return Number.isNaN(date.getTime()) ? '' : window.I18n.formatDateTime(date);
  };

  // A factor as the locale writes numbers: 1000 stays "1000", 0.001 keeps its decimals.
  const factorText = factor => {
    const value = Number(Number(factor).toPrecision(6));
    const decimals = (String(value).split('.')[1] || '').length;
    return window.I18n.formatNumber(value, decimals);
  };

  // "Balkon: Plug Balkon und Speicher AC melden entgegengesetzte Vorzeichen."
  const findingText = (chain, finding) => window.I18n.t(`chains.finding.${finding.kind}`, {
    chain: chain.label || finding.chain_label || '',
    from: pointLabel(chain, finding.from),
    to: pointLabel(chain, finding.to),
    since: sinceText(finding.since),
  });

  // The fix the learned model proposes, or '' without one.
  const suggestionText = (chain, finding) => {
    const suggestion = finding.suggestion;
    if (!suggestion) return '';
    const point = pointLabel(chain, suggestion.entity_id);
    if (suggestion.invert) return window.I18n.t('chains.suggestion.invert', {point});
    if (suggestion.scale_factor) {
      return window.I18n.t('chains.suggestion.scale', {point, factor: factorText(suggestion.scale_factor)});
    }
    return '';
  };

  // The order of the server: unit, frozen, contradiction, sign, drift, then From and To.
  const KIND_ORDER = ['unit', 'frozen', 'contradiction', 'sign', 'drift'];
  const kindRank = kind => (KIND_ORDER.includes(kind) ? KIND_ORDER.indexOf(kind) : KIND_ORDER.length);
  const compareText = (a, b) => (a < b ? -1 : (a > b ? 1 : 0));

  // Every finding of every chain as {chain, finding}, chains by ID.
  const allFindings = chainStates => idsOf(chainStates).flatMap(id => {
    const chain = chainStates[id] || {};
    return [...(chain.findings || [])]
      .sort((a, b) => kindRank(a.kind) - kindRank(b.kind)
        || compareText(a.from || '', b.from || '')
        || compareText(a.to || '', b.to || ''))
      .map(finding => ({chain, finding}));
  });

  window.EnergyChains = {
    insertPoint, removePoint, setCounting, chainOf, isReference, shortPath, lossText, lossPowerOf, efficiencyPercent,
    percentText, linkBetween, pickerItems, edgeChange, findingText, suggestionText, allFindings,
  };

  // How x-roll writes the frames of the chain values (data-roll-format).
  window.RollFormats = {
    ...window.RollFormats,
    chainPower: value => model().formatPower(value),
    chainLoss: lossPowerText,
    chainEfficiency: percentText,
  };
})();
