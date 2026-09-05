// "Statuskarte": the most compressed of the six alternative energy-flow
// cards - one sentence, one balance beam, four state tiles. Port of
// Vorschlag F from the six-proposals exploration. Two of its four options
// (Überschuss-/Netzbezug-Schwelle) are fachliche Schwellwerte, not
// Darstellungsparameter, so - unlike the other five cards' options - they
// live in the Energie-Einstellungen (Interpretation.SurplusThresholdW/
// ImportThresholdW, embedded on the snapshot) instead of the layout item;
// beam_span and show_advice are ordinary layout-editor options, read from
// the item's data-* attributes. See
// knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
(() => {
  const DEFAULT_SURPLUS_THRESHOLD = 800;
  const DEFAULT_IMPORT_THRESHOLD = 1500;

  const stateColors = () => window.DashboardTheme.colors({
    gut: 'ok',
    Hinweis: 'warn',
    kritisch: 'bad',
    veraltet: 'text-faint',
    neutral: 'border',
  });

  // Priority order mirrors the prototype: data problems outrank any advice.
  // thresholds = {surplus, import} in W (Interpretation.SurplusThresholdW/
  // ImportThresholdW, see the file header); showAdvice toggles the
  // actionable sentence ("guter Zeitpunkt für ...") vs. a bare state note.
  function statusLead(balance, staleCount, thresholds, showAdvice) {
    const model = window.EnergyModel;
    const hasGap = balance.unbalanced;
    if (hasGap || staleCount) {
      return {
        state: 'kritisch',
        color: stateColors().kritisch,
        title: hasGap ? `Bilanzlücke ${model.formatPower(Math.abs(balance.gap))}` : `${staleCount} Rolle${staleCount > 1 ? 'n' : ''} veraltet`,
        sub: hasGap
          ? 'Erzeugung und Verbrauch gehen nicht auf. Ein Zähler fehlt oder ist falsch zugeordnet - die Werte unten sind mit Vorsicht zu lesen.'
          : 'Mindestens eine Rolle meldet keine frischen Werte. Angezeigt wird der letzte bekannte Stand.',
      };
    }
    if (balance.gridExport >= thresholds.surplus && balance.gridExport > 0.5) {
      return {
        state: 'gut', color: stateColors().gut,
        title: `${model.formatPower(balance.gridExport)} Überschuss`,
        sub: showAdvice ? 'Guter Zeitpunkt für Wallbox, Warmwasser oder Werkstattgeräte - der Strom ginge sonst ins Netz.' : 'Die Anlage speist ins Netz ein.',
      };
    }
    if (balance.gridImport >= thresholds.import && balance.gridImport > 0.5) {
      return {
        state: 'Hinweis', color: stateColors().Hinweis,
        title: `${model.formatPower(balance.gridImport)} aus dem Netz`,
        sub: showAdvice ? 'Verschiebbare Verbraucher später einplanen. Batterie und PV decken den Bedarf gerade nicht.' : 'Der Bedarf wird überwiegend aus dem Netz gedeckt.',
      };
    }
    return {
      state: 'gut', color: stateColors().gut,
      title: 'Ausgeglichen',
      sub: `Hausverbrauch ${model.formatPower(balance.load)}, Netzaustausch unter den eingestellten Schwellen.`,
    };
  }

  // Balken-Geometrie auf einer 0..width-Spur, Mitte bei width/2. Positive
  // Bilanz (Bezug > Einspeisung) zeigt nach rechts, negative nach links.
  //
  // width ist ein Parameter statt einer Konstanten, weil die Karte ihr SVG
  // sonst mit preserveAspectRatio="none" auf die Kachelbreite quetschen
  // muesste - x und y bekaemen unterschiedliche Massstaebe und der Text
  // unter der Waage wuerde horizontal zusammengedrueckt. Gerechnet wird
  // darum gegen die gemessene Pixelbreite, dieselbe Loesung wie in
  // energy-day.js. 980 bleibt die Vorgabe fuer Aufrufe ohne Messung.
  function beamGeometry(balance, span, width = 980) {
    const mid = width / 2;
    const half = width / 2 - 20;
    const net = balance.gridImport - balance.gridExport;
    const clamped = Math.max(-1, Math.min(1, net / span));
    const x = mid + clamped * half;
    const fillFrom = Math.min(mid, x);
    const fillTo = Math.max(mid, x);
    const isImport = net > 0;
    const model = window.EnergyModel;
    const label = Math.abs(net) < 0.5 ? 'ausgeglichen' : `${isImport ? 'Bezug ' : 'Einspeisung '}${model.formatPower(Math.abs(net))}`;
    return {x, mid, fillFrom, fillTo, width: Math.max(fillTo - fillFrom, 2), isImport, label};
  }

  function statusTiles(balance, snapshot, staleCount, thresholds) {
    const model = window.EnergyModel;
    const hasGap = balance.unbalanced;
    const batteryNet = balance.charge - balance.discharge;
    const batteryValue = Math.abs(batteryNet);
    const batteryNote = batteryNet > 0.5 ? 'lädt' : batteryNet < -0.5 ? 'entlädt' : 'ruht';
    const batteryStale = model.isStale(snapshot, 'battery') || model.isStale(snapshot, 'battery_charge') || model.isStale(snapshot, 'battery_discharge');
    const surplusOk = balance.gridExport >= thresholds.surplus && balance.gridExport > 0.5;
    const importOk = balance.gridImport >= thresholds.import && balance.gridImport > 0.5;
    const tiles = [
      {
        label: 'Überschuss',
        value: balance.gridExport > 0.5 ? model.formatPower(balance.gridExport) : '—',
        note: surplusOk ? `über der Schwelle von ${model.formatPower(thresholds.surplus)}` : `Schwelle ${model.formatPower(thresholds.surplus)}`,
        state: surplusOk ? 'gut' : 'neutral',
      },
      {
        label: 'Netzbezug',
        value: balance.gridImport > 0.5 ? model.formatPower(balance.gridImport) : '—',
        note: importOk ? `über der Schwelle von ${model.formatPower(thresholds.import)}` : `Schwelle ${model.formatPower(thresholds.import)}`,
        state: importOk ? 'Hinweis' : 'neutral',
      },
      {
        label: 'Batterie',
        value: batteryValue > 0.5 ? model.formatPower(batteryValue) : '—',
        note: batteryNote,
        state: batteryStale ? 'veraltet' : 'neutral',
      },
      {
        label: 'Datenqualität',
        value: hasGap ? model.formatPower(Math.abs(balance.gap)) : staleCount ? String(staleCount) : 'vollständig',
        note: hasGap ? 'nicht zugeordnet' : staleCount ? 'veraltete Rollen' : 'alle Rollen frisch',
        state: hasGap ? 'kritisch' : staleCount ? 'Hinweis' : 'gut',
      },
    ];
    return tiles.map(tile => ({...tile, color: stateColors()[tile.state], stateLabel: tile.state === 'neutral' ? '' : tile.state}));
  }

  const BEAM_HEIGHT = 52;
  const DEFAULT_BEAM_WIDTH = 980;

  const energyStatusCard = () => {
    // Bewusst ausserhalb des reaktiven Alpine-Objekts, gleiche Begruendung
    // wie bei energy-day.js: ein Plattform-Handle, kein Ansichtszustand.
    let resizeObserver = null;
    let lastWidth = null;

    return {
      snapshot: null,
      lead: {state: 'neutral', color: stateColors().neutral, title: 'Kein Energiefluss', sub: 'Alle zugeordneten Rollen melden 0 W.'},
      tiles: [],
      options: {beamSpan: '6000', showAdvice: 'on'},
      presenter: null,
      snapshotOverride: null,

      init() {
        const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
        this.options = {
          beamSpan: data.beamSpan || '6000',
          showAdvice: data.showAdvice || 'on',
        };
        const raw = this.snapshotOverride || window.EnergyModel.readEmbeddedSnapshot('energy-status-initial');
        const reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
        // Die Zahlen dieser Karte federn vom sichtbaren Wert der
        // Vorgaengerinstanz aus, statt beim outerHTML-Tausch von
        // #overview-live auf den Zielwert zu springen. compute() bleibt
        // unveraendert - es bekommt nur einen Schnappschuss, dessen Zahlen die
        // gerade dargestellten sind.
        this.presenter = window.EnergyPresentation.present({
          key: data.layoutItemId || 'energy-status',
          raw,
          reducedMotion,
          alive: () => !!(this.$root && this.$root.isConnected),
          apply: presented => { this.snapshot = presented; this.compute(); },
        });
        this.themeOff = window.DashboardTheme.onChange(() => this.compute());
        if (this.$refs.beamWrap && window.ResizeObserver) {
          resizeObserver = new ResizeObserver(() => this.handleResize());
          resizeObserver.observe(this.$refs.beamWrap);
        }
      },

      destroy() {
        if (this.presenter) this.presenter.release();
        if (this.themeOff) this.themeOff();
        if (resizeObserver) resizeObserver.disconnect();
      },

      get thresholds() {
        const interpretation = (this.snapshot && this.snapshot.interpretation) || {};
        return {
          surplus: interpretation.surplus_threshold_w || DEFAULT_SURPLUS_THRESHOLD,
          import: interpretation.import_threshold_w || DEFAULT_IMPORT_THRESHOLD,
        };
      },

      compute() {
        const model = window.EnergyModel;
        if (!this.snapshot) return;
        const balance = model.balanceOf(this.snapshot);
        const staleCount = (this.snapshot.roles || []).filter(state => state.freshness === 'stale').length;
        const thresholds = this.thresholds;
        this.lead = statusLead(balance, staleCount, thresholds, this.options.showAdvice !== 'off');
        this.tiles = statusTiles(balance, this.snapshot, staleCount, thresholds);
        this.renderBeam(balance);
      },

      // Ueberspringt den Neuaufbau bei blossem Sub-Pixel-Zittern, gleiche
      // Begruendung wie energy-day.js's handleResize().
      handleResize() {
        const width = (this.$refs.beamWrap && this.$refs.beamWrap.clientWidth) || DEFAULT_BEAM_WIDTH;
        if (Math.round(width) === lastWidth) return;
        this.compute();
      },

      renderBeam(balance) {
        const model = window.EnergyModel;
        const svg = this.$refs.beam;
        if (!svg) return;
        const width = Math.round((this.$refs.beamWrap && this.$refs.beamWrap.clientWidth) || DEFAULT_BEAM_WIDTH);
        lastWidth = width;
        svg.setAttribute('viewBox', `0 0 ${width} ${BEAM_HEIGHT}`);
        model.clearSvgChildren(svg);
        const span = Number(this.options.beamSpan) || 6000;
        const geometry = beamGeometry(balance, span, width);
        svg.append(
          model.svgEl('rect', {x: 20, y: 20, width: Math.max(width - 40, 2), height: 12, rx: 6, fill: window.DashboardTheme.color('track')}),
          model.svgEl('rect', {
            x: geometry.fillFrom, y: 20, width: geometry.width, height: 12, rx: 6,
            fill: geometry.isImport ? model.COLORS.gridImport : model.COLORS.pv,
          }),
          model.svgEl('line', {x1: geometry.mid, y1: 12, x2: geometry.mid, y2: 40, stroke: window.DashboardTheme.color('border'), 'stroke-width': 1.5}),
          model.svgEl('polygon', {points: `${geometry.x - 8},8 ${geometry.x + 8},8 ${geometry.x},20`, fill: this.lead.color}),
          model.svgEl('text', {
            x: Math.max(60, Math.min(width - 60, geometry.x)), y: 50, 'text-anchor': 'middle', 'font-size': 13, fill: window.DashboardTheme.color('text'),
          }, geometry.label),
        );
        const desc = svg.querySelector('desc');
        if (desc) desc.textContent = `Netzbilanz ${model.formatPower(balance.gridImport - balance.gridExport)} bei einem Skalenende von ${model.formatPower(span)}.`;
      },
    };
  };

  energyStatusCard.statusLead = statusLead;
  energyStatusCard.beamGeometry = beamGeometry;
  energyStatusCard.statusTiles = statusTiles;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyStatusCard', energyStatusCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
