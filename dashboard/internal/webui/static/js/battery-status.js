// Dashboard-Adapter der Batterie-Statuskarte. Er bildet den Energie-
// Schnappschuss auf das BatteryInput des Kerns ab und reicht die IndexedDB
// als Verlaufsleser hinein - mehr nicht. Gerechnet und gezeichnet wird in
// battery-card-core.js, damit dieselbe Karte in Home Assistant laufen kann
// (siehe custom_components/battery_soc/www/battery-soc-card.js).
(() => {
  'use strict';

  const core = window.BatteryCardCore;

  // Reihenfolge der Verdichtungsstufen. Die feinste Stufe, in der ueberhaupt
  // zwei Punkte stehen, gewinnt.
  const TIER_ORDER = ['raw', '1m', '5m'];

  function inputFrom(snapshot, history, nowTs, opts = {}) {
    const model = window.EnergyModel;
    const balance = model.balanceOf(snapshot);
    const interpretation = (snapshot && snapshot.interpretation) || {};
    const reserve = interpretation.battery_reserve_percent;
    return {
      soc: model.hasValue(snapshot, 'battery_soc') ? model.roleValue(snapshot, 'battery_soc') : null,
      capacity: model.roleValue(snapshot, 'battery_capacity_kwh'),
      // + laedt, - entlaedt. balanceOf liefert beide Richtungen getrennt
      // und vorzeichenfrei; der Kern will eine Zahl mit Richtung.
      watts: balance.charge - balance.discharge,
      reserve: Number.isFinite(Number(reserve)) ? Number(reserve) : core.DEFAULT_RESERVE,
      stale: model.isStale(snapshot, 'battery_soc'),
      nowTs,
      history,
      // Zeitfenster der Trajektorie (Saeule ignoriert beides). Undefined
      // laesst dem Kern seinen Sechs-Stunden-Default.
      historyHours: opts.historyHours,
      forecastHours: opts.forecastHours,
    };
  }

  // Liest das Verlaufsfenster (und die optionale abweichende Projektion) vom
  // umschliessenden Layout-Item. data-battery-window traegt die Stundenzahl,
  // data-battery-projection-window ueberschreibt nur die Fortschreibung.
  function windowHours(root) {
    const ds = (root && root.closest && root.closest('[data-layout-item-id]')?.dataset) || {};
    const win = Number(ds.batteryWindow);
    if (!(win > 0)) return {};
    const proj = Number(ds.batteryProjectionWindow);
    return {historyHours: win, forecastHours: proj > 0 ? proj : win};
  }

  // Der Leser, den der Kern bekommt. HistoryStore steht wegen der
  // defer-Reihenfolge in base.html erst nach diesem Skript im Dokument -
  // deshalb wird es hier bei jedem Aufruf frisch nachgeschlagen und nicht
  // beim Laden festgehalten.
  function historyReader() {
    return async (fromTs, toTs) => {
      const store = window.HistoryStore;
      if (!store) return [];
      for (const tier of TIER_ORDER) {
        // eslint-disable-next-line no-await-in-loop
        const rows = await store.readRange(tier, 'role:battery_soc', fromTs, toTs);
        if ((rows || []).length >= 2) return rows;
      }
      return [];
    };
  }

  // Gemeinsamer Rumpf beider Karten. mount ist die Kernfunktion, die das
  // Geruest baut; withHistory schaltet das Nachladen des Verlaufs zu (nur
  // die Trajektorie zeichnet ihn).
  function cardFactory(mount, withHistory) {
    return () => {
      let timer = null;
      let card = null;
      return {
        snapshot: null,
        history: [],
        presenter: null,
        snapshotOverride: null,
        win: {},

        init() {
          core.injectStyle(document);
          this.win = windowHours(this.$root);
          card = mount(this.$root);
          const raw = this.snapshotOverride || window.EnergyModel.readEmbeddedSnapshot('energy-status-initial');
          const reducedMotion = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
          this.presenter = window.EnergyPresentation.present({
            key: (this.$root.closest('[data-layout-item-id]')?.dataset.layoutItemId) || 'battery-card',
            raw,
            reducedMotion,
            alive: () => !!(this.$root && this.$root.isConnected),
            apply: presented => { this.snapshot = presented; this.paint(); },
          });
          if (withHistory) {
            this.refreshHistory();
            // Der Verlauf aendert sich im Minutenraster; oefter zu lesen
            // kostet IndexedDB-Zugriffe und zeigt dasselbe Bild.
            timer = window.setInterval(() => this.refreshHistory(), 60000);
          }
        },

        destroy() {
          if (this.presenter) this.presenter.release();
          if (timer) window.clearInterval(timer);
          if (card && card.destroy) card.destroy();
        },

        async refreshHistory() {
          const now = Date.now();
          const spanMs = (this.win.historyHours > 0 ? this.win.historyHours : 6) * 3600000;
          this.history = await core.readHistory(historyReader(), now - spanMs, now);
          this.paint();
        },

        paint() {
          if (!this.snapshot || !card) return;
          card.update(core.viewFrom(inputFrom(this.snapshot, this.history, Date.now(), this.win)));
        },
      };
    };
  }

  const batteryColumnCard = cardFactory(root => core.mountColumn(root), false);
  const batteryTrajectoryCard = cardFactory(root => core.mountTrajectory(root), true);

  [batteryColumnCard, batteryTrajectoryCard].forEach(factory => {
    Object.assign(factory, {inputFrom, historyReader, windowHours});
  });

  const register = () => {
    if (!window.Alpine) return;
    window.Alpine.data('batteryColumnCard', batteryColumnCard);
    window.Alpine.data('batteryTrajectoryCard', batteryTrajectoryCard);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
