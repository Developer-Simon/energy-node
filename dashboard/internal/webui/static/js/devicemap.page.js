(() => {
  const requestJSON = async (url, options) => {
    // The single chokepoint for every URL literal in this file: behind a
    // reverse-proxy subpath base.html puts the prefix into
    // __DASHBOARD_BASE_PATH__; on direct access it is empty.
    const response = await fetch(`${window.__DASHBOARD_BASE_PATH__ || ''}${url}`, options);
    if (response.status === 204) return null;
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.message || "Anfrage fehlgeschlagen");
    return body;
  };
  const relationKey = (a, b) => [a, b].sort().join('::');

  const DEFAULT_VIEW = {snap_to_grid: false, show_grid: false, grid_size: 40, edge_style: 'straight'};

  // Synthetic id for the snap-preview node (see onNodeDrag()) - never a real
  // device_id, so it can't collide with one.
  const SNAP_GHOST_ID = '__devicemap-snap-ghost__';

  // taxi/unbundled-bezier ship in the vendored cytoscape.min.js already, no new dependency.
  const EDGE_STYLES = {
    straight: {'curve-style': 'straight'},
    elbow: {'curve-style': 'taxi', 'taxi-direction': 'auto', 'taxi-turn': '50%', 'taxi-turn-min-distance': 5},
    curved: {'curve-style': 'unbundled-bezier', 'control-point-distances': [40], 'control-point-weights': [0.5]},
  };

  // Ratio of entities currently reporting available=true, counting only
  // entities that actually publish an availability topic (has_availability)
  // - most entities don't, and treating those as "unavailable" would make
  // almost every device look degraded.
  const statusClass = device => {
    const tracked = (device.entities || []).filter(entity => entity.has_availability);
    if (tracked.length === 0) return 'devicemap-status-unknown';
    const availableCount = tracked.filter(entity => entity.available).length;
    if (availableCount === tracked.length) return 'devicemap-status-ok';
    if (availableCount === 0) return 'devicemap-status-down';
    return 'devicemap-status-degraded';
  };

  const devicemapPanel = () => {
    // Kept outside the returned (Alpine-reactive) object on purpose: a
    // Cytoscape instance is a large mutable object graph that Alpine would
    // otherwise deep-proxy, which is unnecessary work and a likely source of
    // subtle bugs (Cytoscape mutates itself heavily on every render/drag).
    let cy = null;

    return {
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
      selectedEdge: null, // {id, source, target, overrideId} | null
      unsaved: false,
      placedCount: 0,
      themeOff: null,

      async load() {
        // load() läuft nach jedem Speichern erneut, die Registrierung darf
        // sich deshalb nicht stapeln. cy.style() statt renderGraph(), weil
        // ein voller Neuaufbau die Knotenpositionen neu berechnen würde.
        if (!this.themeOff) {
          this.themeOff = window.DashboardTheme.onChange(() => {
            if (cy) cy.style(this.graphStyle());
          });
        }
        this.loading = true;
        try {
          const [devices, deviceMap] = await Promise.all([
            requestJSON('/api/v1/devices'),
            requestJSON('/api/v1/device/map'),
          ]);
          this.devices = devices || [];
          this.deviceMap = deviceMap || {version: 1, nodes: [], edges: []};
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
          reload: () => this.load(),
          label: 'Revisionen der Device-Map',
        };
      },

      destroy() {
        if (this.themeOff) this.themeOff();
      },

      positionFor(deviceId) {
        return (this.deviceMap.nodes || []).find(node => node.device_id === deviceId);
      },

      // Places devices that have no saved position below the existing
      // arrangement instead of triggering a full re-layout (which would
      // discard every hand-placed position - see renderGraph()). Returns
      // how many devices were placed this way, or 0 when there is either no
      // existing arrangement to anchor to (first-ever load) or nothing missing.
      placeNewDevices() {
        const existingNodes = (this.deviceMap.nodes || []).filter(node =>
          this.devices.some(device => device.id === node.device_id));
        const missing = this.devices.filter(device => !this.positionFor(device.id));
        if (existingNodes.length === 0 || missing.length === 0) return 0;

        const stepX = 140;
        const stepY = 110;
        const minX = Math.min(...existingNodes.map(node => node.x));
        const maxX = Math.max(...existingNodes.map(node => node.x));
        const maxY = Math.max(...existingNodes.map(node => node.y));
        const columns = Math.max(1, Math.min(8, Math.round((maxX - minX) / stepX) + 1));
        const startY = maxY + stepY;
        const gridSize = this.view.grid_size;
        const snap = value => this.view.snap_to_grid ? Math.round(value / gridSize) * gridSize : value;

        const nodes = [...(this.deviceMap.nodes || [])];
        missing.forEach((device, index) => {
          const col = index % columns;
          const row = Math.floor(index / columns);
          nodes.push({device_id: device.id, x: snap(minX + col * stepX), y: snap(startY + row * stepY)});
        });
        this.deviceMap = {...this.deviceMap, nodes};
        this.unsaved = true;
        return missing.length;
      },

      deviceLabel(deviceId) {
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
        return {...DEFAULT_VIEW, ...(this.deviceMap.view || {})};
      },

      buildElements() {
        const nodeIds = new Set(this.devices.map(device => device.id));
        const elements = this.devices.map(device => {
          const position = this.positionFor(device.id);
          const entityCount = (device.entities || []).length;
          const element = {
            data: {
              id: device.id,
              label: `${device.name || device.id}\n${entityCount} Entität${entityCount === 1 ? '' : 'en'}`,
            },
            classes: statusClass(device),
          };
          if (position) element.position = {x: position.x, y: position.y};
          return element;
        });
        const seenEdges = new Set();
        // overrideId is only set for manually created relations (settings.RelationOverride) -
        // those are the only ones removeSelectedRelation() is allowed to delete.
        const addEdge = (childId, parentId, overrideId) => {
          if (!nodeIds.has(childId) || !nodeIds.has(parentId) || childId === parentId) return;
          const key = relationKey(childId, parentId);
          if (seenEdges.has(key)) return;
          seenEdges.add(key);
          const data = {id: `edge-${key}`, source: parentId, target: childId};
          if (overrideId) data.overrideId = overrideId;
          elements.push({data});
        };
        // deviceMap.edges (RelationOverride, carries the real overrideId)
        // must be processed before device.relations: the registry merges
        // overrides into each device's relations list too (so the graph and
        // other UIs see them as normal relations), but DeviceRelation has no
        // overrideId field there. addEdge()'s pair-dedup means whichever
        // pass runs first "wins" the overrideId - if device.relations ran
        // first, every override-derived edge would silently lose its
        // overrideId and render as an unremovable via_device edge.
        for (const edge of this.deviceMap.edges || []) {
          addEdge(edge.child_id, edge.parent_id, edge.id);
        }
        for (const device of this.devices) {
          for (const relation of device.relations || []) {
            if (relation.kind === 'parent') addEdge(device.id, relation.id);
            else if (relation.kind === 'child') addEdge(relation.id, device.id);
          }
        }
        return elements;
      },

      graphStyle() {
        // Cytoscape malt auf Canvas und kann kein var(--token) auflösen -
        // die Farben müssen deshalb als fertige Werte hereingereicht werden.
        const theme = window.DashboardTheme.colors({
          label: 'text-subtle', labelBg: 'bg', node: 'panel', line: 'border',
          okLine: 'ok-line', okBg: 'ok-bg', warnLine: 'warn-line', warnBg: 'warn-bg',
          badLine: 'bad-line', badBg: 'bad-bg', accent: 'accent',
        });
        return [
          {selector: 'node', style: {
            label: 'data(label)', 'text-wrap': 'wrap', 'text-max-width': '90px',
            'font-size': '9px', 'font-family': 'system-ui, sans-serif', color: theme.label,
            'text-valign': 'bottom', 'text-halign': 'center', 'text-margin-y': 6,
            'text-background-color': theme.labelBg, 'text-background-opacity': 0.85, 'text-background-padding': '2px',
            width: 34, height: 34, 'background-color': theme.node, 'border-width': 2, 'border-color': theme.line,
          }},
          {selector: 'node.devicemap-status-ok', style: {'border-color': theme.okLine, 'background-color': theme.okBg}},
          {selector: 'node.devicemap-status-degraded', style: {'border-color': theme.warnLine, 'background-color': theme.warnBg}},
          {selector: 'node.devicemap-status-down', style: {'border-color': theme.badLine, 'background-color': theme.badBg}},
          {selector: 'node.devicemap-status-unknown', style: {'border-color': theme.line, 'background-color': theme.node}},
          {selector: 'node.devicemap-connect-source', style: {'border-width': 3, 'border-color': theme.accent}},
          // Snap-preview (see onNodeDrag()): an unselectable, non-interactive
          // placeholder at the grid cell the dragged node will land on.
          // 'events: no' keeps it from stealing taps/drags from whatever is
          // underneath it.
          {selector: 'node.devicemap-ghost', style: {
            label: '', 'background-opacity': 0,
            'border-width': 2, 'border-style': 'dashed', 'border-color': theme.accent,
            events: 'no',
          }},
          {selector: 'edge', style: {
            width: 2, 'line-color': theme.line, 'target-arrow-color': theme.line,
            'target-arrow-shape': 'triangle', 'arrow-scale': 0.9,
            ...(EDGE_STYLES[this.view.edge_style] || EDGE_STYLES.straight),
          }},
          {selector: 'edge.devicemap-selected-edge', style: {width: 3, 'line-color': theme.accent, 'target-arrow-color': theme.accent}},
        ];
      },

      renderGraph() {
        this.selectedEdge = null; // any prior selection refers to a now-stale cy instance
        if (!this.$refs.canvas) return;
        this.placedCount = this.placeNewDevices();
        const elements = this.buildElements();
        const hasAllPositions = this.devices.length > 0 && this.devices.every(device => this.positionFor(device.id));
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
        cy.on('tap', 'edge', event => { if (!this.connectMode) this.onEdgeTap(event); });
        cy.on('tap', event => { if (event.target === cy) this.clearEdgeSelection(); });
        cy.on('viewport', () => this.syncGridBackground());
        this.syncGridBackground();
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
        const nodes = (this.deviceMap.nodes || []).filter(existing => existing.device_id !== node.id());
        nodes.push({device_id: node.id(), x: position.x, y: position.y});
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
        if (!EDGE_STYLES[style]) return;
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

      toggleConnectMode() {
        this.connectMode = !this.connectMode;
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
        if (!this.connectMode) return;
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
        const confirmed = await this.$store.modal.confirm({
          title: `${this.deviceLabel(childId)} als Untergerät von ${this.deviceLabel(parentId)} verbinden?`,
          confirmLabel: 'Verbinden',
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
          this.$store.toasts.push('Beziehung verbunden.');
          this.renderGraph();
        } catch (error) {
          this.$store.toasts.push(error.message, 'critical');
        }
      },

      // Selecting an edge is the first step of "Lösen von Pfaden": it only
      // highlights the edge and surfaces removeSelectedRelation() in the
      // toolbar - nothing is deleted until that explicit, confirmed action.
      onEdgeTap(event) {
        const edge = event.target;
        const data = edge.data();
        this.selectedEdge = {id: data.id, source: data.source, target: data.target, overrideId: data.overrideId || null};
        if (cy) {
          cy.edges().removeClass('devicemap-selected-edge');
          edge.addClass('devicemap-selected-edge');
        }
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
        if (!this.selectedEdge || !this.selectedEdge.overrideId) return;
        const confirmed = await this.$store.modal.confirm({
          title: `Verbindung "${this.selectedEdgeLabel}" lösen?`,
          confirmLabel: 'Lösen',
          danger: true,
        });
        if (!confirmed) return;
        try {
          await requestJSON(`/api/v1/device/map/relations/${this.selectedEdge.overrideId}`, {method: 'DELETE'});
          this.deviceMap = {...this.deviceMap, edges: (this.deviceMap.edges || []).filter(edge => edge.id !== this.selectedEdge.overrideId)};
          this.savedDeviceMap = {...this.savedDeviceMap, edges: this.deviceMap.edges};
          this.$store.toasts.push('Verbindung gelöst.');
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
          const nodes = cy ? cy.nodes().filter(node => node.id() !== SNAP_GHOST_ID).map(node => {
            const position = node.position();
            return {device_id: node.id(), x: position.x, y: position.y};
          }) : (this.deviceMap.nodes || []);
          const value = {version: this.deviceMap.version || 1, nodes, edges: this.deviceMap.edges || [], view: this.view};
          await requestJSON('/api/v1/device/map', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(value),
          });
          this.deviceMap = value;
          this.savedDeviceMap = JSON.parse(JSON.stringify(value));
          this.unsaved = false;
          this.$store.toasts.push('Positionen gespeichert.');
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
        this.$store.toasts.push('Änderungen verworfen.');
        this.renderGraph();
      },

      confirmUnsavedUnload(event) {
        if (!this.unsaved) return;
        event.preventDefault();
        event.returnValue = '';
      },
    };
  };

  const register = () => {
    if (window.Alpine) window.Alpine.data("devicemapPanel", devicemapPanel);
  };
  if (window.Alpine) register(); else document.addEventListener("alpine:init", register, {once: true});
})();
