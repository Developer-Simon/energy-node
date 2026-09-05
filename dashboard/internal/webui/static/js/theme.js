// Farb-Tokens für die Zeichner. base.css definiert sie als Custom
// Properties auf :root; SVG-Generatoren und Cytoscape brauchen sie aber als
// echte Farbwerte, weil sie ihre Ausgabe als String zusammensetzen bzw. auf
// Canvas malen. Der Rückfall ist die Mint-Palette - damit liefern die
// jsdom-Tests deterministische Werte und ein fehlgeschlagenes Stylesheet
// führt nicht zu unsichtbaren Diagrammen.
(() => {
  const FALLBACK = {
    'bg': '#111418',
    'panel': '#171a20',
    'panel-alt': '#1c2027',
    'sunken': '#12151a',
    'track': '#26303a',
    'border': '#46515d',
    'border-soft': '#333333',
    'text-strong': '#f0f3f6',
    'text': '#e6e6e6',
    'text-subtle': '#b8c7d9',
    'text-muted': '#8b949e',
    'text-faint': '#68717d',
    'accent': '#9fd',
    'accent-ink': '#0f1216',
    'accent-soft': '#9fe5d0',
    'accent-line': '#4d9c87',
    'accent-bg': '#18321f',
    'ok': '#8fdb8f',
    'ok-bg': '#18321f',
    'ok-line': '#4d9c87',
    'warn': '#e9cf68',
    'warn-bg': '#382f16',
    'warn-line': '#c6944f',
    'bad': '#f08b8b',
    'bad-strong': '#e06868',
    'bad-bg': '#351b1b',
    'bad-bg-hover': '#4a1e24',
    'bad-line': '#9a4d55',
    'bad-ink': '#ffd9dd',
    'info': '#86c5da',
    'info-bg': '#1b3540',
    'info-line': '#7cb9f2',
    'flow-pv': '#f3c969',
    'flow-grid': '#7cb9f2',
    'flow-battery': '#d69af5',
    'flow-load': '#ff9d7a',
    'flow-wallbox': '#ffc9b3',
    'flow-heatpump': '#cf6c47',
    'flow-measured': '#e8748f',
    'flow-rest': '#7d8792',
    'series-1': '#9fd',
    'series-2': '#f0a36b',
    'series-3': '#86c5da',
    'series-4': '#d99adf',
    'series-5': '#a8d08d',
    'series-6': '#e9cf68',
  };

  const cache = new Map();

  const color = (token) => {
    if (cache.has(token)) return cache.get(token);
    let value = '';
    try {
      value = getComputedStyle(document.documentElement).getPropertyValue(`--${token}`).trim();
    } catch (error) {
      value = '';
    }
    if (!value) value = FALLBACK[token] || '';
    cache.set(token, value);
    return value;
  };

  const colors = (tokens) => {
    const result = {};
    for (const key of Object.keys(tokens)) result[key] = color(tokens[key]);
    return result;
  };

  const onChange = (handler) => {
    const listener = (event) => {
      cache.clear();
      handler(event.detail && event.detail.theme);
    };
    document.addEventListener('dashboard-theme-changed', listener);
    return () => document.removeEventListener('dashboard-theme-changed', listener);
  };

  window.DashboardTheme = {color, colors, onChange};
})();
