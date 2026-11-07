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

  // The status line next to the chain title, in this order: no current values,
  // findings (the text of the first one, or the count), a substitute, still
  // learning, and otherwise plausible. The text is already translated.
  const statusOf = state => {
    const findings = state.findings || [];
    const status = (tone, key, params = {}, text = t(key, params)) => ({ tone, key, params, text });
    if (state.no_current_values) {
      return status('warn', 'efficiency.status.no_values');
    }
    if (findings.length === 1) {
      const [finding] = findings;
      return status('warn', `chains.finding.${finding.kind}`, {}, window.EnergyChains.findingText(state, finding));
    }
    if (findings.length > 1) {
      return status('warn', 'efficiency.status.findings', { n: findings.length });
    }
    if (state.substitute_from) {
      const point = (state.points || []).find(item => item.entity_id === state.substitute_from);
      const name = point && point.label ? point.label : state.substitute_from;
      return status('warn', 'efficiency.status.substitute', { name });
    }
    if (state.learning) {
      return status('muted', 'efficiency.status.learning');
    }
    return status('ok', 'efficiency.status.plausible');
  };

  // Storage roles keep their sign only when the direction is not fixed. The
  // custom categories count with their base, as in stateKey.
  const isStorageState = (state, categories) => isStorage(state.role, categories) && signIsMeaningful(state.direction);

  const round1 = value => Math.round(value * 10) / 10;

  // The points of one curve direction as [W, percent], sorted by power.
  const curvePoints = list => (list || [])
    .filter(point => Number.isFinite(point.power_w) && Number.isFinite(point.efficiency))
    .map(point => [point.power_w, round1(point.efficiency * 100)])
    .sort((a, b) => a[0] - b[0]);

  // The series of the efficiency curve. A storage with a free sign names the
  // directions: down is charging, up is discharging, and a reversed chain swaps
  // them. Any other chain shows each direction that has points as plain efficiency.
  // Discharging is dashed. Without any points the list is empty.
  const curveSeries = (curve, state, categories) => {
    const source = curve || {};
    const down = curvePoints(source.down);
    const up = curvePoints(source.up);
    const series = [];
    if (isStorageState(state, categories)) {
      const reversed = state.direction === 'reversed';
      const charge = reversed ? up : down;
      const discharge = reversed ? down : up;
      if (charge.length) series.push({ key: 'charge', name: t('efficiency.curve.charge'), dash: 0, data: charge });
      if (discharge.length) series.push({ key: 'discharge', name: t('efficiency.curve.discharge'), dash: 5, data: discharge });
      return series;
    }
    if (down.length) series.push({ key: 'down', name: t('efficiency.curve.series'), dash: 0, data: down });
    if (up.length) series.push({ key: 'up', name: t('efficiency.curve.series'), dash: 0, data: up });
    return series;
  };

  // A CSS custom property of the page, '' without a document or a computed style.
  const cssVar = name => {
    try {
      if (typeof document === 'undefined' || !document.documentElement || typeof getComputedStyle !== 'function') return '';
      return getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim();
    } catch (error) {
      return '';
    }
  };

  const formatInt = value => window.I18n.formatNumber(Math.round(value), 0);

  // The current theme of the page as ApexCharts names it, the rule of the stats chart.
  const themeMode = () => {
    if (typeof document === 'undefined' || !document.documentElement) return 'dark';
    const attr = (document.documentElement.dataset.theme || '').toLowerCase();
    if (attr === 'light' || attr === 'dark') return attr;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  };

  const hasBest = best => Boolean(best) && Number.isFinite(best.from_w) && Number.isFinite(best.to_w);

  // The x range the curve covers: its points and the best band. Null without any.
  const plotRange = (series, best) => {
    const xs = [];
    for (const item of series || []) {
      for (const point of item.data) xs.push(point[0]);
    }
    if (hasBest(best)) xs.push(best.from_w, best.to_w);
    return xs.length ? { min: Math.min(...xs), max: Math.max(...xs) } : null;
  };

  // The live power of the chain in W, the absolute value of its counting point.
  // Null when the chain is idle, has no current values or the point has no value.
  const livePowerOf = state => {
    if (!state || state.no_current_values || state.flow === 'idle') return null;
    const points = state.points || [];
    const point = points.find(item => item.entity_id === state.counting) || points.find(item => item.counting);
    if (!point || !point.has_value) return null;
    const value = Math.abs(Number(point.value));
    return Number.isFinite(value) ? value : null;
  };

  // The operating line sits at the live power when that lies at or right of the plotted curve.
  const operatingX = (power, range) => (Number.isFinite(power) && range && power >= range.min ? power : null);

  // The x annotations of the curve and the x axis max. The best band has no label text,
  // it is explained by the HTML legend. The max covers the operating line.
  const curveMarks = (series, best, power) => {
    const range = plotRange(series, best);
    const x = operatingX(power, range);
    const muted = cssVar('text-muted');
    const xaxis = [];
    if (hasBest(best)) {
      xaxis.push({
        x: best.from_w,
        x2: best.to_w,
        fillColor: cssVar('ok'),
        opacity: 0.18,
        label: { text: '', borderWidth: 0, style: { background: 'transparent' } },
      });
    }
    if (x !== null) {
      xaxis.push({
        x,
        borderColor: muted,
        strokeDashArray: 4,
        label: { text: '', borderWidth: 0, style: { background: 'transparent' } },
      });
    }
    if (!range) return { xaxis, max: null };
    return { xaxis, max: x !== null ? Math.max(range.max, x) : range.max };
  };

  // The entries of the HTML legend under the curve. Only what the chart draws:
  // the best band and the operating line.
  const curveLegend = (series, best, power) => {
    const items = [];
    if (hasBest(best)) {
      items.push({ id: 'best', text: t('efficiency.curve.best', { from: formatInt(best.from_w), to: formatInt(best.to_w) }) });
    }
    const x = operatingX(power, plotRange(series, best));
    if (x !== null) items.push({ id: 'now', text: t('efficiency.curve.now', { power: formatInt(x) }) });
    return items;
  };

  // ApexCharts options for the curve, following EnergyStatsModel.chartOptions:
  // colours from the theme tokens, the theme mode of the page, no animation.
  // The legend only shows with two series.
  const curveOptions = (series, best, width = 0, power = null) => {
    const list = series || [];
    const battery = cssVar('flow-battery');
    const muted = cssVar('text-muted');
    const labelStyle = { colors: Array(12).fill(muted), fontSize: '12px' };
    const marks = curveMarks(list, best, power);
    const ticks = width > 0 ? Math.max(2, Math.min(8, Math.floor(width / 90))) : undefined;
    return {
      chart: { type: 'line', height: 260, toolbar: { show: false }, animations: { enabled: false }, background: 'transparent', fontFamily: 'inherit', zoom: { enabled: false } },
      theme: { mode: themeMode() },
      series: list.map(item => ({ name: item.name, data: item.data })),
      colors: list.map(item => (item.dash ? muted : battery)),
      stroke: { width: 2, curve: 'straight', dashArray: list.map(item => item.dash) },
      markers: { size: 3 },
      dataLabels: { enabled: false },
      legend: { show: list.length > 1, position: 'bottom', fontSize: '12px', labels: { colors: muted } },
      xaxis: { type: 'numeric', max: marks.max === null ? undefined : marks.max, tickAmount: ticks, labels: { style: labelStyle, formatter: value => formatInt(value) }, title: { text: 'W', style: { color: muted, fontWeight: 400 } } },
      yaxis: { forceNiceScale: true, labels: { style: labelStyle, formatter: value => formatInt(value) }, title: { text: '%', style: { color: muted, fontWeight: 400 } } },
      annotations: { xaxis: marks.xaxis },
      grid: { borderColor: cssVar('border-soft'), strokeDashArray: 3 },
      tooltip: {
        x: { formatter: value => `${formatInt(value)} W` },
        y: { formatter: value => `${window.I18n.formatNumber(value, 1)} %` },
      },
      noData: { text: t('efficiency.curve.learning') },
    };
  };

  // One row per segment of the chain, in the order of the points: a is the
  // upstream point, b the next one, whatever the current flow. A row names the
  // normal band, or why there is none, and the tolerance of the segment, if any.
  const segmentRows = state => {
    const points = state.points || [];
    const segments = state.segments || [];
    const tolerances = state.tolerances || [];
    const labelOf = id => {
      const point = points.find(item => item.entity_id === id);
      return point && point.label ? point.label : id;
    };
    const pairOf = (item, a, b) => (item.from === a && item.to === b) || (item.from === b && item.to === a);
    const rows = [];
    for (let index = 0; index + 1 < points.length; index += 1) {
      const a = points[index].entity_id;
      const b = points[index + 1].entity_id;
      const segment = segments.find(item => pairOf(item, a, b));
      const normal = segment && segment.normal ? segment.normal : null;
      const tolerance = tolerances.find(item => (item.a === a && item.b === b) || (item.a === b && item.b === a)) || null;
      let normalText;
      if (normal) {
        normalText = t('efficiency.segment.normal', { low: formatInt(normal.low_w), high: formatInt(normal.high_w) });
      } else if (!segment || !segment.available || state.no_current_values || state.flow === 'idle') {
        normalText = t('efficiency.segment.no_compare');
      } else {
        normalText = t('efficiency.segment.learning');
      }
      rows.push({
        a,
        b,
        fromLabel: labelOf(a),
        toLabel: labelOf(b),
        normalText,
        tolerance,
        learned: Boolean(normal),
      });
    }
    return rows;
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
  // efficiency.state.active, efficiency.state.idle, efficiency.state.no_values,
  // efficiency.status.plausible, efficiency.status.learning, efficiency.status.findings,
  // efficiency.curve.charge, efficiency.curve.discharge, efficiency.curve.series,
  // efficiency.curve.best, efficiency.curve.learning, efficiency.curve.now,
  // efficiency.segment.normal, efficiency.segment.learning, efficiency.segment.no_compare

  window.EfficiencyModel = {
    stateKey, statusOf, overallPercent, kpiTiles, dayRows, curveSeries, curveOptions, curveMarks, curveLegend, livePowerOf, segmentRows,
  };
})();
