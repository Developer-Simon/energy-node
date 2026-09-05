// "Autarkie-Ring": inner ring is where power comes from, outer ring is
// where it goes, a single KPI sits in the middle. Port of Vorschlag B from
// the six-proposals exploration; its three layout-editor options (kpi,
// label_mode, animate) are read from the layout item's data-* attributes,
// see knowhow/dashboard/energiegrafiken-konfiguration-backlog.md.
//
// Unlike energy-flow.js, this card (like the other five) fully re-mounts on
// every #overview-live refresh instead of patching an existing SVG in
// place, so there is no need for the imperative "reuse existing DOM nodes
// for a CSS transition" trick - segment/label data is computed as plain
// objects and the template binds to it declaratively via x-for/x-bind.
(() => {
  // Pie-slice-with-a-hole path between radius r-thick/2 and r+thick/2,
  // spanning angle a0..a1 (radians, 0 = +x axis, clockwise).
  function annulusPath(cx, cy, r, thick, a0, a1) {
    const outer = r + thick / 2;
    const inner = r - thick / 2;
    const point = (radius, angle) => [cx + radius * Math.cos(angle), cy + radius * Math.sin(angle)];
    const large = (a1 - a0) > Math.PI ? 1 : 0;
    const [x1, y1] = point(outer, a0);
    const [x2, y2] = point(outer, a1);
    const [x3, y3] = point(inner, a1);
    const [x4, y4] = point(inner, a0);
    const fmt = n => n.toFixed(2);
    return `M ${fmt(x1)} ${fmt(y1)} A ${outer} ${outer} 0 ${large} 1 ${fmt(x2)} ${fmt(y2)} L ${fmt(x3)} ${fmt(y3)} A ${inner} ${inner} 0 ${large} 0 ${fmt(x4)} ${fmt(y4)} Z`;
  }

  // Just the outer arc of annulusPath (the "M ... A ... 0 large 1 ..." head,
  // without the inner return leg) - the dashed direction indicator in
  // segmentsMarkup() draws along this curve when animate is on.
  function outerArcPath(cx, cy, r, thick, a0, a1) {
    const outer = r + thick / 2;
    const point = (radius, angle) => [cx + radius * Math.cos(angle), cy + radius * Math.sin(angle)];
    const large = (a1 - a0) > Math.PI ? 1 : 0;
    const [x1, y1] = point(outer, a0);
    const [x2, y2] = point(outer, a1);
    const fmt = n => n.toFixed(2);
    return `M ${fmt(x1)} ${fmt(y1)} A ${outer} ${outer} 0 ${large} 1 ${fmt(x2)} ${fmt(y2)}`;
  }

  // Lays flows around a full circle starting at 12 o'clock (-90deg),
  // proportional to value, with a fixed 2px gap (converted to radians)
  // between segments so same-colored neighbors stay visually separate.
  function ringSegments(flows, cx, cy, r, thick) {
    const sum = flows.reduce((total, flow) => total + flow.value, 0) || 1;
    const gapAngle = 2 / r;
    let angle = -Math.PI / 2 + gapAngle / 2;
    const segments = [];
    for (const flow of flows) {
      const span = (flow.value / sum) * (Math.PI * 2) - gapAngle;
      if (span <= 0) continue;
      segments.push({
        flow, fraction: flow.value / sum,
        path: annulusPath(cx, cy, r, thick, angle, angle + span),
        outerArc: outerArcPath(cx, cy, r, thick, angle, angle + span),
      });
      angle += span + gapAngle;
    }
    return segments;
  }

  // Die Leseliste ist eine Rangliste: die groesste Position steht oben, und ein
  // Rangwechsel ist selbst die Information. Weil die Werte gefedert laufen,
  // kreuzen zwei fast gleich grosse Positionen sonst mehrmals je Minute und die
  // Liste flackert. Ein Wechsel braucht darum Vorsprung: erst wenn die untere
  // die obere um RANK_MARGIN der Gruppensumme ueberholt, tauschen die beiden -
  // und bleiben dann getauscht, bis der Vorsprung andersherum wieder reicht.
  //
  // Der Ring behaelt bewusst seine feste fachliche Reihenfolge (PV, Netz,
  // Batterie): dort ist der Ort eines Segments etwas, das man sich merkt.
  // Wandert es mit jedem Rangwechsel, ist diese Karte nicht mehr lesbar.
  const RANK_MARGIN = 0.02;

  function rankFlows(flows, previousOrder) {
    const byID = new Map(flows.map(flow => [flow.id, flow]));
    const ordered = [];
    for (const id of previousOrder || []) {
      if (!byID.has(id)) continue;
      ordered.push(byID.get(id));
      byID.delete(id);
    }
    // Neu hinzugekommene Positionen sortieren sich frei ein - sie haben noch
    // keinen Rang, den der Vorsprung schuetzen muesste.
    ordered.push(...[...byID.values()].sort((a, b) => b.value - a.value));
    const sum = ordered.reduce((total, flow) => total + flow.value, 0) || 1;
    const margin = sum * RANK_MARGIN;
    for (let pass = 0; pass < ordered.length; pass += 1) {
      let swapped = false;
      for (let i = 0; i + 1 < ordered.length; i += 1) {
        if (ordered[i + 1].value <= ordered[i].value + margin) continue;
        [ordered[i], ordered[i + 1]] = [ordered[i + 1], ordered[i]];
        swapped = true;
      }
      if (!swapped) break;
    }
    return ordered;
  }

  // labelMode mirrors energy-band.js's unit option: "both" (default) shows
  // percent and absolute power together, "pct"/"abs" show only one.
  // `share` misst gegen die groesste Position der Gruppe, nicht gegen die
  // Summe: der Spitzenreiter hat damit immer den vollen Balken, und die Liste
  // liest sich als Rangliste statt als zweites, blasseres Tortendiagramm.
  function readoutRows(flows, labelMode) {
    const model = window.EnergyModel;
    const sum = flows.reduce((total, flow) => total + flow.value, 0) || 1;
    const peak = flows.reduce((max, flow) => Math.max(max, flow.value), 0) || 1;
    return flows.map(flow => {
      const pct = model.formatPercent(flow.value / sum);
      const abs = model.formatPower(flow.value);
      const text = labelMode === 'pct' ? pct : labelMode === 'abs' ? abs : `${pct} · ${abs}`;
      return {id: flow.id, label: flow.label, color: flow.color, text, share: flow.value / peak};
    });
  }

  // Die Laufschrift trug bisher nur "hier fliesst etwas" - alle Striche liefen
  // gleich schnell, egal ob 80 W oder 4 kW durch das Segment gingen. Die
  // Umlaufzeit traegt jetzt den Anteil: der groesste Strang laeuft in
  // DASH_FAST_SECONDS um, der kleinste in DASH_SLOW_SECONDS, dazwischen linear.
  // Damit ist die Bewegung eine Aussage und keine Dekoration mehr.
  const DASH_FAST_SECONDS = 0.9;
  const DASH_SLOW_SECONDS = 2.6;

  function dashSeconds(fraction) {
    const share = Math.min(Math.max(fraction || 0, 0), 1);
    return Number((DASH_SLOW_SECONDS + (DASH_FAST_SECONDS - DASH_SLOW_SECONDS) * share).toFixed(2));
  }

  // <template x-for> doesn't clone inside <svg> (browsers parse a <template>
  // found in foreign/SVG content as a plain SVG element with no .content
  // DocumentFragment, so Alpine's document.importNode(el.content, ...)
  // throws) - segmentsMarkup() instead renders the repeated elements to an
  // SVG markup string the template binds once via x-html on a plain <g>.
  // A dashed overlay along the segment's outer arc stands in for flow
  // direction (there is no natural "in/out" arrow on a ring) - suppressed
  // for the rest segment (no real direction) and when animate is off.
  // key ist die Layout-Kachel-ID. Mit ihr holt sich die Laufschrift ihre
  // Phase aus energy-presentation.js zurueck: die CSS-Animation eines neu
  // erzeugten <path> beginnt zwangslaeufig bei 0, eine negative
  // animation-delay spult sie an die Stelle vor, an der die vorige Instanz
  // aufgehoert hat. Ohne key (aeltere Aufrufe) bleibt alles wie bisher.
  function segmentsMarkup(segments, suppressAnimation, key) {
    const presentation = window.EnergyPresentation;
    return segments.map(segment => {
      if (suppressAnimation || segment.flow.rest) {
        if (key && presentation) presentation.pauseDash(key, `ring:${segment.flow.id}`);
        return `<path d="${segment.path}" fill="${segment.flow.color}" opacity="${segment.flow.rest ? .35 : .9}"></path>`;
      }
      const seconds = dashSeconds(segment.fraction);
      const timing = key && presentation
        ? ` style="animation-duration:${seconds}s;animation-delay:${presentation.dashDelay(key, `ring:${segment.flow.id}`, seconds)}"`
        : ` style="animation-duration:${seconds}s"`;
      const dash = `<path d="${segment.outerArc}" class="energy-ring-flow-dash"${timing} fill="none" stroke="${window.DashboardTheme.color('panel')}" stroke-width="1.4" stroke-dasharray="3 11" opacity=".55"></path>`;
      return `<path d="${segment.path}" fill="${segment.flow.color}" opacity=".9"></path>${dash}`;
    }).join('');
  }

  function kpiValue(balance, key) {
    const model = window.EnergyModel;
    switch (key) {
      case 'eigen': return {label: 'Eigenverbrauch', value: model.formatPercent(balance.kpi.eigen), sub: 'PV selbst genutzt'};
      case 'netz': return {label: 'Netzbilanz', value: model.formatPower(balance.kpi.netz), sub: balance.kpi.netz >= 0 ? 'Bezug' : 'Einspeisung'};
      case 'last': return {label: 'Hausverbrauch', value: model.formatPower(balance.kpi.last), sub: 'alle Verbraucher'};
      case 'autarkie':
      default:
        return {label: 'Autarkiegrad', value: model.formatPercent(balance.kpi.autarkie), sub: 'Deckung ohne Netzbezug'};
    }
  }

  const energyRingCard = () => ({
    snapshot: null,
    hasFlow: false,
    sourceSegments: [],
    sinkSegments: [],
    sourceSegmentsMarkup: '',
    sinkSegmentsMarkup: '',
    sourceRows: [],
    sinkRows: [],
    kpi: {label: '', value: '', sub: ''},
    totalLabel: '',
    description: 'Kein Energiefluss: alle zugeordneten Rollen melden 0 W.',
    reducedMotion: false,
    cardKey: 'energy-ring',
    presenter: null,
    // Der zuletzt gezeigte Rang je Gruppe. Er ist Eingabe von rankFlows() -
    // der Vorsprung, den ein Ueberholvorgang braucht, misst sich gegen ihn -
    // und zugleich das, woran compute() einen echten Rangwechsel von den
    // 60 Federbildern je Sekunde unterscheidet, in denen sich nur Zahlen
    // aendern.
    rowOrder: {source: [], sink: []},
    options: {kpi: 'autarkie', labelMode: 'both', animate: 'on', measuredSplit: 'sum'},

    init() {
      this.reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
      this.cardKey = data.layoutItemId || 'energy-ring';
      this.options = {
        kpi: data.kpi || 'autarkie',
        labelMode: data.labelMode || 'both',
        animate: data.animate || 'on',
        measuredSplit: data.measuredSplit || 'sum',
      };
      const raw = window.EnergyModel.readEmbeddedSnapshot('energy-ring-initial');
      // Die Zahlen dieser Karte federn vom sichtbaren Wert der Vorgaengerin
      // aus, statt beim outerHTML-Tausch von #overview-live auf den Zielwert
      // zu springen. compute() bleibt unveraendert - es bekommt nur einen
      // Schnappschuss, dessen Zahlen die gerade dargestellten sind. cardKey
      // adressiert die Laufschrift-Phasen (Task 4), key hier dieselben
      // Federn - beide tragen denselben Wert.
      this.presenter = window.EnergyPresentation.present({
        key: this.cardKey,
        raw,
        reducedMotion: this.reducedMotion,
        alive: () => !!(this.$root && this.$root.isConnected),
        apply: presented => { this.snapshot = presented; this.compute(); },
      });
      this.themeOff = window.DashboardTheme.onChange(() => this.compute());
    },

    destroy() {
      if (this.presenter) this.presenter.release();
      if (this.themeOff) this.themeOff();
    },

    // FLIP fuer den Rangwechsel: erst messen, wo die Zeilen stehen, dann Alpine
    // die Liste umsortieren lassen, dann jede Zeile per transform an ihren
    // alten Platz zuruecksetzen und von dort herueberlaufen lassen. Nur
    // transform, also kein Layout je Bild - und nur fuer Zeilen, die es vorher
    // schon gab; eine neu dazugekommene Position kommt ueber ihre eigene
    // Eintrittsbewegung herein statt von irgendwoher zu fliegen.
    measureRows() {
      const positions = new Map();
      if (!this.$root) return positions;
      for (const row of this.$root.querySelectorAll('.energy-ring-readout-row[data-row-id]')) {
        positions.set(row.dataset.rowId, row.getBoundingClientRect().top);
      }
      return positions;
    },

    playRowFlip(before) {
      if (!this.$root) return;
      const rows = [...this.$root.querySelectorAll('.energy-ring-readout-row[data-row-id]')];
      const moved = [];
      for (const row of rows) {
        const previousTop = before.get(row.dataset.rowId);
        if (previousTop === undefined) continue;
        const delta = previousTop - row.getBoundingClientRect().top;
        if (!delta) continue;
        row.style.transition = 'none';
        row.style.transform = `translateY(${delta}px)`;
        moved.push(row);
      }
      if (!moved.length) return;
      // Erzwungener Umbruch, sonst fasst der Browser Hin- und Rueckweg zu
      // einem einzigen Stilschreibvorgang zusammen und es bewegt sich nichts.
      void this.$root.offsetHeight;
      for (const row of moved) {
        row.style.transition = '';
        row.style.transform = '';
      }
    },

    compute() {
      const model = window.EnergyModel;
      if (!this.snapshot) return;
      const balance = model.balanceOf(this.snapshot, {measuredSplit: this.options.measuredSplit});
      this.hasFlow = balance.sources.length > 0 || balance.sinks.length > 0;
      if (!this.hasFlow) {
        this.sourceSegments = [];
        this.sinkSegments = [];
        this.sourceSegmentsMarkup = '';
        this.sinkSegmentsMarkup = '';
        this.sourceRows = [];
        this.sinkRows = [];
        this.rowOrder = {source: [], sink: []};
        this.description = 'Kein Energiefluss: alle zugeordneten Rollen melden 0 W.';
        return;
      }
      const suppressAnimation = this.reducedMotion || this.options.animate === 'off';
      this.sourceSegments = ringSegments(balance.sources, 170, 170, 128, 20);
      this.sinkSegments = ringSegments(balance.sinks, 170, 170, 100, 20);
      this.sourceSegmentsMarkup = segmentsMarkup(this.sourceSegments, suppressAnimation, this.cardKey);
      this.sinkSegmentsMarkup = segmentsMarkup(this.sinkSegments, suppressAnimation, this.cardKey);

      const rankedSources = rankFlows(balance.sources, this.rowOrder.source);
      const rankedSinks = rankFlows(balance.sinks, this.rowOrder.sink);
      const nextOrder = {source: rankedSources.map(f => f.id), sink: rankedSinks.map(f => f.id)};
      // Nur beim echten Rangwechsel messen. compute() laeuft, solange eine
      // Feder unterwegs ist, in jedem Bild - ein FLIP je Bild waere ein
      // Dauerlauf gegen sich selbst.
      const reordered = !suppressAnimation
        && this.rowOrder.source.length + this.rowOrder.sink.length > 0
        && (nextOrder.source.join() !== this.rowOrder.source.join()
          || nextOrder.sink.join() !== this.rowOrder.sink.join());
      const before = reordered ? this.measureRows() : null;
      this.rowOrder = nextOrder;
      this.sourceRows = readoutRows(rankedSources, this.options.labelMode);
      this.sinkRows = readoutRows(rankedSinks, this.options.labelMode);
      if (before && this.$nextTick) this.$nextTick(() => this.playRowFlip(before));
      this.kpi = kpiValue(balance, this.options.kpi);
      this.totalLabel = `${model.formatPower(balance.total)} gesamt`;
      this.description = `${this.kpi.label} ${this.kpi.value}, Gesamtleistung ${model.formatPower(balance.total)}.`;
    },
  });

  energyRingCard.annulusPath = annulusPath;
  energyRingCard.outerArcPath = outerArcPath;
  energyRingCard.ringSegments = ringSegments;
  energyRingCard.segmentsMarkup = segmentsMarkup;
  energyRingCard.readoutRows = readoutRows;
  energyRingCard.rankFlows = rankFlows;
  energyRingCard.dashSeconds = dashSeconds;
  energyRingCard.kpiValue = kpiValue;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyRingCard', energyRingCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
