// "Anlagenschema": a single-line electrical diagram - one busbar, branch y
// positions that never move regardless of values, power as a number at the
// branch. Port of Vorschlag E from the six-proposals exploration; its
// layout-editor options (stroke_mode, entity_labels, hide_inactive,
// display_size, animate) are read from the layout item's data-* attributes,
// see knowhow/dashboard/energiegrafiken-konfiguration-backlog.md. Entity IDs
// for "Leistung + Entität" come from Snapshot.Sources (role -> entity
// unique_id list), already present on the embedded snapshot; "base" (übrige
// Verbraucher) has no single source entity - it is load minus wallbox minus
// heat_pump - so it keeps the prototype's literal "abgeleitet" note instead.
//
// Since the "fließende Skalierung" spec (2026-08-22) the x-geometry is a
// function of the measured container width W (see schemaGeometry()) - only
// the y-values (BUS_TOP/BUS_BOT, branch.y) stay fixed. The busbar carries
// the sources' mix stacked by height, and every consuming branch gets a
// short bar at its bus end showing which sources cover it - allocate() in
// energy-model.js does that Quelle->Verbraucher assignment.
(() => {
  const BUS_TOP = 92;
  const BUS_BOT = 312;
  const BUS_WIDTH = 14;
  const MIN_LINE = 26;
  const GRID_LINE_GAP = 33; // Netzanschluss-Kasten bis BUS_TOP, wie im vorigen festen Layout (59 -> 92)
  const DEFAULT_WIDTH = 980;
  const HEIGHT = 384;

  const SIZE_FACTORS = {xs: 0.86, s: 0.93, m: 1.00, l: 1.09, xl: 1.18};

  // Zwei feste y-Koordinatentabellen statt einer, weil der Kombiniert-Modus
  // rechts einen vierten Zweig braucht. Die Zusage der Karte bleibt: das Bild
  // bewegt sich nicht, was immer die Werte tun. Es wechselt nur, wenn der
  // Nutzer den load_mode aendert - und dann sichtbar und vollstaendig.
  const BRANCHES = [
    {id: 'pv', side: 'left', y: 122, label: 'PV-Wechselrichter'},
    {id: 'battery', side: 'left', y: 246, label: 'Batteriespeicher'},
    {id: 'wallbox', side: 'right', y: 122, label: 'Wallbox'},
    {id: 'heat_pump', side: 'right', y: 196, label: 'Wärmepumpe'},
    {id: 'base', side: 'right', y: 276, label: 'Übrige Verbraucher'},
  ];

  // Vier Kaesten a 46px zwischen BUS_TOP (92) und BUS_BOT (312): 122/176/230/284
  // laesst 8px Luft zwischen den Kaesten und endet bei 307, knapp ueber der
  // Sammelschienen-Unterkante.
  const BRANCHES_WITH_MEASURED = [
    {id: 'pv', side: 'left', y: 122, label: 'PV-Wechselrichter'},
    {id: 'battery', side: 'left', y: 246, label: 'Batteriespeicher'},
    {id: 'wallbox', side: 'right', y: 122, label: 'Wallbox'},
    {id: 'heat_pump', side: 'right', y: 176, label: 'Wärmepumpe'},
    {id: 'load_measured', side: 'right', y: 230, label: 'Gemessene Verbraucher'},
    {id: 'base', side: 'right', y: 284, label: 'Übrige Verbraucher'},
  ];

  const branchesFor = hasMeasured => (hasMeasured ? BRANCHES_WITH_MEASURED : BRANCHES);

  function clamp(min, value, max) {
    return Math.min(Math.max(value, min), max);
  }

  // Section 1+2 der Skalierungs-Spec: leitet jedes x-Mass aus der gemessenen
  // Containerbreite W und dem Groessenfaktor der Darstellungsgroesse ab. Die
  // 26px Mindestlinie ist die einzige harte Untergrenze - reicht der Platz
  // nicht, gibt der Kasten nach, nicht die Linie.
  function schemaGeometry(W, size) {
    const margin = clamp(6, W * 0.030, 42);
    const fontSize = clamp(9.5, W * 0.0145, 13.5) * size;
    const boxHeight = clamp(30, fontSize * 3.5, 52);
    const busX = Math.round(W / 2);
    const half = Math.min(busX, W - busX) - BUS_WIDTH / 2 - margin;
    const idealBoxWidth = Math.min(clamp(94, W * 0.215, 196) * size, W * 0.30);
    const boxWidth = Math.max(Math.min(idealBoxWidth, half - MIN_LINE), 0);
    return {W, margin, fontSize, boxHeight, busX, busWidth: BUS_WIDTH, boxWidth};
  }

  function branchValues(snapshot, balance) {
    const batteryNet = balance.charge - balance.discharge;
    return {
      pv: {value: window.EnergyModel.roleValue(snapshot, 'pv'), dir: 'in'},
      battery: {value: Math.abs(batteryNet), dir: batteryNet > 0.5 ? 'out' : 'in'},
      wallbox: {value: balance.wallbox, dir: 'out'},
      heat_pump: {value: balance.heatPump, dir: 'out'},
      base: {value: balance.base, dir: 'out'},
      load_measured: {value: balance.measured, dir: 'out'},
    };
  }

  // battery_soc only appears in snapshot.values when at least one entity
  // carries a capacity_kwh (see energie-interpretation.md) - no key means no
  // configured storage, so the label stays empty rather than showing "0 %".
  // Rounded to a whole percent, same convention as energy-flow.js's battery
  // node text, so both graphics read consistently.
  function batterySocLabel(snapshot) {
    const model = window.EnergyModel;
    if (!model.hasValue(snapshot, 'battery_soc')) return {label: '', stale: false};
    return {
      label: `${Math.round(model.roleValue(snapshot, 'battery_soc'))} %`,
      stale: model.isStale(snapshot, 'battery_soc'),
    };
  }

  // stroke-width: "power" (default) is proportional, 1.6..7px on a sqrt
  // curve so small branches stay visible next to a much bigger one; "const"
  // is a fixed 2.4px regardless of value.
  function widthFor(value, peak, strokeMode) {
    if (strokeMode === 'const') return 2.4;
    return 1.6 + 5.4 * Math.sqrt(Math.min(value / Math.max(peak, 1), 1));
  }

  // role -> entity unique_id list for the "Leistung + Entität" label mode.
  // "base" (übrige Verbraucher) is load minus wallbox minus heat_pump, a
  // derived quantity with no single source entity of its own.
  function branchEntities(snapshot, branchID) {
    const sources = (snapshot && snapshot.sources) || {};
    if (branchID === 'base') return null;
    if (branchID === 'load_measured') return sources.load || [];
    if (branchID === 'battery') return [...(sources.battery || []), ...(sources.battery_charge || []), ...(sources.battery_discharge || [])];
    return sources[branchID] || [];
  }

  // Arrow direction: "in" points from the branch box toward the busbar,
  // "out" points from the busbar toward the box. x0/x1 always run in the
  // direction "weg vom Kasten" -> das haelt branchBarSegments()/arrowSegment()
  // einfach: fuer "left" ist x0 immer die Kasten-, x1 die Busseite, und
  // umgekehrt fuer "right".
  function branchGeometry(branch, value, peak, stale, strokeMode, geom) {
    const left = branch.side === 'left';
    const boxX = left ? geom.margin : geom.W - geom.margin - geom.boxWidth;
    const busEdge = geom.busX + (left ? -1 : 1) * (geom.busWidth / 2);
    const x0 = left ? boxX + geom.boxWidth : busEdge;
    const x1 = left ? busEdge : boxX;
    const active = value.value > 0.5;
    return {
      id: branch.id, label: branch.label, y: branch.y, boxX, boxWidth: geom.boxWidth, boxHeight: geom.boxHeight,
      fontSize: geom.fontSize, busX: geom.busX, x0, x1, active, stale, left, dir: value.dir,
      strokeWidth: active ? widthFor(value.value, peak, strokeMode) : 1.2,
      labelX: (x0 + x1) / 2,
    };
  }

  // Laenge/Segmente des Verbraucherbalkens (Spec Abschnitt 4). Der Balken
  // ersetzt das erste (busnahe) Stueck der Zweiglinie - die Segmente sind
  // vom Bus aus nach aussen (Richtung Kasten) gestapelt.
  function branchBarSegments(bg, barLen, mix, value, barHeight) {
    if (!mix || !mix.length || value <= 0) return [];
    const scale = barLen / value;
    let offset = 0;
    return mix.map(m => {
      const w = Math.max(m.v * scale, 0);
      const x = bg.left ? bg.x1 - offset - w : bg.x0 + offset;
      offset += w;
      return {x, y: bg.y - barHeight / 2, w, h: barHeight, color: m.color};
    });
  }

  // Der freie (unbelegte) Teil der Zweiglinie, ueber den der Pfeil laeuft -
  // hinter dem Verbraucherbalken los, bei Erzeugern (kein Balken) die ganze
  // Linie. Reihenfolge (from -> to) ist die Flussrichtung.
  function arrowSegment(bg, barLen) {
    if (bg.dir === 'in') return bg.left ? {from: bg.x0, to: bg.x1} : {from: bg.x1, to: bg.x0};
    return bg.left ? {from: bg.x1 - barLen, to: bg.x0} : {from: bg.x0 + barLen, to: bg.x1};
  }

  function offsetPoints(points, dx, dy) {
    return points.split(' ').map(pair => {
      const [x, y] = pair.split(',').map(Number);
      return `${x + dx},${y + dy}`;
    }).join(' ');
  }

  // Zwei geschachtelte <g>: aussen das statische transform (Startpunkt),
  // innen die per CSS animierte Gruppe - eine CSS-Animation, die transform
  // setzt, wuerde sonst ein transform-Attribut auf demselben Element
  // ueberschreiben und der Pfeil startete bei (0,0). Bleibt weniger als 10px
  // freier Weg, wird nicht animiert, sondern der Pfeil mittig gesetzt.
  function arrowMarkup(branch, suppressAnimation, key) {
    if (!branch.active) return '';
    const {arrowFrom: from, arrowTo: to, animateDuration: duration, color, y} = branch;
    const dx = to - from;
    const len = Math.abs(dx);
    const tri = dx >= 0 ? '-7,-6 -7,6 7,0' : '7,-6 7,6 -7,0';
    if (suppressAnimation || len < 10) {
      if (key && window.EnergyPresentation) window.EnergyPresentation.pauseDash(key, `schema:${branch.id}`);
      const mid = (from + to) / 2;
      return `<polygon points="${offsetPoints(tri, mid, y)}" fill="${color}"></polygon>`;
    }
    const delay = key && window.EnergyPresentation
      ? `;animation-delay:${window.EnergyPresentation.dashDelay(key, `schema:${branch.id}`, duration)}`
      : '';
    return `<g transform="translate(${from} ${y})"><g class="energy-schema-flow-anim" style="--dx:${dx}px;animation-duration:${duration}s${delay}"><polygon points="${tri}" fill="${color}"></polygon></g></g>`;
  }

  // <template x-for> doesn't clone inside <svg> (browsers parse a <template>
  // found in foreign/SVG content as a plain SVG element with no .content
  // DocumentFragment, so Alpine's document.importNode(el.content, ...)
  // throws) - branchesMarkup() instead renders the repeated elements to an
  // SVG markup string the template binds once via x-html on a plain <g>.
  // entity_labels "entity" adds a small entity-ID line under the branch
  // label - branch.entityLabel is precomputed by compute() (undefined when
  // entity_labels is "power", the default).
  function branchesMarkup(branches, suppressAnimation, key) {
    const theme = window.DashboardTheme.colors({bad: 'bad', text: 'text', faint: 'text-faint', box: 'panel-alt', line: 'border'});
    return branches.map(branch => {
      const arrow = arrowMarkup(branch, suppressAnimation, key);
      const labelColor = branch.stale ? theme.bad : (branch.active ? theme.text : theme.faint);
      const top = branch.y - branch.boxHeight / 2;
      const labelY = top + branch.fontSize + 3;
      const secondLineY = labelY + branch.fontSize * 0.9;
      const socLine = branch.socLabel
        ? `<text x="${branch.boxX + 12}" y="${secondLineY}" font-size="${branch.fontSize * 0.88}" fill="${branch.socStale ? theme.bad : (branch.active ? theme.text : theme.faint)}">${branch.socLabel}</text>`
        : '';
      const entityLine = branch.entityLabel
        ? `<text x="${branch.boxX + 12}" y="${secondLineY}" font-size="${branch.fontSize * 0.77}" fill="${theme.faint}">${branch.entityLabel}</text>`
        : '';
      const bars = (branch.barSegments || []).map(s => `<rect x="${s.x}" y="${s.y}" width="${s.w}" height="${s.h}" fill="${s.color}"></rect>`).join('');
      return `<g><line x1="${branch.x0}" y1="${branch.y}" x2="${branch.x1}" y2="${branch.y}" stroke="${branch.color}" stroke-width="${branch.strokeWidth}" stroke-dasharray="${branch.active ? 'none' : '4 6'}"></line>` +
        bars +
        `<circle cx="${branch.busX}" cy="${branch.y}" r="4" fill="${branch.color}"></circle>${arrow}` +
        `<text x="${branch.labelX}" y="${branch.y - branch.boxHeight / 2 - 8}" text-anchor="middle" font-size="${branch.fontSize * 0.96}" fill="${labelColor}">${branch.valueLabel}</text>` +
        `<rect x="${branch.boxX}" y="${top}" width="${branch.boxWidth}" height="${branch.boxHeight}" rx="3" fill="${theme.box}" stroke="${branch.color}" stroke-width="${branch.stale ? 1 : (branch.active ? 1.6 : 1)}"></rect>` +
        `<rect x="${branch.boxX}" y="${top}" width="3" height="${branch.boxHeight}" fill="${branch.color}"></rect>` +
        `<text x="${branch.boxX + 12}" y="${labelY}" font-size="${branch.fontSize}" fill="${branch.active ? theme.text : theme.faint}">${branch.label}</text>${socLine}${entityLine}</g>`;
    }).join('');
  }

  // Reihenfolge PV -> Speicherentladung -> Netzbezug fuer die Schienen-
  // Bilanz (Spec Abschnitt 4) - unabhaengig von composeBalance()s Reihenfolge
  // (dort steht grid_import vor battery_discharge).
  const SOURCE_STACK_ORDER = {pv: 0, battery_discharge: 1, grid_import: 2};
  function orderSources(sources) {
    return [...sources].sort((a, b) => (SOURCE_STACK_ORDER[a.id] ?? 3) - (SOURCE_STACK_ORDER[b.id] ?? 3));
  }

  // Section 4, erster Punkt: die Schiene traegt den Gesamtmix, gestapelt
  // proportional zum Durchsatz ueber ihre volle Hoehe (BUS_TOP..BUS_BOT).
  function busMixMarkup(sources, geom, total) {
    const x = geom.busX - geom.busWidth / 2;
    const height = BUS_BOT - BUS_TOP;
    const ordered = orderSources(sources);
    if (!ordered.length) return `<rect x="${x}" y="${BUS_TOP}" width="${geom.busWidth}" height="${height}" rx="2" fill="var(--track)"></rect>`;
    let y = BUS_TOP;
    return ordered.map(s => {
      const h = Math.max(height * (s.value / total), 0);
      const rect = `<rect x="${x}" y="${y}" width="${geom.busWidth}" height="${h}" fill="${s.color}"></rect>`;
      y += h;
      return rect;
    }).join('');
  }

  // Section 4, dritter Punkt: die Einspeisung ist ein Verbraucher ohne
  // eigenen Zweig - ihr Balken steht senkrecht ueber der Schiene, unter dem
  // Netzanschluss. Dieselbe "linienlaenge - 12"-Regel wie bei den
  // Zweigbalken haelt ihn vom Netzanschluss-Kasten fern, damit die
  // Mix-Farben dort nicht hineinlaufen.
  function exportBarMarkup(entry, geom, total) {
    if (!entry || entry.v <= 0.5 || !entry.mix.length) return '';
    const barMax = clamp(14, Math.min(geom.W * 0.14, GRID_LINE_GAP - 12), 130);
    if (barMax <= 0) return '';
    const length = Math.max(barMax * (entry.v / total), 6);
    const barW = clamp(9, geom.fontSize * 0.95, 15);
    const scale = length / entry.v;
    const x = geom.busX - barW / 2;
    let offset = 0;
    return entry.mix.map(m => {
      const h = Math.max(m.v * scale, 0);
      const y = BUS_TOP - offset - h;
      offset += h;
      return `<rect x="${x}" y="${y}" width="${barW}" height="${h}" fill="${m.color}"></rect>`;
    }).join('');
  }

  // Section 4, letzter Punkt: der Gesamtmix als Legende unter dem Durchsatz.
  // Passen die Namen nicht in die Breite, fallen sie weg - Farbflaeche und
  // Prozent bleiben immer.
  function legendMarkup(sources, geom, total) {
    const ordered = orderSources(sources);
    if (!ordered.length) return '';
    const showNames = geom.W >= ordered.length * 90 + 160;
    const itemW = showNames ? 118 : 46;
    const gap = 10;
    const totalW = ordered.length * itemW + (ordered.length - 1) * gap;
    let x = geom.busX - totalW / 2;
    const theme = window.DashboardTheme.colors({text: 'text', muted: 'text-muted'});
    const y = 368;
    return ordered.map(s => {
      const pct = window.EnergyModel.formatPercent(s.value / total);
      const nameText = showNames ? `<text x="${x + 16}" y="${y + 4}" font-size="10.5" fill="${theme.text}">${s.label}</text>` : '';
      const pctY = showNames ? y + 16 : y + 4;
      const pctText = `<text x="${x + 16}" y="${pctY}" font-size="10" fill="${theme.muted}">${pct}</text>`;
      const swatch = `<rect x="${x}" y="${y - 8}" width="10" height="10" rx="2" fill="${s.color}"></rect>`;
      const item = `<g>${swatch}${nameText}${pctText}</g>`;
      x += itemW + gap;
      return item;
    }).join('');
  }

  const energySchemaCard = () => {
    let resizeObserver = null;
    let lastWidth = null;

    return {
      snapshot: null,
      branches: [],
      branchesMarkup: '',
      busMixMarkup: '',
      exportBarMarkup: '',
      legendMarkup: '',
      grid: {active: false, stale: false, strokeWidth: 1.2, label: 'ruht', arrowPoints: ''},
      throughputLabel: '',
      description: '',
      width: DEFAULT_WIDTH,
      geom: schemaGeometry(DEFAULT_WIDTH, 1),
      reducedMotion: false,
      cardKey: 'energy-schema',
      options: {strokeMode: 'power', entityLabels: 'power', hideInactive: 'off', displaySize: 'm', animate: 'off'},
      presenter: null,

      init() {
        this.reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
        const data = this.$root.closest('[data-layout-item-id]')?.dataset || {};
        this.cardKey = data.layoutItemId || 'energy-schema';
        this.options = {
          strokeMode: data.strokeMode || 'power',
          entityLabels: data.entityLabels || 'power',
          hideInactive: data.hideInactive || 'off',
          displaySize: data.displaySize || 'm',
          animate: data.animate || 'off',
        };
        const raw = window.EnergyModel.readEmbeddedSnapshot('energy-schema-initial');
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

      // Setzt Breite und viewBox aus der gemessenen Containerbreite neu und
      // baut die Geometrie komplett neu auf. Laeuft bedingungslos (init,
      // Themewechsel); handleResize() ist der ueber ResizeObserver bewachte
      // Einstiegspunkt, der Sub-Pixel-Zittern verwirft.
      recompute() {
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        lastWidth = Math.round(width);
        this.width = lastWidth;
        if (this.$refs.svg) this.$refs.svg.setAttribute('viewBox', `0 0 ${lastWidth} ${HEIGHT}`);
        this.compute();
      },

      handleResize() {
        const width = (this.$refs.canvas && this.$refs.canvas.clientWidth) || DEFAULT_WIDTH;
        if (Math.round(width) === lastWidth) return;
        this.recompute();
      },

      compute() {
        const model = window.EnergyModel;
        if (!this.snapshot) return;
        const balance = model.balanceOf(this.snapshot);
        const branchDefs = branchesFor(balance.measured > 0.5);
        const values = branchValues(this.snapshot, balance);
        const peak = Math.max(...Object.values(values).map(v => v.value), balance.gridImport, balance.gridExport, 1);
        const staleRoles = {
          pv: ['pv'], battery: ['battery', 'battery_charge', 'battery_discharge'],
          wallbox: ['wallbox'], heat_pump: ['heat_pump'], load_measured: ['load'], base: ['load'],
        };
        const colorFor = {pv: model.COLORS.pv, battery: model.COLORS.batteryCharge, wallbox: model.COLORS.wallbox, heat_pump: model.COLORS.heatPump, load_measured: model.COLORS.loadMeasured, base: model.COLORS.base};
        const theme = window.DashboardTheme.colors({bad: 'bad', line: 'border'});
        const soc = batterySocLabel(this.snapshot);
        const withEntities = this.options.entityLabels === 'entity';
        const size = SIZE_FACTORS[this.options.displaySize] || 1;
        const geom = schemaGeometry(this.width, size);
        this.geom = geom;

        const allocation = model.allocate(balance, 'merit');
        const mixByID = new Map(allocation.map(a => [a.id, a.mix]));
        const suppressAnimation = this.reducedMotion || this.options.animate !== 'on';

        this.branches = branchDefs
          .map(branch => {
            const value = values[branch.id];
            const stale = staleRoles[branch.id].some(role => model.isStale(this.snapshot, role));
            const bg = branchGeometry(branch, value, peak, stale, this.options.strokeMode, geom);
            let entityLabel = '';
            if (withEntities) {
              const entities = branchEntities(this.snapshot, branch.id);
              entityLabel = entities === null ? 'abgeleitet aus Hausverbrauch' : entities.length ? entities.join(', ') : '';
            }
            const color = bg.stale ? theme.bad : bg.active ? colorFor[branch.id] : theme.line;
            const consumerID = branch.id === 'battery' ? 'battery_charge' : branch.id;
            const mix = value.dir === 'out' && bg.active ? mixByID.get(consumerID) : null;
            const lineLen = Math.abs(bg.x1 - bg.x0);
            const barMax = clamp(14, Math.min(geom.W * 0.14, lineLen - 12), 130);
            const barLen = mix && mix.length && barMax > 0 ? Math.max(barMax * (value.value / balance.total), 6) : 0;
            const barHeight = clamp(9, geom.fontSize * 0.95, 15);
            const barSegments = branchBarSegments(bg, barLen, mix, value.value, barHeight);
            const {from, to} = arrowSegment(bg, barLen);
            const duration = clamp(1.0, 3.6 - 2.5 * Math.sqrt(value.value / peak), 3.6);
            return {
              ...bg,
              color, valueLabel: stale ? 'veraltet' : bg.active ? model.formatPower(value.value) : '—',
              socLabel: branch.id === 'battery' ? soc.label : '',
              socStale: branch.id === 'battery' ? soc.stale : false,
              entityLabel, barSegments,
              arrowFrom: from, arrowTo: to, animateDuration: duration,
            };
          })
          .filter(branch => branch.active || this.options.hideInactive !== 'on');
        this.branchesMarkup = branchesMarkup(this.branches, suppressAnimation, this.cardKey);
        this.busMixMarkup = busMixMarkup(balance.sources, geom, balance.total);
        this.legendMarkup = legendMarkup(balance.sources, geom, balance.total);

        const gridActive = balance.gridImport > 0.5 || balance.gridExport > 0.5;
        const gridStale = balance.unbalanced;
        const gridPower = Math.max(balance.gridImport, balance.gridExport);
        const gp = geom.busX;
        this.grid = {
          active: gridActive,
          stale: gridStale,
          strokeWidth: gridActive ? widthFor(gridPower, peak) : 1.2,
          down: balance.gridImport > 0.5,
          label: gridActive ? `${balance.gridImport > 0.5 ? 'Bezug ' : 'Einspeisung '}${model.formatPower(gridPower)}` : 'ruht',
          arrowPoints: gridActive ? (balance.gridImport > 0.5 ? `${gp - 6},72 ${gp + 6},72 ${gp},84` : `${gp - 6},84 ${gp + 6},84 ${gp},72`) : '',
        };

        const exportEntry = allocation.find(a => a.id === 'grid_export');
        this.exportBarMarkup = exportBarMarkup(exportEntry, geom, balance.total);

        this.throughputLabel = `${model.formatPower(balance.total)} Durchsatz`;
        this.description = `Einlinien-Schaltbild: ${branchDefs.map(b => `${b.label} ${values[b.id].value > 0.5 ? model.formatPower(values[b.id].value) : 'ruht'}`).join(', ')}.`;
      },
    };
  };

  energySchemaCard.BRANCHES = BRANCHES;
  energySchemaCard.branchesFor = branchesFor;
  energySchemaCard.branchValues = branchValues;
  energySchemaCard.branchEntities = branchEntities;
  energySchemaCard.widthFor = widthFor;
  energySchemaCard.branchGeometry = branchGeometry;
  energySchemaCard.branchesMarkup = branchesMarkup;
  energySchemaCard.batterySocLabel = batterySocLabel;
  energySchemaCard.schemaGeometry = schemaGeometry;
  energySchemaCard.branchBarSegments = branchBarSegments;
  energySchemaCard.arrowSegment = arrowSegment;
  energySchemaCard.arrowMarkup = arrowMarkup;
  energySchemaCard.SIZE_FACTORS = SIZE_FACTORS;

  const register = () => {
    if (window.Alpine) window.Alpine.data('energySchemaCard', energySchemaCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
