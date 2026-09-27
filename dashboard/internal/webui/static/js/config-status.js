(() => {
  'use strict'; // i18n-ignore

  // Status after saving. A service reports the result of its last load
  // attempt retained on settings/status, together with the SHA-256 of the
  // file it read (config_revision). The dashboard computes the same checksum
  // of the file it writes, so a page only trusts a status that carries its
  // own checksum. A retained status from before the save stays "pending".

  // i18n-keys: config.status.pending, config.status.applied, config.status.rejected, config.status.no_response
  const LABELS = {
    pending: 'config.status.pending',
    applied: 'config.status.applied',
    rejected: 'config.status.rejected',
    no_response: 'config.status.no_response',
  };

  // error_code -> i18n key. Services fill in their codes (see battery_soc).
  // i18n-keys: config.validation.bank_a_voltage_required, config.validation.bank_b_voltage_required, config.validation.charge_source_required, config.validation.discharge_source_required, config.validation.current_only_on_dc, config.validation.ac_source_in_dc_system
  const ERROR_TEXTS = {
    // battery_soc (battery_soc_core.sources.validate_sources)
    bank_a_voltage_required: 'config.validation.bank_a_voltage_required',
    bank_b_voltage_required: 'config.validation.bank_b_voltage_required',
    charge_source_required: 'config.validation.charge_source_required',
    discharge_source_required: 'config.validation.discharge_source_required',
    current_only_on_dc: 'config.validation.current_only_on_dc',
    ac_source_in_dc_system: 'config.validation.ac_source_in_dc_system',
  };

  function t(key, params) {
    return window.I18n ? window.I18n.t(key, params) : key;
  }

  function classify(status, revision) {
    if (!status || !status.received || !revision || status.config_revision !== revision) return 'pending';
    if (status.runtime_status === 'rejected') return 'rejected';
    if (status.runtime_status === 'pending') return 'pending';
    return 'applied';
  }

  const label = state => LABELS[state] ? t(LABELS[state]) : '';

  function errorText(status) {
    if (!status) return '';
    return ERROR_TEXTS[status.error_code] ? t(ERROR_TEXTS[status.error_code]) : (status.error || '');
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
