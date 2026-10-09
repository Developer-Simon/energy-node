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

  const DEFAULT_VIEW = {snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight', width_by_power: false};

  // Synthetic id for the snap-preview node (see onNodeDrag()) - never a real
  // device_id, so it can't collide with one.
  const SNAP_GHOST_ID = '__devicemap-snap-ghost__';

  const statusClass = device => `devicemap-status-${window.DeviceMapModel.deviceHealth(device)}`;

  const devicemapPanel = () => {
    // Kept outside the returned (Alpine-reactive) object on purpose: a
    // Cytoscape instance is a large mutable object graph that Alpine would
    // otherwise deep-proxy, which is unnecessary work and a likely source of
    // subtle bugs (Cytoscape mutates itself heavily on every render/drag).
    let cy = null;
    let labels = null;

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
      placedCount: 0,
      themeOff: null,
      energy: null,
      energyUnavailable: false,
      flows: null, // {nodes, edges, unresolved} from /api/v1/device/map/flows, null while unavailable
      flowsUnavailable: false,
      invalidConfigs: 0,
      popover: null, // {eyebrow, title, rows, lists, text, linkText, link, action, left, top, origin, open} | null
      focusId: null,
      focusRelated: new Set(),
      panelId: null,
      drafts: {},
      savedAssignments: {},
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
      csrfToken: '',
      panelSaving: false,
      registryOff: null,
      _energyTimer: null,
      _energyPending: false,
      _setTimeout: (fn, ms) => setTimeout(fn, ms),
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
          const [devices, deviceMap, energy, roles, session, icons, flows] = await Promise.all([
            requestJSON('/api/v1/devices'),
            requestJSON('/api/v1/device/map'),
            requestJSON('/api/v1/energy').catch(() => null),
            requestJSON('/api/v1/energy/roles').catch(() => null),
            requestJSON('/api/v1/auth/session').catch(() => null),
            requestJSON('/api/v1/device/icons').catch(() => []),
            requestJSON('/api/v1/device/map/flows').catch(() => null),
          ]);
          this.iconCatalogue = Array.isArray(icons) ? icons : [];
          this.iconMarkup = Object.fromEntries(this.iconCatalogue.map(icon => [icon.name, icon.markup]));
          this.savedAssignments = (roles && roles.assignments) || {};
          this.savedGroups = (roles && roles.groups) || {};
          this.savedCategories = (roles && roles.categories) || {};
          // Ohne Sitzungs-API laeuft die Instanz ohne Authentifizierung, dann
          // laesst requireEnergyMutation den Schreibzugriff durch.
          this.csrfToken = (session && session.csrf_token) || '';
          this.canEditEnergy = !session || session.edit_energy !== false;
          this.devices = devices || [];
          this.deviceMap = deviceMap || {version: 1, nodes: [], edges: []};
          this.energy = energy;
          this.energyUnavailable = energy === null;
          this.flows = flows;
          this.flowsUnavailable = flows === null;
          this.invalidConfigs = this.stubInfo().invalid;
          this.savedDeviceMap = JSON.parse(JSON.stringify(this.deviceMap));
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
        if (this.lifecycleOff) this.lifecycleOff();
        this.stopFlowAnimation();
        clearTimeout(this._energyTimer);
        if (labels) { labels.destroy(); labels = null; }
      },

      flowElements() {
        if (!this.view.layers.energy || !this.energy) return [];
        return window.DeviceMapGraph.flowElements({pairs: this.wiringPairs(), energy: this.energyByDevice(), view: this.view});
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
        if (id === window.DeviceMapDataflow.BALANCE_ID) return t('devicemap.balance.name');
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
            const described = view.part === 'action' ? window.__automationsView.describeAction(raw, null) : window.__automationsView.describeCondition(raw, null);
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
        if (kind === 'balance') {
          const items = df.roleEdges(this.previewEnergy()).map(edge => `${this.labelOf(edge.from)}: ${edge.roles.map(role => (role.startsWith('custom:') ? this.labelOf(role) : t(`energy.role_label.${role}`))).join(', ')}`);
          this.placePopover(rendered, {eyebrow: t('devicemap.pop.eyebrow.virtual'), title: t('devicemap.balance.name'), rows: [], text: t('devicemap.balance.description'),
            lists: [{label: '', items}], linkText: t('devicemap.pop.link.energy'), link: {tab: 'energy', entities: df.roleEdges(this.previewEnergy()).flatMap(edge => edge.entities)}, action: null});
          return;
        }
        const node = this.flowNodes().find(candidate => candidate.id === id);
        if (!node) return;
        if (kind === 'rule') {
          const view = window.__automationsView;
          const line = described => [described.title, described.summary].filter(Boolean).join(': ');
          this.placePopover(rendered, {eyebrow: t('devicemap.pop.eyebrow.rule'), title: node.name, rows: [], text: '',
            lists: [
              {label: t('devicemap.pop.when'), items: (node.node.conditions || []).map(condition => line(view.describeCondition(condition, null)))},
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
      openWiringPopover(rendered) {
        const edge = this.selectedEdge;
        if (!edge) return;
        const model = window.DeviceMapModel;
        const energy = this.energyByDevice();
        const flow = model.edgeFlow(edge.target, energy, model.childrenIndex(this.wiringPairs()));
        const entry = energy.get(edge.target);
        const origin = edge.membership ? 'group' : edge.overrideId ? 'manual' : 'discovery';
        const rows = [];
        if (flow) rows.push([t('devicemap.pop.row.power'), model.flowLabel(flow)]);
        if (entry && entry.roles.length) {
          rows.push([t('devicemap.pop.row.roles'), entry.roles.map(role => (role.startsWith('custom:') ? this.labelOf(role) : t(`energy.role_label.${role}`))).join(', ')]);
        }
        rows.push([t('devicemap.pop.row.origin'), t(`devicemap.pop.origin.${origin}`)]);
        const opensPanel = edge.target !== model.OWN_ENERGY_DEVICE_ID;
        this.placePopover(rendered, {
          eyebrow: t(origin === 'group' ? 'devicemap.pop.eyebrow.membership' : 'devicemap.pop.eyebrow.wiring'),
          title: this.selectedEdgeLabel, rows, lists: [],
          text: origin === 'discovery' ? t('devicemap.edge_readonly_hint') : '',
          linkText: opensPanel ? t('devicemap.pop.link.open_device', {name: this.deviceLabel(edge.target)}) : '',
          link: opensPanel ? {tab: 'panel', target: edge.target} : null,
          action: origin === 'discovery' ? null : {label: t('devicemap.disconnect_button')},
        });
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
        try {
          this.energy = await requestJSON('/api/v1/energy');
          this.energyUnavailable = false;
        } catch (error) {
          this.energy = null;
          this.energyUnavailable = true;
        }
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
        return new Set([...this.devices.map(device => device.id), ...this.groupIds(), df.BALANCE_ID, ...this.flowNodes().map(node => node.id)]);
      },

      stubInfo() {
        return window.DeviceMapDataflow.stubs(this.flows, this.baseNodeIds());
      },

      dataEdges() {
        const df = window.DeviceMapDataflow;
        return [...df.roleEdges(this.previewEnergy()), ...df.flowEdges(this.flows, this.baseNodeIds()), ...this.stubInfo().edges];
      },

      previewEnergy() {
        return window.DeviceMapModel.applyDrafts(this.energy, this.drafts, this.devices);
      },

      energyByDevice() {
        return window.DeviceMapModel.deviceEnergy(this.previewEnergy());
      },

      setDraft(entityId, field, value) {
        const model = window.DeviceMapModel;
        const base = model.baseAssignment(entityId, this.savedAssignments, this.energy);
        const current = this.drafts[entityId] || {role: base.role, scale: base.scale, invert: base.invert, capacity_kwh: base.capacity_kwh};
        let next = {...current, [field]: value};
        if (field === 'scale') next.scale = Math.max(0.01, Number(value) || 1);
        if (field === 'capacity_kwh') next.capacity_kwh = Math.max(0, Number(value) || 0);
        if (field === 'role' && current.pin) next = {...next, pin: false};
        const drafts = {...this.drafts};
        if (model.isDraftChange(base, next)) drafts[entityId] = next; else delete drafts[entityId];
        this.drafts = drafts;
        this.applyEnergy();
      },

      get panelView() {
        if (!this.panelId) return null;
        const model = window.DeviceMapModel;
        if (model.isGroupId(this.panelId)) {
          const id = this.panelId.slice(model.GROUP_PREFIX.length);
          const group = this.savedGroups[id];
          if (!group) return null;
          const draft = this.groupDraft || {};
          const energy = this.energyByDevice();
          const children = model.childrenIndex(this.wiringPairs());
          const flow = model.edgeFlow(this.panelId, energy, children);
          const members = [
            ...(group.members.devices || []).map(deviceId => ({id: deviceId, name: this.deviceLabel(deviceId)})),
            ...(group.members.groups || []).map(child => ({id: model.GROUP_PREFIX + child, name: (this.savedGroups[child] || {}).label || child})),
          ].map(member => {
            const memberFlow = model.edgeFlow(member.id, energy, children);
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
        const {rows, others} = model.panelRows({device, snapshot: this.energy, saved: this.savedAssignments, drafts: this.drafts});
        let empty = '';
        if (!rows.length && !others.length) empty = 'no_values';
        else if (!rows.length) empty = 'no_power';
        return {
          kind: 'device',
          title: device.name || device.id,
          health: t(`devicemap.panel.health.${model.deviceHealth(device)}`),
          rows, others, empty,
          flows: window.DeviceMapDataflow.deviceFlows(device.id, this.dataEdges(), id => this.labelOf(id)),
        };
      },

      openPanel(id) {
        this.panelId = id === window.DeviceMapModel.OWN_ENERGY_DEVICE_ID ? null : id;
        this.groupDraft = null;
        this.groupWholePending = false;
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
        const base = window.DeviceMapModel.baseAssignment(entityId, this.savedAssignments, this.energy);
        this.drafts = {...this.drafts, [entityId]: {role: base.role, scale: base.scale, invert: base.invert, capacity_kwh: base.capacity_kwh, pin: true}};
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

      nodeSvg(device, energy = this.energyByDevice(), colorOf = this.themeColor()) {
        const model = window.DeviceMapModel;
        const spec = model.ringSpec(energy.get(device.id), model.deviceHealth(device), (device.entities || []).length > 0, this.iconMarkup);
        return window.DeviceMapNodeSvg.dataUri(spec, colorOf);
      },

      labelItems() {
        const model = window.DeviceMapModel;
        const energy = this.energyByDevice();
        const devices = this.devices.map(device => ({
          id: device.id,
          name: device.name || device.id,
          value: model.nodeValueText(device, energy.get(device.id)),
        }));
        const children = model.childrenIndex(this.wiringPairs());
        const groups = Object.entries(this.savedGroups).map(([id, group]) => {
          const nodeId = model.GROUP_PREFIX + id;
          return {id: nodeId, name: group.label, value: model.groupValueText(model.edgeFlow(nodeId, energy, children))};
        });
        const balance = this.energy && this.energy.balance;
        const net = balance ? (Number(balance.grid_import) || 0) - (Number(balance.grid_export) || 0) : null;
        const virtual = [
          {id: 'balance', name: t('devicemap.balance.name'), value: net == null ? '' : t(net >= 0 ? 'devicemap.value.grid_import' : 'devicemap.value.grid_export', {value: model.formatPower(net)})},
          ...this.flowNodes().map(node => ({id: node.id, name: node.name, value: t(`devicemap.${node.kind}.value`)})),
          ...this.stubInfo().nodes.map(stub => ({id: stub.id, name: t('devicemap.stub.name'), value: ''})),
        ];
        return [...devices, ...groups, ...virtual];
      },

      // Live refresh without renderGraph(): a rebuild would reset Cytoscape's
      // internal state and restart every animation for what is only a value change.
      applyEnergy() {
        if (labels) labels.update(this.labelItems());
        if (!cy) return;
        const energy = this.energyByDevice();
        const colorOf = this.themeColor();
        cy.batch(() => {
          for (const device of this.devices) {
            const node = cy.getElementById(device.id);
            if (!node.empty()) node.data('svg', this.nodeSvg(device, energy, colorOf));
          }
        });
        if (labels) labels.sync(cy);
        this.applyFlows();
        this.applyDataEdges();
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
        window.dispatchEvent(new CustomEvent('device-map-changed'));
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
        if (layers.balance && !has(df.BALANCE_ID)) {
          const roleDevices = new Set(df.roleEdges(this.energy).map(edge => edge.from));
          const rolePositions = nodes.filter(node => roleDevices.has(node.device_id));
          nodes.push({virtual_id: df.BALANCE_ID, ...df.placeBalance({rolePositions, allPositions: nodes, snap})});
          placed += 1;
        }
        if (layers.data) {
          const edges = df.flowEdges(this.flows, this.baseNodeIds());
          const missing = this.flowNodes().filter(node => !has(node.id)).map(node => ({
            id: node.id,
            inputXs: edges.filter(edge => edge.to === node.id).map(edge => positionOf(edge.from)).filter(Boolean).map(position => position.x),
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
      get view() {
        const stored = this.deviceMap.view || {};
        return {...DEFAULT_VIEW, ...stored, layers: {...window.DeviceMapModel.DEFAULT_LAYERS, ...(stored.layers || {})}};
      },

      isLayerPending(name) {
        return window.DeviceMapModel.PENDING_LAYERS.includes(name);
      },

      toggleWidthByPower() {
        this.deviceMap = {...this.deviceMap, view: {...this.view, width_by_power: !this.view.width_by_power}};
        this.unsaved = true;
        this.applyFlows();
      },

      async toggleLayer(name) {
        if (this.isLayerPending(name) || !(name in window.DeviceMapModel.DEFAULT_LAYERS)) return;
        const turningOn = !this.view.layers[name];
        const layers = {...this.view.layers, [name]: turningOn};
        this.deviceMap = {...this.deviceMap, view: {...this.view, layers}};
        this.unsaved = true;
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
        this.startFlowAnimation();
      },

      // Nodes, wiring edges and, with the energy layer on, the flow edges.
      buildElements() {
        const energy = this.energyByDevice();
        const colorOf = this.themeColor();
        const nodes = this.devices.map(device => {
          const position = this.positionFor(device.id);
          const element = {
            data: {id: device.id, name: device.name || device.id, svg: this.nodeSvg(device, energy, colorOf)},
            classes: statusClass(device),
          };
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
          {id: df.BALANCE_ID, kind: 'balance', name: t('devicemap.balance.name')},
          ...this.flowNodes().map(node => ({id: node.id, kind: node.kind, name: node.name})),
          ...stubInfo.nodes.map(stub => ({id: stub.id, kind: 'stub', name: t('devicemap.stub.name')})),
        ];
        const virtualNodes = window.DeviceMapGraph.virtualElements({kinds: virtualKinds, positions, colorOf});
        return [...nodes, ...groupNodes, ...virtualNodes, ...this.buildEdgeElements(), ...this.flowElements(), ...window.DeviceMapGraph.dataEdgeElements(this.dataEdges())];
      },

      buildEdgeElements() {
        return window.DeviceMapGraph.wiringElements({devices: this.devices, deviceMap: this.deviceMap, groups: this.savedGroups, nodeIds: this.nodeIds()});
      },

      graphStyle() {
        // Cytoscape malt auf Canvas und kann kein var(--token) auflösen -
        // die Farben müssen deshalb als fertige Werte hereingereicht werden.
        const theme = window.DashboardTheme.colors({
          line: 'border', accent: 'accent', labelStrong: 'text-strong', panel: 'panel',
          info: 'info-line', faint: 'text-faint', warn: 'warn-line', subtle: 'text-subtle',
        });
        return window.DeviceMapGraph.style({view: this.view, theme, color: token => this.themeColor()(token), reducedMotion: this.reducedMotion()});
      },

      renderGraph() {
        this.selectedEdge = null; // any prior selection refers to a now-stale cy instance
        if (!this.$refs.canvas) return;
        this.placedCount = this.placeNewDevices() + this.placeNewGroups() + this.placeVirtualNodes();
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
        cy.autoungrabify(this.connectMode);
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
          labels.update(this.labelItems());
          cy.on('render', () => labels.sync(cy));
          labels.sync(cy);
        }
        this.applyVisibility();
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
        this.deviceMap = {...this.deviceMap, view: {...this.view, snap_to_grid: !this.view.snap_to_grid}};
        this.unsaved = true;
      },

      toggleShowGrid() {
        this.deviceMap = {...this.deviceMap, view: {...this.view, show_grid: !this.view.show_grid}};
        this.unsaved = true;
        this.syncGridBackground();
      },

      setEdgeStyle(style) {
        if (!window.DeviceMapGraph.EDGE_STYLES[style]) return;
        this.deviceMap = {...this.deviceMap, view: {...this.view, edge_style: style}};
        this.unsaved = true;
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
          cy.autoungrabify(this.connectMode);
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
          if (kind === 'balance' || kind === 'rule' || kind === 'service' || kind === 'stub') {
            if (!(await this.leavePanel())) return;
            this.closePanel();
            this.setFocus(id);
            this.openNodePopover(id, event.renderedPosition);
            return;
          }
          this.closePopover();
          if (this.panelId === id || (this.focusId === id && !this.panelId && id === window.DeviceMapModel.OWN_ENERGY_DEVICE_ID)) return;
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
            headers: {'Content-Type': 'application/json'},
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
        const energy = this.energyByDevice();
        const children = model.childrenIndex(this.wiringPairs());
        const valueText = id => {
          const flow = model.edgeFlow(id, energy, children);
          return flow ? model.formatPower(flow.value) : t('devicemap.value.no_values');
        };
        const memberOf = id => {
          const previous = this.groupOf(id);
          return previous && previous[0] !== groupId ? previous[1].label : '';
        };
        const fallbackIcon = (this.iconCatalogue[0] || {}).markup || '';
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
        this.selectedEdge = {id: data.id, source: data.source, target: data.target, overrideId: data.overrideId || null, membership: data.membership || null};
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
        if (!selected || (!selected.overrideId && !selected.membership)) return;
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
          await requestJSON(`/api/v1/device/map/relations/${selected.overrideId}`, {method: 'DELETE'});
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
          const value = {version: 2, nodes, edges: this.deviceMap.edges || [], view: this.view};
          await requestJSON('/api/v1/device/map', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
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
