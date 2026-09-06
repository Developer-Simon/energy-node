# Security policy

Energy Node runs as a local appliance on a private network: it drives real
switchable loads, a battery bank, and a web dashboard with login. Please
report anything that could affect that safely and privately rather than in a
public issue.

## Reporting a vulnerability

Use GitHub's private reporting for this repository:
**Security → Advisories → Report a vulnerability**
(or open a draft advisory directly at
`https://github.com/Developer-Simon/energy-node/security/advisories/new`
once the repository is public).

Please include:

- what component is affected (dashboard, a specific bridge under `src/`, the
  Home Assistant integration under `integrations/homeassistant/`, or the
  deploy scripts)
- the impact you expect (e.g. auth bypass, credential exposure, a bridge
  accepting unvalidated input from the network)
- reproduction steps, ideally against the local smoke test
  (`dashboard/test/smoke/run-local-dashboard.sh`) rather than real hardware

There is no bug bounty; this is a hobby project run by one maintainer.
Expect an acknowledgement within a few days and a fix or a public
explanation once one is available — there is no fixed SLA.

## Scope

In scope: the dashboard's auth/session handling, its bridge-config and
automation-rule validation, the Mosquitto bridge configuration this project
generates, and the Python services' handling of local network input.

Out of scope: the security of Mosquitto, Tailscale, Caddy, or the underlying
OS themselves — report those upstream. Denial-of-service against a Raspberry
Pi 1 is expected behavior of underpowered hardware, not a vulnerability,
unless it is triggered by unauthenticated network input.

## Credentials in this repository

`scripts/deploy/check_tracked_secrets.sh` runs as a deploy preflight and
rejects tracked files that look like real credentials. If you believe a
secret has nonetheless been committed, report it the same way as above —
do not open a public issue that repeats the secret.
