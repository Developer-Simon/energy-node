// Read-only plant view on the energy tab: the device map arrangement with
// the energy and balance layers fixed on, no dragging, no panel. Clicking a
// device flashes its rows in the role table, clicking a row highlights the
// device. Spec section "Energie-Tab".
(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const requestJSON = async url => {
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error((body && body.message) || 'request failed');
    return body;
  };
  const LAYERS = Object.freeze({wiring: false, energy: true, balance: true, data: false});
  const REFRESH_MS = 1000;

  const energyPlantView = () => {
    let cy = null;
    let labels = null;
    return {
      devices: [],
      deviceMap: {version: 2, nodes: [], edges: []},
      energy: null,
      groups: {},
      categories: {},
      iconMarkup: {},
      focusId: null,
      empty: false,
      loading: false,
      _timer: null,
      _pending: false,
      _animator: null,
      _setTimeout: (fn, ms) => setTimeout(fn, ms),
      _offs: [],

      get view() {
        const stored = this.deviceMap.view || {};
        return {edge_style: 'straight', width_by_power: false, ...stored, layers: LAYERS};
      },

      async load() {
        if (!this._offs.length) {
          const on = (target, name, fn) => { target.addEventListener(name, fn); this._offs.push(() => target.removeEventListener(name, fn)); };
          on(window, 'registry-updated', () => this.onRegistryUpdated());
          on(window, 'energy-roles-changed', () => this.load());
          on(window, 'energy-plant-focus', event => this.focusEntity((event.detail || {}).entityId));
          const wake = () => setTimeout(() => this.flowAnimator().start(), 0);
          on(document, 'visibilitychange', wake);
          on(window, 'dashboard-panel-changed', wake);
        }
        this.loading = true;
        try {
          const [devices, deviceMap, energy, roles, icons] = await Promise.all([
            requestJSON('/api/v1/devices'),
            requestJSON('/api/v1/device/map').catch(() => null),
            requestJSON('/api/v1/energy').catch(() => null),
            requestJSON('/api/v1/energy/roles').catch(() => null),
            requestJSON('/api/v1/device/icons').catch(() => []),
          ]);
          this.devices = (devices || []).filter(device => device.id !== window.DeviceMapModel.OWN_ENERGY_DEVICE_ID);
          this.deviceMap = deviceMap || {version: 2, nodes: [], edges: []};
          this.energy = energy;
          this.groups = (roles && roles.groups) || {};
          this.categories = (roles && roles.categories) || {};
          this.iconMarkup = Object.fromEntries((Array.isArray(icons) ? icons : []).map(icon => [icon.name, icon.markup]));
          this.empty = this.devices.length === 0;
          this.render();
        } finally {
          this.loading = false;
        }
      },

      destroy() {
        this._offs.forEach(off => off());
        this._offs = [];
        this.flowAnimator().stop();
        if (labels) { labels.destroy(); labels = null; }
        if (cy) { cy.destroy(); cy = null; }
      },

      isActive() {
        const panel = document.getElementById('energy-panel');
        return Boolean(panel && panel.classList.contains('active')) && document.visibilityState === 'visible';
      },

      onRegistryUpdated() {
        if (!this.isActive()) return;
        if (this._timer) { this._pending = true; return; }
        this.refreshEnergy();
        this._timer = this._setTimeout(() => {
          this._timer = null;
          if (this._pending) { this._pending = false; this.onRegistryUpdated(); }
        }, REFRESH_MS);
      },

      async refreshEnergy() {
        this.energy = await requestJSON('/api/v1/energy').catch(() => null);
        this.applyEnergy();
      },

      groupIds() {
        return Object.keys(this.groups).map(id => window.DeviceMapModel.GROUP_PREFIX + id);
      },

      nodeIds() {
        return new Set([...this.devices.map(device => device.id), ...this.groupIds(), window.DeviceMapDataflow.BALANCE_ID]);
      },

      energyByDevice() {
        return window.DeviceMapModel.deviceEnergy(this.energy);
      },

      wiringEdges() {
        return window.DeviceMapGraph.wiringElements({devices: this.devices, deviceMap: this.deviceMap, groups: this.groups, nodeIds: this.nodeIds()});
      },

      positions() {
        const model = window.DeviceMapModel;
        const df = window.DeviceMapDataflow;
        const snap = value => value;
        const positions = new Map((this.deviceMap.nodes || []).map(node => [node.device_id || node.virtual_id, {x: node.x, y: node.y}]));
        const all = () => [...positions.values()];
        for (const [id, group] of Object.entries(this.groups)) {
          const nodeId = model.GROUP_PREFIX + id;
          if (positions.has(nodeId)) continue;
          const members = [...(group.members.devices || []), ...(group.members.groups || []).map(child => model.GROUP_PREFIX + child)];
          positions.set(nodeId, model.placeGroup({memberPositions: members.map(member => positions.get(member)).filter(Boolean), allPositions: all(), snap}));
        }
        if (!positions.has(df.BALANCE_ID)) {
          const roleDevices = df.roleEdges(this.energy).map(edge => positions.get(edge.from)).filter(Boolean);
          positions.set(df.BALANCE_ID, df.placeBalance({rolePositions: roleDevices, allPositions: all(), snap}));
        }
        return positions;
      },

      colorOf() {
        const cache = {};
        return token => {
          if (!(token in cache)) cache[token] = window.DashboardTheme.color(token);
          return cache[token];
        };
      },

      elements() {
        const model = window.DeviceMapModel;
        const graph = window.DeviceMapGraph;
        const df = window.DeviceMapDataflow;
        const energy = this.energyByDevice();
        const colorOf = this.colorOf();
        const positions = this.positions();
        const withPosition = (element, id) => (positions.get(id) ? {...element, position: positions.get(id)} : element);
        const devices = this.devices.map(device => withPosition({
          data: {id: device.id, name: device.name || device.id,
            svg: window.DeviceMapNodeSvg.dataUri(model.ringSpec(energy.get(device.id), model.deviceHealth(device), (device.entities || []).length > 0, this.iconMarkup), colorOf)},
        }, device.id));
        const groups = Object.entries(this.groups).map(([id, group]) => withPosition({
          data: {id: model.GROUP_PREFIX + id, name: group.label, svg: window.DeviceMapNodeSvg.groupDataUri(colorOf)}, classes: 'devicemap-group',
        }, model.GROUP_PREFIX + id));
        const balance = graph.virtualElements({kinds: [{id: df.BALANCE_ID, kind: 'balance', name: t('devicemap.balance.name')}], positions, colorOf});
        const wiring = this.wiringEdges();
        const flows = graph.flowElements({pairs: graph.pairsOf(wiring), energy, view: this.view});
        const roles = graph.dataEdgeElements(df.roleEdges(this.energy).filter(edge => this.nodeIds().has(edge.from)));
        return [...devices, ...groups, ...balance, ...wiring, ...flows, ...roles];
      },

      labelItems() {
        const model = window.DeviceMapModel;
        const energy = this.energyByDevice();
        const children = model.childrenIndex(window.DeviceMapGraph.pairsOf(this.wiringEdges()));
        const balance = this.energy && this.energy.balance;
        const net = balance ? (Number(balance.grid_import) || 0) - (Number(balance.grid_export) || 0) : null;
        return [
          ...this.devices.map(device => ({id: device.id, name: device.name || device.id, value: model.nodeValueText(device, energy.get(device.id))})),
          ...Object.entries(this.groups).map(([id, group]) => ({id: model.GROUP_PREFIX + id, name: group.label, value: model.groupValueText(model.edgeFlow(model.GROUP_PREFIX + id, energy, children))})),
          {id: 'balance', name: t('devicemap.balance.name'), value: net == null ? '' : t(net >= 0 ? 'devicemap.value.grid_import' : 'devicemap.value.grid_export', {value: model.formatPower(net)})},
        ];
      },

      reducedMotion() {
        return Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      },

      graphStyle() {
        const theme = window.DashboardTheme.colors({line: 'border', accent: 'accent', labelStrong: 'text-strong', panel: 'panel', info: 'info-line', faint: 'text-faint', warn: 'warn-line', subtle: 'text-subtle'});
        const colorOf = this.colorOf();
        return window.DeviceMapGraph.style({view: this.view, theme, color: token => colorOf(token), reducedMotion: this.reducedMotion()});
      },

      flowAnimator() {
        if (!this._animator) {
          this._animator = window.DeviceMapGraph.createFlowAnimator({
            getCy: () => cy,
            shouldAnimate: () => this.isActive() && !this.reducedMotion(),
            requestFrame: fn => (window.requestAnimationFrame ? window.requestAnimationFrame(fn) : null),
            cancelFrame: id => { if (window.cancelAnimationFrame) window.cancelAnimationFrame(id); },
          });
        }
        return this._animator;
      },

      render() {
        if (!this.$refs.canvas || typeof window.cytoscape !== 'function') return;
        if (cy) cy.destroy();
        const hasPositions = (this.deviceMap.nodes || []).length > 0;
        cy = window.cytoscape({
          container: this.$refs.canvas,
          elements: this.elements(),
          style: this.graphStyle(),
          layout: hasPositions ? {name: 'preset', fit: true, padding: 24} : {name: 'breadthfirst', directed: true, padding: 24},
          autoungrabify: true,
          userZoomingEnabled: false,
          userPanningEnabled: false,
          boxSelectionEnabled: false,
        });
        cy.on('tap', 'node', event => this.onNodeTap(event.target.id()));
        cy.on('tap', event => { if (event.target === cy) this.setFocus(null); });
        if (this.$refs.labels && !labels) labels = window.DeviceMapLabels.create(this.$refs.labels);
        if (labels) {
          labels.update(this.labelItems());
          cy.on('render', () => labels.sync(cy));
          labels.sync(cy);
        }
        this.flowAnimator().start();
      },

      applyEnergy() {
        if (!cy) return;
        // A value change rebuilds the elements but keeps the instance, like
        // the device map's applyEnergy(): nodes get new images, flows and
        // role edges are replaced.
        const fresh = new Map(this.elements().map(element => [element.data.id, element]));
        cy.batch(() => {
          cy.nodes().forEach(node => { const next = fresh.get(node.id()); if (next && next.data.svg) node.data('svg', next.data.svg); });
          cy.edges('.devicemap-flow, .devicemap-data').remove();
          for (const element of fresh.values()) {
            if (element.classes && /devicemap-(flow|data)/.test(element.classes)) cy.add({group: 'edges', ...element});
          }
        });
        if (labels) { labels.update(this.labelItems()); labels.sync(cy); }
        if (this.focusId) this.setFocus(this.focusId);
        this.flowAnimator().start();
      },

      setFocus(id) {
        this.focusId = id;
        const df = window.DeviceMapDataflow;
        const related = id ? df.relatedFocus(id, {wiringPairs: window.DeviceMapGraph.pairsOf(this.wiringEdges()), dataEdges: df.roleEdges(this.energy), layers: LAYERS}) : null;
        if (labels) labels.setDimmed(related ? related.nodes : null);
        if (!cy) return;
        cy.batch(() => {
          cy.elements().removeClass('devicemap-dimmed devicemap-focused');
          if (!related) return;
          cy.nodes().forEach(node => { if (!related.nodes.has(node.id())) node.addClass('devicemap-dimmed'); });
          cy.edges().forEach(edge => {
            if (!(related.nodes.has(edge.data('source')) && related.nodes.has(edge.data('target')))) edge.addClass('devicemap-dimmed');
          });
          cy.getElementById(id).addClass('devicemap-focused');
        });
      },

      onNodeTap(id) {
        this.setFocus(id);
        const model = window.DeviceMapModel;
        if (model.isGroupId(id)) {
          window.dispatchEvent(new CustomEvent('energy-focus-group', {detail: {id: id.slice(model.GROUP_PREFIX.length)}}));
          return;
        }
        const device = this.devices.find(candidate => candidate.id === id);
        if (!device) return;
        const ids = (device.entities || []).filter(entity => model.isEligibleUnit(entity.unit_of_measurement)).map(entity => entity.unique_id);
        if (ids.length) window.dispatchEvent(new CustomEvent('energy-focus-rows', {detail: {ids}}));
      },

      focusEntity(entityId) {
        const device = this.devices.find(candidate => (candidate.entities || []).some(entity => entity.unique_id === entityId));
        if (device) this.setFocus(device.id);
      },
    };
  };

  const register = () => {
    if (window.Alpine) window.Alpine.data('energyPlantView', energyPlantView);
  };
  if (window.Alpine) register(); else document.addEventListener('alpine:init', register, {once: true});
})();
