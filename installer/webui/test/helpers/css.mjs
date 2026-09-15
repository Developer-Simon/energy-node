// Zerlegt CSS in Regeln - gerade genug fuer den Treuetest: Kommentare weg,
// geschweifte Klammern zaehlen, @media als Kontext, @keyframes als Ganzes.
// Kein allgemeiner CSS-Parser; die Vorlagen benutzen nichts darueber hinaus.

const BLOCK = /\/\*\s*==\s*Vorlage:\s*(\S+)\s*==\s*\*\/([\s\S]*?)\/\*\s*==\s*Ende Vorlage\s*==\s*\*\//g;

function squash(text) {
  return text.replace(/\s+/g, ' ').trim();
}

export function normalizeSelector(selector) {
  return squash(selector).replace(/\s*([>,+~])\s*/g, '$1');
}

function parseDeclarations(body) {
  const out = new Map();
  for (const part of body.split(';')) {
    const colon = part.indexOf(':');
    if (colon < 0) {
      continue;
    }
    const prop = squash(part.slice(0, colon)).toLowerCase();
    const value = squash(part.slice(colon + 1)).replace(/\s*,\s*/g, ',');
    if (prop) {
      out.set(prop, value);
    }
  }
  return out;
}

export function parseCss(text, context = '') {
  const src = text.replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [];
  let i = 0;
  while (i < src.length) {
    const open = src.indexOf('{', i);
    if (open < 0) {
      break;
    }
    const prelude = squash(src.slice(i, open));
    let depth = 1;
    let j = open + 1;
    while (j < src.length && depth > 0) {
      if (src[j] === '{') depth++;
      else if (src[j] === '}') depth--;
      j++;
    }
    const body = src.slice(open + 1, j - 1);
    if (prelude.startsWith('@media') || prelude.startsWith('@supports')) {
      rules.push(...parseCss(body, prelude));
    } else if (prelude.startsWith('@keyframes')) {
      rules.push({ context, selector: prelude, decls: new Map([['@frames', squash(body).replace(/\s*([{};:,])\s*/g, '$1')]]) });
    } else if (prelude) {
      rules.push({ context, selector: normalizeSelector(prelude), decls: parseDeclarations(body) });
    }
    i = j;
  }
  return rules;
}

export function templateBlocks(text) {
  const out = {};
  for (const match of text.matchAll(BLOCK)) {
    out[match[1]] = match[2];
  }
  return out;
}

export function outsideBlocks(text) {
  return text.replace(BLOCK, '');
}

export function scopeSelector(selector, screen) {
  if (selector.startsWith('@keyframes')) {
    return selector;
  }
  return selector.split(',').map((part) => `.app[data-screen="${screen}"] ${part}`).join(',');
}

export function key(rule) {
  return `${rule.context}|${rule.selector}`;
}
