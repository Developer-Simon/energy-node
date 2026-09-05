// "Tagesband": time is the main axis instead of topology - stacked areas
// over time, supply mirrored above the centerline, demand mirrored below
// (or either side alone, full-height, via the display_mode option). Port of
// Vorschlag D from the six-proposals exploration, with one intentional
// substitution: instead of a new backend history endpoint (the plan doc's
// original proposal), this reads the browser's own IndexedDB history via
// window.dashboardHistorizer - the same "role:<role>" samples
// history-recorder.js now also records for energy-board.js's sparklines.
// That means no fixed hour window either: the x-axis spans whatever
// history actually exists (up to the historizer's 6h TTL), so right after
// a restart or on a new device the plotted area is narrow - a known,
// accepted consequence of not adding backend aggregation, and why the
// prototype's "Zeitfenster" selector has no layout-editor equivalent (see
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md). The other
// two options (display_mode, show_now) are read from the layout item's
// data-* attributes.
(() => {
  const DEFAULT_WIDTH = 980;
  const H = 360;
  const PAD_L = 58;
  const PAD_R = 18;
  const PAD_T = 22;
  const PAD_B = 30;
  const TOP = PAD_T;
  const BOTTOM = H - PAD_B;
  const CY = (TOP + BOTTOM) / 2;
  const HALF_H = (BOTTOM - TOP) / 2;

  // Module-level (not per-instance) on purpose: dashboard.js's
  // refreshLiveFragment() swaps this whole card's DOM via htmx
  // outerHTML on every "registry" SSE tick (a few seconds apart),
  // destroying and re-creating this Alpine component each time. Without
  // a seed, the freshly mounted instance sits in the loading state -
  // and the <svg>'s static viewBox="0 0 980 360" default - until the
  // IndexedDB read in init() resolves, which flashes the placeholder
  // and visibly resizes the tile (real width's viewBox has a different
  // aspect ratio than the 980-wide default) on every refresh. Caching
  // the last successfully read samples lets a new instance render the
  // previous chart synchronously before its own read completes.
  let cachedSamples = null;

  function colorsFor(model) {
    return {
      pv: model.COLORS.pv,
      battery_discharge: model.COLORS.batteryDischarge,
      grid_import: model.COLORS.gridImport,
      base: model.COLORS.base,
      load_measured: model.COLORS.loadMeasured,
      wallbox: model.COLORS.wallbox,
      heat_pump: model.COLORS.heatPump,
      grid_export: model.COLORS.gridExport,
      battery_charge: model.COLORS.batteryCharge,
      rest: model.COLORS.rest,
    };
  }

  const SUPPLY_KEYS = ['pv', 'battery_discharge', 'grid_import', 'rest'];
  // load_measured steht direkt hinter base: beide zusammen sind der
  // Hausverbrauch ohne Wallbox und Waermepumpe. Immer die Sammelposition -
  // der Verlaufsspeicher kennt nur Rollensummen, keine Entitaetswerte, eine
  // Aufteilung je Entitaet ist hier also gar nicht rekonstruierbar.
  const DEMAND_KEYS = ['base', 'load_measured', 'wallbox', 'heat_pump', 'grid_export', 'battery_charge', 'rest'];
  const LABELS = {
    pv: 'PV', battery_discharge: 'Batterie entlädt', grid_import: 'Netzbezug', rest: 'Nicht zugeordnet',
    base: 'Übriger Verbrauch', load_measured: 'Gemessene Verbraucher', wallbox: 'Wallbox', heat_pump: 'Wärmepumpe',
    grid_export: 'Einspeisung', battery_charge: 'Batterie lädt',
  };

  // Runs each historical point through the same deriveBalance() the
  // snapshot-only cards use, so the stacked area chart closes its balance
  // at every instant exactly like the current-moment cards do. interpretation
  // comes from the live snapshot embedded alongside this card (energy-day-initial)
  // so history points use the same balance settings the user configured, not
  // silently reverting to defaults.
  function pointSeries(points, interpretation) {
    const model = window.EnergyModel;
    return points.map(point => {
      const snapshot = model.snapshotFromPoint(point);
      const balance = model.deriveBalance(snapshot, interpretation);
      // Mirrors composeBalance()'s gap handling (energy-model.js): outside
      // diagnostic mode, a positive gap is already folded into balance.base
      // by deriveBalanceCore(), so it must not also become a "rest" entry
      // here - only diagnostic mode's unresolved gap (balance.unbalanced)
      // does. A negative gap is never folded in, so it always draws as
      // "rest" supply (gapAbsorbed mirrors gap outside diagnostic mode too).
      const restSupply = balance.unbalanced
        ? (balance.gap < -0.5 ? -balance.gap : 0)
        : (balance.gapAbsorbed < -0.5 ? -balance.gapAbsorbed : 0);
      const restDemand = balance.unbalanced && balance.gap > 0.5 ? balance.gap : 0;
      return {
        timestamp: point.timestamp,
        total: balance.total,
        supply: {
          pv: model.roleValue(snapshot, 'pv'), battery_discharge: balance.discharge, grid_import: balance.gridImport,
          rest: restSupply,
        },
        demand: {
          base: balance.base, load_measured: balance.measured, wallbox: balance.wallbox, heat_pump: balance.heatPump,
          grid_export: balance.gridExport, battery_charge: balance.charge, rest: restDemand,
        },
      };
    });
  }

  function xPositions(series, width) {
    const plotW = width - PAD_L - PAD_R;
    const n = series.length;
    if (n < 2) return series.map(() => PAD_L + plotW / 2);
    const start = Date.parse(series[0].timestamp);
    const end = Date.parse(series[n - 1].timestamp);
    return series.map(point => (start === end ? PAD_L + plotW / 2 : PAD_L + ((Date.parse(point.timestamp) - start) / (end - start)) * plotW));
  }

  // One stacked-area path per key present in the series (a key absent from
  // every point, e.g. no wallbox ever recorded, is skipped entirely rather
  // than drawing a flat zero band). baseline/sign let the caller grow the
  // stack up or down from any y, not just the mirrored centerline.
  function stackAreas(series, xs, side, keys, sign, k, baseline) {
    const model = window.EnergyModel;
    const colors = colorsFor(model);
    const cumulative = new Array(series.length).fill(0);
    const areas = [];
    for (const id of keys) {
      const values = series.map(point => point[side][id] || 0);
      if (!values.some(v => v > 0.5)) continue;
      const lower = cumulative.map(c => baseline + sign * c * k);
      const upper = cumulative.map((c, i) => baseline + sign * (c + values[i]) * k);
      let d = `M ${xs[0].toFixed(1)} ${upper[0].toFixed(1)}`;
      for (let i = 1; i < xs.length; i++) d += ` L ${xs[i].toFixed(1)} ${upper[i].toFixed(1)}`;
      for (let i = xs.length - 1; i >= 0; i--) d += ` L ${xs[i].toFixed(1)} ${lower[i].toFixed(1)}`;
      areas.push({id, label: LABELS[id], color: colors[id], path: `${d} Z`, opacity: id === 'rest' ? 0.3 : 0.72});
      values.forEach((v, i) => { cumulative[i] += v; });
    }
    return areas;
  }

  // Horizontal kW gridlines. Mirrored mode draws them above and below the
  // centerline; supply/demand-only modes only draw the single direction the
  // stack actually grows in (sign -1, i.e. upward from baseline).
  function gridLines(maxTotal, k, mirrored, baseline) {
    const stepW = maxTotal / 4;
    const lines = [];
    for (let i = 0; i <= 4; i++) {
      const v = i * stepW;
      const signs = mirrored ? [-1, 1] : [-1];
      for (const sign of signs) {
        if (sign === 1 && i === 0) continue;
        const y = baseline + sign * v * k;
        if (y < TOP - 1 || y > BOTTOM + 1) continue;
        lines.push({y, label: (v / 1000).toFixed(1).replace('.', ','), strong: i === 0});
      }
    }
    return lines;
  }

  // <template x-for> cloning doesn't work for elements nested inside <svg>
  // (browsers parse a <template> encountered in foreign/SVG content as a
  // plain SVG element with no .content DocumentFragment, so Alpine's
  // document.importNode(el.content, ...) throws) - gridLinesMarkup() and
  // areasMarkup() instead render the repeated elements to an SVG markup
  // string the template binds once via x-html on a plain <g>.
  function gridLinesMarkup(lines, width) {
    const theme = window.DashboardTheme.colors({strong: 'border', soft: 'border-soft', label: 'text-muted'});
    return lines.map(line => `<g><line x1="${PAD_L}" y1="${line.y}" x2="${width - PAD_R}" y2="${line.y}" stroke="${line.strong ? theme.strong : theme.soft}" stroke-width="${line.strong ? 1.2 : 1}"></line><text x="50" y="${line.y + 4}" text-anchor="end" font-size="12" fill="${theme.label}">${line.label}</text></g>`).join('');
  }

  function areasMarkup(areas) {
    return areas.map(area => `<path d="${area.path}" fill="${area.color}" opacity="${area.opacity}"></path>`).join('');
  }

  // options: {displayMode: 'mirror'|'supply'|'demand', showNow: 'on'|'off'}.
  function dayGeometry(samples, interpretation, width, options) {
    const {displayMode, showNow} = options || {};
    const mirrored = displayMode !== 'supply' && displayMode !== 'demand';
    const model = window.EnergyModel;
    const points = window.EnergyModel.groupRoleSamples(samples);
    if (points.length < 2) return null;
    const series = pointSeries(points, interpretation);
    const maxTotal = Math.max(...series.map(p => p.total), 1);
    const xs = xPositions(series, width);
    // Mirrored: the stack has half the plot height to grow into on each side
    // of the centerline. Single-direction modes get the full height, growing
    // upward from the bottom edge instead.
    const halfH = mirrored ? HALF_H : (BOTTOM - TOP);
    const k = halfH / maxTotal;
    const baseline = mirrored ? CY : BOTTOM;
    const last = series[series.length - 1];
    const nowX = xs[xs.length - 1];
    const lines = gridLines(maxTotal, k, mirrored, baseline);

    let supplyAreas = [], demandAreas = [], nowSupplyLabel = '', nowDemandLabel = '', nowSupplyY = 0, nowDemandY = 0;
    if (displayMode === 'supply') {
      supplyAreas = stackAreas(series, xs, 'supply', SUPPLY_KEYS, -1, k, baseline);
      if (showNow !== 'off') { nowSupplyLabel = `Deckung ${model.formatPower(last.total)}`; nowSupplyY = baseline - last.total * k - 8; }
    } else if (displayMode === 'demand') {
      demandAreas = stackAreas(series, xs, 'demand', DEMAND_KEYS, -1, k, baseline);
      if (showNow !== 'off') { nowDemandLabel = `Verwendung ${model.formatPower(last.total)}`; nowDemandY = baseline - last.total * k - 8; }
    } else {
      supplyAreas = stackAreas(series, xs, 'supply', SUPPLY_KEYS, -1, k, baseline);
      demandAreas = stackAreas(series, xs, 'demand', DEMAND_KEYS, 1, k, baseline);
      if (showNow !== 'off') {
        nowSupplyLabel = `Deckung ${model.formatPower(last.total)}`;
        nowSupplyY = baseline - last.total * k - 8;
        nowDemandLabel = `Verwendung ${model.formatPower(last.total)}`;
        nowDemandY = baseline + last.total * k + 16;
      }
    }

    const seen = new Set();
    const legend = [...supplyAreas, ...demandAreas].filter(area => (seen.has(area.label) ? false : seen.add(area.label)));

    return {
      supplyAreas, demandAreas, nowX, top: TOP, bottom: BOTTOM,
      gridLines: lines,
      gridLinesMarkup: gridLinesMarkup(lines, width),
      supplyAreasMarkup: areasMarkup(supplyAreas),
      demandAreasMarkup: areasMarkup(demandAreas),
      startLabel: new Date(series[0].timestamp).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}),
      endLabel: new Date(last.timestamp).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}),
      nowSupplyLabel, nowSupplyY, nowDemandLabel, nowDemandY,
      legend,
      description: `Verlauf über ${series.length} Messpunkte, Maximum ${model.formatPower(maxTotal)}, aktueller Wert ${model.formatPower(last.total)}.`,
    };
  }

  const energyDayCard = () => {
    // Kept outside the reactive Alpine object on purpose, same reasoning as
    // energy-flow.js's resizeObserver handle: a plain platform handle, not
    // view state.
    let resizeObserver = null;
    let lastWidth = null;

    return {
      loading: true,
      geometry: null,
      liveSnapshot: null,
      width: DEFAULT_WIDTH,
      samples: [],
      options: {displayMode: 'mirror', showNow: 'on'},
      presenter: null,

      async init() {
        this.loading = true;
        const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
        this.options = {
          displayMode: data.displayMode || 'mirror',
          showNow: data.showNow || 'on',
        };
        const raw = window.EnergyModel.readEmbeddedSnapshot('energy-day-initial');
        const reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
        this.presenter = window.EnergyPresentation.present({
          key: data.layoutItemId || 'energy-day',
          raw,
          reducedMotion,
          alive: () => !!(this.$root && this.$root.isConnected),
          apply: presented => { this.liveSnapshot = presented; this.recompute(); },
        });

        // Synchronous seed from the previous instance, see cachedSamples
        // above - renders immediately instead of flashing the loading
        // placeholder while the IndexedDB read below is in flight.
        if (cachedSamples) {
          this.samples = cachedSamples;
          this.recompute();
          this.loading = false;
        }

        try {
          this.samples = window.dashboardHistorizer ? await window.dashboardHistorizer.readSamples() : [];
        } catch (error) {
          this.samples = [];
        }
        cachedSamples = this.samples;
        this.recompute();
        this.loading = false;
        this.themeOff = window.DashboardTheme.onChange(() => this.recompute());
        if (this.$refs.canvas && window.ResizeObserver) {
          resizeObserver = new ResizeObserver(() => this.handleResize());
          resizeObserver.observe(this.$refs.canvas);
        }
      },

      // Rebuilds geometry from the samples already in memory - never
      // re-reads IndexedDB. Runs unconditionally (init, theme change);
      // handleResize() is the guarded entry point for the ResizeObserver.
      recompute() {
        // clientWidth is 0 while the overview tab is hidden (x-show panel)
        // and always in JSDOM - the old fixed constant becomes the
        // fallback, not an error case.
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        lastWidth = Math.round(width);
        this.width = lastWidth;
        this.geometry = dayGeometry(this.samples, this.liveSnapshot && this.liveSnapshot.interpretation, lastWidth, this.options);
        if (this.$refs.svg) this.$refs.svg.setAttribute('viewBox', `0 0 ${lastWidth} ${H}`);
      },

      // ResizeObserver callback: skips the rebuild when only a sub-pixel
      // jitter fired, so dragging the window edge doesn't rebuild every
      // path string on every frame - and avoids the feedback loop that
      // triggers "ResizeObserver loop completed with undelivered
      // notifications".
      handleResize() {
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        if (Math.round(width) === lastWidth) return;
        this.recompute();
      },

      destroy() {
        if (this.presenter) this.presenter.release();
        if (this.themeOff) this.themeOff();
        if (resizeObserver) resizeObserver.disconnect();
      },
    };
  };

  energyDayCard.pointSeries = pointSeries;
  energyDayCard.xPositions = xPositions;
  energyDayCard.stackAreas = stackAreas;
  energyDayCard.gridLines = gridLines;
  energyDayCard.gridLinesMarkup = gridLinesMarkup;
  energyDayCard.areasMarkup = areasMarkup;
  energyDayCard.dayGeometry = dayGeometry;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyDayCard', energyDayCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
