---
title: "Dashboard behind a reverse proxy under a sub-path (`/node/`)"
---

# Dashboard behind a reverse proxy under a sub-path (`/node/`)

Status: 2026-08-08. This file documents the **implemented** state of the
sub-path support: in addition to direct access (`http://<node>:8080/`), the
dashboard is also reachable through the Home Assistant system's nginx at
`https://ha.example/node/`.

For root operation behind Caddy (HTTPS, admin login, certificates), the
Caddy / HTTPS / admin-login setup still applies. This file only supplements it
with the sub-path case and does not replace it.

## Overview

The dashboard learns its base prefix **per request** from a proxy header. There
is deliberately **no ENV setting and no configuration on the dashboard** — only
the proxy has to set the header:

| Header | Purpose |
|---|---|
| `X-Ingress-Path` | Home Assistant add-on ingress (takes precedence) |
| `X-Forwarded-Prefix` | ordinary reverse proxy |

Without a header the prefix is `""`, and **every generated URL is byte-identical
to the state before the change**. Direct access, Caddy at root, and Tailscale
therefore behave unchanged. This is not a statement of intent but is backed by
the existing test suite: the existing Go and Node tests, with their several
hundred root-relative path literals, kept passing without a single adjustment.

**Scope, as currently defined:** no sub-path operation without a header (i.e. no
ENV fallback), no domain/host rewriting, no adjustment of `js-deps/` (htmx,
Alpine, Cytoscape, …), and no absolute paths in CSS — there are none there.

## Why `sub_filter` is not enough

Before the change, the dashboard produced exclusively **absolute paths from
root**: `/static/...`, `/api/v1/...`, `hx-get="/?fragment=devices-live"`. The
browser resolves these against the HA domain (`https://ha.example/static/...`)
instead of against `/node/` — result: 404/502 for every asset.

`sub_filter` in nginx does not fix this: it is pure text replacement in the
HTML response and does not catch URLs that are only built in the browser,
e.g. `` `/api/v1/devices/${id}` `` in `device-tile.js`. If an existing nginx
configuration still contains a `sub_filter`, it must **go** — it would rewrite
the paths, which are already correct now, a second time.

## Architecture

A middleware wrapper on the very outside around the router resolves the prefix
and removes it from the request path. As a result, **all mux patterns (~70 as
of this writing) and the `TrimPrefix`/`HasPrefix` locations in `internal/httpapi`
stay unchanged and root-relative** — they still see `/api/v1/...`. Only the
*output* side is prefixed.

### `internal/basepath`

| Function | Purpose |
|---|---|
| `Normalize(raw) string` | header value → `""` or a clean `/node` |
| `Middleware(next) http.Handler` | resolves the prefix, strips it from the path, puts it in the context |
| `From(r) string` / `FromContext(ctx) string` | read the prefix, default `""` |
| `Join(base, path) string` | building URLs on the Go side |
| `CookiePath(r) string` | `base + "/"` or `"/"` |

`Middleware` is wrapped around `httpapi.NewAuthenticatedRouter(...)` in
[`cmd/dashboard/main.go`](../../../dashboard/cmd/dashboard/main.go) — that is the
only change there. Behavior:

- Prefix from `X-Ingress-Path`, otherwise `X-Forwarded-Prefix`.
- If the result is `""`, the **unchanged** request is passed through (no
  context, no copy).
- Otherwise the prefix is removed from `URL.Path` **and** `URL.RawPath`, if it
  is still attached (proxy without a trailing slash on `proxy_pass`). `/node`
  alone becomes `/`.
- A path that merely happens to start the same way (`/nodes/api`) is **not**
  touched — stripping happens only on an exact match or a genuine sub-path.

### The three output channels

1. **Templates** — every app path literal appears as `{{.BasePath}}/static/...`
   or `{{.BasePath}}/?fragment=...`. For this, `internal/webui` puts
   `view["BasePath"] = basepath.From(r)` into the view map, from which all
   fragments and panels render; `requiredEnergyCardScripts(layout, basePath)`
   prefixes the six optional `energy-*.js`; `Login()` passes `BasePath` through
   as well.
