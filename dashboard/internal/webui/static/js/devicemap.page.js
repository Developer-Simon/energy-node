(() => {
  const t = (key, params) => (window.I18n ? window.I18n.t(key, params) : key);
  const tn = (key, n, params) => (window.I18n ? window.I18n.tn(key, n, params) : key);
  const apiError = (body, fallbackKey) => (window.I18n ? window.I18n.error(body, fallbackKey) : (body && body.message) || fallbackKey || 'common.request_failed');

  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    if (response.status === 204) return null;
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(apiError(body));
    return body;
  };

  const ENERGY_REFRESH_MS = 1000;

  // Switch fade timing (draft motion table): 250 ms per node, 60 ms per hop
  // below the switch, reduced motion 150 ms for all nodes at once.
  const SWITCH_FADE_MS = 250;
  const SWITCH_HOP_MS = 60;
  const SWITCH_REDUCED_MS = 150;
  // The state of a node that is neither switched off nor without power.
  const NO_SWITCH = Object.freeze({quiet: false, off: false, hops: 0});
  // Switch port states; any state other than on or off counts as unknown.
  const PORT_STATES = ['on', 'off', 'unknown'];
  const portState = state => (state === 'on' || state === 'off' ? state : 'unknown');

  const DEFAULT_VIEW = {snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'curved', width_by_power: false};

  // Browser storage of the local view. Reading the property itself can throw
  // (blocked storage), then null goes on and DeviceMapViewPrefs keeps nothing.
  const viewStorage = () => {
    try {
      return window.localStorage;
    } catch (error) {
      return null;
    }
  };

  // Synthetic id for the snap-preview node (see onNodeDrag()) - never a real
  // device_id, so it can't collide with one.
  const SNAP_GHOST_ID = '__devicemap-snap-ghost__';

  const statusClass = device => `devicemap-status-${window.DeviceMapModel.deviceHealth(device)}`;

  // Channels of a sub-device form, the same bound as the server's maximum.
  const SUB_CHANNEL_MAX = 16;
  // Icon of a new sub-device when its meter has no icon of its own.
  const SUB_FALLBACK_ICON = 'energy-node:plug-smart';

  const devicemapPanel = () => {
    // Kept outside the returned (Alpine-reactive) object on purpose: a
    // Cytoscape instance is a large mutable object graph that Alpine would
    // otherwise deep-proxy, which is unnecessary work and a likely source of
    // subtle bugs (Cytoscape mutates itself heavily on every render/drag).
    let cy = null;
    let labels = null;
    // Switch state as last committed on the map: id -> {quiet, off, hops}.
    // A running fade only moves a node here when it completes.
    let switchShown = new Map();
    // Port state of each switched wiring edge (relation key -> port state), as
    // last applied with the goal. Updated together with the goal.
    let switchPorts = new Map();
    // Goal of the last applySwitchState() call; an unchanged goal keeps running fades.
    let switchGoal = null;
    let switchGen = 0;
    let switchTimers = [];

    return {
      ...window.DevicePicker.mixin(),
      devices: [],
      deviceMap: {version: 1, nodes: [], edges: []},
      // Snapshot of the last state confirmed with the server (by load() or a
      // successful save()/relation change) - discardChanges() restores this
      // instead of re-fetching, same shape as config.page.js's resetForm().
      savedDeviceMap: {version: 1, nodes: [], edges: []},
      loading: false,
      saving: false,
      connectMode: false,
      connectSourceId: null,
      connectSourceLabel: '',
      selectedEdge: null, // {id, source, target, overrideId, membership} | null
      viewMenuOpen: false,
      unsaved: false,
      // Browser-side view settings that differ from the server default, see
      // setView(). Sparse, so a default changed on the server still reaches this browser.
      localView: {},
      // Sub-device form while open: {mode, subId, deviceId, entityId, name, icon, channel, error}.
      subForm: null,
      subSaving: false,
      subDevicesOff: null,
      placedCount: 0,
      themeOff: null,
      energy: null,
      energyUnavailable: false,
      switches: [], // Answer of /api/v1/devices/switches, kept when a request fails
      flows: null, // {nodes, edges, unresolved} from /api/v1/device/map/flows, null while unavailable
      flowsUnavailable: false,
      invalidConfigs: 0,
      popover: null, // {eyebrow, title, rows, lists, text, linkText, link, action, left, top, origin, open} | null
      edgeFixedOpen: false, // the fixed direction disclosure of a wiring card, toggled by the user
      focusId: null,
      focusRelated: new Set(),
      panelId: null,
      drafts: {},
      savedAssignments: {},
      savedHidden: new Set(), // entity ids the role table hides, see DeviceMapModel.panelRows
      savedGroups: {},
      savedCategories: {},
      iconMarkup: {},
      iconCatalogue: [],
      groupName: '',
      groupDraft: null,
      // The group panel's switch "count as a whole" is on, but no consumer
      // category exists yet to count as.
      groupWholePending: false,
      categoryForGroup: false,
      canEditEnergy: true,
      canEditMap: true,
      canEditMapView: true,
      filterMenuOpen: false,
      csrfToken: '',
      panelSaving: false,
      registryOff: null,
      _energyTimer: null,
      _energyPending: false,
      _setTimeout: (fn, ms) => setTimeout(fn, ms),
      _clearTimeout: id => clearTimeout(id),
      _cyForTest: null,
      _requestFrame: fn => (window.requestAnimationFrame ? window.requestAnimationFrame(fn) : null),
      _cancelFrame: id => { if (window.cancelAnimationFrame) window.cancelAnimationFrame(id); },
      lifecycleOff: null,

      async load() {
        // load() läuft nach jedem Speichern erneut, die Registrierung darf
        // sich deshalb nicht stapeln. cy.style() statt renderGraph(), weil
        // ein voller Neuaufbau die Knotenpositionen neu berechnen würde.
        if (!this.themeOff) {
          this.themeOff = window.DashboardTheme.onChange(() => {
            if (cy) cy.style(this.graphStyle());
            this.applyEnergy();
          });
        }
        if (!this.registryOff) {
          const listener = () => this.onRegistryUpdated();
          window.addEventListener('registry-updated', listener);
          this.registryOff = () => window.removeEventListener('registry-updated', listener);
        }
        if (!this.energyRolesOff) {
          // Roles, groups and categories saved on the energy page. The map's
          // own PATCH already updated everything here, so its event is skipped.
          const listener = event => {
            if ((event.detail || {}).source === 'devicemap') return;
            this.reloadEnergyState();
          };
          window.addEventListener('energy-roles-changed', listener);
          this.energyRolesOff = () => window.removeEventListener('energy-roles-changed', listener);
        }
        if (!this.subDevicesOff) {
          // Sub-devices changed outside this page. The page's own writes merge
          // their answer directly, so only events from elsewhere refetch.
          const listener = event => {
            if ((event.detail || {}).source === 'devicemap') return;
            this.refreshSubDevices();
          };
          window.addEventListener('device-map-changed', listener);
          this.subDevicesOff = () => window.removeEventListener('device-map-changed', listener);
        }
        if (!this.lifecycleOff) {
          // Restart the flow loop when the browser tab or the dashboard tab
          // becomes visible again. The loop stops itself while hidden.
          const wake = () => setTimeout(() => this.startFlowAnimation(), 0);
          document.addEventListener('visibilitychange', wake);
          window.addEventListener('dashboard-panel-changed', wake);
          this.lifecycleOff = () => {
            document.removeEventListener('visibilitychange', wake);
            window.removeEventListener('dashboard-panel-changed', wake);
          };
        }
        this.loading = true;
        try {
          const [devices, deviceMap, energy, roles, session, icons, flows, switches] = await Promise.all([
            requestJSON('/api/v1/devices'),
            requestJSON('/api/v1/device/map'),
            requestJSON('/api/v1/energy').catch(() => null),
            requestJSON('/api/v1/energy/roles').catch(() => null),
            requestJSON('/api/v1/auth/session').catch(() => null),
            requestJSON('/api/v1/device/icons').catch(() => []),
            requestJSON('/api/v1/device/map/flows').catch(() => null),
            requestJSON('/api/v1/devices/switches').catch(() => []),
          ]);
          this.switches = Array.isArray(switches) ? switches : [];
          this.iconCatalogue = Array.isArray(icons) ? icons : [];
          this.iconMarkup = Object.fromEntries(this.iconCatalogue.map(icon => [icon.name, icon.markup]));
          this.savedAssignments = (roles && roles.assignments) || {};
          this.savedHidden = new Set((roles && roles.hidden) || []);
          this.savedGroups = (roles && roles.groups) || {};
          this.savedCategories = (roles && roles.categories) || {};
          // Ohne Sitzungs-API laeuft die Instanz ohne Authentifizierung, dann
          // laesst requireEnergyMutation den Schreibzugriff durch.
          this.csrfToken = (session && session.csrf_token) || '';
          this.canEditEnergy = !session || session.edit_energy !== false;
          this.canEditMap = !session || session.edit_device_map !== false;
          this.canEditMapView = !session || session.edit_device_map_view !== false;
          this.devices = devices || [];
          this.deviceMap = deviceMap || {version: 1, nodes: [], edges: []};
          this.energy = energy;
          this.energyUnavailable = energy === null;
          this.flows = flows;
          this.flowsUnavailable = flows === null;
          this.invalidConfigs = this.stubInfo().invalid;
          this.savedDeviceMap = JSON.parse(JSON.stringify(this.deviceMap));
          this.initLocalView();
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        } finally {
          this.loading = false;
        }
      },

      revisionConfig() {
        return {
          basePath: '/api/v1/device/map',
          current: () => this.deviceMap,
          headers: () => ({'X-CSRF-Token': this.csrfToken}),
          reload: async () => {
            await this.load();
            this.notifyMapChanged();
          },
          label: t('devicemap.revisions_label'),
        };
      },

      destroy() {
        if (this.themeOff) this.themeOff();
        if (this.registryOff) this.registryOff();
        if (this.energyRolesOff) this.energyRolesOff();
        if (this.subDevicesOff) this.subDevicesOff();
        if (this.lifecycleOff) this.lifecycleOff();
        this.stopFlowAnimation();
        clearTimeout(this._energyTimer);
        if (labels) { labels.destroy(); labels = null; }
        switchGen += 1;
        for (const timer of switchTimers) this._clearTimeout(timer);
        switchTimers = [];
        if (cy) cy.nodes().forEach(node => node.stop(true, true));
      },

      flowElements() {
        if (!this.view.layers.energy || !this.energy) return [];
        // Off outputs and the dead area carry no flow.
        const cut = new Set(this.switchTargets(this.switchStatus()).keys());
        return window.DeviceMapGraph.flowElements({pairs: this.wiringPairs(), energy: this.energyByDevice(), view: this.view, edgeProps: this.deviceMap.edge_props, cut});
      },

      // The switch roles as they stand: the saved energy assignments with the
      // panel drafts laid over them, so a draft shows its effect before saving.
      effectiveAssignments() {
        const model = window.DeviceMapModel;
        const assignments = Object.fromEntries(Object.entries(this.savedAssignments || {})
          .map(([id, value]) => [id, model.normalizeAssignment(value)]));
        for (const [id, draft] of Object.entries(this.drafts)) assignments[id] = model.normalizeAssignment(draft);
        return assignments;
      },

      // Switch channels and the area without power, from the switch states,
      // the effective roles and the energy edges with their channels.
      switchStatus() {
        const pairs = window.DeviceMapSwitch.pathPairs(this.buildEdgeElements(), this.deviceMap.edge_props);
        return window.DeviceMapSwitch.status({switches: this.switches, assignments: this.effectiveAssignments(), devices: this.devices, pairs});
      },

      // Panel info of every switch of one device: state, whether only the
      // detection (heuristic, from the saved roles) names it, and how many
      // switches the device has, which bounds its channel.
      switchInfoFor(deviceId) {
        const items = this.switches.filter(item => item.device_id === deviceId);
        const detected = window.DeviceMapSwitch.resolveSwitches({switches: this.switches, assignments: this.savedAssignments, devices: this.devices}).get(deviceId) || [];
        const heuristic = new Set(detected.filter(entry => entry.source === 'heuristic').map(entry => entry.entityId));
        return new Map(items.map(item => [item.entity_id, {state: item.state, heuristic: heuristic.has(item.entity_id), channels: items.length}]));
      },

      switchInfoOf(entityId) {
        const item = this.switches.find(entry => entry.entity_id === entityId);
        return item ? this.switchInfoFor(item.device_id).get(entityId) || null : null;
      },

      // Base of one entity before any draft, as the panel rows see it: a
      // switch takes its role from the saved entry or the detection.
      baseFor(entityId) {
        const model = window.DeviceMapModel;
        const info = this.switchInfoOf(entityId);
        if (!info) return model.baseAssignment(entityId, this.savedAssignments, this.energy);
        if (Object.prototype.hasOwnProperty.call(this.savedAssignments, entityId)) {
          return {...model.normalizeAssignment(this.savedAssignments[entityId]), source: 'override'};
        }
        if (info.heuristic) return {...model.normalizeAssignment({role: 'switch', channel: 1}), source: 'heuristic'};
        return {...model.normalizeAssignment(null), source: 'none'};
      },

      // What an off switch takes away: the devices whose nearest plug is this
      // device. Empty while the switch is on or nothing sits below it.
      switchConsequence(status, deviceId, info) {
        if (!info || info.state !== 'off') return '';
        const count = [...status.dead.values()].filter(entry => entry.plug === deviceId).length;
        return count ? tn('devicemap.panel.consequence', count, {n: count}) : '';
      },

      switchVisible() {
        return Boolean(this.view.layers.wiring || this.view.layers.energy);
      },

      // Quiet nodes of a status: off outputs (hop 0) and the dead area with
      // its hop distance. Nothing is quiet while neither the wiring nor the
      // energy layer is on.
      switchTargets(status) {
        const targets = new Map();
        if (!this.switchVisible()) return targets;
        for (const id of status.off) targets.set(id, {quiet: true, off: true, hops: 0});
        for (const [id, {hops}] of status.dead) targets.set(id, {quiet: true, off: false, hops});
        return targets;
      },

      // Label text for an off output or a node in the dead area, empty otherwise.
      offLabel(status, id) {
        if (!this.switchVisible()) return {};
        return status.off.has(id) || status.dead.has(id) ? {off: true, offText: t('devicemap.node.off')} : {};
      },

      // The committed switch state as a status, for labels and wiring edges.
      shownStatus() {
        const off = new Set();
        const dead = new Map();
        for (const [id, state] of switchShown) {
          if (state.off) off.add(id);
          else if (state.quiet) dead.set(id, state);
        }
        return {off, dead, ports: switchPorts};
      },

      // Port state of every switched wiring edge, keyed by relation key.
      portsOf(status) {
        return new Map([...status.edges].map(([key, edge]) => [key, portState(edge.state)]));
      },

      // Node data for one device: the normal image, plus the quiet image for
      // nodes that are switched off or without power. A leaf switch device
      // (no switched edge) carries its port on the node itself. graphStyle()
      // stacks the images, the quiet data picks one.
      nodeImages(device, status, energy, colorOf) {
        const port = this.switchVisible() && status.leaf.has(device.id) ? portState(status.leaf.get(device.id)) : null;
        const images = {svg: this.nodeSvg(device, energy, colorOf, {port})};
        if (this.switchTargets(status).has(device.id)) images.svgQuiet = this.nodeSvg(device, energy, colorOf, {port, quiet: true});
        return images;
      },

      // The only place that sets a node's quiet data or removes its opacity
      // bypass. animate: changed nodes fade one after another, the switched-off
      // plug first and each hop of the dead area 60 ms later. A new call
      // discards a running fade and starts from the last committed state.
      // Reduced motion fades all nodes together. Without animate, the state is
      // set at once (first render, new graph).
      applySwitchState({animate = false} = {}) {
        if (!cy) return;
        const graph = cy;
        const status = this.switchStatus();
        const targets = this.switchTargets(status);
        const ports = this.portsOf(status);
        // Ports are part of the goal, so a channel that flips without moving
        // any node still reaches refreshSwitchViews().
        const goal = [...targets].map(([id, target]) => `${id}:${target.quiet}:${target.off}:${target.hops}`).sort().join('|')
          + '#' + [...ports].map(([key, port]) => `${key}=${port}`).sort().join('|');
        if (goal === switchGoal) return;
        switchGoal = goal;
        switchPorts = ports;

        switchGen += 1;
        const gen = switchGen;
        for (const timer of switchTimers) this._clearTimeout(timer);
        switchTimers = [];
        const ids = [...this.devices.map(device => device.id), ...this.subDevices().map(sub => sub.id)];
        const nodes = ids.map(id => ({id, node: graph.getElementById(id)})).filter(({node}) => !node.empty());
        for (const {node} of nodes) {
          node.stop(true, true);
          node.removeStyle('background-image-opacity');
        }

        const commit = (node, id, target) => {
          switchShown.set(id, target);
          node.data('quiet', target.quiet);
          node.removeStyle('background-image-opacity');
          this.refreshSwitchViews();
        };
        const reduced = this.reducedMotion();
        const fades = [];
        for (const {id, node} of nodes) {
          const target = targets.get(id) || NO_SWITCH;
          if (!animate) {
            commit(node, id, target);
            continue;
          }
          const shown = switchShown.get(id) || NO_SWITCH;
          if (shown.quiet !== target.quiet || shown.off !== target.off) fades.push({id, node, target, shown});
        }
        for (const {id, node, target, shown} of fades) {
          // Fading in starts at the plug, fading out at the plug as well:
          // the hop distance comes from the state that the node moves to,
          // or from the previous one when it turns back on.
          const hops = target.quiet ? target.hops : shown.hops;
          const delay = reduced ? 0 : hops * SWITCH_HOP_MS;
          const duration = reduced ? SWITCH_REDUCED_MS : SWITCH_FADE_MS;
          switchTimers.push(this._setTimeout(() => {
            if (gen !== switchGen) return;
            node.animate({style: {'background-image-opacity': target.quiet ? [0, 1] : [1, 0]}}, {
              duration,
              complete: () => { if (gen === switchGen) commit(node, id, target); },
            });
          }, delay));
        }
        this.refreshSwitchViews();
      },

      // Labels, wiring edges and their ports follow the committed state only,
      // so a fade that is still running does not change them early.
      refreshSwitchViews() {
        const {ports} = this.shownStatus();
        if (labels) labels.update(this.labelItems(this.shownStatus()));
        if (!cy) return;
        const relation = window.DeviceMapGraph.relationKey;
        cy.edges('.devicemap-wiring').forEach(edge => {
          const shown = switchShown.get(edge.data('source')) || NO_SWITCH;
          const port = ports.get(relation(edge.data('source'), edge.data('target')));
          edge.toggleClass('devicemap-dead', shown.quiet);
          for (const state of PORT_STATES) edge.toggleClass(`devicemap-port-${state}`, port === state);
        });
      },

      applyFlows() {
        const graph = cy || this._cyForTest;
        if (!graph) return;
        const wanted = new Map(this.flowElements().map(element => [element.data.id, element]));
        graph.batch(() => {
          graph.edges('.devicemap-flow').forEach(edge => {
            const next = wanted.get(edge.id());
            if (!next || next.data.source !== edge.data('source')) { edge.remove(); return; }
            for (const key of ['label', 'color', 'width', 'speed']) edge.data(key, next.data[key]);
            wanted.delete(edge.id());
          });
          for (const element of wanted.values()) graph.add(element);
        });
        if (this.focusId) this.setFocus(this.focusId);
      },

      flowOffset(nowMs, speed) {
        return window.DeviceMapGraph.flowOffset(nowMs, speed);
      },

      shouldAnimate() {
        return this.view.layers.energy && this.isPanelActive()
          && document.visibilityState === 'visible' && !this.reducedMotion();
      },

      // One requestAnimationFrame loop for all flows. Constant motion, linear
      // (draft table "Bewegung und Verhalten"). It stops itself when nothing
      // should move and is restarted by visibility, layer and panel changes.
      flowAnimator() {
        if (!this._animator) {
          this._animator = window.DeviceMapGraph.createFlowAnimator({
            getCy: () => cy, shouldAnimate: () => this.shouldAnimate(),
            requestFrame: fn => this._requestFrame(fn), cancelFrame: id => this._cancelFrame(id),
          });
        }
        return this._animator;
      },

      startFlowAnimation() { this.flowAnimator().start(); },

      stopFlowAnimation() { this.flowAnimator().stop(); },

      reducedMotion() {
        return Boolean(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
      },

      wiringPairs() {
        return window.DeviceMapGraph.pairsOf(this.buildEdgeElements());
      },

      // Flow context for edgeFlow(): only energy edges carry values, and each
      // one turns by its direction.
      energyFlowContext() {
        const energy = this.energyByDevice();
        return {energy, ...window.DeviceMapWiring.flowContext(this.wiringPairs(), this.deviceMap.edge_props, energy)};
      },

      setFocus(id) {
        const df = window.DeviceMapDataflow;
        this.focusId = id;
        const related = id
          ? df.relatedFocus(id, {wiringPairs: this.wiringPairs(), dataEdges: this.dataEdges(), layers: this.view.layers})
          : {nodes: new Set(), edges: new Set()};
        this.focusRelated = related.nodes;
        if (labels) labels.setDimmed(id ? related.nodes : null);
        if (!cy) return;
        const key = window.DeviceMapGraph.relationKey;
        cy.batch(() => {
          cy.elements().removeClass('devicemap-dimmed devicemap-focused');
          if (!id) return;
          cy.nodes().forEach(node => { if (!related.nodes.has(node.id())) node.addClass('devicemap-dimmed'); });
          cy.edges().forEach(edge => {
            const inFocus = edge.hasClass('devicemap-data')
              ? related.edges.has(edge.data('flowId'))
              : related.edges.has(key(edge.data('source'), edge.data('target')));
            edge.addClass(inFocus ? 'devicemap-focused' : 'devicemap-dimmed');
          });
          cy.getElementById(id).addClass('devicemap-focused');
        });
      },

      labelOf(id) {
        if (id && id.startsWith('custom:')) return (this.savedCategories[id.slice(7)] || {}).label || id;
        const flowNode = this.flowNodes().find(node => node.id === id);
        if (flowNode) return flowNode.name;
        if (id === this.balanceId()) return t('devicemap.balance.name');
        return this.deviceLabel(id);
      },

      placePopover(rendered, view) {
        // Draft openPop(): next to the tap point, flipped at the stage edges,
        // the transform origin at the corner it grows from.
        const stage = this.$refs.canvas ? this.$refs.canvas.getBoundingClientRect() : {width: 800, height: 600};
        const x = rendered ? rendered.x : stage.width / 2;
        const y = rendered ? rendered.y : stage.height / 2;
        const width = Math.min(300, stage.width - 24);
        const flipX = x + 12 + width > stage.width - 12;
        const flipY = y + 12 + 260 > stage.height - 12;
        this.popover = {
          edge: null,
          ...view,
          left: flipX ? Math.max(12, x - 12 - width) : x + 12,
          top: flipY ? Math.max(12, y - 12 - 260) : y + 12,
          origin: `${flipX ? 'right' : 'left'} ${flipY ? 'bottom' : 'top'}`,
          open: false,
        };
        this._requestFrame(() => { if (this.popover) this.popover = {...this.popover, open: true}; });
      },

      openEdgePopover(flowId, rendered) {
        const edge = this.dataEdges().find(candidate => candidate.id === flowId);
        if (!edge) return;
        const view = window.DeviceMapDataflow.edgeView(edge, id => this.labelOf(id));
        const rows = view.rows.map(([key, value]) => [t(key), value]);
        if (edge.cat === 'automation' && window.__automationsView) {
          const rule = this.flowNodes().find(node => node.id === edge.from || node.id === edge.to);
          const list = rule && (view.part === 'action' ? rule.node.actions : rule.node.conditions);
          const raw = list && list[view.index];
          if (raw) {
            const described = view.part === 'action' ? window.__automationsView.describeAction(raw, null) : window.__automationsView.describeCondition(raw, null, this.savedCategories);
            rows.push([t('devicemap.pop.row.condition'), described.summary || described.title]);
          }
        }
        this.placePopover(rendered, {eyebrow: t(view.eyebrowKey), title: view.title, rows, lists: [], text: '', linkText: t(view.linkKey), link: view.link, action: null});
      },

      openNodePopover(id, rendered) {
        const df = window.DeviceMapDataflow;
        const kind = df.nodeKind(id);
        if (kind === 'stub') {
          const stub = this.stubInfo().edges.find(edge => edge.from === id || edge.to === id);
          if (stub) this.openEdgePopover(stub.id, rendered);
          return;
        }
        if (kind === 'balance' || id === this.balanceId()) {
          const items = df.roleEdges(this.previewEnergy(), this.balanceId(), this.deviceMap).map(edge => `${this.labelOf(edge.from)}: ${edge.roles.map(role => (role.startsWith('custom:') ? this.labelOf(role) : t(`energy.role_label.${role}`))).join(', ')}`);
          this.placePopover(rendered, {eyebrow: t('devicemap.pop.eyebrow.virtual'), title: t('devicemap.balance.name'), rows: [], text: t('devicemap.balance.description'),
            lists: [{label: '', items}], linkText: t('devicemap.pop.link.energy'), link: {tab: 'energy', entities: df.roleEdges(this.previewEnergy(), this.balanceId(), this.deviceMap).flatMap(edge => edge.entities)}, action: null});
          return;
        }
        const node = this.flowNodes().find(candidate => candidate.id === id);
        if (!node) return;
        if (kind === 'rule') {
          const view = window.__automationsView;
          const line = described => [described.title, described.summary].filter(Boolean).join(': ');
          this.placePopover(rendered, {eyebrow: t('devicemap.pop.eyebrow.rule'), title: node.name, rows: [], text: '',
            lists: [
              {label: t('devicemap.pop.when'), items: (node.node.conditions || []).map(condition => line(view.describeCondition(condition, null, this.savedCategories)))},
              {label: t('devicemap.pop.then'), items: (node.node.actions || []).map(action => line(view.describeAction(action, null)))},
            ],
            linkText: t('devicemap.pop.link.automations'), link: node.node.link, action: null});
          return;
        }
        const inputs = this.dataEdges().filter(edge => edge.to === id).map(edge => `${df.titleOf(edge)}: ${this.labelOf(edge.from)}`);
        this.placePopover(rendered, {eyebrow: t('devicemap.pop.eyebrow.service'), title: node.name, rows: [], text: '', lists: [{label: '', items: inputs}],
          linkText: t('devicemap.pop.link.config'), link: node.node.link, action: null});
      },

      // The card for a wiring edge, same look as the data flow cards: what
      // flows over it, which roles the lower device carries and where the
      // relation comes from. Only manual relations and group memberships can
      // be dissolved, a Discovery relation reflects real wiring.
      // keepDisclosure is set when the card is re-rendered after a change of the
      // same edge, so the fixed direction disclosure stays as the user left it.
      openWiringPopover(rendered, keepDisclosure = false) {
        const edge = this.selectedEdge;
        if (!edge) return;
        const model = window.DeviceMapModel;
        const {energy, children, dirOf} = this.energyFlowContext();
        const flow = model.edgeFlow(edge.target, energy, children, {dirOf, parentId: edge.source});
        const entry = energy.get(edge.target);
        const origin = edge.membership ? 'group' : edge.overrideId ? 'manual' : 'discovery';
        const rows = this.switchRowsOf(edge);
        if (flow) rows.push([t('devicemap.pop.row.power'), model.flowLabel(flow)]);
        if (entry && entry.roles.length) {
          rows.push([t('devicemap.pop.row.roles'), entry.roles.map(role => (role.startsWith('custom:') ? this.labelOf(role) : t(`energy.role_label.${role}`))).join(', ')]);
        }
        // A sub-device edge is written by the sub-device form, its origin is not a relation.
        if (!edge.subDevice) rows.push([t('devicemap.pop.row.origin'), t(`devicemap.pop.origin.${origin}`)]);
        const opensPanel = edge.target !== model.OWN_ENERGY_DEVICE_ID;
        this.edgeCardAnchor = rendered || null;
        if (!keepDisclosure) this.edgeFixedOpen = false;
        this.placePopover(rendered, {
          eyebrow: t(origin === 'group' ? 'devicemap.pop.eyebrow.membership' : 'devicemap.pop.eyebrow.wiring'),
          title: this.selectedEdgeLabel, rows, lists: [],
          text: origin === 'discovery' && !edge.subDevice ? t('devicemap.edge_readonly_hint') : '',
          linkText: opensPanel ? t('devicemap.pop.link.open_device', {name: this.deviceLabel(edge.target)}) : '',
          link: opensPanel ? {tab: 'panel', target: edge.target} : null,
          action: origin === 'discovery' || edge.subDevice ? null : {label: t('devicemap.disconnect_button')},
          edge: {
            ...this.edgePropsFor(edge),
            channels: this.channelsOf(edge),
            channel: window.DeviceMapSwitch.edgeChannel(this.deviceMap.edge_props, edge.source, edge.target),
            canReverse: Boolean(edge.overrideId) && !edge.membership && !model.isGroupId(edge.target) && !model.isGroupId(edge.source),
            manual: Boolean(edge.overrideId),
            parentName: this.deviceLabel(edge.source),
            childName: this.deviceLabel(edge.target),
            membership: Boolean(edge.membership),
            subDevice: Boolean(edge.subDevice),
          },
        });
      },

      // Channel saved for the pair of the edge, whatever its orientation.
      // Undefined when none is saved, so setProp writes no channel.
      storedEdgeChannel(edge) {
        const pairKey = window.DeviceMapGraph.relationKey(edge.source, edge.target);
        const prop = (this.deviceMap.edge_props || []).find(item => window.DeviceMapGraph.relationKey(item.parent_id, item.child_id) === pairKey);
        return prop && Number.isInteger(prop.channel) ? prop.channel : undefined;
      },

      // Connection type and flow direction of the selected wiring edge. Both
      // write a device_map edge_props entry; the map is saved with save().
      // setProp keeps the channel for energy and drops it for other types.
      setEdgeType(type) {
        const edge = this.selectedEdge;
        if (!this.canEditMap || !edge || edge.membership || edge.subDevice || !window.DeviceMapWiring.TYPES.includes(type)) return;
        const current = this.edgePropsFor(edge);
        this.deviceMap = {...this.deviceMap, edge_props: window.DeviceMapWiring.setProp(this.deviceMap.edge_props, {parent: edge.source, child: edge.target, type, direction: current.direction, channel: this.storedEdgeChannel(edge)})};
        this.unsaved = true;
        this.afterEdgePropsChange();
      },

      setEdgeDirection(direction) {
        const edge = this.selectedEdge;
        if (!this.canEditMap || !edge || edge.membership || edge.subDevice) return;
        this.deviceMap = {...this.deviceMap, edge_props: window.DeviceMapWiring.setProp(this.deviceMap.edge_props, {parent: edge.source, child: edge.target, type: 'energy', direction, channel: this.storedEdgeChannel(edge)})};
        this.unsaved = true;
        this.afterEdgePropsChange();
      },

      // The switch channel that feeds the selected energy edge. The channel
      // moves the edge to another switch output, so the state follows it.
      setEdgeChannel(channel) {
        const edge = this.selectedEdge;
        if (!this.canEditMap || !edge || edge.membership || !Number.isInteger(channel)) return;
        const current = this.edgePropsFor(edge);
        if (current.type !== 'energy') return;
        this.deviceMap = {...this.deviceMap, edge_props: window.DeviceMapWiring.setProp(this.deviceMap.edge_props, {parent: edge.source, child: edge.target, type: 'energy', direction: current.direction, channel})};
        this.unsaved = true;
        this.afterEdgePropsChange();
      },

      // Fixed directions sit behind a disclosure: it is open while one is set
      // or when the user opened it. A set fixed direction cannot be folded away.
      edgeFixedShown() {
        const edge = this.popover && this.popover.edge;
        return Boolean(this.edgeFixedOpen || (edge && String(edge.direction || '').startsWith('fixed')));
      },

      toggleEdgeFixedOpen() {
        this.edgeFixedOpen = !this.edgeFixedShown();
      },

      edgePropsFor(edge) {
        const wiring = window.DeviceMapWiring;
        return wiring.resolve({parent: edge.source, child: edge.target, membership: edge.membership}, wiring.propsIndex(this.deviceMap.edge_props), this.energyByDevice());
      },

      // Class and data of the wiring edge change in place (no renderGraph(),
      // which would reset the viewport), then flows and the card follow.
      afterEdgePropsChange() {
        const graph = cy || this._cyForTest;
        if (graph) {
          const fresh = new Map(this.buildEdgeElements().map(element => [element.data.id, element]));
          const transient = ['devicemap-selected-edge', 'devicemap-focused', 'devicemap-dimmed'];
          graph.edges('.devicemap-wiring').forEach(edge => {
            const next = fresh.get(edge.id());
            if (!next) return;
            const kept = transient.filter(name => edge.hasClass(name));
            edge.classes([next.classes, ...kept].join(' '));
            for (const name of ['connType', 'direction', 'heuristic']) edge.data(name, next.data[name]);
          });
        }
        // Edge props can move an edge in or out of the switched paths, so the
        // switch state is recomputed; an unchanged goal keeps it as it is.
        this.applySwitchState();
        this.applyFlows();
        this.refreshSwitchViews();
        if (this.popover && this.popover.edge) {
          // Re-open at the original tap point, not at the flipped card corner.
          const anchor = this.edgeCardAnchor || {x: this.popover.left - 12, y: this.popover.top - 12};
          this.openWiringPopover(anchor, true);
        }
      },

      async reverseSelectedRelation() {
        const selected = this.selectedEdge;
        if (!this.canEditMap || !selected || !selected.overrideId) return;
        try {
          const result = await requestJSON(`/api/v1/device/map/relations/${selected.overrideId}/reverse`, {method: 'POST', headers: {'X-CSRF-Token': this.csrfToken}});
          const edges = (this.deviceMap.edges || []).map(edge => (edge.id === result.relation.id ? result.relation : edge));
          const swap = {fixed_down: 'fixed_up', fixed_up: 'fixed_down'};
          const pair = window.DeviceMapGraph.relationKey(selected.source, selected.target);
          const props = (this.deviceMap.edge_props || []).map(prop => {
            if (window.DeviceMapGraph.relationKey(prop.parent_id, prop.child_id) !== pair) return prop;
            const next = {...prop, parent_id: prop.child_id, child_id: prop.parent_id};
            if (swap[next.direction]) next.direction = swap[next.direction];
            return next;
          });
          this.deviceMap = {...this.deviceMap, edges, edge_props: props};
          this.savedDeviceMap = {...this.savedDeviceMap, edges, edge_props: result.edge_props};
          this.$store.toasts.push(t('devicemap.relation_reversed'));
          this.closePopover();
          this.notifyMapChanged();
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      closePopover() {
        this.popover = null;
        this.clearEdgeSelection();
      },

      runPopoverAction() {
        return this.removeSelectedRelation();
      },

      toggleViewMenu(open = !this.viewMenuOpen) {
        this.viewMenuOpen = open;
        if (open) this.closePopover();
      },

      async openFlowLink(link) {
        if (!link) return;
        this.closePopover();
        if (link.tab === 'panel') {
          if (!(await this.leavePanel())) return;
          this.setFocus(link.target);
          this.openPanel(link.target);
          return;
        }
        if (link.tab === 'devices') {
          this.openDeviceInDevicesTab(link.target);
          return;
        }
        const detail = link.tab === 'automations' ? {panel: 'automations-panel', automationFocus: link.target}
          : link.tab === 'config' ? {panel: 'config-panel', configFocus: {config: link.target, item: link.item || ''}}
            : {panel: 'energy-panel', energyFocus: link.entities || []};
        window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail}));
      },

      clearFocus() {
        this.setFocus(null);
      },

      focusSummary() {
        if (!this.focusId) return '';
        const n = this.focusRelated.size - 1;
        return tn('devicemap.focus.summary', n, {name: this.labelOf(this.focusId), n});
      },

      isPanelActive() {
        return Boolean(this.$root && this.$root.classList && this.$root.classList.contains('active'));
      },

      // dashboard.js publishes every registry update (SSE, 30 s fallback) as
      // "registry-updated". Leading call plus one trailing call per window,
      // and nothing at all from a hidden browser tab or another dashboard tab
      // (Pi kiosk tablets, see the plan's review focus).
      onRegistryUpdated() {
        if (document.visibilityState !== 'visible' || !this.isPanelActive()) return;
        if (this._energyTimer) { this._energyPending = true; return; }
        this.refreshEnergy();
        this._energyTimer = this._setTimeout(() => {
          this._energyTimer = null;
          if (this._energyPending) { this._energyPending = false; this.onRegistryUpdated(); }
        }, ENERGY_REFRESH_MS);
      },

      async refreshEnergy() {
        // Switch states run in parallel. A failed request keeps the last
        // known states and shows no toast, the energy values behave as before.
        const switches = requestJSON('/api/v1/devices/switches').catch(() => null);
        try {
          this.energy = await requestJSON('/api/v1/energy');
          this.energyUnavailable = false;
        } catch (error) {
          this.energy = null;
          this.energyUnavailable = true;
        }
        const list = await switches;
        if (list !== null) this.switches = Array.isArray(list) ? list : [];
        this.applyEnergy();
      },

      // Data flow snapshot. Not refetched on registry updates, only when a
      // data or balance layer is switched on (see toggleLayer()).
      async loadFlows() {
        try {
          this.flows = await requestJSON('/api/v1/device/map/flows');
          this.flowsUnavailable = false;
        } catch (error) {
          this.flows = null;
          this.flowsUnavailable = true;
        }
        this.invalidConfigs = this.stubInfo().invalid;
      },

      flowNodes() {
        return ((this.flows && this.flows.nodes) || []).map(node => ({
          id: node.virtual_id,
          kind: node.kind,
          name: node.label_key ? t(node.label_key) : node.label,
          node,
        }));
      },

      baseNodeIds() {
        const df = window.DeviceMapDataflow;
        return new Set([...this.devices.map(device => device.id), ...this.subDevices().map(sub => sub.id), ...this.groupIds(), this.balanceId(), ...this.flowNodes().map(node => node.id)]);
      },

      stubInfo() {
        return window.DeviceMapDataflow.stubs(this.flows, this.baseNodeIds());
      },

      dataEdges() {
        const df = window.DeviceMapDataflow;
        const edges = [...df.roleEdges(this.previewEnergy(), this.balanceId(), this.deviceMap), ...df.flowEdges(this.flows, this.baseNodeIds(), this.balanceId()), ...this.stubInfo().edges];
        return this.view.hide_automation_links ? edges.filter(edge => !df.isAutomationLink(edge)) : edges;
      },

      previewEnergy() {
        return window.DeviceMapModel.applyDrafts(this.energy, this.drafts, this.devices);
      },

      // Entities with a sub-device are measured under the sub-device's ID
      // (drafts included), so the meter keeps the ring of its other entities.
      energyByDevice() {
        return window.DeviceMapModel.deviceEnergy(window.DeviceMapSubDevices.remapSnapshot(this.previewEnergy(), this.deviceMap));
      },

      subDevices() {
        return window.DeviceMapSubDevices.list(this.deviceMap);
      },

      subIsOrphan(sub) {
        return window.DeviceMapSubDevices.isOrphan(sub, this.devices);
      },

      // Value of a sub-device node: its entity's reading, or the hint when
      // the meter or the entity is gone.
      subValueText(sub, energy) {
        if (this.subIsOrphan(sub)) return t('devicemap.sub.missing');
        const device = this.devices.find(item => item.id === sub.device_id);
        const entities = ((device && device.entities) || []).filter(entity => entity && entity.unique_id === sub.entity_id);
        return window.DeviceMapModel.nodeValueText({entities}, energy.get(sub.id));
      },

      // Sub-device panel: name and icon, and the role row of the entity on the
      // meter, so the role stays changeable there. An orphan (meter or entity
      // gone) has no row. Without edit_energy only name and icon show.
      subPanelView() {
        const sub = this.subDevices().find(item => item.id === this.panelId);
        if (!sub) return null;
        const orphan = this.subIsOrphan(sub);
        const device = this.devices.find(item => item.id === sub.device_id);
        const canEdit = this.canEditEnergy;
        let row = null;
        if (device && !orphan && canEdit) {
          const status = this.switchStatus();
          const switchInfo = this.switchInfoFor(device.id);
          const consequenceOf = entityId => this.switchConsequence(status, device.id, switchInfo.get(entityId));
          const {rows} = window.DeviceMapModel.panelRows({device, snapshot: this.energy, saved: this.savedAssignments, drafts: this.drafts, switchInfo, consequenceOf, hidden: this.savedHidden, subDevices: this.subDevices()});
          row = rows.find(item => item.id === sub.entity_id) || null;
        }
        return {kind: 'sub', title: sub.label || sub.id, icon: sub.icon, row, orphan, deviceId: sub.device_id, canEdit};
      },

      // Channels 1 to 16 when the device has several switch roles, none otherwise.
      channelChoices(deviceId) {
        const entries = this.switchStatus().byDevice.get(deviceId) || [];
        return entries.length < 2 ? [] : Array.from({length: SUB_CHANNEL_MAX}, (_, index) => index + 1);
      },

      subFormChannels() {
        return this.subForm ? this.channelChoices(this.subForm.deviceId) : [];
      },

      // Power entities a sub-device form can measure. Move lists those of all
      // devices, the other modes those of the meter. An entity that another
      // sub-device measures is left out, the form's own entity stays.
      subFormEntities() {
        const form = this.subForm;
        if (!form) return [];
        const sub = window.DeviceMapSubDevices;
        const devices = form.mode === 'move' ? this.devices : this.devices.filter(device => device.id === form.deviceId);
        const taken = entityId => {
          const owner = sub.byEntity(this.deviceMap).get(entityId);
          return Boolean(owner) && owner.id !== form.subId;
        };
        return devices.flatMap(device => sub.powerEntities(device)
          .filter(entity => !taken(entity.unique_id))
          .map(entity => ({
            entityId: entity.unique_id,
            deviceId: device.id,
            label: form.mode === 'move' ? `${device.name || device.id}: ${entity.name || entity.unique_id}` : (entity.name || entity.unique_id),
          })));
      },

      // The measurement shows when a sub-device moves or its meter has more
      // than one power entity to choose from.
      subFormShowsMeasurement() {
        const form = this.subForm;
        if (!form) return false;
        const meter = this.devices.find(device => device.id === form.deviceId);
        return form.mode === 'move' || window.DeviceMapSubDevices.powerEntities(meter).length > 1;
      },

      setSubFormEntity(entityId) {
        const form = this.subForm;
        const choice = form && this.subFormEntities().find(item => item.entityId === entityId);
        if (!choice) return;
        this.subForm = {...form, entityId: choice.entityId, deviceId: choice.deviceId, error: ''};
      },

      // Icon of a meter for a new sub-device: its own icon, else the fallback.
      meterIcon(device) {
        return (device && (device.icon_name || device.suggested_icon)) || SUB_FALLBACK_ICON;
      },

      // Opens the sub-device form. create measures one power entity of a
      // meter, edit changes the label, icon and channel of a sub-device, move
      // also changes the measured entity. Nothing opens without edit_energy.
      openSubForm({mode, entityId = null, subId = null} = {}) {
        if (!this.canEditEnergy) return;
        const sub = window.DeviceMapSubDevices;
        if (mode === 'create') {
          const device = this.devices.find(item => (item.entities || []).some(entity => entity && entity.unique_id === entityId));
          if (!device || sub.byEntity(this.deviceMap).has(entityId)) return;
          const entity = device.entities.find(item => item.unique_id === entityId);
          this.subForm = {mode, subId: null, deviceId: device.id, entityId, name: entity.name || entityId,
            icon: this.meterIcon(device), channel: this.channelChoices(device.id).length ? 1 : 0, error: ''};
          return;
        }
        const existing = this.subDevices().find(item => item.id === subId);
        if (!existing || (mode !== 'edit' && mode !== 'move')) return;
        const prop = (this.deviceMap.edge_props || []).find(item => item.child_id === existing.id);
        // Without a stored channel the form shows and saves channel 1, the
        // first option of the select, so an untouched save matches the view.
        const stored = prop && Number.isInteger(prop.channel) && prop.channel >= 1 ? prop.channel : 1;
        this.subForm = {mode, subId: existing.id, deviceId: existing.device_id, entityId: existing.entity_id,
          name: existing.label, icon: existing.icon, channel: this.channelChoices(existing.device_id).length ? stored : 0, error: ''};
      },

      // Creates (POST, with the spot from placeNew) or changes (PUT) the
      // sub-device of the open form. The answer is merged, the unsaved
      // positions stay. A refusal, like a duplicate on move, keeps the form
      // open with the server's message as a toast.
      async saveSubForm() {
        const form = this.subForm;
        if (!form || this.subSaving || !this.canEditEnergy) return;
        const label = form.name.trim();
        if (!label) {
          this.subForm = {...form, error: t('devicemap.sub.name_required')};
          return;
        }
        const body = {label, icon: form.icon, device_id: form.deviceId, entity_id: form.entityId,
          channel: this.subFormChannels().length ? form.channel : 0};
        const headers = {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken};
        this.subSaving = true;
        try {
          let serverMap;
          if (form.mode === 'create') {
            const meter = (this.deviceMap.nodes || []).find(node => node.device_id === form.deviceId) || {x: 0, y: 0};
            const snap = this.view.snap_to_grid ? this.view.grid_size : null;
            const spot = window.DeviceMapSubDevices.placeNew({anchor: meter, taken: this.deviceMap.nodes || [], snap});
            serverMap = await requestJSON('/api/v1/device/map/sub-devices', {method: 'POST', headers, body: JSON.stringify({...body, x: spot.x, y: spot.y})});
          } else {
            serverMap = await requestJSON(`/api/v1/device/map/sub-devices/${encodeURIComponent(form.subId)}`, {method: 'PUT', headers, body: JSON.stringify(body)});
          }
          this.mergeSubDeviceResponse(serverMap);
          this.subForm = null;
          this.renderGraph();
          this.$store.toasts.push(t('devicemap.sub.saved'));
          this.notifyMapChanged();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        } finally {
          this.subSaving = false;
        }
      },

      // Removes a sub-device after one confirmation. Its entity reads on the
      // meter again, so the meter ring comes back.
      async deleteSubDevice(id) {
        const sub = this.subDevices().find(item => item.id === id);
        if (!sub || !this.canEditEnergy) return;
        const confirmed = await this.$store.modal.confirm({
          title: t('devicemap.sub.delete_title', {name: sub.label || sub.id}),
          confirmLabel: t('devicemap.sub.delete'),
          danger: true,
        });
        if (!confirmed) return;
        try {
          const serverMap = await requestJSON(`/api/v1/device/map/sub-devices/${encodeURIComponent(id)}`, {method: 'DELETE', headers: {'X-CSRF-Token': this.csrfToken}});
          this.mergeSubDeviceResponse(serverMap);
          if (this.panelId === id) this.panelId = null;
          if (this.subForm && this.subForm.subId === id) this.subForm = null;
          if (this.selectedEdge && this.selectedEdge.target === id) this.selectedEdge = null;
          this.popover = null;
          this.renderGraph();
          this.$store.toasts.push(t('devicemap.sub.deleted'));
          this.notifyMapChanged();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      // Takes a server answer's sub-devices into the map: the list itself,
      // the sub-device nodes the map lacks, and the sub-device pairs. Nodes of
      // removed sub-devices go. Positions and the unsaved flag of everything
      // else stay. The saved snapshot takes the same change, so
      // discardChanges() keeps it.
      mergeSubDeviceResponse(serverMap) {
        if (!serverMap) return;
        const sub = window.DeviceMapSubDevices;
        const subs = sub.list(serverMap);
        const live = new Set(subs.map(item => item.id));
        const merge = local => {
          const nodes = (local.nodes || []).filter(node => !sub.isSubId(node.virtual_id) || live.has(node.virtual_id));
          const known = new Set(nodes.map(node => node.virtual_id));
          const missing = (serverMap.nodes || []).filter(node => sub.isSubId(node.virtual_id) && !known.has(node.virtual_id));
          return {
            ...local,
            nodes: [...nodes, ...missing],
            sub_devices: subs,
            edge_props: [
              ...(local.edge_props || []).filter(prop => !sub.isSubId(prop.child_id)),
              ...(serverMap.edge_props || []).filter(prop => sub.isSubId(prop.child_id)),
            ],
          };
        };
        this.deviceMap = merge(this.deviceMap);
        this.savedDeviceMap = merge(this.savedDeviceMap);
      },

      // Refetches the map after a change made elsewhere.
      async refreshSubDevices() {
        try {
          this.mergeSubDeviceResponse(await requestJSON('/api/v1/device/map'));
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      // Node data of one sub-device: a dashed ring in its role colour, the
      // sub-device icon, and the quiet image when its channel is off.
      subNodeImages(sub, status, energy, colorOf) {
        const entry = energy.get(sub.id);
        const role = entry && entry.primaryRole;
        const spec = {
          segments: entry ? entry.roles.map(item => entry.meta[item].color) : [],
          icon: sub.icon ? `cat:${sub.icon}` : 'box',
          iconColor: role ? entry.meta[role].color : 'text-muted',
          health: 'unknown',
          subDevice: true,
          iconMarkup: this.iconMarkup[sub.icon] || '',
        };
        const images = {svg: window.DeviceMapNodeSvg.dataUri({...spec, quiet: false, port: null}, colorOf)};
        if (this.switchTargets(status).has(sub.id)) images.svgQuiet = window.DeviceMapNodeSvg.dataUri({...spec, quiet: true, port: null}, colorOf);
        return images;
      },

      setDraft(entityId, field, value) {
        const model = window.DeviceMapModel;
        const base = this.baseFor(entityId);
        const current = this.drafts[entityId] || {role: base.role, scale: base.scale, invert: base.invert, capacity_kwh: base.capacity_kwh, channel: base.channel};
        let next = {...current, [field]: value};
        if (field === 'scale') next.scale = Math.max(0.01, Number(value) || 1);
        if (field === 'capacity_kwh') next.capacity_kwh = Math.max(0, Number(value) || 0);
        if (field === 'channel') next.channel = Math.min(16, Math.max(1, Number(value) || 1));
        if (field === 'role' && current.pin) next = {...next, pin: false};
        const drafts = {...this.drafts};
        if (model.isDraftChange(base, next)) drafts[entityId] = next; else delete drafts[entityId];
        this.drafts = drafts;
        this.applyEnergy();
      },

      // Opens the device in the devices tab, where its switch can be operated.
      openDeviceInDevicesTab(deviceId) {
        this.closePopover();
        window.dispatchEvent(new CustomEvent('dashboard-open-panel', {detail: {panel: 'devices-panel', deviceFocus: deviceId}}));
      },

      // The legend entries for switch outputs show only when the map has one.
      switchLegendVisible() {
        return this.switchVisible() && this.switchStatus().byDevice.size > 0;
      },

      // Rows of the edge card that tell the switch state. A switched edge
      // shows its channel state (and, when off, the devices that lose power),
      // an edge below a cut edge names the cause, any other edge has none.
      // Rows are [label, value, {tone?, link?}].
      switchRowsOf(edge) {
        const status = this.switchStatus();
        const entry = status.edges.get(window.DeviceMapGraph.relationKey(edge.source, edge.target));
        if (entry) {
          const state = portState(entry.state);
          const rows = [[t('devicemap.pop.row.state'), t(`devicemap.pop.state.${state}`), {tone: state}]];
          if (state === 'off') {
            const count = this.deadBelowEdge(status, edge);
            rows.push([t('devicemap.pop.row.consequence'), tn('devicemap.panel.consequence', count, {n: count})]);
          }
          rows.push([t('devicemap.pop.row.switch'), this.switchLabelOf(status, entry), {link: {tab: 'devices', target: edge.source}}]);
          return rows;
        }
        const cut = status.dead.get(edge.target);
        if (cut) {
          return [
            [t('devicemap.pop.row.state'), t('devicemap.pop.state.no_power'), {tone: 'off'}],
            [t('devicemap.pop.row.cause'), t('devicemap.pop.cause', {name: this.deviceLabel(cut.plug)}), {link: {tab: 'panel', target: cut.plug}}],
          ];
        }
        return [];
      },

      // Number of devices that lose power through this cut edge: the child
      // and the devices below it up to the next cut edge, where deadArea
      // names this edge's plug as the nearest one.
      deadBelowEdge(status, edge) {
        const relation = window.DeviceMapGraph.relationKey;
        const cuts = new Set([...status.edges].filter(([, entry]) => entry.state === 'off').map(([key]) => key));
        const children = new Map();
        for (const {parent, child} of window.DeviceMapSwitch.pathPairs(this.buildEdgeElements(), this.deviceMap.edge_props)) {
          if (!children.has(parent)) children.set(parent, []);
          children.get(parent).push(child);
        }
        const reached = new Set([edge.target]);
        const queue = [edge.target];
        while (queue.length) {
          const node = queue.shift();
          for (const child of children.get(node) || []) {
            if (reached.has(child) || cuts.has(relation(node, child))) continue;
            reached.add(child);
            queue.push(child);
          }
        }
        return [...reached].filter(id => status.dead.has(id) && status.dead.get(id).plug === edge.source).length;
      },

      // The switch name, with its channel when the device has more than one switch role.
      switchLabelOf(status, entry) {
        const channels = (status.byDevice.get(entry.plug) || []).length;
        return channels > 1 ? t('devicemap.pop.switch_channel', {name: entry.name, n: entry.channel}) : entry.name;
      },

      // Channels the edge card offers: only for an energy edge whose source
      // device has more than one switch role, none otherwise.
      channelsOf(edge) {
        if (edge.membership || this.edgePropsFor(edge).type !== 'energy') return [];
        const entries = this.switchStatus().byDevice.get(edge.source) || [];
        if (entries.length < 2) return [];
        const top = Math.max(entries.length, ...entries.map(entry => entry.channel));
        return Array.from({length: top}, (_, index) => index + 1);
      },

      get panelView() {
        if (!this.panelId) return null;
        if (window.DeviceMapSubDevices.isSubId(this.panelId)) return this.subPanelView();
        const model = window.DeviceMapModel;
        if (model.isGroupId(this.panelId)) {
          const id = this.panelId.slice(model.GROUP_PREFIX.length);
          const group = this.savedGroups[id];
          if (!group) return null;
          const draft = this.groupDraft || {};
          const {energy, children, dirOf} = this.energyFlowContext();
          const flow = model.edgeFlow(this.panelId, energy, children, {dirOf});
          const members = [
            ...(group.members.devices || []).map(deviceId => ({id: deviceId, name: this.deviceLabel(deviceId)})),
            ...(group.members.groups || []).map(child => ({id: model.GROUP_PREFIX + child, name: (this.savedGroups[child] || {}).label || child})),
          ].map(member => {
            const memberFlow = model.edgeFlow(member.id, energy, children, {dirOf});
            return {...member, valueText: memberFlow ? model.formatPower(memberFlow.value) : t('devicemap.value.no_values')};
          });
          const consumers = model.roleOptions('W', this.savedCategories)
            .filter(option => option.group === 'custom' && (this.savedCategories[option.value.slice('custom:'.length)] || {}).base === 'consumer');
          return {
            kind: 'group',
            title: draft.label !== undefined ? draft.label : group.label,
            health: t('devicemap.group.eyebrow_hint'),
            sumText: flow ? model.formatPower(flow.value) : t('devicemap.group.no_data'),
            members,
            addable: this.pickerItems(id),
            role: draft.role !== undefined ? draft.role : (group.role || ''),
            roleOptions: [{value: '', label: t('devicemap.group.no_role')}, ...consumers.map(option => {
              const def = this.savedCategories[option.value.slice('custom:'.length)] || {};
              return {...option, color: def.color || 'cat_1', iconMarkup: this.iconMarkup[def.icon] || ''};
            })],
            categories: Object.entries(this.savedCategories).map(([cid, def]) => ({id: cid, label: def.label, base: def.base, color: def.color})),
          };
        }
        const device = this.devices.find(candidate => candidate.id === this.panelId);
        if (!device) return null;
        const status = this.switchStatus();
        const switchInfo = this.switchInfoFor(device.id);
        const consequenceOf = entityId => this.switchConsequence(status, device.id, switchInfo.get(entityId));
        const {rows, others} = model.panelRows({device, snapshot: this.energy, saved: this.savedAssignments, drafts: this.drafts, switchInfo, consequenceOf, hidden: this.savedHidden, subDevices: this.subDevices()});
        let empty = '';
        if (!rows.length && !others.length) empty = 'no_values';
        else if (!rows.length) empty = 'no_power';
        return {
          kind: 'device',
          title: device.name || device.id,
          health: t(`devicemap.panel.health.${model.deviceHealth(device)}`),
          canEdit: this.canEditEnergy,
          rows: rows.map(row => ({...row, canAddSub: this.canEditEnergy && row.canAddSub})), others, empty,
          hasSwitches: switchInfo.size > 0,
          flows: window.DeviceMapDataflow.deviceFlows(device.id, this.dataEdges(), id => this.labelOf(id)),
        };
      },

      openPanel(id) {
        this.panelId = id === window.DeviceMapModel.OWN_ENERGY_DEVICE_ID ? null : id;
        this.groupDraft = null;
        this.groupWholePending = false;
        this.subForm = null;
      },

      // Opens another panel from the open one (sub-device and meter links).
      // The drafts of the open panel go through the same guard as every other
      // way out of a panel.
      async navigatePanel(id) {
        if (!(await this.leavePanel())) return;
        this.openPanel(id);
      },

      closePanel() {
        this.panelId = null;
      },

      panelDirty() {
        return Object.keys(this.drafts).length > 0 || Boolean(this.groupDraft);
      },

      discardPanel() {
        this.drafts = {};
        this.groupDraft = null;
        this.groupWholePending = false;
        this.applyEnergy();
      },

      // Every way out of a panel with drafts goes through here: the shared
      // confirm modal, "Weiter bearbeiten" keeps everything, "Verwerfen"
      // drops the drafts. Resolves true when leaving may proceed.
      async leavePanel() {
        if (!this.panelDirty()) return true;
        const ok = await this.$store.modal.confirm({
          title: t('devicemap.panel.guard_title'),
          body: t('devicemap.panel.guard_body', {name: this.deviceLabel(this.panelId)}),
          cancelLabel: t('devicemap.panel.guard_continue'),
          confirmLabel: t('devicemap.panel.guard_discard'),
          danger: true,
        });
        if (ok) this.discardPanel();
        return ok;
      },

      roleOptionsFor(unit) {
        return window.DeviceMapModel.roleOptions(unit, this.savedCategories);
      },

      pinAssignment(entityId) {
        const base = this.baseFor(entityId);
        this.drafts = {...this.drafts, [entityId]: {role: base.role, scale: base.scale, invert: base.invert, capacity_kwh: base.capacity_kwh, channel: base.channel, pin: true}};
        this.applyEnergy();
      },

      panelStatusText() {
        const n = Object.keys(this.drafts).length;
        return n ? tn('devicemap.panel.status', n, {n}) : '';
      },

      async savePanel() {
        if (!this.panelDirty() || this.panelSaving) return;
        this.panelSaving = true;
        const assignments = Object.fromEntries(Object.entries(this.drafts)
          .map(([id, draft]) => [id, window.DeviceMapModel.assignmentPayload(draft)]));
        try {
          await this.patchEnergy({assignments});
          this.drafts = {};
          this.$store.toasts.push(t('devicemap.panel.saved'));
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        } finally {
          this.panelSaving = false;
        }
      },

      showInRoleTable() {
        const view = this.panelView;
        if (!view) return;
        window.dispatchEvent(new CustomEvent('dashboard-open-panel', {
          detail: {panel: 'energy-panel', energyFocus: view.rows.map(row => row.id)},
        }));
      },

      async requestClosePanel() {
        if (!(await this.leavePanel())) return;
        this.closePanel();
        this.clearFocus();
      },

      async onBackgroundTap() {
        this.viewMenuOpen = false;
        if (this.popover || this.selectedEdge) { this.closePopover(); return; }
        await this.requestClosePanel();
      },

      async onEscape() {
        // An open dialog handles its own Escape (native cancel event).
        if (this.$store.modal.open || this.picker) return;
        if (this.viewMenuOpen) { this.viewMenuOpen = false; return; }
        if (this.popover) { this.closePopover(); return; }
        await this.requestClosePanel();
      },

      themeColor() {
        const cache = {};
        return token => {
          if (!(token in cache)) cache[token] = window.DashboardTheme.color(token);
          return cache[token];
        };
      },

      nodeSvg(device, energy = this.energyByDevice(), colorOf = this.themeColor(), {port = null, quiet = false} = {}) {
        if (device.id === this.balanceId()) return window.DeviceMapNodeSvg.virtualDataUri('balance', colorOf);
        const model = window.DeviceMapModel;
        const spec = model.ringSpec(energy.get(device.id), model.deviceHealth(device), (device.entities || []).length > 0, this.iconMarkup);
        return window.DeviceMapNodeSvg.dataUri({...spec, quiet, port}, colorOf);
      },

      labelItems(status = this.switchStatus()) {
        const model = window.DeviceMapModel;
        const {energy, children, dirOf} = this.energyFlowContext();
        const devices = this.devices.map(device => ({
          id: device.id,
          name: device.name || device.id,
          value: device.id === this.balanceId() ? model.balanceValueText(this.energy && this.energy.balance) : model.nodeValueText(device, energy.get(device.id)),
          ...this.offLabel(status, device.id),
        }));
        const subs = this.subDevices().map(sub => ({id: sub.id, name: sub.label || sub.id, value: this.subValueText(sub, energy), ...this.offLabel(status, sub.id)}));
        const groups = Object.entries(this.savedGroups).map(([id, group]) => {
          const nodeId = model.GROUP_PREFIX + id;
          return {id: nodeId, name: group.label, value: model.groupValueText(model.edgeFlow(nodeId, energy, children, {dirOf}))};
        });
        const virtual = [
          ...(this.balanceId() === window.DeviceMapDataflow.BALANCE_ID
            ? [{id: window.DeviceMapDataflow.BALANCE_ID, name: t('devicemap.balance.name'), value: model.balanceValueText(this.energy && this.energy.balance)}]
            : []),
          ...this.flowNodes().map(node => ({id: node.id, name: node.name, value: t(`devicemap.${node.kind}.value`)})),
          ...this.stubInfo().nodes.map(stub => ({id: stub.id, name: t('devicemap.stub.name'), value: ''})),
        ];
        return [...devices, ...subs, ...groups, ...virtual];
      },

      // Live refresh without renderGraph(): a rebuild would reset Cytoscape's
      // internal state and restart every animation for what is only a value change.
      applyEnergy() {
        if (!cy) return;
        const status = this.switchStatus();
        const energy = this.energyByDevice();
        const colorOf = this.themeColor();
        cy.batch(() => {
          for (const device of this.devices) {
            const node = cy.getElementById(device.id);
            if (!node.empty()) node.data(this.nodeImages(device, status, energy, colorOf));
          }
          for (const sub of this.subDevices()) {
            const node = cy.getElementById(sub.id);
            if (!node.empty()) node.data(this.subNodeImages(sub, status, energy, colorOf));
          }
        });
        if (labels) labels.update(this.labelItems(this.shownStatus()));
        if (labels) labels.sync(cy);
        this.applyFlows();
        this.applyDataEdges();
        this.applySwitchState({animate: true});
        this.startFlowAnimation();
      },

      // Brings the role, data and stub edges in line with dataEdges(), the
      // same way applyFlows() does for the energy flows.
      applyDataEdges() {
        const graph = cy || this._cyForTest;
        if (!graph) return;
        const wanted = new Map(window.DeviceMapGraph.dataEdgeElements(this.dataEdges()).map(element => [element.data.id, element]));
        graph.batch(() => {
          graph.edges('.devicemap-data').forEach(edge => {
            if (!wanted.has(edge.id())) edge.remove(); else wanted.delete(edge.id());
          });
          for (const element of wanted.values()) graph.add(element);
        });
        this.applyVisibility();
      },

      // Shows or hides every virtual node, data edge and stub for the current
      // layers. With stagger, the nodes appear one after another (40 ms apart).
      applyVisibility({stagger = false} = {}) {
        if (!cy) return;
        const df = window.DeviceMapDataflow;
        const {layers} = this.view;
        const hiddenIds = new Set();
        const delays = new Map();
        let index = 0;
        cy.nodes('.devicemap-virtual').forEach(node => {
          const visible = df.nodeVisible(df.nodeKind(node.id()), layers);
          const delay = stagger && !this.reducedMotion() ? 40 * index : 0;
          index += 1;
          if (!visible) hiddenIds.add(node.id());
          delays.set(node.id(), delay);
          const apply = () => node.toggleClass('devicemap-hidden', !visible);
          if (delay) this._setTimeout(apply, delay); else apply();
        });
        const edgesById = new Map(this.dataEdges().map(edge => [edge.id, edge]));
        cy.edges('.devicemap-data').forEach(edge => {
          const data = edgesById.get(edge.data('flowId'));
          edge.toggleClass('devicemap-hidden', !data || !df.edgeVisible(data, layers));
        });
        if (labels) labels.setHidden(hiddenIds, delays);
        if (this.focusId && hiddenIds.has(this.focusId)) this.clearFocus();
      },

      groupIds() {
        return Object.keys(this.savedGroups).map(id => window.DeviceMapModel.GROUP_PREFIX + id);
      },

      nodeIds() {
        return new Set([...this.baseNodeIds(), ...this.stubInfo().nodes.map(stub => stub.id)]);
      },

      positionFor(id) {
        return (this.deviceMap.nodes || []).find(node => node.device_id === id || node.virtual_id === id);
      },

      // Every write of energy.json from the map goes through here: CSRF,
      // fresh saved state, a fresh energy snapshot and the event the energy
      // page reloads on.
      async patchEnergy(body) {
        const saved = await requestJSON('/api/v1/energy/roles', {
          method: 'PATCH',
          headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
          body: JSON.stringify(body),
        });
        if (saved && saved.assignments) this.savedAssignments = saved.assignments;
        if (saved && saved.assignments) this.savedHidden = new Set(saved.hidden || []);
        if (saved && 'groups' in saved) this.savedGroups = saved.groups || {};
        if (saved && 'categories' in saved) this.savedCategories = saved.categories || {};
        await this.refreshEnergy();
        window.dispatchEvent(new CustomEvent('energy-roles-changed', {detail: {source: 'devicemap'}}));
        return saved;
      },

      // Saved state from the energy page. Unsaved positions and drafts stay,
      // only what the energy page owns is replaced, then the graph is rebuilt
      // so removed members and groups lose their lines right away.
      async reloadEnergyState() {
        const [roles, energy] = await Promise.all([
          requestJSON('/api/v1/energy/roles').catch(() => null),
          requestJSON('/api/v1/energy').catch(() => null),
        ]);
        if (!roles) return;
        this.savedAssignments = roles.assignments || {};
        this.savedHidden = new Set(roles.hidden || []);
        this.savedGroups = roles.groups || {};
        this.savedCategories = roles.categories || {};
        this.energy = energy;
        this.energyUnavailable = energy === null;
        const model = window.DeviceMapModel;
        if (this.panelId && model.isGroupId(this.panelId) && !this.savedGroups[this.panelId.slice(model.GROUP_PREFIX.length)]) this.closePanel();
        this.renderGraph();
      },

      // The panel body scrolls and would clip a tooltip, so the (?) texts are
      // positioned against the whole panel when they are about to show.
      placeHelpTip(event) {
        const help = event.target && event.target.closest ? event.target.closest('.field-help') : null;
        const panel = event.currentTarget;
        if (!help || !panel) return;
        const tip = help.querySelector('.field-help-text');
        const trigger = help.querySelector('.field-help-trigger') || help;
        if (!tip) return;
        const {left, top} = window.DeviceMapModel.helpTipPosition({
          panel: panel.getBoundingClientRect(),
          trigger: trigger.getBoundingClientRect(),
          tip: {width: tip.offsetWidth, height: tip.offsetHeight},
        });
        tip.style.left = `${left}px`;
        tip.style.top = `${top}px`;
      },

      // Tells the energy tab's plant view that the saved map changed.
      notifyMapChanged() {
        window.dispatchEvent(new CustomEvent('device-map-changed', {detail: {source: 'devicemap'}}));
      },

      // Places devices that have no saved position below the existing
      // arrangement instead of triggering a full re-layout (which would
      // discard every hand-placed position - see renderGraph()). Returns
      // how many devices were placed this way, or 0 when there is either no
      // existing arrangement to anchor to (first-ever load) or nothing missing.
      placeNewDevices() {
        const existingNodes = (this.deviceMap.nodes || []).filter(node =>
          this.devices.some(device => device.id === node.device_id));
        const missing = this.devices.filter(device => !this.positionFor(device.id)).map(device => device.id);
        const gridSize = this.view.grid_size;
        const snap = value => this.view.snap_to_grid ? Math.round(value / gridSize) * gridSize : value;
        const spots = window.DeviceMapModel.placeDevices({ids: missing, allPositions: existingNodes, snap});
        if (!spots.length) return 0;
        const nodes = [...(this.deviceMap.nodes || []), ...spots.map(spot => ({device_id: spot.id, x: spot.x, y: spot.y}))];
        this.deviceMap = {...this.deviceMap, nodes};
        this.unsaved = true;
        return spots.length;
      },

      placeNewGroups() {
        const model = window.DeviceMapModel;
        const gridSize = this.view.grid_size;
        const snap = value => (this.view.snap_to_grid ? Math.round(value / gridSize) * gridSize : value);
        const nodes = [...(this.deviceMap.nodes || [])];
        let placed = 0;
        for (const [id, group] of Object.entries(this.savedGroups)) {
          const nodeId = model.GROUP_PREFIX + id;
          if (nodes.some(node => node.virtual_id === nodeId)) continue;
          const members = [...(group.members.devices || []), ...(group.members.groups || []).map(child => model.GROUP_PREFIX + child)];
          const memberPositions = nodes.filter(node => members.includes(node.device_id || node.virtual_id));
          const spot = model.placeGroup({memberPositions, allPositions: nodes, snap});
          nodes.push({virtual_id: nodeId, x: spot.x, y: spot.y});
          placed += 1;
        }
        if (placed) {
          this.deviceMap = {...this.deviceMap, nodes};
          this.unsaved = true;
        }
        return placed;
      },

      // Sub-devices without a saved position go right of their meter, or one
      // step right of the origin when the meter is gone (orphan). Returns how
      // many were placed.
      placeNewSubDevices() {
        const sub = window.DeviceMapSubDevices;
        const unplaced = this.subDevices().filter(item => !this.positionFor(item.id));
        if (!unplaced.length) return 0;
        const snap = this.view.snap_to_grid ? this.view.grid_size : null;
        const nodes = [...(this.deviceMap.nodes || [])];
        for (const item of unplaced) {
          const anchor = nodes.find(node => node.device_id === item.device_id) || {x: 0, y: 0};
          const spot = sub.placeNew({anchor, taken: nodes, snap});
          nodes.push({virtual_id: item.id, x: spot.x, y: spot.y});
        }
        this.deviceMap = {...this.deviceMap, nodes};
        this.unsaved = true;
        return unplaced.length;
      },

      balanceId() {
        return window.DeviceMapDataflow.balanceNodeId(new Set(this.devices.map(device => device.id)));
      },

      // Decision 3 (2026-10-11): a balance position saved before energy_node
      // took over moves to the device once, then the virtual entry is gone.
      adoptBalancePosition() {
        const df = window.DeviceMapDataflow;
        if (this.balanceId() === df.BALANCE_ID) return false;
        const nodes = this.deviceMap.nodes || [];
        const virtual = nodes.find(node => node.virtual_id === df.BALANCE_ID);
        if (!virtual) return false;
        const own = window.DeviceMapModel.OWN_ENERGY_DEVICE_ID;
        const hasOwn = nodes.some(node => node.device_id === own);
        const next = nodes.filter(node => node !== virtual);
        if (!hasOwn) next.push({device_id: own, x: virtual.x, y: virtual.y});
        this.deviceMap = {...this.deviceMap, nodes: next};
        this.unsaved = true;
        return true;
      },

      // Balance and data flow nodes. Placed only while their layer is on, so
      // a layer that was never shown leaves no positions in the saved map.
      // Returns how many nodes were placed.
      placeVirtualNodes() {
        const df = window.DeviceMapDataflow;
        const {layers} = this.view;
        const gridSize = this.view.grid_size;
        const snap = value => (this.view.snap_to_grid ? Math.round(value / gridSize) * gridSize : value);
        const nodes = [...(this.deviceMap.nodes || [])];
        const has = id => nodes.some(node => (node.device_id || node.virtual_id) === id);
        const positionOf = id => nodes.find(node => (node.device_id || node.virtual_id) === id);
        let placed = 0;
        if (this.balanceId() === df.BALANCE_ID && (layers.balance || layers.data) && !has(df.BALANCE_ID)) {
          const roleDevices = new Set(df.roleEdges(this.energy, df.BALANCE_ID, this.deviceMap).map(edge => edge.from));
          const rolePositions = nodes.filter(node => roleDevices.has(node.device_id || node.virtual_id));
          nodes.push({virtual_id: df.BALANCE_ID, ...df.placeBalance({rolePositions, allPositions: nodes, snap})});
          placed += 1;
        }
        if (layers.data) {
          const edges = df.flowEdges(this.flows, this.baseNodeIds(), this.balanceId());
          // Rules hang under the automation device when it has a position.
          const automation = positionOf('automation');
          const anchorOf = node => (node.kind === 'rule' && automation ? {x: automation.x, y: automation.y} : undefined);
          const missing = this.flowNodes().filter(node => !has(node.id)).map(node => ({
            id: node.id,
            inputXs: edges.filter(edge => edge.to === node.id).map(edge => positionOf(edge.from)).filter(Boolean).map(position => position.x),
            anchor: anchorOf(node),
          }));
          for (const spot of df.placeFlowNodes({nodes: missing, allPositions: nodes, snap})) {
            nodes.push({virtual_id: spot.id, x: spot.x, y: spot.y});
            placed += 1;
          }
        }
        if (placed) {
          this.deviceMap = {...this.deviceMap, nodes};
          this.unsaved = true;
        }
        return placed;
      },

      deviceLabel(deviceId) {
        if (window.DeviceMapSubDevices.isSubId(deviceId)) {
          const sub = this.subDevices().find(item => item.id === deviceId);
          return sub && sub.label ? sub.label : deviceId;
        }
        if (window.DeviceMapModel.isGroupId(deviceId)) {
          return (this.savedGroups[deviceId.slice(window.DeviceMapModel.GROUP_PREFIX.length)] || {}).label || deviceId;
        }
        const device = this.devices.find(candidate => candidate.id === deviceId);
        return device ? (device.name || device.id) : deviceId;
      },

      get selectedEdgeLabel() {
        if (!this.selectedEdge) return '';
        return `${this.deviceLabel(this.selectedEdge.target)} → ${this.deviceLabel(this.selectedEdge.source)}`;
      },

      // deviceMap can arrive without a view (older revisions, or tests that
      // build deviceMap by hand) - merge onto defaults everywhere instead of
      // requiring every call site to null-check.
      // The view saved on the server, with defaults for every key. save() sends
      // this one, the browser settings in localView never reach the map.
      get serverView() {
        const model = window.DeviceMapModel;
        const base = {...DEFAULT_VIEW, layers: model.DEFAULT_LAYERS, wiring_types: model.DEFAULT_WIRING_TYPES};
        return window.DeviceMapViewPrefs.merge(base, this.deviceMap.view || {});
      },

      // What the page shows: the server view with this browser's settings on top.
      get view() {
        return window.DeviceMapViewPrefs.merge(this.serverView, this.localView);
      },

      // Keeps only the browser settings that still differ from the server view,
      // so a key that became the server default drops out of storage.
      initLocalView() {
        const prefs = window.DeviceMapViewPrefs;
        const storage = viewStorage();
        const server = this.serverView;
        this.localView = prefs.diff(server, prefs.merge(server, prefs.load(storage, server)));
        prefs.store(storage, this.localView);
      },

      // Changes a view setting for this browser only. The map stays as it is
      // (no unsaved flag), the caller redraws what the change touches.
      setView(patch) {
        const prefs = window.DeviceMapViewPrefs;
        this.localView = prefs.diff(this.serverView, prefs.merge(this.view, patch));
        prefs.store(viewStorage(), this.localView);
      },

      viewDiffersFromDefault() {
        return !window.DeviceMapViewPrefs.isEmpty(this.localView);
      },

      // Shows or hides one connection type for this browser. The wiring edges
      // and the energy flows are restyled from the new filter.
      toggleWiringType(type) {
        if (!window.DeviceMapWiring.TYPES.includes(type)) return;
        const wiringTypes = {...this.view.wiring_types, [type]: !this.view.wiring_types[type]};
        this.setView({wiring_types: wiringTypes});
        if (cy) cy.style(this.graphStyle());
        this.applyFlows();
      },

      // A type is greyed out while the wiring layer is off, the energy type
      // stays available because the energy layer draws its own edges.
      wiringTypeDisabled(type) {
        return type !== 'energy' && !this.view.layers.wiring;
      },

      wiringFilterActive() {
        return window.DeviceMapWiring.TYPES.some(type => !this.view.wiring_types[type]);
      },

      energyFlowHidden() {
        return Boolean(this.view.layers.energy && !this.view.wiring_types.energy);
      },

      showEnergyEdges() {
        const wiringTypes = {...this.view.wiring_types, energy: true};
        this.setView({wiring_types: wiringTypes});
        if (cy) cy.style(this.graphStyle());
        this.applyFlows();
      },

      // Writes this browser's view as the server default. Node positions and
      // the unsaved flag are left alone.
      async saveDefaultView() {
        try {
          const saved = await requestJSON('/api/v1/device/map/view', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
            body: JSON.stringify(this.view),
          });
          const view = (saved && saved.view) || this.view;
          this.deviceMap = {...this.deviceMap, view};
          this.savedDeviceMap = {...this.savedDeviceMap, view};
          this.localView = {};
          window.DeviceMapViewPrefs.store(viewStorage(), this.localView);
          this.$store.toasts.push(t('devicemap.view.default_saved'));
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      // Drops this browser's view settings, the server default applies again.
      resetViewToDefault() {
        this.localView = {};
        window.DeviceMapViewPrefs.store(viewStorage(), this.localView);
        this.renderGraph();
        this.$store.toasts.push(t('devicemap.view.default_restored'));
      },

      isLayerPending(name) {
        return window.DeviceMapModel.PENDING_LAYERS.includes(name);
      },

      toggleAutomationLinks() {
        this.setView({hide_automation_links: !this.view.hide_automation_links});
        this.renderGraph();
      },

      toggleWidthByPower() {
        this.setView({width_by_power: !this.view.width_by_power});
        this.applyFlows();
      },

      async toggleLayer(name) {
        if (this.isLayerPending(name) || !(name in window.DeviceMapModel.DEFAULT_LAYERS)) return;
        const turningOn = !this.view.layers[name];
        this.setView({layers: {...this.view.layers, [name]: turningOn}});
        this.closePopover();
        if (turningOn && (name === 'data' || name === 'balance')) {
          await this.loadFlows();
          const placed = this.placeVirtualNodes();
          if (placed) {
            this.placedCount = placed;
            this.renderGraph();
            return;
          }
        }
        if (cy) cy.style(this.graphStyle());
        this.applyFlows();
        this.applyDataEdges();
        this.applyVisibility({stagger: true});
        this.applySwitchState({animate: false});
        this.startFlowAnimation();
      },

      // Nodes, wiring edges and, with the energy layer on, the flow edges.
      buildElements() {
        const energy = this.energyByDevice();
        const colorOf = this.themeColor();
        const status = this.switchStatus();
        const nodes = this.devices.map(device => {
          const position = this.positionFor(device.id);
          const element = {
            data: {id: device.id, name: device.name || device.id, ...this.nodeImages(device, status, energy, colorOf)},
            classes: statusClass(device),
          };
          if (position) element.position = {x: position.x, y: position.y};
          return element;
        });
        const subNodes = this.subDevices().map(sub => {
          const orphan = this.subIsOrphan(sub);
          const element = {
            data: {id: sub.id, name: sub.label || sub.id, ...this.subNodeImages(sub, status, energy, colorOf)},
            classes: orphan ? 'devicemap-sub devicemap-sub-orphan' : 'devicemap-sub',
          };
          const position = this.positionFor(sub.id);
          if (position) element.position = {x: position.x, y: position.y};
          return element;
        });
        const groupNodes = Object.entries(this.savedGroups).map(([id, group]) => {
          const nodeId = window.DeviceMapModel.GROUP_PREFIX + id;
          const element = {data: {id: nodeId, name: group.label, svg: window.DeviceMapNodeSvg.groupDataUri(colorOf)}, classes: 'devicemap-group'};
          const position = this.positionFor(nodeId);
          if (position) element.position = {x: position.x, y: position.y};
          return element;
        });
        const df = window.DeviceMapDataflow;
        const positions = new Map((this.deviceMap.nodes || []).map(node => [node.device_id || node.virtual_id, node]));
        const stubInfo = this.stubInfo();
        for (const stub of stubInfo.nodes) {
          const anchor = positions.get(stub.anchor);
          if (anchor) positions.set(stub.id, df.stubPosition(anchor, stub.unresolved.direction, stub.index));
        }
        const virtualKinds = [
          ...(this.balanceId() === df.BALANCE_ID ? [{id: df.BALANCE_ID, kind: 'balance', name: t('devicemap.balance.name')}] : []),
          ...this.flowNodes().map(node => ({id: node.id, kind: node.kind, name: node.name})),
          ...stubInfo.nodes.map(stub => ({id: stub.id, kind: 'stub', name: t('devicemap.stub.name')})),
        ];
        const virtualNodes = window.DeviceMapGraph.virtualElements({kinds: virtualKinds, positions, colorOf});
        return [...nodes, ...subNodes, ...groupNodes, ...virtualNodes, ...this.buildEdgeElements(), ...this.flowElements(), ...window.DeviceMapGraph.dataEdgeElements(this.dataEdges())];
      },

      buildEdgeElements() {
        const wiring = window.DeviceMapGraph.wiringElements({devices: this.devices, deviceMap: this.deviceMap, groups: this.savedGroups, nodeIds: this.nodeIds(), energy: this.energyByDevice()});
        if (!this.view.hide_automation_links) return wiring;
        return wiring.filter(element => !window.DeviceMapDataflow.isAutomationLink({from: element.data.source, to: element.data.target}));
      },

      graphStyle() {
        // Cytoscape malt auf Canvas und kann kein var(--token) auflösen -
        // die Farben müssen deshalb als fertige Werte hereingereicht werden.
        const theme = window.DashboardTheme.colors({
          line: 'border', accent: 'accent', labelStrong: 'text-strong', panel: 'panel',
          info: 'info-line', faint: 'text-faint', warn: 'warn-line', subtle: 'text-subtle',
          ok: 'ok', bad: 'bad',
        });
        return window.DeviceMapGraph.style({view: this.view, theme, color: token => this.themeColor()(token), reducedMotion: this.reducedMotion()});
      },

      renderGraph() {
        this.selectedEdge = null; // any prior selection refers to a now-stale cy instance
        if (!this.$refs.canvas) return;
        this.adoptBalancePosition();
        this.placedCount = this.placeNewDevices() + this.placeNewGroups() + this.placeNewSubDevices() + this.placeVirtualNodes();
        const elements = this.buildElements();
        const hasAllPositions = this.devices.length > 0 && this.devices.every(device => this.positionFor(device.id))
          && this.groupIds().every(id => this.positionFor(id));
        // renderGraph() destroys and recreates the cy instance on every
        // structural change (connect, disconnect, discard) - a fresh
        // instance otherwise resets pan/zoom to the layout's default fit,
        // throwing the user out of whatever part of the map they were
        // looking at for what is, from their perspective, a single edge
        // changing. Capture the outgoing viewport and re-apply it verbatim
        // (fit: false so the new layout doesn't fight it) instead.
        const previousViewport = cy ? {zoom: cy.zoom(), pan: cy.pan()} : null;
        if (cy) {
          cy.destroy();
          cy = null;
        }
        const layout = hasAllPositions ? {name: 'preset'} : {name: 'breadthfirst', directed: true, padding: 30};
        if (previousViewport) layout.fit = false;
        // A new graph starts with no committed switch state and no running fade.
        switchGen += 1;
        for (const timer of switchTimers) this._clearTimeout(timer);
        switchTimers = [];
        switchShown = new Map();
        switchPorts = new Map();
        switchGoal = null;
        cy = cytoscape({
          container: this.$refs.canvas,
          elements,
          style: this.graphStyle(),
          layout,
          boxSelectionEnabled: false,
        });
        if (previousViewport) {
          cy.viewport(previousViewport);
        } else {
          // First-ever render: give the floating toolbar (.devicemap-overlay,
          // manager.css) room above the graph instead of letting top-row
          // nodes land right underneath it. Later renders skip this and
          // restore the exact prior pan/zoom instead (see above).
          cy.panBy({x: 0, y: 72});
        }
        // Nodes are grabbable by default, so with a mouse the tiniest bit of
        // movement between mousedown/mouseup turns a click into a drag
        // (firing dragfree instead of tap) - connect mode would then never
        // see the tap. Disabling grab while connect mode is active makes a
        // click unambiguously a tap, on mouse and touch alike.
        cy.autoungrabify(this.connectMode || !this.canEditMap);
        cy.on('drag', 'node', event => this.onNodeDrag(event));
        cy.on('dragfree', 'node', event => this.onNodeDragFree(event));
        cy.on('tap', 'node', event => this.onNodeTap(event));
        cy.on('tap', 'edge', event => {
          if (this.connectMode) return;
          this.viewMenuOpen = false;
          if (event.target.hasClass('devicemap-data')) { this.clearEdgeSelection(); this.openEdgePopover(event.target.data('flowId'), event.renderedPosition); }
          else this.onEdgeTap(event);
        });
        cy.on('tap', event => { if (event.target === cy) this.onBackgroundTap(); });
        cy.on('mouseover', 'edge.devicemap-data', event => event.target.addClass('devicemap-hover'));
        cy.on('mouseout', 'edge.devicemap-data', event => event.target.removeClass('devicemap-hover'));
        cy.on('viewport', () => this.syncGridBackground());
        this.syncGridBackground();
        if (this.$refs.labels && !labels) labels = window.DeviceMapLabels.create(this.$refs.labels);
        if (labels) {
          labels.update(this.labelItems(this.shownStatus()));
          cy.on('render', () => labels.sync(cy));
          labels.sync(cy);
        }
        this.applyVisibility();
        this.applySwitchState({animate: false});
        if (this.focusId) this.setFocus(this.focusId);
        this.startFlowAnimation();
      },

      // Continuous feedback while the pointer is still down (apple-design
      // Skill Abschnitt 1/2 - Response/Direct manipulation), without fighting
      // the gesture: forcing the dragged node itself onto the grid every
      // frame (the previous approach) fought Cytoscape's own drag tracking,
      // which computes each frame's position from the pointer's delta since
      // grab - overwriting it mid-drag desynced that delta and made
      // dragging feel broken/stuck once the node reached a grid line. The
      // real node now always tracks the pointer 1:1, unmodified; a separate,
      // non-interactive ghost node previews where it will actually land.
      onNodeDrag(event) {
        if (!this.view.snap_to_grid || !cy) return;
        const gridSize = this.view.grid_size;
        const position = event.target.position();
        const snapped = {x: Math.round(position.x / gridSize) * gridSize, y: Math.round(position.y / gridSize) * gridSize};
        const ghost = cy.getElementById(SNAP_GHOST_ID);
        if (ghost.empty()) {
          cy.add({group: 'nodes', data: {id: SNAP_GHOST_ID}, position: snapped, classes: 'devicemap-ghost', selectable: false, grabbable: false});
        } else {
          ghost.position(snapped);
        }
      },

      onNodeDragFree(event) {
        if (cy) {
          const ghost = cy.getElementById(SNAP_GHOST_ID);
          if (!ghost.empty()) ghost.remove();
        }
        const node = event.target;
        let position = node.position();
        if (this.view.snap_to_grid) {
          const gridSize = this.view.grid_size;
          position = {x: Math.round(position.x / gridSize) * gridSize, y: Math.round(position.y / gridSize) * gridSize};
          node.position(position);
        }
        const id = node.id();
        const df = window.DeviceMapDataflow;
        const key = df.isVirtualId(id) ? 'virtual_id' : 'device_id';
        const nodes = (this.deviceMap.nodes || []).filter(existing => (existing.device_id || existing.virtual_id) !== id);
        // Stubs have no position of their own and are never saved.
        if (key === 'device_id' || df.isPersistedVirtual(id)) nodes.push({[key]: id, x: position.x, y: position.y});
        this.deviceMap = {...this.deviceMap, nodes};
        this.unsaved = true;
      },

      toggleSnapToGrid() {
        this.setView({snap_to_grid: !this.view.snap_to_grid});
      },

      toggleShowGrid() {
        this.setView({show_grid: !this.view.show_grid});
        this.syncGridBackground();
      },

      setEdgeStyle(style) {
        if (!window.DeviceMapGraph.EDGE_STYLES[style]) return;
        this.setView({edge_style: style});
        if (cy) cy.style(this.graphStyle());
      },

      // Keeps the CSS background grid aligned with the Cytoscape canvas as
      // the user zooms/pans, without redrawing anything on every frame.
      syncGridBackground() {
        if (!this.$refs.canvas) return;
        const canvas = this.$refs.canvas;
        canvas.classList.toggle('devicemap-grid', !!this.view.show_grid);
        if (!cy) return;
        const gridSize = this.view.grid_size;
        const zoom = cy.zoom();
        const pan = cy.pan();
        canvas.style.setProperty('--devicemap-grid-px', `${gridSize * zoom}px`);
        canvas.style.setProperty('--devicemap-grid-x', `${pan.x}px`);
        canvas.style.setProperty('--devicemap-grid-y', `${pan.y}px`);
      },

      async toggleConnectMode() {
        if (!this.canEditMap) return;
        if (!this.connectMode && this.panelId) {
          if (!(await this.leavePanel())) return;
          this.closePanel();
        }
        this.connectMode = !this.connectMode;
        if (this.focusId) this.clearFocus();
        this.connectSourceId = null;
        this.connectSourceLabel = '';
        this.clearEdgeSelection();
        if (cy) {
          cy.autoungrabify(this.connectMode || !this.canEditMap);
          cy.nodes().removeClass('devicemap-connect-source');
        }
      },

      // Normal mode only ever updates x/y (onNodeDragFree). A structural
      // relation is only ever created here, after an explicit two-click
      // gesture plus a confirmation dialog - never as a side effect of drag.
      async onNodeTap(event) {
        if (!this.connectMode) {
          const id = event.target.id();
          const kind = window.DeviceMapDataflow.nodeKind(id);
          if (kind === 'balance' || id === this.balanceId() || kind === 'rule' || kind === 'service' || kind === 'stub') {
            if (!(await this.leavePanel())) return;
            this.closePanel();
            this.setFocus(id);
            this.openNodePopover(id, event.renderedPosition);
            return;
          }
          this.closePopover();
          if (this.panelId === id) return;
          if (!(await this.leavePanel())) return;
          this.setFocus(id);
          this.openPanel(id);
          return;
        }
        // In connect mode only devices and groups can be connected.
        if (window.DeviceMapDataflow.nodeKind(event.target.id()) !== 'device' && !window.DeviceMapModel.isGroupId(event.target.id())) return;
        const node = event.target;
        if (!this.connectSourceId) {
          this.connectSourceId = node.id();
          this.connectSourceLabel = this.deviceLabel(node.id());
          node.addClass('devicemap-connect-source');
          return;
        }
        const childId = this.connectSourceId;
        const parentId = node.id();
        if (cy) cy.nodes().removeClass('devicemap-connect-source');
        this.connectSourceId = null;
        this.connectSourceLabel = '';
        if (childId === parentId) return;
        const model = window.DeviceMapModel;
        if (model.isGroupId(parentId)) {
          await this.joinGroup(childId, parentId.slice(model.GROUP_PREFIX.length));
          return;
        }
        const confirmed = await this.$store.modal.confirm({
          title: t('devicemap.connect_confirmation_title', {child_name: this.deviceLabel(childId), parent_name: this.deviceLabel(parentId)}),
          confirmLabel: t('devicemap.connect_button'),
        });
        if (!confirmed) return;
        try {
          const created = await requestJSON('/api/v1/device/map/relations', {
            method: 'POST',
            headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
            body: JSON.stringify({child_id: childId, parent_id: parentId, kind: 'via_device'}),
          });
          this.deviceMap = {...this.deviceMap, edges: [...(this.deviceMap.edges || []), created]};
          // Relations are written to the server immediately (unlike node
          // positions) - fold the change into the snapshot too, so a later
          // discardChanges() only reverts still-unsaved positions/view.
          this.savedDeviceMap = {...this.savedDeviceMap, edges: this.deviceMap.edges};
          this.$store.toasts.push(t('devicemap.relation_connected'));
          this.notifyMapChanged();
          this.renderGraph();
          // Decision note: the connect dialog cannot ask for the type
          // (modal.confirm has no fields), so the new edge's card opens instead.
          // Set after renderGraph(), which clears the selection.
          this.selectedEdge = {id: `edge-${window.DeviceMapGraph.relationKey(childId, parentId)}`, source: parentId, target: childId, overrideId: created.id, membership: null};
          const element = cy && cy.getElementById(this.selectedEdge.id);
          if (element && !element.empty()) element.addClass('devicemap-selected-edge');
          this.openWiringPopover(element && !element.empty() ? element.renderedMidpoint() : null);
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      // The group a device or group currently belongs to, as [id, group], or
      // undefined. childId is a device id or a "group:" node id.
      groupOf(childId) {
        const model = window.DeviceMapModel;
        const memberKey = model.isGroupId(childId) ? 'groups' : 'devices';
        const member = model.isGroupId(childId) ? childId.slice(model.GROUP_PREFIX.length) : childId;
        return Object.entries(this.savedGroups).find(([, group]) => (group.members[memberKey] || []).includes(member));
      },

      // The groups patch that moves every child into groupId, taking each one
      // out of the group it was in before.
      membershipPatch(childIds, groupId) {
        const model = window.DeviceMapModel;
        const groups = {};
        const copy = id => groups[id] || (groups[id] = JSON.parse(JSON.stringify(this.savedGroups[id])));
        for (const childId of childIds) {
          const memberKey = model.isGroupId(childId) ? 'groups' : 'devices';
          const member = model.isGroupId(childId) ? childId.slice(model.GROUP_PREFIX.length) : childId;
          const previous = this.groupOf(childId);
          if (previous && previous[0] !== groupId) {
            const old = copy(previous[0]);
            old.members[memberKey] = old.members[memberKey].filter(id => id !== member);
          }
          const target = copy(groupId);
          if (!target.members[memberKey].includes(member)) target.members[memberKey].push(member);
        }
        return groups;
      },

      // Everything the device picker offers for groupId: devices (not the
      // node's own energy device) and other groups that are not members yet.
      pickerItems(groupId) {
        const model = window.DeviceMapModel;
        const group = this.savedGroups[groupId] || {members: {}};
        const {energy, children, dirOf} = this.energyFlowContext();
        const valueText = id => {
          const flow = model.edgeFlow(id, energy, children, {dirOf});
          return flow ? model.formatPower(flow.value) : t('devicemap.value.no_values');
        };
        const memberOf = id => {
          const previous = this.groupOf(id);
          return previous && previous[0] !== groupId ? previous[1].label : '';
        };
        const fallbackIcon = this.iconMarkup['energy-node:chip'] || '';
        const devices = this.devices
          .filter(device => device.id !== model.OWN_ENERGY_DEVICE_ID && !(group.members.devices || []).includes(device.id))
          .map(device => ({
            id: device.id, kind: 'device', name: device.name || device.id, section: device.manufacturer || '',
            valueText: valueText(device.id), memberOf: memberOf(device.id),
            iconMarkup: this.iconMarkup[device.icon_name] || this.iconMarkup[device.suggested_icon] || fallbackIcon,
          }));
        const groups = Object.entries(this.savedGroups)
          .filter(([other]) => other !== groupId && !(group.members.groups || []).includes(other))
          .map(([other, def]) => ({
            id: model.GROUP_PREFIX + other, kind: 'group', name: def.label, section: '',
            valueText: valueText(model.GROUP_PREFIX + other), memberOf: memberOf(model.GROUP_PREFIX + other), iconMarkup: '',
          }));
        return [...groups, ...devices];
      },

      async addGroupMembers() {
        const groupId = this.panelId.slice(window.DeviceMapModel.GROUP_PREFIX.length);
        const group = this.savedGroups[groupId];
        const ids = await this.openPicker({title: t('device_picker.title', {group: group.label}), items: this.pickerItems(groupId)});
        if (!ids) return;
        const moving = ids.filter(id => this.groupOf(id));
        if (moving.length) {
          const confirmed = await this.$store.modal.confirm({
            title: t('devicemap.group.join_many_title', {group: group.label}),
            body: t('devicemap.group.join_many_move_body', {names: moving.map(id => `„${this.deviceLabel(id)}“`).join(', ')}),
            confirmLabel: t('devicemap.connect_button'),
          });
          if (!confirmed) return;
        }
        try {
          await this.patchEnergy({groups: this.membershipPatch(ids, groupId)});
          this.$store.toasts.push(t('devicemap.group.joined'));
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      async joinGroup(childId, groupId) {
        const previous = this.groupOf(childId);
        const moving = previous && previous[0] !== groupId;
        const confirmed = await this.$store.modal.confirm({
          title: t('devicemap.group.join_title', {child_name: this.deviceLabel(childId), group: this.savedGroups[groupId].label}),
          body: moving ? t('devicemap.group.join_move_body', {group: previous[1].label}) : '',
          confirmLabel: t('devicemap.connect_button'),
        });
        if (!confirmed) return;
        const groups = this.membershipPatch([childId], groupId);
        try {
          await this.patchEnergy({groups});
          this.$store.toasts.push(t('devicemap.group.joined'));
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      openGroupDialog() {
        this.groupName = '';
        const dialog = this.$refs.groupDialog;
        if (dialog && typeof dialog.showModal === 'function') dialog.showModal(); else if (dialog) dialog.open = true;
      },

      async createGroup(label) {
        const name = String(label || '').trim();
        if (!name) return;
        const id = window.DeviceMapModel.slugId(name, Object.keys(this.savedGroups));
        try {
          await this.patchEnergy({groups: {[id]: {label: name, members: {devices: [], groups: []}}}});
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      setGroupDraft(field, value) {
        const id = this.panelId.slice(window.DeviceMapModel.GROUP_PREFIX.length);
        const group = this.savedGroups[id];
        const next = {...(this.groupDraft || {}), [field]: value};
        const sameLabel = next.label === undefined || next.label === group.label;
        const sameRole = next.role === undefined || next.role === (group.role || '');
        this.groupDraft = sameLabel && sameRole ? null : next;
      },

      async saveGroupPanel() {
        if (!this.groupDraft) return;
        const id = this.panelId.slice(window.DeviceMapModel.GROUP_PREFIX.length);
        const group = JSON.parse(JSON.stringify(this.savedGroups[id]));
        if (this.groupDraft.label !== undefined) group.label = this.groupDraft.label.trim() || group.label;
        if (this.groupDraft.role !== undefined) {
          if (this.groupDraft.role) group.role = this.groupDraft.role; else delete group.role;
        }
        this.panelSaving = true;
        try {
          await this.patchEnergy({groups: {[id]: group}});
          this.groupDraft = null;
          this.renderGraph();
          this.$store.toasts.push(t('devicemap.group.saved'));
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        } finally {
          this.panelSaving = false;
        }
      },

      groupCountsWhole() {
        const view = this.panelView;
        return Boolean(view && view.role) || this.groupWholePending;
      },

      // Switch "count as a whole": off clears the role draft, on picks the
      // first consumer category or waits until one is created.
      setGroupWhole(on) {
        const view = this.panelView;
        if (!view) return;
        this.groupWholePending = on;
        if (!on) { this.setGroupDraft('role', ''); return; }
        const first = view.roleOptions.find(option => option.value);
        if (!view.role && first) this.setGroupDraft('role', first.value);
      },

      savePanelAny() {
        return window.DeviceMapModel.isGroupId(this.panelId) ? this.saveGroupPanel() : this.savePanel();
      },

      async removeGroupMember(memberId) {
        const id = this.panelId.slice(window.DeviceMapModel.GROUP_PREFIX.length);
        this.selectedEdge = {id: '', source: this.panelId, target: memberId, overrideId: null, membership: {group: id, member: memberId}};
        await this.removeSelectedRelation();
        this.selectedEdge = null;
      },

      async deleteGroup() {
        const id = this.panelId.slice(window.DeviceMapModel.GROUP_PREFIX.length);
        const confirmed = await this.$store.modal.confirm({
          title: t('devicemap.group.delete_title', {group: this.savedGroups[id].label}),
          body: t('devicemap.group.delete_body'),
          confirmLabel: t('common.delete'),
          danger: true,
        });
        if (!confirmed) return;
        try {
          await this.patchEnergy({groups: {[id]: null}});
          this.groupDraft = null;
          this.closePanel();
          this.clearFocus();
          const nodeId = `group:${id}`;
          this.deviceMap = {...this.deviceMap,
            nodes: (this.deviceMap.nodes || []).filter(node => node.virtual_id !== nodeId),
            edges: (this.deviceMap.edges || []).filter(edge => edge.child_id !== nodeId)};
          this.savedDeviceMap = {...this.savedDeviceMap, edges: this.deviceMap.edges};
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      categoryForm: {label: '', base: 'consumer', color: 'cat_1', icon: ''},

      // forGroup: opened from the group panel's role chips. The new category
      // then becomes the group's role draft.
      openCategoryDialog(forGroup = false) {
        this.categoryForGroup = forGroup === true;
        this.categoryForm = {label: '', base: 'consumer', color: 'cat_1', icon: (this.iconCatalogue[0] || {}).name || ''};
        const dialog = this.$refs.categoryDialog;
        if (dialog && typeof dialog.showModal === 'function') dialog.showModal(); else if (dialog) dialog.open = true;
      },

      // slugId() falls back to "gruppe" when the label has no letters or digits at all.
      async createCategory() {
        const form = this.categoryForm;
        const label = String(form.label || '').trim();
        if (!label) return;
        const id = window.DeviceMapModel.slugId(label, Object.keys(this.savedCategories));
        try {
          await this.patchEnergy({categories: {[id]: {label, base: form.base, color: form.color, icon: form.icon}}});
          if (this.categoryForGroup && form.base === 'consumer' && window.DeviceMapModel.isGroupId(this.panelId)) {
            this.setGroupDraft('role', `custom:${id}`);
          }
          this.categoryForGroup = false;
          if (this.$refs.categoryDialog && this.$refs.categoryDialog.close) this.$refs.categoryDialog.close();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      customHint(role) {
        if (!role || !role.startsWith('custom:')) return '';
        const def = this.savedCategories[role.slice('custom:'.length)];
        if (!def) return '';
        const base = t(`devicemap.category.base.${def.base}`);
        return t('devicemap.panel.custom_hint', {base});
      },

      // Selecting an edge is the first step of "Lösen von Pfaden": it only
      // highlights the edge and opens its card, which offers
      // removeSelectedRelation() - nothing is deleted until that explicit,
      // confirmed action.
      onEdgeTap(event) {
        const edge = event.target;
        const data = edge.data();
        this.popover = null;
        this.selectedEdge = {id: data.id, source: data.source, target: data.target, overrideId: data.overrideId || null, membership: data.membership || null,
          subDevice: window.DeviceMapSubDevices.isSubId(data.target)};
        if (cy) {
          cy.edges().removeClass('devicemap-selected-edge');
          edge.addClass('devicemap-selected-edge');
        }
        this.openWiringPopover(event.renderedPosition);
      },

      clearEdgeSelection() {
        this.selectedEdge = null;
        if (cy) cy.edges().removeClass('devicemap-selected-edge');
      },

      // Only relations created through the connect gesture (RelationOverride,
      // identified by overrideId) can be dissolved here. Edges derived purely
      // from MQTT discovery (via_device) have no overrideId and reflect real
      // wiring, not a locally stored decision, so there is nothing to delete.
      async removeSelectedRelation() {
        // Captured up front: the confirmation is awaited, and closing the
        // card meanwhile clears this.selectedEdge.
        const selected = this.selectedEdge;
        if (!selected || (!selected.overrideId && !selected.membership) || selected.subDevice) return;
        const confirmed = await this.$store.modal.confirm({
          title: t('devicemap.disconnect_confirmation_title', {label: this.selectedEdgeLabel}),
          confirmLabel: t('devicemap.disconnect_confirm'),
          danger: true,
        });
        if (!confirmed) return;
        this.popover = null;
        const membership = selected.membership;
        if (membership) {
          const model = window.DeviceMapModel;
          const memberKey = model.isGroupId(membership.member) ? 'groups' : 'devices';
          const member = model.isGroupId(membership.member) ? membership.member.slice(model.GROUP_PREFIX.length) : membership.member;
          const group = JSON.parse(JSON.stringify(this.savedGroups[membership.group]));
          group.members[memberKey] = group.members[memberKey].filter(id => id !== member);
          try {
            await this.patchEnergy({groups: {[membership.group]: group}});
            this.$store.toasts.push(t('devicemap.relation_disconnected'));
            this.renderGraph();
          } catch (error) {
            this.$store.toasts.push(error.message, 'critical');
          }
          return;
        }
        try {
          await requestJSON(`/api/v1/device/map/relations/${selected.overrideId}`, {method: 'DELETE', headers: {'X-CSRF-Token': this.csrfToken}});
          this.deviceMap = {...this.deviceMap, edges: (this.deviceMap.edges || []).filter(edge => edge.id !== selected.overrideId)};
          this.savedDeviceMap = {...this.savedDeviceMap, edges: this.deviceMap.edges};
          this.$store.toasts.push(t('devicemap.relation_disconnected'));
          this.notifyMapChanged();
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      async save() {
        this.saving = true;
        try {
          // Defensive filter, not a normal-path concern: the snap ghost
          // (onNodeDrag()) is always removed on dragfree, so it should never
          // still be in cy.nodes() here - but it's not a real device, and
          // saving it would corrupt device-map.json.
          const df = window.DeviceMapDataflow;
          const graph = cy || this._cyForTest;
          const positioned = graph ? graph.nodes().filter(node => node.id() !== SNAP_GHOST_ID).map(node => ({id: node.id(), position: node.position()}))
            : (this.deviceMap.nodes || []).map(node => ({id: node.device_id || node.virtual_id, position: {x: node.x, y: node.y}}));
          // Groups that were deleted and devices that vanished are dropped
          // here, so the map never keeps nodes that point at nothing. Stubs
          // are never saved, only devices and the persisted virtual nodes.
          // The graph also holds the balance and data flow nodes of a hidden
          // layer, at the origin until the layer places them. Only a virtual
          // node that was placed is saved, otherwise turning the layer on
          // later would find it "placed" on top of the first device.
          const known = this.nodeIds();
          const placed = new Set((this.deviceMap.nodes || []).map(node => node.virtual_id).filter(Boolean));
          const nodes = positioned
            .filter(entry => known.has(entry.id) && (df.nodeKind(entry.id) === 'device' || (df.isPersistedVirtual(entry.id) && placed.has(entry.id))))
            .map(entry => (df.isVirtualId(entry.id)
              ? {virtual_id: entry.id, x: entry.position.x, y: entry.position.y}
              : {device_id: entry.id, x: entry.position.x, y: entry.position.y}));
          // edge_props of pairs that no longer exist are dropped here, the
          // same way vanished devices are dropped from nodes above.
          // Sub-device pairs are live as long as their meter is on the map, so
          // prune keeps their channel.
          const liveKeys = new Set([...this.buildEdgeElements().filter(element => !element.data.membership)
            .map(element => window.DeviceMapGraph.relationKey(element.data.source, element.data.target)),
            ...window.DeviceMapSubDevices.pairs(this.deviceMap, known).map(pair => window.DeviceMapGraph.relationKey(pair.parent, pair.child))]);
          const edgeProps = window.DeviceMapWiring.prune(this.deviceMap.edge_props, liveKeys);
          // The switch roles live in the energy roles, the map keeps only the
          // edge channels (edge_props).
          // sub_devices go back unchanged, the server keeps its own copy anyway.
          const value = {version: 2, nodes, edges: this.deviceMap.edges || [], edge_props: edgeProps, view: this.serverView, sub_devices: this.subDevices()};
          await requestJSON('/api/v1/device/map', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json', 'X-CSRF-Token': this.csrfToken},
            body: JSON.stringify(value),
          });
          this.deviceMap = value;
          this.savedDeviceMap = JSON.parse(JSON.stringify(value));
          this.unsaved = false;
          this.$store.toasts.push(t('devicemap.positions_saved'));
          this.notifyMapChanged();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        } finally {
          this.saving = false;
        }
      },

      // Discards unsaved node/view changes by restoring the last
      // server-confirmed snapshot, without a network round-trip - mirrors
      // config.page.js's resetForm()/renderForm() shape. Relation edges are
      // never "unsaved" (they're written to the server immediately, see
      // onNodeTap()/removeSelectedRelation()), so this never undoes those.
      discardChanges() {
        this.deviceMap = JSON.parse(JSON.stringify(this.savedDeviceMap));
        this.unsaved = false;
        this.$store.toasts.push(t('devicemap.changes_discarded'));
        this.renderGraph();
      },

      confirmUnsavedUnload(event) {
        if (!this.unsaved && !this.panelDirty()) return;
        event.preventDefault();
        event.returnValue = '';
      },

      saveButtonLabel() {
        return this.saving ? t('devicemap.saving') : t('devicemap.save_button');
      },

      connectHintText() {
        return this.connectSourceLabel
          ? t('devicemap.connect_hint_selecting', {name: this.connectSourceLabel})
          : t('devicemap.connect_hint_initial');
      },

      placedHintText() {
        return tn('devicemap.placed_hint', this.placedCount, {n: this.placedCount});
      },
    };
  };

  const register = () => {
    if (window.Alpine) window.Alpine.data("devicemapPanel", devicemapPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();
