---
title: "Dashboard API: Authentication"
---

# Dashboard API: Authentication

Part of the [Dashboard API documentation](api.md). The fundamentals there
(base URL, response format, error codes) apply to every endpoint.

In the *Gate* column, `–` means a session is enough. Otherwise the column names
the role or other requirements.

## Authentication and permissions

### Sessions

A middleware (`authMiddleware`) protects everything except `/static/*` and
`/api/v1/auth/*`. Without a valid session, `GET /` returns the login page and
any `/api/*` call returns `401`.

There are two cookies, depending on the transport:

| Cookie | When set/read | Session lifetime |
|---|---|---|
| `energy_node_session` | HTTPS requests | 24 h (password login) |
| `energy_node_guest_session` | HTTP requests | 7 days (guest session) |

Both are `HttpOnly` and `SameSite=Lax`. A request counts as HTTPS if the server
terminates TLS itself or if `X-Forwarded-Proto: https` is set.

Sessions survive a restart of the dashboard, so an update from the
Re-Deploy screen keeps the operator logged in. They live in `sessions.json`
next to `users.json` (mode 0600). The file stores a SHA-256 hash of each
session token, never the token itself. Expired sessions and sessions of
deleted users are dropped when the dashboard starts.

In practice the transport decides the session type. Password login only works
over HTTPS, guest access also over HTTP.

A session with the `system_actions` role is useless over plain HTTP. The
middleware discards it and asks for authentication again, so admin work needs
HTTPS.

### Roles

The catalog below is the complete list of permissions, in the order the user
management page shows them. The source is `internal/auth/roles.go`.

| Key | Category | Requires | HTTPS only | Guest |
|---|---|---|---|---|
| `view_overview` | view | – | no | yes |
| `view_devices` | view | – | no | yes |
| `view_device_analysis` | view | `view_devices` | no | yes |
| `view_history` | view | – | no | yes |
| `view_stats` | view | – | no | yes |
| `view_efficiency` | view | – | no | yes |
| `view_energy` | view | – | no | yes |
| `view_device_map` | view | – | no | yes |
| `view_config` | view | – | no | yes |
| `view_automations` | view | – | no | yes |
| `view_diagnostics` | view | – | no | yes |
| `operate_devices` | operate | `view_devices` | no | yes |
| `operate_automations` | operate | `view_automations` | no | yes |
| `edit_layout` | design | `view_overview` | no | yes |
| `edit_device_appearance` | design | `view_devices` | no | yes |
| `edit_device_map_view` | design | `view_device_map` | no | yes |
| `edit_settings` | design | – | no | yes |
| `automations` | setup | `view_automations` | no | yes |
| `edit_energy` | setup | `view_energy` | no | yes |
| `edit_device_map` | setup | `view_device_map` | no | yes |
| `edit_config` | setup | `view_config` | no | yes |
| `delete_device_discovery` | setup | `view_devices` | no | yes |
| `mqtt_config` | setup | – | yes | no |
| `edit_setup` | setup | – | no | yes |
| `setup_tiny_tuya` | setup | – | yes | no |
| `tailscale` | setup | – | yes | no |
| `check_updates` | updates | – | no | yes |
| `install_updates` | updates | – | no | no |
| `update_settings` | updates | – | yes | no |
| `view_logs` | system | `view_diagnostics` | yes | no |
| `system_actions` | system | – | yes | no |
| `user_management` | system | – | yes | no |
| `http_login` | system | – | no | no |

A permission marked "Requires" is dropped when its requirement is missing, so
the stored list is always consistent. "HTTPS only" permissions are withheld
from restricted sessions (see below). "Guest" tells whether the permission may
be part of the guest roles, because guest accounts only get permissions with
this flag.

`user_management` lets its holder manage accounts, guest roles and the
password of other accounts. Every account can change its own password without
it. The last administrator cannot lose `user_management`, and an account
cannot delete or disable itself.

#### Route access groups

Every route is registered with one of three access groups. `routeTable` in
`internal/httpapi/access.go` names the group of each pattern. The web UI
applies the same rules to its panel fragments, so a hidden panel is refused on
the server as well.

| Group | Rule | Examples |
|---|---|---|
| `open` | No view gate. The handler does its own checks, such as the write role, HTTPS or CSRF | `/api/v1/settings`, `/api/v1/users`, `/api/v1/auth/login` |
| `any view` | At least one `view_*` permission is needed | `/api/v1/history/entities`, `/api/v1/energy/interpretation` |
| `page` | The `view_*` permission of the page is needed. Some routes pick it from the request | `/api/v1/diagnostics` needs `view_diagnostics`, `/api/v1/automation/state` needs `view_automations` |

