// Pure model for the measurement chain suggestions on the energy page: the
// text of a suggestion, the dialog that saves it and the change it becomes.
// No DOM and no Alpine here, the energy page renders and saves. A suggestion
// has the shape of the server: {a, b, device_a, device_b, r, days, minutes,
// mean_a_w, mean_b_w, edge, chain_id}. A is the upstream point (higher mean).
// The functions return new objects, the inputs stay untouched. modalView turns
// a dialog into the view the modal renders: title, evidence, chain nodes and the pair.
(() => {
  // i18n-keys: energy.chains.suggest.measures, energy.chains.suggest.extends, energy.chains.suggest.evidence.one, energy.chains.suggest.evidence.other, energy.chains.suggest.days.one, energy.chains.suggest.days.other
  const percentText = r => `${Math.round(r * 100)} %`;

  // "Plug Balkon misst vermutlich Speicher AC", or for a point that joins a
  // chain "Plug Balkon gehört vermutlich zur Messkette Balkon".
  const suggestionText = (suggestion, nameOf, chains) => {
    const name = id => nameOf(id) || id;
    const chain = suggestion.chain_id && chains ? chains[suggestion.chain_id] : null;
    if (chain) {
      const free = chain.points.includes(suggestion.a) ? suggestion.b : suggestion.a;
      return window.I18n.t('energy.chains.suggest.extends', {point: name(free), chain: chain.label || suggestion.chain_id});
    }
    return window.I18n.t('energy.chains.suggest.measures', {upstream: name(suggestion.a), downstream: name(suggestion.b)});
  };

  // "98 % Übereinstimmung an 5 Tagen"
  const evidenceText = suggestion => window.I18n.tn('energy.chains.suggest.evidence', suggestion.days, {
    percent: percentText(suggestion.r),
  });

  // The dialog starts with A upstream. The label is the device name of B, a
  // chain extension has no label, the chain keeps its own.
  const dialogFor = (suggestion, nameOf) => ({
    upstream: suggestion.a,
    downstream: suggestion.b,
    label: suggestion.chain_id ? '' : (nameOf(suggestion.device_b) || ''),
    chainId: suggestion.chain_id || '',
    suggestion,
  });

  const swap = dialog => ({...dialog, upstream: dialog.downstream, downstream: dialog.upstream});

  // The save the dialog order asks for, as EnergyChains.edgeChange decides it.
  const changeFor = (dialog, chains) => window.EnergyChains.edgeChange({
    chains,
    upstream: dialog.upstream,
    downstream: dialog.downstream,
    label: dialog.label,
  });

  // The modal view of a dialog. Without a chain the two points stand in the
  // dialog order. With a chain they stand in the order the save leads to, the
  // new point is marked, and a conflict shows the chain unchanged.
  const modalView = (dialog, host) => {
    const {suggestion} = dialog;
    const chain = dialog.chainId ? host.chains[dialog.chainId] : null;
    const extend = !!chain;
    const change = changeFor(dialog, host.chains);
    const fits = change.action !== 'conflict';
    const pair = [dialog.upstream, dialog.downstream];
    let order = pair;
    if (chain) order = change.action === 'update' ? change.body.points : chain.points;

    const nodes = order.map((id, index) => {
      const isNew = extend && !chain.points.includes(id);
      let eyebrow = '';
      if (!extend) eyebrow = window.I18n.t(index === 0 ? 'energy.chains.suggest.first' : 'energy.chains.suggest.second');
      else if (isNew) eyebrow = window.I18n.t('energy.chains.suggest.new');
      let valueText = '';
      if (id === suggestion.a) valueText = window.I18n.t('energy.chains.suggest.mean', {value: window.DeviceMapModel.formatPower(suggestion.mean_a_w)});
      else if (id === suggestion.b) valueText = window.I18n.t('energy.chains.suggest.mean', {value: window.DeviceMapModel.formatPower(suggestion.mean_b_w)});
      else if (chain) valueText = host.chainPointValue(dialog.chainId, id);
      return {
        id,
        index,
        name: host.chainSuggestName(id) || id,
        eyebrow,
        valueText,
        isNew,
        inPair: id === dialog.upstream || id === dialog.downstream,
        iconMarkup: host.chainSuggestIcon(id) || '',
        last: index === order.length - 1,
      };
    });

    let pairLink = -1;
    for (let i = 0; i < order.length - 1; i += 1) {
      if (pair.includes(order[i]) && pair.includes(order[i + 1])) {
        pairLink = i;
        break;
      }
    }

    return {
      extend,
      fits,
      title: window.I18n.t(extend ? 'energy.chains.suggest.dialog_title_extend' : 'energy.chains.suggest.dialog_title'),
      chainLabel: chain ? (chain.label || dialog.chainId) : '',
      nodes,
      pairLink,
      percent: Number.isFinite(suggestion.r) ? percentText(suggestion.r) : '',
      daysText: window.I18n.tn('energy.chains.suggest.days', suggestion.days),
      edge: !!suggestion.edge,
    };
  };

  window.EnergyChainSuggest = {suggestionText, evidenceText, dialogFor, swap, changeFor, modalView};
})();
