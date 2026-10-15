// Gemeinsame Anzeige-Logik der Energiestatistik (Kachel energy_stats und
// Seite "Statistik"). Rechnet nichts nach: Summen, Autarkie, Eigenverbrauch
// und die Nachbarzeitraeume kommen fertig von GET /api/v1/energy/stats.
// Hier stehen nur Beschriftung, Format und die Zuordnung Rolle zu Farbe.
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const PERIODS = ['day', 'week', 'month', 'year'];

  const requestJSON = async url => {
    // Einzige Stelle mit URL-Literal: hinter einem Reverse-Proxy-Unterpfad
    // setzt base.html den Praefix in __DASHBOARD_BASE_PATH__.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };

  function fetchStats(period, at) {
    const params = new URLSearchParams({period});
    if (at) params.set('at', at);
    return requestJSON(`/api/v1/energy/stats?${params}`);
  }

  function formatKWh(kwh) {
    if (kwh == null || !Number.isFinite(kwh)) return '';
    const abs = Math.abs(kwh);
    if (abs >= 1000) return `${window.I18n.formatNumber(kwh / 1000, 2)} MWh`;
    return `${window.I18n.formatNumber(kwh, abs < 10 ? 2 : abs < 100 ? 1 : 0)} kWh`;
  }

  // i18n-keys: energy_stats.kpi.consumption, energy_stats.kpi.pv,
  // energy_stats.kpi.grid_import, energy_stats.kpi.grid_export,
  // energy_stats.kpi.battery_charge, energy_stats.kpi.battery_discharge,
  // energy_stats.kpi.autarky, energy_stats.kpi.self_consumption
  const KPI = {
    consumption: {role: 'load_total'},
    pv: {role: 'pv'},
    grid_import: {role: 'grid_import'},
    grid_export: {role: 'grid_export'},
    battery_charge: {role: 'battery_charge'},
    battery_discharge: {role: 'battery_discharge'},
    autarky: {ratio: 'autarky'},
    self_consumption: {ratio: 'self_consumption'},
  };

  function kpis(totals, ids) {
    const source = totals || {};
    return ids.map(id => {
      const def = KPI[id];
      const label = t(`energy_stats.kpi.${id}`);
      if (def.ratio) {
        const value = source[def.ratio];
        return {id, label, value: value == null ? '' : window.EnergyModel.formatPercent(value)};
      }
      const kwh = source.kwh ? source.kwh[def.role] : undefined;
      return {id, label, value: kwh == null ? '' : formatKWh(kwh)};
    });
  }

  function coverageText(hours) {
    const h = hours || {};
    const parts = [];
    if (h.ha) parts.push(t('energy_stats.coverage.ha', {n: h.ha}));
    if (h.own) parts.push(t('energy_stats.coverage.own', {n: h.own}));
    if (!parts.length) return t('energy_stats.coverage.empty');
    if (h.none) parts.push(t('energy_stats.coverage.none', {n: h.none}));
    const text = parts.join(', ');
    return text.charAt(0).toLocaleUpperCase() + text.slice(1);
  }

  // Die Datenlage als einzelne Teile fuer Pillen: {kind, text}. kind ist
  // ha, own, none oder empty. Ohne jede Stunde bleibt nur "Noch keine Daten".
  function coverageParts(hours) {
    const h = hours || {};
    const parts = [];
    if (h.ha) parts.push({kind: 'ha', text: t('energy_stats.coverage.ha', {n: h.ha})});
    if (h.own) parts.push({kind: 'own', text: t('energy_stats.coverage.own', {n: h.own})});
    if (!parts.length) return [{kind: 'empty', text: t('energy_stats.coverage.empty')}];
    if (h.none) parts.push({kind: 'none', text: t('energy_stats.coverage.none', {n: h.none})});
    return parts.map(part => ({...part, text: part.text.charAt(0).toLocaleUpperCase() + part.text.slice(1)}));
  }

  // local ist "2006-01-02T15:04" in Ortszeit des Pi. Als Date im Browser
  // gebaut, nur zum Beschriften: Stunde und Tag bleiben die des Pi.
  function localDate(local) {
    const [day, time = '00:00'] = String(local).split('T');
    const [y, m = 1, d = 1] = day.split('-').map(Number);
    const [hh, mm] = time.split(':').map(Number);
    return new Date(y, m - 1, d, hh || 0, mm || 0);
  }

  function bucketLabel(period, local) {
    const date = localDate(local);
    switch (period) {
      case 'day': return window.I18n.formatTime(date, {hour: '2-digit', minute: '2-digit'});
      case 'week': return window.I18n.formatDate(date, {weekday: 'short', day: '2-digit'});
      case 'month': return String(date.getDate());
      default: return window.I18n.formatDate(date, {month: 'short'});
    }
  }

  function periodTitle(result) {
    const date = localDate(result.at);
    switch (result.period) {
      case 'day': return window.I18n.formatDate(date, {weekday: 'long', day: 'numeric', month: 'long', year: 'numeric'});
      case 'week': return t('energy_stats.week_title', {date: window.I18n.formatDate(date, {day: '2-digit', month: '2-digit', year: 'numeric'})});
      case 'month': return window.I18n.formatDate(date, {month: 'long', year: 'numeric'});
      default: return String(date.getFullYear());
    }
  }

  // Feste Reihenfolge der Rollen in Tabelle und Diagramm, custom:<id>
  // alphabetisch dahinter. Farben wie in energy-model.js.
  const ROLE_ORDER = ['pv', 'grid_import', 'grid_export', 'battery_charge', 'battery_discharge', 'load_total', 'base', 'wallbox', 'heat_pump'];
  // i18n-keys: energy.role.grid_import, energy.role.grid_export,
  // energy.role.battery_charge, energy.role.battery_discharge,
  // energy.role.base, energy.role.wallbox, energy.role.heat_pump,
  // energy_stats.role.load_total
  const ROLE_META = {
    pv: {key: '', token: 'flow-pv'},
    grid_import: {key: 'energy.role.grid_import', token: 'flow-grid'},
    grid_export: {key: 'energy.role.grid_export', token: 'flow-grid'},
    battery_charge: {key: 'energy.role.battery_charge', token: 'flow-battery'},
    battery_discharge: {key: 'energy.role.battery_discharge', token: 'flow-battery'},
    load_total: {key: 'energy_stats.role.load_total', token: 'flow-load'},
    base: {key: 'energy.role.base', token: 'flow-load'},
    wallbox: {key: 'energy.role.wallbox', token: 'flow-wallbox'},
    heat_pump: {key: 'energy.role.heat_pump', token: 'flow-heatpump'},
  };

  function roleMeta(role, categories) {
    const known = ROLE_META[role];
    if (known) {
      // PV ist eine technische Abkuerzung und wird nicht uebersetzt (E1).
      return {label: known.key ? t(known.key) : 'PV', color: window.DashboardTheme.color(known.token)};
    }
    const id = role.startsWith('custom:') ? role.slice('custom:'.length) : role;
    const def = (categories && categories[id]) || {};
    const token = def.color ? `flow-${def.color.replace('_', '-')}` : 'flow-rest';
    return {label: def.label || id, color: window.DashboardTheme.color(token)};
  }

  const ABOVE = ['pv', 'battery_discharge', 'grid_import'];
  const BELOW = ['load_total', 'battery_charge', 'grid_export'];
  // Gleiche Farbe für beide Richtungen einer Rolle, die Richtung ins Netz und
  // in den Speicher zeigt eine Schraffur.
  const HATCHED_ROLES = ['grid_export', 'battery_charge'];
  const round3 = value => Math.round(value * 1000) / 1000;

  function chartSeries(result) {
    const total = (result.total && result.total.kwh) || {};
    const series = [];
    const add = (role, sign) => {
      if (!(role in total)) return;
      const meta = roleMeta(role, result.categories);
      series.push({
        name: meta.label, color: meta.color, role, hatched: HATCHED_ROLES.includes(role),
        data: result.buckets.map(bucket => (bucket.kwh && role in bucket.kwh ? sign * round3(bucket.kwh[role]) : null)),
      });
    };
    ABOVE.forEach(role => add(role, 1));
    BELOW.forEach(role => add(role, -1));
    return series;
  }

  function tableRows(result) {
    const total = (result.total && result.total.kwh) || {};
    const roles = Object.keys(total);
    const custom = roles.filter(role => !ROLE_ORDER.includes(role)).sort();
    return [...ROLE_ORDER.filter(role => roles.includes(role)), ...custom].map(role => {
      const meta = roleMeta(role, result.categories);
      return {
        role, label: meta.label, color: meta.color, hatched: HATCHED_ROLES.includes(role),
        values: result.buckets.map(bucket => (bucket.kwh && role in bucket.kwh ? formatKWh(bucket.kwh[role]) : '')),
        total: formatKWh(total[role]),
      };
    });
  }

  window.EnergyStatsModel = {
    PERIODS, fetchStats, formatKWh, kpis, coverageText, coverageParts, localDate, bucketLabel, periodTitle, roleMeta, chartSeries, tableRows, HATCHED_ROLES,
  };
})();