A refused request gets HTTP 403 with the error key `view_forbidden`. A request
without a user, as on an instance without accounts, passes every gate.

A new route without an entry in `routeTable` panics when it is registered, so
the dashboard does not start. Add the entry in the same change as the route.

### Restricted HTTP sessions

A session counts as restricted when the login came over plain HTTP, or when an
HTTPS login matched the HTTP password instead of the main password. A
restricted session keeps only the permissions that are not marked "HTTPS only".
`GET /api/v1/auth/session` reports the effective roles, and `withheld` lists the
permissions the account has but this session does not use. The change happens
on every request, so a permission that is withdrawn from an account is gone
from its open sessions at once, without a restart.

Over HTTP only an account with `http_login` can sign in. Once an HTTP password
is set, only that password works over HTTP, and the main password never does.
An HTTP password must differ from the main password, and it needs `http_login`
to be set at all.

### Users (`/api/v1/users`)

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/api/v1/users` | `user_management`, HTTPS | Accounts, guest roles, guest access, catalog and the caller's name |
| POST | `/api/v1/users` | `user_management`, HTTPS, CSRF | Create an account with password and roles |
| PUT | `/api/v1/users/{name}` | `user_management`, HTTPS, CSRF | Set roles and `disabled` |
| DELETE | `/api/v1/users/{name}` | `user_management`, HTTPS, CSRF | Delete an account |
| PUT | `/api/v1/users/{name}/password` | `user_management`, HTTPS, CSRF | Reset the password of an account |
| PUT | `/api/v1/users/{name}/http-password` | `user_management`, HTTPS, CSRF | Set or, with an empty value, remove the HTTP password |
| PUT | `/api/v1/users/guest-roles` | `user_management`, HTTPS, CSRF | Permissions every guest account gets |
| PUT | `/api/v1/users/guest-access` | `user_management`, HTTPS, CSRF | Switch guest access on or off, body `{"enabled": bool}` |
| PUT | `/api/v1/users/me/password` | HTTPS, CSRF | Change the own password, needs the old one |
| PUT | `/api/v1/users/me/http-password` | HTTPS, CSRF | Set or remove the own HTTP password, needs `http_login` |
| POST | `/api/v1/users/companion/pairing-code` | `user_management`, HTTPS, CSRF | Create the 12-character pairing code, valid for 10 minutes |
| DELETE | `/api/v1/users/companion/pairing-code` | `user_management`, HTTPS, CSRF | Discard an open pairing code |
| DELETE | `/api/v1/users/companion` | `user_management`, HTTPS, CSRF | Unpair the companion, all Home Assistant sessions end |
| PUT | `/api/v1/users/ha-roles` | `user_management`, HTTPS, CSRF | Presets `admin` and `user` for Home Assistant accounts |
| PUT | `/api/v1/users/ha:{ha_user_id}` | `user_management`, HTTPS, CSRF | Set roles, `roles_custom` and `disabled` of a Home Assistant account |
| DELETE | `/api/v1/users/ha:{ha_user_id}` | `user_management`, HTTPS, CSRF | Delete a Home Assistant account |

Home Assistant accounts have no password and no local sign-in. They are
listed with the local accounts, but the `ha:` prefix keeps their path apart
from local names, which cannot contain a colon. A local account cannot take
the name of a Home Assistant account.

Errors use the usual JSON form with a key such as `last_admin`,
`self_protection` or `user_exists`.

### Guest roles

Guest sessions get the permissions listed under `guest_roles` in
`GET /api/v1/users`, and only permissions with the Guest flag above can be
chosen there. An admin sets them with `PUT /api/v1/users/guest-roles`. Whatever
they include, a guest can act with it, for example write automation rules that
publish to MQTT within `publish_allowed_prefixes`. Keep this in mind before you
open guest access to people you do not trust.

The guest preset is view-only. It gives `view_overview`, `view_devices`,
`view_history`, `view_stats`, `view_efficiency`, `view_energy` and
`view_device_map`, and nothing that changes data.

### Guest access

`GET /api/v1/users` reports the switch as `guest_access`. An admin changes it
with `PUT /api/v1/users/guest-access`, body `{"enabled": false}`. While guest
access is off, `POST /api/v1/auth/guest` responds with 403 and the error key
`guest_access_disabled`. Switching it off ends all guest sessions and deletes
the guest accounts.

### CSRF

Privileged write endpoints need the `X-CSRF-Token` header with the value from
`GET /api/v1/auth/session` (field `csrf_token`). It is checked against the
cookie's session.

### Three-stage pattern

The security critical endpoints (bridge, MQTT configuration, Tailscale) always
check in this order: role, then HTTPS, then CSRF.

## Auth

| Method | Path | Gate | Purpose |
|---|---|---|---|
| POST | `/api/v1/auth/login` | HTTPS | Log in with username/password |
| POST | `/api/v1/auth/guest` | – | Guest session without a password |
| GET | `/api/v1/auth/session` | – | Current session, roles, CSRF token |
| POST | `/api/v1/auth/logout` | – | Log out, delete cookie |

`GET /api/v1/auth/session` responds with:

```json
{
  "username": "admin",
  "guest": false,
  "roles": ["system_actions", "mqtt_config"],
  "system_actions": true,
  "delete_device_discovery": true,
  "edit_setup": true,
  "setup_tiny_tuya": false,
  "mqtt_config": true,
  "automations": true,
  "edit_layout": false,
  "view_device_analysis": false,
  "edit_device_appearance": false,
  "csrf_token": "…",
  "expires_at": "2026-08-12T09:00:00Z"
}
```

## Companion port (8081)

The Home Assistant companion talks to the dashboard on its own listener,
port 8081, next to the main port (8080). It serves HTTPS with a certificate
the dashboard creates once (ECDSA P-256, valid for 20 years) and keeps in the
data directory as `companion-tls.pem` and `companion-tls.key`. Only the
companion's own routes are served there, and the main port answers
`/api/v1/companion/*` with 404.

Every request to the companion port needs `Authorization: Bearer <token>`,
the pairing token, with one exception: `POST /api/v1/companion/pair`. A
missing or unknown token gets 401 with the code `companion_unauthorized`.
The gate removes the header before the request reaches the handlers and
treats the request as insecure unless `X-Forwarded-Proto` is exactly `https`.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/companion/pair` | Pair with the proofs of the companion, see below. Answers the token and the server proof |
| GET | `/api/v1/companion/stream` | Server-sent events. `verify` asks for a Home Assistant sign-in check, `ping` every 25 seconds. A new stream ends the old one |
| POST | `/api/v1/companion/verify/{id}` | Answer one `verify` event with `{ok, ha_user_id, username, name, is_admin}`. 204 on success, 404 `verify_unknown` when the check is no longer pending |
| POST | `/api/v1/companion/session` | Start the dashboard session of a Home Assistant user. Body: `ha_user_id`, `username`, `name`, `is_admin`. Answers `token`, `csrf_token` and `expires_at`. Only valid on the companion port |
| POST | `/api/v1/companion/user-changed` | Report a changed Home Assistant user, `active` and `removed`. An inactive or removed user loses all sessions. 204 |

Pairing is a proof exchange, the code never travels. The companion sends
`nonce` (64 hex characters) and `proof`, an HMAC-SHA256 over the context
`energy-node-pair-v1`, `client`, the certificate fingerprint (SHA-256 of the
DER certificate of port 8081) and the nonce, keyed with the code. The
dashboard answers with a new `token` and a `proof` that also covers the token
(`server`). Each side checks the other's proof, so neither side sends the code
and a proof does not work against another certificate. The code has 12
characters from `ABCDEFGHJKLMNPQRSTUVWXYZ23456789`, is valid for 10 minutes,
and expires after five wrong attempts. Wrong attempts are also rate limited.

A new pairing replaces the old one, and unpairing removes it. Both end all
Home Assistant sessions at once and close the stream. Open `verify` checks
then end as unavailable.

Errors on these routes use the usual form:

| Code | Status | Meaning |
|---|---|---|
| `companion_unauthorized` | 401 | Token missing or unknown, or the companion is not paired |
| `pairing_invalid` | 403 | Wrong, used or expired pairing code |
| `account_disabled` | 403 | The Home Assistant account is disabled in the dashboard |
| `verify_unknown` | 404 | The `verify` check is no longer pending |
| `invalid_body` | 400 | Body not decodable, nonce not valid, or `ha_user_id` or `username` missing |
| `companion_failed` | 500 | The change could not be saved |
