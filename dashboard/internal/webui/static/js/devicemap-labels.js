// Name and live value under each device map node, as HTML instead of a
// Cytoscape label: the draft sets the name bold and the value smaller and
// lighter, which a single Cytoscape label cannot do (approved deviation 1 in
// the plan). sync() runs on Cytoscape's "render" event, so labels follow
// pan, zoom and drag frame by frame.
(() => {
  const NODE_RADIUS = 28; // DeviceMapNodeSvg.SIZE / 2
  const LABEL_GAP = 4;

  const create = container => {
    const byId = new Map();

    const update = items => {
      const keep = new Set();
      for (const item of items) {
        keep.add(item.id);
        let el = byId.get(item.id);
        if (!el) {
          el = document.createElement('div');
          el.className = 'devicemap-node-label';
          el.dataset.labelId = item.id;
          const name = document.createElement('span');
          name.className = 'devicemap-node-name';
          // The reading wrapper is the pill for switched-off nodes, so it
          // wraps the value and the off text together.
          const reading = document.createElement('span');
          reading.className = 'devicemap-node-reading';
          const value = document.createElement('span');
          value.className = 'devicemap-node-value';
          reading.appendChild(value);
          el.append(name, reading);
          container.appendChild(el);
          byId.set(item.id, el);
        }
        el.querySelector('.devicemap-node-name').textContent = item.name;
        el.querySelector('.devicemap-node-value').textContent = item.value;
        const reading = el.querySelector('.devicemap-node-reading');
        const off = reading.querySelector('.devicemap-node-off');
        if (item.off) {
          el.classList.add('is-off');
          if (off) {
            off.textContent = `· ${item.offText}`;
          } else {
            const pill = document.createElement('span');
            pill.className = 'devicemap-node-off';
            pill.textContent = `· ${item.offText}`;
            reading.appendChild(pill);
          }
        } else {
          el.classList.remove('is-off');
          if (off) off.remove();
        }
      }
      for (const [id, el] of byId) {
        if (!keep.has(id)) { el.remove(); byId.delete(id); }
      }
    };

    const sync = cy => {
      const zoom = cy.zoom();
      for (const [id, el] of byId) {
        const node = cy.getElementById(id);
        if (!node || node.empty()) { el.hidden = true; continue; }
        const pos = node.renderedPosition();
        el.hidden = false;
        el.style.transform = `translate(${pos.x}px, ${pos.y + NODE_RADIUS * zoom + LABEL_GAP}px) translateX(-50%) scale(${zoom})`;
      }
    };

    const setDimmed = ids => {
      for (const [id, el] of byId) el.classList.toggle('is-dimmed', Boolean(ids) && !ids.has(id));
    };

    const setHidden = (ids, delays) => {
      for (const [id, el] of byId) {
        const hidden = Boolean(ids) && ids.has(id);
        el.style.transitionDelay = `${(delays && delays.get(id)) || 0}ms`;
        el.classList.toggle('is-hidden', hidden);
      }
    };

    const destroy = () => {
      for (const el of byId.values()) el.remove();
      byId.clear();
    };

    return {update, sync, setDimmed, setHidden, destroy};
  };

  window.DeviceMapLabels = {create};
})();