2. **JavaScript** — `base.html` writes the prefix into
   `<html data-base-path="…">`. A **non**-`defer` inline script in the `<head>`
   reads it into `window.__DASHBOARD_BASE_PATH__` and provides
   `dashboardPath(p)`. Nine JS files already have their own local
   `requestJSON()` wrapper anyway — there, exactly **one** line appends the
   prefix, which handles the bulk of all URL literals without a call-site
   change. Touched individually: the `htmx.ajax` targets and
   `new EventSource(...)` in `dashboard.js` (via the local helper `withBase()`),
   the raw `fetch()` in `history-recorder.js`, and the logout redirect in
   `settings.page.js`.
3. **Cookies** — `Path` becomes `basepath.CookiePath(r)` in `setSessionCookie`
   and `handleAuthLogout`
   ([`internal/httpapi/httpapi.go`](../../../dashboard/internal/httpapi/httpapi.go)),
   i.e. `/node/` instead of `/`. Otherwise the browser does not send the session
   back to `/node/…`, and the cookie would sit in the scope of every other app
   on the proxy host.

> **Rule of thumb for future frontend changes:** URLs that come from the
> template (`data-panel-src`, `data-panel-script`, `data-panel-css`) are already
> prefixed and must **not** be prefixed again in JS; URL literals in JS prefix
> themselves. That is why `loadPanel`'s `fetch(source, …)` and `loadSingleAsset`
> in `dashboard.js` deliberately stay without `withBase()` — both places carry a
> comment to that effect.

### Why the prefix lives in an attribute and not in the script

`html/template` escapes every `/` to `\/` in a JS context. A
`window.__DASHBOARD_BASE_PATH__ = "{{.BasePath}}"` would have worked
(`"\/node"` is valid JS), but it would produce output that is unreadable in the
source and in every `grep`/`curl`. The detour via `<html data-base-path>` yields
a clean `/node` and at the same time keeps every dynamic value out of the
`<script>` block. `login.html` uses the same pattern via a local `const base`.

## Security model

The header is **client-controllable** — anyone who can bypass the proxy or send
their own header would otherwise also determine what gets written into HTML
attributes, JS strings, and the `Set-Cookie` path. `Normalize()` therefore
validates strictly and rejects when in doubt (→ `""`, i.e. falling back to
root-relative):

- leading slash enforced, trailing slashes removed, whitespace trimmed;
- per segment only `[A-Za-z0-9._~-]`, separator `/`;
- **no empty segments** — this also covers `//evil.com` (protocol-relative URL)
  and `/a//b`;
- no `.` or `..` segments.

This makes `//evil.com`, `http://evil.com`, `/node"><script>x</script>`,
`/node\evil`, `/node:8080`, `/node?a=1`, `/node/../etc`, `/no de`, and
control characters/line breaks all fall back to `""`. `html/template` escapes on
top of that — the validation is the first layer, not the only one.

**Deliberately no caching** of the `Normalize()` results: a map with header
values as keys would be an unbounded memory-DoS vector. At 8.5 ns per call it is
not worth it anyway (see Performance).

`X-Forwarded-Proto` is unaffected by this and remains a prerequisite for admin
sessions — see the nginx block below.

## nginx configuration

```nginx
location /node/ {
    proxy_pass http://100.82.49.43:8080/;
    proxy_http_version 1.1;
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;   # otherwise no admin login
    proxy_set_header X-Forwarded-Prefix /node;    # <- the prefix
    proxy_buffering off;                          # SSE /api/v1/events
    proxy_read_timeout 1h;
}
```

Important here:

- **`X-Forwarded-Proto: $scheme`** is a prerequisite for administrator login:
  `isSecureRequest()` in `internal/httpapi/httpapi.go` only allows password
  login and system actions over HTTPS and, behind the proxy, recognizes that
  solely from this header. If it is missing, only guest access remains.
- **`proxy_buffering off`** for the event stream `/api/v1/events` — same
  rationale as the `not path /api/v1/events` exception in the
  [Caddyfile](../../../dashboard/Caddyfile): buffered SSE never reaches the
  browser, and the live update would stall.
- The **trailing slash** in `proxy_pass http://…:8080/` already removes `/node`
  from the path. Both variants are fine — the middleware strips the prefix only
  if it is still attached.
- **Remove `sub_filter`** if present (see above).

### Home Assistant add-on ingress

Ingress sets `X-Ingress-Path` with a token path of the form
`/api/hassio_ingress/<token>`. It is accepted by `Normalize()` and takes
precedence over `X-Forwarded-Prefix`. So far only the nginx path has been
tested; the ingress path is implemented and unit-tested, but not verified on a
live system.

