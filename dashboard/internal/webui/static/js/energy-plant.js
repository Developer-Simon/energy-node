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
  // Role edges are opt-in on the plant view, remembered per browser.
  const ROLE_EDGES_KEY = 'energy-plant-role-edges';
  const readRoleEdges = () => {
    try { return window.localStorage.getItem(ROLE_EDGES_KEY) === '1'; } catch (error) { return false; }
  };
  const writeRoleEdges = on => {
    try { window.localStorage.setItem(ROLE_EDGES_KEY, on ? '1' : '0'); } catch (error) { /* the choice lasts for this page only */ }
  };

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
      roleEdgesOn: false,
      empty: false,
      loading: false,
      _timer: null,
      _pending: false,
      _animator: null,
      _setTimeout: (fn, ms) => setTimeout(fn, ms),
      _offs: [],

      get view() {
        const stored = this.deviceMap.view || {};
        return {edge_style: 'curved', width_by_power: false, ...stored, layers: LAYERS};
      },

      async load() {
        if (!this._offs.length) {
          const on = (target, name, fn) => { target.addEventListener(name, fn); this._offs.push(() => target.removeEventListener(name, fn)); };
          on(window, 'registry-updated', () => this.onRegistryUpdated());
          on(window, 'energy-roles-changed', () => this.load());
          on(window, 'device-map-changed', () => this.load());
          on(window, 'energy-plant-focus', event => this.focusEntity((event.detail || {}).entityId));
          const wake = () => setTimeout(() => this.flowAnimator().start(), 0);
          on(document, 'visibilitychange', wake);
          on(window, 'dashboard-panel-changed', wake);
          this.roleEdgesOn = readRoleEdges();
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
          this.devices = devices || [];
          this.deviceMap = deviceMap || {version: 2, nodes: [], edges: []};
          this.energy = energy;
          this.groups = (roles && roles.groups) || {};
          this.categories = (roles && roles.categories) || {};
          this.iconMarkup = Object.fromEntries((Array.isArray(icons) ? icons : []).map(icon => [icon.name, icon.markup]));
          this.empty = this.devices.every(device => device.id === window.DeviceMapModel.OWN_ENERGY_DEVICE_ID);
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

      balanceId() {
        return window.DeviceMapDataflow.balanceNodeId(new Set(this.devices.map(device => device.id)));
      },

      subDevices() {
        return window.DeviceMapSubDevices.list(this.deviceMap);
      },

      // Role edges from the devices (and sub-devices) to the balance node.
      roleEdgeList() {
        return window.DeviceMapDataflow.roleEdges(this.energy, this.balanceId(), this.deviceMap);
      },

      // Switch in the plant head. The edges are rebuilt in place, the
      // arrangement and the flows stay as they are.
      toggleRoleEdges(on) {
        this.roleEdgesOn = Boolean(on);
        writeRoleEdges(this.roleEdgesOn);
        this.applyEnergy();
      },

      subIsOrphan(sub) {
        return window.DeviceMapSubDevices.isOrphan(sub, this.devices);
      },

      nodeIds() {
        return new Set([...this.devices.map(device => device.id), ...this.subDevices().map(sub => sub.id), ...this.groupIds(), this.balanceId()]);
      },

      // Entities with a sub-device are measured under the sub-device's ID, so
      // the meter keeps the ring of its other entities (as on the map page).
      energyByDevice() {
        return window.DeviceMapModel.deviceEnergy(window.DeviceMapSubDevices.remapSnapshot(this.energy, this.deviceMap));
      },

      // Value of a sub-device node: its entity's reading, or the hint when
      // the meter or the entity is gone.
      subValueText(sub, energy) {
        if (this.subIsOrphan(sub)) return t('devicemap.sub.missing');
        const device = this.devices.find(item => item.id === sub.device_id);
        const entities = ((device && device.entities) || []).filter(entity => entity && entity.unique_id === sub.entity_id);
        return window.DeviceMapModel.nodeValueText({entities}, energy.get(sub.id));
      },

      // Dashed ring in the colour of the first role, the sub-device icon, no
      // health dot (same spec as the map page).
      subSpec(sub, energy) {
        const entry = energy.get(sub.id);
        const role = entry && entry.primaryRole;
        return {
          segments: entry ? entry.roles.map(item => entry.meta[item].color) : [],
          icon: sub.icon ? `cat:${sub.icon}` : 'box',
          iconColor: role ? entry.meta[role].color : 'text-muted',
          health: 'unknown',
          subDevice: true,
          iconMarkup: this.iconMarkup[sub.icon] || '',
        };
      },

      wiringEdges() {
        const wiring = window.DeviceMapGraph.wiringElements({devices: this.devices, deviceMap: this.deviceMap, groups: this.groups, nodeIds: this.nodeIds(), energy: this.energyByDevice()});
        if (!this.view.hide_automation_links) return wiring;
        return wiring.filter(element => !window.DeviceMapDataflow.isAutomationLink({from: element.data.source, to: element.data.target}));
      },

      positions() {
        const model = window.DeviceMapModel;
        const df = window.DeviceMapDataflow;
        const snap = value => value;
        const positions = new Map((this.deviceMap.nodes || []).map(node => [node.device_id || node.virtual_id, {x: node.x, y: node.y}]));
        const all = () => [...positions.values()];
        // Same spots the device map would pick, so a device that is not yet
        // saved there does not land on the origin on top of another one.
        const anchors = this.devices.map(device => positions.get(device.id)).filter(Boolean);
        const balanceId = this.balanceId();
        const missing = this.devices.filter(device => !positions.has(device.id) && device.id !== balanceId).map(device => device.id);
        if (balanceId !== df.BALANCE_ID && !(this.deviceMap.nodes || []).some(node => node.device_id === balanceId) && positions.has(df.BALANCE_ID)) {
          positions.set(balanceId, positions.get(df.BALANCE_ID));
        }
        if (!positions.has(balanceId)) {
          const roleDevices = this.roleEdgeList().map(edge => positions.get(edge.from)).filter(Boolean);
          positions.set(balanceId, df.placeBalance({rolePositions: roleDevices, allPositions: all(), snap}));
        }
        for (const spot of model.placeDevices({ids: missing, allPositions: anchors, snap})) positions.set(spot.id, {x: spot.x, y: spot.y});
        // Sub-devices without a saved spot go right of their meter, or one step
        // right of the origin when the meter is gone (same rule as the map page).
        for (const sub of this.subDevices()) {
          if (positions.has(sub.id)) continue;
          const anchor = positions.get(sub.device_id) || {x: 0, y: 0};
          positions.set(sub.id, window.DeviceMapSubDevices.placeNew({anchor, taken: all(), snap: null}));
        }
        for (const [id, group] of Object.entries(this.groups)) {
          const nodeId = model.GROUP_PREFIX + id;
          if (positions.has(nodeId)) continue;
          const members = [...(group.members.devices || []), ...(group.members.groups || []).map(child => model.GROUP_PREFIX + child)];
          positions.set(nodeId, model.placeGroup({memberPositions: members.map(member => positions.get(member)).filter(Boolean), allPositions: all(), snap}));
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
        const balanceId = this.balanceId();
        const devices = this.devices.map(device => withPosition({
          data: {id: device.id, name: device.name || device.id,
            svg: device.id === balanceId
              ? window.DeviceMapNodeSvg.virtualDataUri('balance', colorOf)
              : window.DeviceMapNodeSvg.dataUri(model.ringSpec(energy.get(device.id), model.deviceHealth(device), (device.entities || []).length > 0, this.iconMarkup), colorOf)},
        }, device.id));
        const subs = this.subDevices().map(sub => withPosition({
          data: {id: sub.id, name: sub.label || sub.id, svg: window.DeviceMapNodeSvg.dataUri(this.subSpec(sub, energy), colorOf)},
          classes: this.subIsOrphan(sub) ? 'devicemap-sub devicemap-sub-orphan' : 'devicemap-sub',
        }, sub.id));
        const groups = Object.entries(this.groups).map(([id, group]) => withPosition({
          data: {id: model.GROUP_PREFIX + id, name: group.label, svg: window.DeviceMapNodeSvg.groupDataUri(colorOf)}, classes: 'devicemap-group',
        }, model.GROUP_PREFIX + id));
        const balance = balanceId === df.BALANCE_ID
          ? graph.virtualElements({kinds: [{id: df.BALANCE_ID, kind: 'balance', name: t('devicemap.balance.name')}], positions, colorOf})
          : [];
        const wiring = this.wiringEdges();
        const flows = graph.flowElements({pairs: graph.pairsOf(wiring), energy, view: this.view, edgeProps: this.deviceMap.edge_props});
        const roles = this.roleEdgesOn ? graph.dataEdgeElements(this.roleEdgeList().filter(edge => this.nodeIds().has(edge.from))) : [];
        return [...devices, ...subs, ...groups, ...balance, ...wiring, ...flows, ...roles];
      },

      labelItems() {
        const model = window.DeviceMapModel;
        const energy = this.energyByDevice();
        const {children, dirOf} = window.DeviceMapWiring.flowContext(window.DeviceMapGraph.pairsOf(this.wiringEdges()), this.deviceMap.edge_props, energy);
        const df = window.DeviceMapDataflow;
        const balanceId = this.balanceId();
        const balanceText = model.balanceValueText(this.energy && this.energy.balance);
        return [
          ...this.devices.map(device => ({id: device.id, name: device.name || device.id,
            value: device.id === balanceId ? balanceText : model.nodeValueText(device, energy.get(device.id))})),
          ...this.subDevices().map(sub => ({id: sub.id, name: sub.label || sub.id, value: this.subValueText(sub, energy)})),
          ...Object.entries(this.groups).map(([id, group]) => ({id: model.GROUP_PREFIX + id, name: group.label, value: model.groupValueText(model.edgeFlow(model.GROUP_PREFIX + id, energy, children, {dirOf}))})),
          ...(balanceId === df.BALANCE_ID ? [{id: df.BALANCE_ID, name: t('devicemap.balance.name'), value: balanceText}] : []),
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
        const dataEdges = this.roleEdgesOn ? this.roleEdgeList() : [];
        const related = id ? df.relatedFocus(id, {wiringPairs: window.DeviceMapGraph.pairsOf(this.wiringEdges()), dataEdges, layers: LAYERS}) : null;
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
