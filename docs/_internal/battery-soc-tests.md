# Battery SoC: tests

The pure SoC domain logic has its own test suite in the core package; the
adapter now only covers MQTT wiring, config loading, and golden-fixture parity:

```bash
.venv/bin/pytest libs/battery_soc_core/tests -v   # coulomb counting, calibration, entity spec, …
.venv/bin/pytest services/battery_soc/tests -v        # MQTT adapter + parity with pre-refactor behavior
```

Three tests are structural rather than behavioral checks and are worth
mentioning:

- `test_schema_properties_match_dataclass_fields` — catches "schema key added,
  dataclass field forgotten" in both directions. With
  `additionalProperties: false` plus `BatteryConfig(**values)` this would
  otherwise only surface as a hard reload error on the Pi.
- `test_*_entities_do_not_reference_missing_payload_keys` — every `value_key`
  from `entity_specs()` must be present in the published `/state` payload. A
  typo in between is otherwise just a silent "unknown" entity in Home
  Assistant.
- `services/battery_soc/tests/test_core_parity.py` — drives a scenario matrix
  (parallel/series, fresh/stale, simulation, …) through the core + adapter and
  compares `/state` and discovery configs byte-for-byte with the
  `golden/*.json` fixtures recorded before the core extraction. A deviation
  there is a real behavior change, not a test artifact.
