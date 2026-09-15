// Laedt Skripte aus static/js in ein frisches jsdom - so, wie der Browser sie
// ohne Modulsystem nacheinander ausfuehrt.
import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const here = path.dirname(fileURLToPath(import.meta.url));

export function source(name) {
  return fs.readFileSync(path.join(here, '..', '..', 'static', 'js', name), 'utf8');
}

export function loadScripts(names, { html, url } = {}) {
  const dom = new JSDOM(html || '<!doctype html><html><body data-base-path="" data-token="tok"></body></html>', {
    runScripts: 'outside-only',
    url: url || 'http://127.0.0.1/',
  });
  const context = dom.getInternalVMContext();
  for (const name of names) {
    vm.runInContext(source(name), context, { filename: name });
  }
  return dom;
}
