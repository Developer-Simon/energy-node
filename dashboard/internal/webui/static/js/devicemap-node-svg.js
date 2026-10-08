// Draws a device map node as an SVG data URI (draft variant C: one ring
// segment per energy role, the primary role's icon, an availability dot).
// Cytoscape renders it as the node's background-image, so the canvas shows
// exactly the draft's geometry. Colors arrive resolved (canvas cannot read
// CSS variables), see DashboardTheme.colors().
(() => {
  const SIZE = 56;
  const R = 24;
  const RING_R = R - 1;

  // Inner markup of the draft's <symbol> elements (viewBox 0 0 24 24).
  const ICONS = {
    pv: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2.5v2.2M12 19.3v2.2M2.5 12h2.2M19.3 12h2.2M5.3 5.3l1.6 1.6M17.1 17.1l1.6 1.6M5.3 18.7l1.6-1.6M17.1 6.9l1.6-1.6"/></g>',
    battery: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/><path d="M7 12h8M11 9.5v5"/></g>',
    soc: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/></g><rect x="5.5" y="9.5" width="7.5" height="5" rx="1" fill="currentColor"/>',
    grid: '<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2.5 7 21.5M12 2.5l5 19M5 6.5h14M7.6 11h8.8M8.9 16h6.2"/></g>',
    load: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round"><path d="M3.5 11 12 4l8.5 7v9.5h-17z"/><path d="M10 20.5v-5h4v5"/></g>',
    wallbox: '<g fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5M15 3v5M7 8h10v3a5 5 0 0 1-10 0z M12 16v5"/></g>',
    heat_pump: '<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2.5"/><circle cx="12" cy="12" r="4.6"/><path d="M12 12c-1.4-1.6-1.2-3.4 0-4.2 1.2.8 1.4 2.6 0 4.2zM12 12c2 .3 3 1.8 2.6 3.2-1.4.3-2.8-.8-2.6-3.2zM12 12c-.6 2-2.3 2.8-3.6 2.1.1-1.4 1.6-2.4 3.6-2.1z" fill="currentColor" stroke="none"/></g>',
    sensor: '<path d="M3 12h3.5l2-5.5 4 11 2.5-5.5H21" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/>',
    box: '<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="4" y="3.5" width="16" height="17" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></g>',
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

  window.DeviceMapNodeSvg = {SIZE, markup, dataUri, groupMarkup, groupDataUri};
})();