## Performance

Measured on the development machine (not on the Pi), middleware in isolation:

| | ns/op | B/op | allocs |
|---|---|---|---|
| Handler without middleware (baseline) | 0.7 | 0 | 0 |
| Direct access, no header | 73 | 0 | **0** |
| Proxy path `/node` | 265 | 528 | 4 |
| `Normalize("/node")` alone | 8.5 | 0 | 0 |

Direct access is practically free. The four allocations in the proxy case
(request copy, URL copy, `valueCtx`, string boxing into `any`) are intrinsic to
context-carrying middleware in Go and amount to ~0.05 % of a page render
(~510 µs).

The ~40 `{{.BasePath}}` actions in `base.html` cost the full-page render +460
allocations (3143 → 3603) and ~7 % time — once per browser session. **The hot
path is not affected:** `overview.html` contains zero `BasePath` references, and
it is exactly this fragment that is re-rendered SSE-driven up to 1×/s per client
(3134 allocations before and after). `devices.html` has exactly one action.

The 460 allocations could only be optimized away via pre-built URLs in Go
(e.g. a `{{static "css/base.css"}}` function). That makes the templates harder
to grep and trades ~30 µs per page view for considerably more indirection —
deliberately not done.

## Tests

| File | Content |
|---|---|
| `internal/basepath/basepath_test.go` | Table test for `Normalize` including all reject cases; strip behavior with/without the prefix in the path, `RawPath`, lookalike siblings (`/nodes/api`); header precedence; no context leak between requests; `Join`/`CookiePath` |
| `internal/webui/webui_test.go` | Renders with `X-Forwarded-Prefix: /node` and checks that **no** unprefixed path remains; energy-card scripts; `hx-get` in the devices fragment; hostile prefix; login page |
| `internal/httpapi/basepath_test.go` | `GET /node/api/v1/health` and `/node/static/...` → 200, also when already stripped; `Set-Cookie` path `/node/` on login **and** logout; without a prefix still `/` |
| `test/mqtt.page.test.mjs` | Node/jsdom: with `__DASHBOARD_BASE_PATH__` set, all three requests go to `/node/api/v1/...` |

## Testing without a proxy

```bash
# Direct access stays unchanged root-relative
curl -s localhost:8080/ | grep -o '"/static/[^"]*"' | head

# Simulate the prefix
curl -s -H 'X-Forwarded-Prefix: /node' localhost:8080/ | grep -o '"/node/static/[^"]*"' | head
curl -s -o /dev/null -w '%{http_code}\n' -H 'X-Forwarded-Prefix: /node' \
     localhost:8080/node/api/v1/health          # -> 200

# Cookie scope
curl -s -D - -o /dev/null -H 'X-Forwarded-Prefix: /node' \
     -X POST localhost:8080/node/api/v1/auth/guest | grep -i set-cookie
# -> ... Path=/node/ ...

# Injection check: hostile prefix is discarded
curl -s -H 'X-Forwarded-Prefix: //evil.com' localhost:8080/ | grep -o 'data-base-path="[^"]*"'
# -> data-base-path=""
```

End-to-end behind nginx: open `https://ha.example/node/`, check for 404/502 in
the DevTools network tab, and open every panel (Devices, History,
Configuration, Energy, Device Map, Settings) once, plus the overview's layout
edit mode — each lazy-loads its assets and is thus its own path test. Run through login/logout (the cookie path
must be `/node/`) and watch the system-status badge: if it updates, the SSE
stream is running.

## Known limitations / open points

- **Path only, no host.** If the dashboard runs under a different domain than
  expected, the prefix does not help — but the dashboard does not produce
  absolute links to its own host anyway.
- **Two cache entries.** `/node/static/…` and `/static/…` are different URLs to
  the browser. Anyone using both access paths downloads the assets twice. Not a
  regression but a property of sub-path operation;
  `Cache-Control: public, max-age=86400` applies to both.
- **Ingress not verified on a live system** (see above).
- **The commented-out configuration editor** at the end of `base.html` (in the
  `{{/* ... */}}` block) is deliberately not prefixed. If it is reactivated, its
  `request('/api/v1/...')` calls must be updated along with it.
- Without a header, no sub-path: a proxy that cannot set `X-Forwarded-Prefix`
  cannot be retrofitted via an ENV setting. A deliberate decision — a static
  prefix would break direct access.
