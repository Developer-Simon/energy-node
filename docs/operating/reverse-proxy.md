---
title: "Dashboard behind a reverse proxy under a sub-path (`/node/`)"
redirect_from:
  - /knowledge/dashboard/reverse-proxy.html
---

# Dashboard behind a reverse proxy under a sub-path (`/node/`)

> You do not need a reverse proxy if you use the
> [Energy Node Companion](../ha/companion.md). It puts the dashboard into the
> Home Assistant sidebar and passes the page through Home Assistant, see
> [Dashboard in the sidebar](../ha/companion-sidebar.md). This page is only for
> setups that serve the dashboard under a sub-path of their own proxy.

Status: 2026-08-08. Besides direct access (`http://<node>:8080/`), the
dashboard can run under a sub-path, for example behind the Home Assistant
system's nginx at `https://ha.example/node/`. This page describes how that
works.

For running at the root behind Caddy (HTTPS, admin login, certificates), the
Caddy setup still applies. This page only adds the sub-path case.

## Overview

The dashboard reads its base prefix from a proxy header on each request. There
is no environment variable and no dashboard setting for it on purpose. Only the
proxy has to set the header:

| Header | Purpose |
|---|---|
| `X-Ingress-Path` | Home Assistant add-on ingress (takes precedence) |
| `X-Forwarded-Prefix` | Any other reverse proxy |

Without a header the prefix is `""` and every generated URL is the same as for
root operation, so direct access, Caddy at the root and Tailscale work as
before. The Go and Node tests, with several hundred root-relative path
literals, passed without any change when sub-path support was added.

Out of scope: sub-path operation without a header (there is no fallback to an
environment variable), rewriting the domain or host, changes to `js-deps/`
(htmx, Alpine, Cytoscape and so on), and absolute paths in CSS, of which there
are none.

## Why `sub_filter` is not enough

Without a prefix, the dashboard produces absolute paths from the root:
`/static/...`, `/api/v1/...`, `hx-get="/?fragment=devices-live"`. Behind a
sub-path proxy the browser resolves them against the HA domain
(`https://ha.example/static/...`) instead of `/node/`, and every asset fails
with 404 or 502.

`sub_filter` in nginx cannot fix this. It only replaces text in the HTML
response and misses URLs that are built in the browser, such as
`` `/api/v1/devices/${id}` `` in `device-tile.js`. Remove any `sub_filter` from
an existing nginx configuration, because it would rewrite the already correct
paths a second time.

## Architecture

A middleware wrapped around the router resolves the prefix and removes it from
the request path. All mux patterns (about 70 at the time of writing) and the
`TrimPrefix` and `HasPrefix` calls in `internal/httpapi` stay root-relative and
still see `/api/v1/...`. Only the output gets the prefix.

### `internal/basepath`

| Function | Purpose |
|---|---|
| `Normalize(raw) string` | Turns the header value into `""` or a clean `/node` |
| `Middleware(next) http.Handler` | Resolves the prefix, strips it from the path and puts it in the context |
| `From(r) string` / `FromContext(ctx) string` | Read the prefix, default `""` |
| `Join(base, path) string` | Builds URLs on the Go side |
| `CookiePath(r) string` | `base + "/"` or `"/"` |

