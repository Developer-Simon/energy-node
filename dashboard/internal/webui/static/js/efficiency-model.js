// Pure helpers of the efficiency tab. The server computes every number, the
// live chain state comes from the registry and the 30 day figures from
// GET /api/v1/energy/efficiency. This file only picks the state key, the
// status line, the KPI tiles and the day rows. No DOM and no fetch here.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const EN_DASH = '–';

  // Roles that store energy. The custom categories count with their base.
  const STORAGE_ROLES = Object.freeze(['battery', 'battery_charge', 'battery_discharge']);

  const isStorage = (role, categories) => {
    if (STORAGE_ROLES.includes(role)) return true;
    if (!role || !role.startsWith('custom:')) return false;
    const category = (categories || {})[role.slice('custom:'.length)];
    return Boolean(category) && category.base === 'storage';
  };

  // The sign of the flow means charging only for a storage role whose
  // direction keeps the sign. A fixed direction hides it, so the chain stays active.
  const signIsMeaningful = direction => !String(direction || '').startsWith('fixed_');

  // Which of the five states the chain shows: no_values, idle, charging,
  // discharging or active.
  const stateKey = (state, categories) => {
    if (state.no_current_values) return 'no_values';
    if (state.flow === 'idle') return 'idle';
    if (isStorage(state.role, categories) && signIsMeaningful(state.direction)) {
      if (state.flow === 'down') return 'charging';
      if (state.flow === 'up') return 'discharging';
    }
    return 'active';
  };

  // The status line next to the chain title. A substitute names the point that
  // delivers the value, by its label.
  const statusOf = state => {
    if (state.no_current_values) {
      return { tone: 'warn', key: 'efficiency.status.no_values', params: {} };
    }
    if (state.substitute_from) {
      const point = (state.points || []).find(item => item.entity_id === state.substitute_from);
      const name = point && point.label ? point.label : state.substitute_from;
      return { tone: 'warn', key: 'efficiency.status.substitute', params: { name } };
    }
    return { tone: 'ok', key: 'efficiency.status.current', params: {} };
  };

  // The efficiency of the whole chain in percent, rounded to two decimals.
  // Null when a segment is not available or has no efficiency, or when there are no segments.
  const overallPercent = state => {
    const segments = (state && state.segments) || [];
    if (!segments.length) return null;
    let product = 1;
    for (const segment of segments) {
      const efficiency = segment.available ? segment.efficiency : null;
      if (typeof efficiency !== 'number' || !Number.isFinite(efficiency)) return null;
      product *= efficiency;
    }
    return Math.round(product * 10000) / 100;
  };

  // The four KPI tiles in fixed order: live efficiency, round trip, standby
  // and the loss of the month. Without a value a tile shows an en dash.
  const kpiTiles = (state, stats, monthLabel) => {
    const source = stats || {};
    const now = overallPercent(state);
    const roundTrip = source.round_trip_30d;
    const standby = source.standby_w_30d;
    const lossKWh = source.loss_month_kwh;
    return [
      {
        id: 'now',
        label: t('efficiency.kpi.now'),
        value: now === null ? EN_DASH : window.EnergyChains.percentText(now),
      },
      {
        id: 'round_trip',
        label: t('efficiency.kpi.round_trip'),
        value: roundTrip == null ? EN_DASH : window.EnergyModel.formatPercent(roundTrip),
      },
      {
        id: 'standby',
        label: t('efficiency.kpi.standby'),
        value: standby == null ? EN_DASH : window.EnergyModel.formatPower(standby),
      },
      {
        id: 'loss_month',
        label: t('efficiency.kpi.loss_month', { month: monthLabel }),
        value: lossKWh == null ? EN_DASH : window.EnergyStatsModel.formatKWh(lossKWh),
      },
    ];
  };

  // One row per day, oldest first. The width is the loss relative to the
  // largest loss of the week, so the largest row is 100. Rows without data
  // have no loss and width 0. Nothing divides by zero when no day has a loss.
  const dayRows = days => {
    const list = days || [];
    const largest = Math.max(0, ...list.map(day => (day.has_data ? day.loss_kwh : 0)));
    return list.map(day => {
      const hasLoss = Boolean(day.has_data);
      const width = hasLoss && largest > 0 ? Math.max(0, (day.loss_kwh / largest) * 100) : 0;
      const efficiency = typeof day.efficiency === 'number' && Number.isFinite(day.efficiency) ? day.efficiency * 100 : null;
      return {
        local: day.local,
        label: window.EnergyStatsModel.bucketLabel('week', day.local),
        lossText: hasLoss ? window.EnergyStatsModel.formatKWh(day.loss_kwh) : EN_DASH,
        efficiencyText: hasLoss && efficiency !== null ? window.EnergyChains.percentText(efficiency) : EN_DASH,
        width,
      };
    });
  };

  // i18n-keys: efficiency.state.charging, efficiency.state.discharging,
  // efficiency.state.active, efficiency.state.idle, efficiency.state.no_values

  window.EfficiencyModel = {
    stateKey, statusOf, overallPercent, kpiTiles, dayRows,
  };
})();
