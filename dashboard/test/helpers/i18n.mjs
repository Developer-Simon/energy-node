// Loads the real i18n.js with a real catalog into a jsdom window, the way
// base.html does: the number-format <meta> tags, then window.__I18N__, then
// the runtime. Tests that render numbers, dates or sorted lists call this
// before loading the module under test.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const webui = path.join(here, '..', '..', 'internal', 'webui');
const source = fs.readFileSync(path.join(webui, 'static', 'js', 'i18n.js'), 'utf8');

export function catalog(lang) {
  return JSON.parse(fs.readFileSync(path.join(webui, 'catalogs', `${lang}.json`), 'utf8'));
}

export function installI18n(window, { lang = 'de', format = 'auto', grouping = 'match' } = {}) {
  const document = window.document;
  for (const [name, content] of [['number-format', format], ['number-grouping', grouping]]) {
    let meta = document.querySelector(`meta[name="${name}"]`);
    if (!meta) {
      meta = document.createElement('meta');
      meta.setAttribute('name', name);
      document.head.appendChild(meta);
    }
    meta.setAttribute('content', content);
  }
  window.__I18N__ = { lang, catalog: catalog(lang) };
  window.eval(source);
  return window.I18n;
}