`Middleware` is wrapped around `httpapi.NewAuthenticatedRouter(...)` in
[`cmd/dashboard/main.go`](https://github.com/Developer-Simon/energy-node/blob/main/dashboard/cmd/dashboard/main.go),
which is its only use there.

The prefix comes from `X-Ingress-Path`, otherwise from `X-Forwarded-Prefix`. If
the result is `""`, the request passes through unchanged, without a context
value or a copy. Otherwise the prefix is removed from `URL.Path` and
`URL.RawPath` if it is still attached, which happens when the proxy has no
trailing slash on `proxy_pass`. `/node` alone becomes `/`. A path that only
starts with the same letters (`/nodes/api`) is left alone. The prefix is only
stripped on an exact match or a real sub-path.

### The three output channels

1. **Templates.** Every app path appears as `{{.BasePath}}/static/...` or
   `{{.BasePath}}/?fragment=...`. `internal/webui` puts
   `view["BasePath"] = basepath.From(r)` into the view map that all fragments
   and panels render from. `requiredEnergyCardScripts(layout, basePath)`
   prefixes the six optional `energy-*.js` files, and `Login()` passes
   `BasePath` on as well.
2. **JavaScript.** `base.html` writes the prefix into
   `<html data-base-path="…">`. An inline script in the `<head>` without
   `defer` reads it into `window.__DASHBOARD_BASE_PATH__` and provides
   `dashboardPath(p)`. Nine JS files have their own local `requestJSON()`
   wrapper, and one line in each wrapper adds the prefix. That covers most URL
   literals without touching the call sites. The rest were changed one by one:
   the `htmx.ajax` targets and `new EventSource(...)` in `dashboard.js` (through
   the local helper `withBase()`), the raw `fetch()` in `history-recorder.js`
   and the logout redirect in `settings.page.js`.
3. **Cookies.** `setSessionCookie` and `handleAuthLogout`
   ([`internal/httpapi/httpapi.go`](https://github.com/Developer-Simon/energy-node/blob/main/dashboard/internal/httpapi/httpapi.go))
   set `Path` to `basepath.CookiePath(r)`, so `/node/` instead of `/`. With
   `/`, the browser would not send the session back to `/node/…`, and the
   cookie would be visible to every other app on the proxy host.

> For frontend changes: URLs from the template (`data-panel-src`,
> `data-panel-script`, `data-panel-css`) already carry the prefix and must not
> get it again in JS. URL literals in JS add the prefix themselves. That is why
> `loadPanel`'s `fetch(source, …)` and `loadSingleAsset` in `dashboard.js` do
> not use `withBase()`. Both places have a comment saying so.

### Why the prefix lives in an attribute and not in the script

`html/template` escapes every `/` to `\/` in a JS context.
`window.__DASHBOARD_BASE_PATH__ = "{{.BasePath}}"` would work (`"\/node"` is
valid JS), but the output would be hard to read in the source and in every
`grep` or `curl`. Going through `<html data-base-path>` gives a clean `/node`
and keeps dynamic values out of the `<script>` block. `login.html` does the
same with a local `const base`.

## Security model

A client can set the header itself. Anyone who can bypass the proxy or send
their own header could otherwise decide what ends up in HTML attributes, JS
strings and the `Set-Cookie` path. `Normalize()` therefore validates strictly
and, when in doubt, returns `""`, which falls back to root-relative paths:

- A leading slash is required, trailing slashes are removed and whitespace is
  trimmed.
- Each segment may only contain `[A-Za-z0-9._~-]`, separated by `/`.
- Empty segments are rejected. That also covers `//evil.com` (a
  protocol-relative URL) and `/a//b`.
- `.` and `..` segments are rejected.

So `//evil.com`, `http://evil.com`, `/node"><script>x</script>`,
`/node\evil`, `/node:8080`, `/node?a=1`, `/node/../etc`, `/no de`, control
characters and line breaks all fall back to `""`. `html/template` escapes on
top of that, so the validation is the first layer of defence and not the only
one.

`Normalize()` results are not cached. A map keyed by header values would let
anyone fill memory without limit, and at 8.5 ns per call a cache would not help
anyway (see Performance).

`X-Forwarded-Proto` is a separate matter and is still required for admin
sessions, see the nginx block below.

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

`X-Forwarded-Proto: $scheme` is required for the admin login.
`isSecureRequest()` in `internal/httpapi/httpapi.go` only allows password login
and system actions over HTTPS, and behind a proxy it can only tell from this
header. Without it, only guest access is left.

`proxy_buffering off` is needed for the event stream `/api/v1/events`, for the
same reason as the `not path /api/v1/events` exception in the
[Caddyfile](https://github.com/Developer-Simon/energy-node/blob/main/dashboard/Caddyfile).
Buffered SSE never reaches the browser and the live updates stop.

The trailing slash in `proxy_pass http://…:8080/` already removes `/node` from
the path. Both variants work, because the middleware only strips the prefix if
it is still there.

Remove `sub_filter` if it is present (see above).

### Home Assistant add-on ingress

Ingress sets `X-Ingress-Path` to a token path of the form
`/api/hassio_ingress/<token>`. `Normalize()` accepts it, and it takes
precedence over `X-Forwarded-Prefix`. Only the nginx setup has been tested on a
live system so far. The ingress path is implemented and covered by unit tests.

### Home Assistant integration panel

The `energy_node_companion` integration proxies the dashboard itself under
`/api/energy_node_companion/proxy/<entry_id>` and sets `X-Forwarded-Prefix` to
that path, so the same prefix logic applies. `X-Forwarded-Proto` is the scheme
of the Home Assistant request. The integration talks to the dashboard port
directly (`http://<MagicDNS name>:8080`) and not through Caddy, because Caddy
replaces an incoming `X-Forwarded-Proto` with `http` and the password login
would never be offered. Session cookies get the path
`/api/energy_node_companion/proxy/<entry_id>/` through `basepath.CookiePath`. See
`integrations/homeassistant/README.md`.

## Performance

Measured on the development machine, not on the Pi, with the middleware on its
own:

| | ns/op | B/op | allocs |
|---|---|---|---|
| Handler without middleware (baseline) | 0.7 | 0 | 0 |
| Direct access, no header | 73 | 0 | **0** |
| Proxy path `/node` | 265 | 528 | 4 |
| `Normalize("/node")` alone | 8.5 | 0 | 0 |

Direct access costs practically nothing. The four allocations in the proxy case
(request copy, URL copy, `valueCtx`, boxing the string into `any`) come with
any Go middleware that carries a context value. They add up to about 0.05 % of
a page render (~510 µs).

The ~40 `{{.BasePath}}` actions in `base.html` add 460 allocations to the full
page render (3143 → 3603) and about 7 % time, once per browser session. The
frequent renders are not affected. `overview.html` has no `BasePath`
reference, and that is the fragment SSE re-renders up to once per second per
client (3134 allocations with and without the prefix). `devices.html` has one
action.

The 460 allocations could only be avoided with URLs built in Go, for example a
`{{static "css/base.css"}}` function. That would make the templates harder to
grep and add indirection to save about 30 µs per page view, so it was not
done.

## Tests

| File | Content |
|---|---|
| `internal/basepath/basepath_test.go` | Table test for `Normalize` with all reject cases. Stripping with and without the prefix in the path, `RawPath`, similar paths (`/nodes/api`), header precedence, no context leak between requests, `Join` and `CookiePath` |
| `internal/webui/webui_test.go` | Renders with `X-Forwarded-Prefix: /node` and checks that no unprefixed path remains. Energy card scripts, `hx-get` in the devices fragment, hostile prefix, login page |
| `internal/httpapi/basepath_test.go` | `GET /node/api/v1/health` and `/node/static/...` return 200, also when already stripped. `Set-Cookie` path `/node/` on login and logout, still `/` without a prefix |
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

To test end to end behind nginx, open `https://ha.example/node/` and look for
404 or 502 in the DevTools network tab. Open every panel once (Devices,
History, Configuration, Energy, Device Map, Settings) and the overview's layout
edit mode. Each loads its own assets, so each one tests its paths. Log in and
out (the cookie path must be `/node/`) and watch the system status badge. If it
updates, the SSE stream works.

## Known limitations and open points

- The prefix only covers the path, not the host. The dashboard does not
  produce absolute links to its own host, so a different domain does not
  matter.
- `/node/static/…` and `/static/…` are different URLs to the browser, so using
  both access paths downloads the assets twice. That comes with sub-path
  operation. `Cache-Control: public, max-age=86400` applies to both.
- Ingress has not been tested on a live system (see above).
- The commented-out configuration editor at the end of `base.html` (in the
  `{{/* ... */}}` block) has no prefix. If it is brought back, its
  `request('/api/v1/...')` calls have to be updated too.
- Without the header there is no sub-path. A proxy that cannot set
  `X-Forwarded-Prefix` cannot be supported through an environment variable
  instead, because a fixed prefix would break direct access.
