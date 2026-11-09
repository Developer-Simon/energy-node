// Pure model for the measurement chain suggestions on the energy page: the
// text of a suggestion, the dialog that saves it and the change it becomes.
// No DOM and no Alpine here, the energy page renders and saves. A suggestion
// has the shape of the server: {a, b, device_a, device_b, r, days, minutes,
// mean_a_w, mean_b_w, edge, chain_id}. A is the upstream point (higher mean).
// The functions return new objects, the inputs stay untouched.
(() => {
  // i18n-keys: energy.chains.suggest.measures, energy.chains.suggest.extends, energy.chains.suggest.evidence.one, energy.chains.suggest.evidence.other
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
  });

  const swap = dialog => ({...dialog, upstream: dialog.downstream, downstream: dialog.upstream});

  // The save the dialog order asks for, as EnergyChains.edgeChange decides it.
  const changeFor = (dialog, chains) => window.EnergyChains.edgeChange({
    chains,
    upstream: dialog.upstream,
    downstream: dialog.downstream,
    label: dialog.label,
  });

  window.EnergyChainSuggest = {suggestionText, evidenceText, dialogFor, swap, changeFor};
})();
