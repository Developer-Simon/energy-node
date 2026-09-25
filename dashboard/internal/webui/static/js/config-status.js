(() => {
  'use strict';

  // Status after saving. A service reports the result of its last load
  // attempt retained on settings/status, together with the SHA-256 of the
  // file it read (config_revision). The dashboard computes the same checksum
  // of the file it writes, so a page only trusts a status that carries its
  // own checksum. A retained status from before the save stays "pending".

  const LABELS = {
    pending: 'Ausstehend',
    applied: 'Übernommen',
    rejected: 'Abgelehnt',
    no_response: 'Dienst antwortet nicht',
  };

  // error_code -> German text. Services fill in their codes (see battery_soc).
  const ERROR_TEXTS = {
    // battery_soc (battery_soc_core.sources.validate_sources)
    bank_a_voltage_required: 'Die Spannung von Bank A fehlt.',
    bank_b_voltage_required: 'Zwei Bänke in Reihe brauchen auch die Spannung von Bank B.',
    charge_source_required: 'Es fehlt eine Quelle für das Laden, AC oder DC.',
    discharge_source_required: 'Es fehlt eine Quelle für das Entladen, AC oder DC.',
    current_only_on_dc: 'Strom (A) ist nur auf den DC-Eingängen möglich.',
    ac_source_in_dc_system: 'Eine reine DC-Anlage darf keine AC-Eingänge haben.',
  };

  function classify(status, revision) {
    if (!status || !status.received || !revision || status.config_revision !== revision) return 'pending';
    if (status.runtime_status === 'rejected') return 'rejected';
    if (status.runtime_status === 'pending') return 'pending';
    return 'applied';
  }

  const label = state => LABELS[state] || '';

  function errorText(status) {
    if (!status) return '';
    return ERROR_TEXTS[status.error_code] || status.error || '';
  }

  const defaultSleep = ms => new Promise(resolve => setTimeout(resolve, ms));

  async function watch({ fetchStatus, revision, timeoutMs = 15000, intervalMs = 1000,
    now = () => Date.now(), sleep = defaultSleep }) {
    const deadline = now() + timeoutMs;
    let status = null;
    for (;;) {
      try {
        status = await fetchStatus();
      } catch (error) {
        // a transient fetch error must not end the wait early
      }
      const state = classify(status, revision);
      if (state !== 'pending') return { state, status };
      if (now() >= deadline) return { state: 'no_response', status };
      await sleep(intervalMs);
    }
  }

  window.ConfigStatus = { classify, watch, label, errorText, ERROR_TEXTS };
})();
