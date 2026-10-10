// Draws a device map node as an SVG data URI (draft variant C: one ring
// segment per energy role, the primary role's icon, an availability dot).
// Cytoscape renders it as the node's background-image, so the canvas shows
// exactly the draft's geometry. Colors arrive resolved (canvas cannot read
// CSS variables), see DashboardTheme.colors().
(() => {
  const SIZE = 56;
  const R = 24;
  const RING_R = R - 1;

  // Locally drawn glyphs: the virtual nodes (balance, rule, data) and the
  // box shown when a catalogue icon is missing. Device and role icons come
  // from the catalogue markup (spec.iconMarkup).
  const ICONS = {
    box: '<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="4" y="3.5" width="16" height="17" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></g>',
    balance: '<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v16M7 20h10M4.5 8h15"/><path d="M4.5 8 2 14h5zM19.5 8 17 14h5z"/></g>',
    rule: '<path d="M13.2 2.8 6 13.2h5l-1.2 8 7.2-10.4h-5z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/>',
    data: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-dasharray="3 3"><path d="M4 18C10 18 14 6 20 6"/></g><path d="M17 3.5 20.5 6 17 8.5" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/>',
  };
  const HEALTH_TOKEN = {ok: 'ok', degraded: 'warn', down: 'bad', unknown: 'text-faint'};

  const ring = spec => {
    const roles = spec.segments;
    if (!roles.length) {
      return `<circle data-ring="none" r="${RING_R}" fill="none" stroke="{border}" stroke-width="3" stroke-dasharray="2 4"/>`;
    }
    const circ = 2 * Math.PI * RING_R;
    const seg = circ / roles.length;
    const gap = roles.length > 1 ? 4 : 0;
    return roles.map((token, index) => {
      const dash = spec.dashed ? '4 3' : `${(seg - gap).toFixed(2)} ${(circ - seg + gap).toFixed(2)}`;
      return `<circle data-ring="${index}" r="${RING_R}" transform="rotate(-90)" fill="none" stroke="{${token}}" stroke-width="4" stroke-dasharray="${dash}" stroke-dashoffset="${(-index * seg).toFixed(2)}"/>`;
    }).join('');
  };

  // Category icons come from the device icon catalogue (GET
  // /api/v1/device/icons): bare stroked shapes, drawn here with the same
  // attributes webui/deviceicons.go puts on its <svg>.
  const iconBody = spec => (spec.iconMarkup
    ? `<g fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${spec.iconMarkup}</g>`
    : (ICONS[spec.icon] || ICONS.box));

  const TOKEN = /\{([a-z0-9-]+)\}/g;

  const groupMarkup = colorOf => {
    const half = SIZE / 2;
    const body = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="${-half} ${-half} ${SIZE} ${SIZE}">`
      + `<circle r="${R}" fill="{panel}" stroke="{flow-rest}" stroke-width="2" stroke-dasharray="5 4"/>`
      + '<text x="0" y="6" text-anchor="middle" font-family="system-ui, sans-serif" font-size="18" font-weight="600" fill="{flow-rest}">Σ</text>'
      + '</svg>';
    return body.replace(TOKEN, (_, token) => colorOf(token));
  };

  const groupDataUri = colorOf => `data:image/svg+xml;utf8,${encodeURIComponent(groupMarkup(colorOf))}`;

  const markup = (spec, colorOf) => {
    const half = SIZE / 2;
    const dot = (R * 0.72).toFixed(2);
    const body = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="${-half} ${-half} ${SIZE} ${SIZE}">`
      + `<circle r="${R}" fill="{panel}" stroke="{border-soft}" stroke-width="1"/>`
      + ring(spec)
      + `<svg data-icon="${spec.icon}" x="-10" y="-10" width="20" height="20" viewBox="0 0 24 24" color="{${spec.iconColor}}">${iconBody(spec)}</svg>`
      + `<circle data-health="${spec.health}" cx="${dot}" cy="-${dot}" r="4.5" fill="{${HEALTH_TOKEN[spec.health] || 'text-faint'}}" stroke="{panel}" stroke-width="2"/>`
      + '</svg>';
    return body.replace(TOKEN, (_, token) => colorOf(token));
  };

  const dataUri = (spec, colorOf) => `data:image/svg+xml;utf8,${encodeURIComponent(markup(spec, colorOf))}`;

  const STUB_SIZE = 24;

  const icon = (name, size, token) => `<svg data-icon="${name}" x="${-size / 2}" y="${-size / 2}" width="${size}" height="${size}" viewBox="0 0 24 24" color="{${token}}">${ICONS[name]}</svg>`;

  // Shapes of the draft's virtual nodes (balance, rule) and of the spec's
  // service square and stub (Freigaben 2 und 5 im Plan).
  const VIRTUAL = {
    balance: () => `<circle r="22" fill="{accent-bg}" stroke="{accent-line}" stroke-width="2"/>${icon('balance', 22, 'accent-line')}`,
    rule: () => `<rect x="-15" y="-15" width="30" height="30" rx="5" transform="rotate(45)" fill="{info-bg}" stroke="{info-line}" stroke-width="2"/>${icon('rule', 18, 'info-line')}`,
    service: () => `<rect x="-20" y="-20" width="40" height="40" rx="9" fill="{info-bg}" stroke="{info-line}" stroke-width="2"/>${icon('data', 20, 'info-line')}`,
  };

  const virtualMarkup = (kind, colorOf) => {
    let body;
    if (kind === 'stub') {
      const half = STUB_SIZE / 2;
      body = `<svg xmlns="http://www.w3.org/2000/svg" width="${STUB_SIZE}" height="${STUB_SIZE}" viewBox="${-half} ${-half} ${STUB_SIZE} ${STUB_SIZE}">`
        + '<circle r="10" fill="{panel}" stroke="{warn-line}" stroke-width="1.6" stroke-dasharray="3 3"/>'
        + '</svg>';
    } else {
      const half = SIZE / 2;
      body = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="${-half} ${-half} ${SIZE} ${SIZE}">`
        + (VIRTUAL[kind] || VIRTUAL.service)()
        + '</svg>';
    }
    return body.replace(TOKEN, (_, token) => colorOf(token));
  };

  const virtualDataUri = (kind, colorOf) => `data:image/svg+xml;utf8,${encodeURIComponent(virtualMarkup(kind, colorOf))}`;

  window.DeviceMapNodeSvg = {SIZE, STUB_SIZE, markup, dataUri, groupMarkup, groupDataUri, virtualMarkup, virtualDataUri};
})();
