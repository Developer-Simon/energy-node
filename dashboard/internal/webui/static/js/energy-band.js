// "Bilanzband": a Sankey-style cut through the current moment - sources
// stack on the left, sinks stack on the right, a "HAUS" bar in the middle.
// Band height is strictly proportional to power (or its square root, see
// scale_mode). Port of Vorschlag A from the six-proposals exploration; its
// five layout-editor options (height_reference, scale_mode, unit,
// bundle_threshold, animate) are read from the layout item's data-*
// attributes, see knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
(() => {
  const DEFAULT_WIDTH = 980;
  const H = 430;
  const PAD_Y = 26;
  const GAP = 2;
  const BAR_W = 30;
  const LABEL_MIN_GAP = 34;

  function ribbonPath(x0, x1, yA0, yA1, yB0, yB1) {
    const c0 = x0 + (x1 - x0) * 0.45;
    const c1 = x0 + (x1 - x0) * 0.55;
    const fmt = n => n.toFixed(1);
    return `M ${fmt(x0)} ${fmt(yA0)} C ${fmt(c0)} ${fmt(yA0)}, ${fmt(c1)} ${fmt(yB0)}, ${fmt(x1)} ${fmt(yB0)}` +
           ` L ${fmt(x1)} ${fmt(yB1)} C ${fmt(c1)} ${fmt(yB1)}, ${fmt(c0)} ${fmt(yA1)}, ${fmt(x0)} ${fmt(yA1)} Z`;
  }

  // Stacks flows at a fixed x, height proportional to weight(value)*k, with
  // a fixed gap between bands. A band is floored to 1.5px so a
  // present-but-tiny flow stays visible instead of vanishing. weight is the
  // identity function for scale_mode "linear", Math.sqrt for "sqrt".
  function stackSlots(flows, x, k, weight) {
    const barH = flows.reduce((sum, f) => sum + weight(f.value) * k, 0) + GAP * Math.max(flows.length - 1, 0);
    let y = PAD_Y + (H - 2 * PAD_Y - barH) / 2;
    return flows.map(flow => {
      const h = Math.max(weight(flow.value) * k, 1.5);
      const slot = {flow, x, y0: y, y1: y + h, h};
      y += h + GAP;
      return slot;
    });
  }

  // "Kleinstflüsse" bundling: flows below threshold (a fraction of total)
  // collapse into one "Sonstiges (n)" entry, unless only one qualifies (then
  // it is left alone rather than "bundled" with nothing). threshold 0
  // (default) disables bundling entirely - same contract as the prototype's
  // bundle().
  function bundleFlows(flows, threshold, total) {
    if (!threshold) return flows;
    const keep = flows.filter(f => f.value / total >= threshold);
    const small = flows.filter(f => f.value / total < threshold);
    if (small.length > 1) {
      keep.push({
        id: 'sonstiges', label: `Sonstiges (${small.length})`,
        value: small.reduce((sum, f) => sum + f.value, 0), color: window.EnergyModel.COLORS.rest,
      });
    } else {
      keep.push(...small);
    }
    return keep;
  }

  // Re-centers the same heights as stackSlots() but as a block aligned with
  // the HAUS bar, so the ribbon's bus-side edge lines up across all flows
  // on that side regardless of how the outer (label) side is spaced out.
  function centerSlots(slots) {
    const barH = slots.reduce((sum, s) => sum + s.h, 0) + GAP * Math.max(slots.length - 1, 0);
    let y = PAD_Y + (H - 2 * PAD_Y - barH) / 2;
    return slots.map(s => {
      const center = {y0: y, y1: y + s.h};
      y += s.h + GAP;
      return center;
    });
  }

  // Numeric relaxation so stacked labels keep a minimum vertical gap, then
  // shifts the whole stack back inside the drawable area. Port of the
  // prototype's spread() - needed once several bands (especially a
  // floor-clamped 1.5px one) would otherwise print labels on top of each
  // other.
  function spreadLabelY(slots, minGap) {
    const ys = slots.map(s => (s.y0 + s.y1) / 2);
    for (let pass = 0; pass < 60; pass++) {
      for (let i = 1; i < ys.length; i++) {
        const overlap = minGap - (ys[i] - ys[i - 1]);
        if (overlap > 0) { ys[i - 1] -= overlap / 2; ys[i] += overlap / 2; }
      }
      if (ys.length) {
        const shift = Math.max(0, 20 - ys[0]) - Math.max(0, ys[ys.length - 1] - (H - 20));
        for (let i = 0; i < ys.length; i++) ys[i] += shift;
      }
    }
    return ys;
  }

  function buildLabels(slots, labelYs, x, anchor, total, unit) {
    const model = window.EnergyModel;
    return slots.map((slot, i) => {
      const y = labelYs[i];
      const swatchX = anchor === 'end' ? x + 10 : x - 16;
      const bandY = (slot.y0 + slot.y1) / 2;
      let connector = null;
      if (Math.abs(bandY - y) > 3) {
        const tipX = anchor === 'end' ? swatchX + 6 : swatchX;
        const endX = anchor === 'end' ? swatchX + 18 : swatchX - 12;
        connector = `M ${tipX} ${y + 4} L ${(tipX + endX) / 2} ${y + 4} L ${endX} ${bandY}`;
      }
      return {
        id: slot.flow.id, color: slot.flow.color, anchor,
        nameX: x, nameY: y - 2, valueX: x, valueY: y + 14,
        name: slot.flow.label,
        value: `${model.formatPower(slot.flow.value, unit)}  ·  ${model.formatPercent(slot.flow.value / total)}`,
        swatchX, swatchY: slot.y0, swatchHeight: Math.max(slot.h, 2),
        connector,
      };
    });
  }

  // Thin animated centerline down the middle of a ribbon, showing flow
  // direction - only drawn for bands thick enough to read (>5px) and never
  // for the "Nicht zugeordnet" rest band, which has no real direction.
  function centerlinePath(x0, x1, ym0, ym1) {
    const c0 = x0 + (x1 - x0) * 0.45;
    const c1 = x0 + (x1 - x0) * 0.55;
    const fmt = n => n.toFixed(1);
    return `M ${fmt(x0)} ${fmt(ym0)} C ${fmt(c0)} ${fmt(ym0)}, ${fmt(c1)} ${fmt(ym1)}, ${fmt(x1)} ${fmt(ym1)}`;
  }

  function ribbonAnimation(slot, x0, x1, ym0, ym1) {
    if (slot.h <= 5 || slot.flow.rest) return null;
    return {path: centerlinePath(x0, x1, ym0, ym1), strokeWidth: Math.min(2, slot.h / 4)};
  }

  // <template x-for>/<template x-if> don't clone inside <svg> (browsers parse
  // a <template> found in foreign/SVG content as a plain SVG element with no
  // .content DocumentFragment, so Alpine's cloning throws) - ribbonsMarkup()/
  // labelsMarkup()/bandMarkup() instead render the repeated elements to an
  // SVG markup string the template binds once via x-html on a plain <g>.
  function ribbonsMarkup(ribbons, suppressAnimation, key) {
    const presentation = window.EnergyPresentation;
    return ribbons.map(ribbon => {
      if (!ribbon.animation || suppressAnimation) {
        if (key && presentation) presentation.pauseDash(key, `band:${ribbon.flow.id}`);
        return `<g><path d="${ribbon.path}" fill="${ribbon.flow.color}" opacity="${ribbon.flow.rest ? .3 : .62}"></path></g>`;
      }
      const delay = key && presentation
        ? ` style="animation-delay:${presentation.dashDelay(key, `band:${ribbon.flow.id}`, presentation.DEFAULT_DASH_SECONDS)}"`
        : '';
      const dash = `<path class="energy-band-flow-dash" d="${ribbon.animation.path}" fill="none" stroke="${ribbon.flow.color}" stroke-width="${ribbon.animation.strokeWidth}" stroke-dasharray="6 14" stroke-linecap="round" opacity=".95"${delay}></path>`;
      return `<g><path d="${ribbon.path}" fill="${ribbon.flow.color}" opacity="${ribbon.flow.rest ? .3 : .62}"></path>${dash}</g>`;
    }).join('');
  }

  function labelsMarkup(labels) {
    const theme = window.DashboardTheme.colors({line: 'border', text: 'text', muted: 'text-muted', bus: 'track'});
    return labels.map(label => {
      const connector = label.connector ? `<path d="${label.connector}" fill="none" stroke="${theme.line}" stroke-width="1"></path>` : '';
      return `<g>${connector}<text x="${label.nameX}" y="${label.nameY}" text-anchor="${label.anchor}" font-size="15" fill="${theme.text}">${label.name}</text><text x="${label.valueX}" y="${label.valueY}" text-anchor="${label.anchor}" font-size="14" fill="${theme.muted}">${label.value}</text><rect x="${label.swatchX}" y="${label.swatchY}" width="6" height="${label.swatchHeight}" rx="2" fill="${label.color}"></rect></g>`;
    }).join('');
  }

  function bandMarkup(geometry, totalLabel, suppressAnimation, key) {
    const theme = window.DashboardTheme.colors({line: 'border', text: 'text', muted: 'text-muted', bus: 'track'});
    const centerX = geometry.width / 2;
    return ribbonsMarkup(geometry.sourceRibbons, suppressAnimation, key) + ribbonsMarkup(geometry.sinkRibbons, suppressAnimation, key) +
      `<rect x="${geometry.busX}" y="${geometry.busY}" width="${geometry.busWidth}" height="${geometry.busHeight}" rx="3" fill="${theme.bus}" stroke="${theme.line}"></rect>` +
      `<text x="${centerX}" y="${geometry.busMidY - 6}" text-anchor="middle" font-size="15" font-weight="600" fill="${theme.text}" transform="rotate(-90 ${centerX} ${geometry.busMidY})">HAUS</text>` +
      `<text x="${centerX}" y="${geometry.busMidY + 10}" text-anchor="middle" font-size="13" fill="${theme.muted}" transform="rotate(-90 ${centerX} ${geometry.busMidY})">${totalLabel}</text>` +
      labelsMarkup(geometry.sourceLabels) + labelsMarkup(geometry.sinkLabels) +
      `<text x="20" y="20" font-size="13" letter-spacing="1.4" fill="${theme.muted}">ERZEUGUNG</text>` +
      `<text x="${geometry.width - 20}" y="20" text-anchor="end" font-size="13" letter-spacing="1.4" fill="${theme.muted}">VERWENDUNG</text>`;
  }

  // Nennleistung fuer den "absolut"-Hoehenbezug - dieselbe feste 10-kW-
  // Referenz wie im Prototyp, siehe die Erlaeuterung in normalizeEnergyGraphicOptions.
  const ABSOLUTE_REFERENCE_W = 10000;

  // Pure geometry: turns deriveBalance()'s sources/sinks into everything
  // the template needs to draw - ribbon paths, the HAUS bar, and label
  // positions with a "feeler" line back to a squeezed band. Returns null in
  // the empty state (no sources and no sinks at all). options carries the
  // four layout-editor fields that affect geometry/labels (scaleMode,
  // heightReference, bundleThreshold, unit) - animate only affects markup,
  // handled separately in bandMarkup().
  function bandGeometry(balance, width, options) {
    const {scaleMode, heightReference, bundleThreshold, unit} = options || {};
    const sources = bundleFlows(balance.sources, Number(bundleThreshold) || 0, balance.total);
    const sinks = bundleFlows(balance.sinks, Number(bundleThreshold) || 0, balance.total);
    if (!sources.length && !sinks.length) return null;

    const LEFT_X = width * 0.273;   // heute 268 von 980
    const RIGHT_X = width * 0.727;  // heute 712 von 980
    const CX_L = width / 2 - BAR_W / 2;
    const CX_R = width / 2 + BAR_W / 2;

    const weight = v => scaleMode === 'sqrt' ? Math.sqrt(v) : v;
    const weightSrc = sources.reduce((sum, f) => sum + weight(f.value), 0) || 1;
    const weightSink = sinks.reduce((sum, f) => sum + weight(f.value), 0) || 1;
    const wMax = Math.max(weightSrc, weightSink);
    const avail = H - 2 * PAD_Y - GAP * Math.max(sources.length, sinks.length, 1);
    // "fill" normiert auf den aktuellen Moment - Verhaeltnisse stimmen, die
    // absolute Hoehe sagt nichts. "abs" bezieht auf eine feste Nennleistung,
    // dann schrumpft die ganze Grafik bei kleiner Momentanleistung sichtbar
    // zusammen - die Einstellung, die "50 W sieht aus wie 8 kW" wirklich loest.
    const k = heightReference === 'abs' ? Math.min(avail / weight(ABSOLUTE_REFERENCE_W), avail / wMax) : avail / wMax;

    const sourceSlots = stackSlots(sources, LEFT_X, k, weight);
    const sinkSlots = stackSlots(sinks, RIGHT_X, k, weight);
    const sourceCenters = centerSlots(sourceSlots);
    const sinkCenters = centerSlots(sinkSlots);
    const mid = s => (s.y0 + s.y1) / 2;

    const sourceRibbons = sourceSlots.map((slot, i) => ({
      flow: slot.flow,
      path: ribbonPath(LEFT_X, CX_L, slot.y0, slot.y1, sourceCenters[i].y0, sourceCenters[i].y1),
      animation: ribbonAnimation(slot, LEFT_X, CX_L, mid(slot), mid(sourceCenters[i])),
    }));
    const sinkRibbons = sinkSlots.map((slot, i) => ({
      flow: slot.flow,
      path: ribbonPath(RIGHT_X, CX_R, sinkCenters[i].y0, sinkCenters[i].y1, slot.y0, slot.y1),
      animation: ribbonAnimation(slot, CX_R, RIGHT_X, mid(sinkCenters[i]), mid(slot)),
    }));

    const busTop = Math.min(sourceCenters[0] ? sourceCenters[0].y0 : PAD_Y, sinkCenters[0] ? sinkCenters[0].y0 : PAD_Y);
    const busBottom = Math.max(
      sourceCenters.length ? sourceCenters[sourceCenters.length - 1].y1 : H - PAD_Y,
      sinkCenters.length ? sinkCenters[sinkCenters.length - 1].y1 : H - PAD_Y,
    );

    return {
      width,
      sourceRibbons, sinkRibbons,
      busX: CX_L, busY: busTop, busWidth: BAR_W, busHeight: Math.max(busBottom - busTop, 2),
      busMidY: (busTop + busBottom) / 2,
      sourceLabels: buildLabels(sourceSlots, spreadLabelY(sourceSlots, LABEL_MIN_GAP), LEFT_X - 18, 'end', balance.total, unit),
      sinkLabels: buildLabels(sinkSlots, spreadLabelY(sinkSlots, LABEL_MIN_GAP), RIGHT_X + 18, 'start', balance.total, unit),
    };
  }

  const energyBandCard = () => {
    let resizeObserver = null;
    let lastWidth = null;

    return {
      snapshot: null,
      geometry: null,
      svgMarkup: '',
      totalLabel: '',
      description: 'Kein Energiefluss: alle zugeordneten Rollen melden 0 W.',
      legend: [],
      reducedMotion: false,
      cardKey: 'energy-band',
      width: DEFAULT_WIDTH,
      options: {scaleMode: 'linear', heightReference: 'fill', unit: 'auto', bundleThreshold: '0', animate: 'on', measuredSplit: 'sum'},
      presenter: null,

      init() {
        this.reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
        const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
        this.cardKey = data.layoutItemId || 'energy-band';
        this.options = {
          scaleMode: data.scaleMode || 'linear',
          heightReference: data.heightReference || 'fill',
          unit: data.unit || 'auto',
          bundleThreshold: data.bundleThreshold || '0',
          animate: data.animate || 'on',
          measuredSplit: data.measuredSplit || 'sum',
        };
        const raw = window.EnergyModel.readEmbeddedSnapshot('energy-band-initial');
        this.presenter = window.EnergyPresentation.present({
          key: this.cardKey,
          raw,
          reducedMotion: this.reducedMotion,
          alive: () => !!(this.$root && this.$root.isConnected),
          apply: presented => { this.snapshot = presented; this.recompute(); },
        });
        this.themeOff = window.DashboardTheme.onChange(() => this.recompute());
        if (this.$refs.canvas && window.ResizeObserver) {
          resizeObserver = new ResizeObserver(() => this.handleResize());
          resizeObserver.observe(this.$refs.canvas);
        }
      },

      destroy() {
        if (this.presenter) this.presenter.release();
        if (this.themeOff) this.themeOff();
        if (resizeObserver) resizeObserver.disconnect();
      },

      // Rebuilds geometry from the current snapshot at the current
      // container width. Runs unconditionally (init, theme change);
      // handleResize() is the guarded entry point for the ResizeObserver.
      recompute() {
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        lastWidth = Math.round(width);
        this.width = lastWidth;
        if (this.$refs.svg) this.$refs.svg.setAttribute('viewBox', `0 0 ${lastWidth} ${H}`);
        this.compute(lastWidth);
      },

      // ResizeObserver callback: skips the rebuild when only a sub-pixel
      // jitter fired, matching energy-day.js's handleResize().
      handleResize() {
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        if (Math.round(width) === lastWidth) return;
        this.recompute();
      },

      compute(width) {
        const model = window.EnergyModel;
        if (!this.snapshot) return;
        const balance = model.balanceOf(this.snapshot, {measuredSplit: this.options.measuredSplit});
        const unit = this.options.unit;
        this.geometry = bandGeometry(balance, width, this.options);
        if (!this.geometry) {
          this.svgMarkup = '';
          this.description = 'Kein Energiefluss: alle zugeordneten Rollen melden 0 W.';
          this.legend = [];
          return;
        }
        this.totalLabel = model.formatPower(balance.total, unit);
        const suppressAnimation = this.reducedMotion || this.options.animate === 'off';
        this.svgMarkup = bandMarkup(this.geometry, this.totalLabel, suppressAnimation, this.cardKey);
        this.description = `Gesamtleistung ${model.formatPower(balance.total, unit)}. Quellen: ${balance.sources.map(f => `${f.label} ${model.formatPower(f.value, unit)}`).join(', ')}. Senken: ${balance.sinks.map(f => `${f.label} ${model.formatPower(f.value, unit)}`).join(', ')}.`;
        const seen = new Map();
        [...balance.sources, ...balance.sinks].forEach(f => seen.set(f.color, f));
        this.legend = [...seen.values()];
      },
    };
  };

  energyBandCard.ribbonPath = ribbonPath;
  energyBandCard.bundleFlows = bundleFlows;
  energyBandCard.stackSlots = stackSlots;
  energyBandCard.centerSlots = centerSlots;
  energyBandCard.spreadLabelY = spreadLabelY;
  energyBandCard.bandGeometry = bandGeometry;
  energyBandCard.ribbonsMarkup = ribbonsMarkup;
  energyBandCard.labelsMarkup = labelsMarkup;
  energyBandCard.bandMarkup = bandMarkup;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyBandCard', energyBandCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
