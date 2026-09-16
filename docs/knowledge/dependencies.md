---
title: "Third-Party Sources"
---

# Third-Party Sources

Where a service's behavior was adapted from — or directly depends on — code
or ideas outside this repository. This is not a full dependency list (see
each service's `requirements.txt` / `pyproject.toml` for that); it exists so
the reasoning behind a non-obvious implementation choice can be traced back to
where it came from.

## APsystems EZ1

- [`apsystems-ez1`](https://pypi.org/project/apsystems-ez1/) (PyPI) — the
  Python client library the service uses to talk to the inverter's local
  REST API.
- [`apsystems-ez1-enhanced`](https://github.com/shopf/apsystems-ez1-enhanced)
  — a community-maintained Home Assistant integration for the same inverter
  family. Its documented RAM-vs-flash power-limit behavior
  (`getDefaultMaxPower` / `setDefaultMaxPower`) and its use of the
  undocumented `getOutputDataDetail` endpoint informed the power-limit
  handling and extended diagnostics described in
  [`knowledge/services/apsystems-ez1.md`](services/apsystems-ez1.md). No code
  was copied — both projects use the same underlying `apsystems-ez1` PyPI
  package, so the endpoint behavior applies directly.
- [Home Assistant community forum thread](https://community.home-assistant.io/t/apsystems-ez1-m-ez1-spe-ez1-lv-ez1-h-ez1d-l-ez1d-ez1d-h-community-enhanced-integration-extended-sensors-all-models-overnight-fix-more/994091)
  — the discussion the enhanced integration above was found through.
