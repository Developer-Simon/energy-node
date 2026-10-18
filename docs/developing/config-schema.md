---
title: "Configuration schemas"
---

# Configuration schemas

A service that wants its settings in the [configuration editor](../dashboard/configuration.md)
ships two files next to each other: `<name>.json` with the values and
`<name>.schema.json` with the rules. The dashboard builds the form from the
schema, validates every save against it and keeps a revision of the old file.
This page lists what the schema has to say and how the dashboard reads it.

## Files and names

The dashboard scans its devices directory for `*.json` files that have a
matching `*.schema.json`. Only such pairs are offered. Schema files themselves
are never edited.

The file name decides which service is asked to reload. `<id>_devices.json`
belongs to the service `<id>` (`battery_soc_devices.json` to `battery_soc`),
`automation_rules.json` to `automation`. A file with any other name can be
edited, but no reload is sent.

## What happens on save

1. The dashboard validates the new content against the schema. If that fails,
   nothing is written and the form shows the path of the offending value, such
   as `$[0].charge_efficiency`.
2. The previous content is stored as a revision.
3. The new file is written atomically.
4. The dashboard publishes `reload` to `outstation/<id>/config/reload`.

The reload is a message, not a call. The dashboard reports whether the message
could be sent, not whether the service accepted the file. The service loads the
file itself and swaps its configuration only after that worked. If it refuses
the file, it keeps the old one and reports `runtime_status: "rejected"` with an
error in its retained `outstation/<id>/settings/status`. A service therefore
has to validate its own input again, with the same rules as the schema and the
checks the schema cannot express.

## How a field becomes a control

| Schema | Control |
|---|---|
| `type: object` | group. Fields listed in `required` come first, all others are folded into an optional section |
| `type: array` | list with add and remove buttons |
| `enum` | dropdown |
| `format: mqtt-topic`, or a key ending in `_topic` | dropdown of the topics the dashboard knows |
| `format: mqtt-json-key`, or a key ending in `_json_key` | text field with suggestions from the last payload |
| `type: boolean` | checkbox with a reset button |
| `integer`, `number` | number field |
| anything else | text field |
| `const`, a single-value `enum` or `minimum` equal to `maximum` | shown, but locked |

`title` is the label and `description` the help text below the field. Both are
shown to the operator, so write them for a person who has never seen the code.

`minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `minLength` and
`multipleOf` also set the limits of the number field. For decimals the browser
limit of an exclusive bound is only a hint, the server rejects the bound value
itself. `multipleOf` is only passed on to the browser when it fits the minimum,
otherwise the field accepts any step. A `type: number` without a step would
reject every decimal in the browser, so the form always sets one.

### Topics and JSON keys

A `*_json_key` field is tied to the `*_topic` field with the same prefix in the
same object (`charger_power_json_key` to `charger_power_topic`). The form lists
numeric keys of the last payload first. It is a suggestion list on purpose. A
key that is not in the last payload, for example because the topic has never
sent anything, stays valid.

Topics that never carried a message are marked in the list. If the sample
endpoint fails, the form keeps working without suggestions.

### Conditions

`allOf` with `if`/`then`/`else` is supported. Fields that exist only in a
branch are hidden until their branch applies and are left out of the saved
file while hidden. The server is lenient in one place: a value in an inactive
branch is only type-checked, so an old revision with such a value can still be
restored. If a combination really matters, the service itself has to reject it
with its own error code.

## What the dashboard validates

The server checks a deliberate subset of JSON Schema:

| Supported | Not supported |
|---|---|
| `type`, `enum`, `const`, `required` | `$ref` |
| `additionalProperties: false`, `unevaluatedProperties: false` | `oneOf`, `anyOf`, `not` |
| `minLength`, `pattern` | `format` (a hint for the form only) |
| `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf` | the draft 4 boolean `exclusiveMinimum` |
| `minItems`, `maxItems`, `items` | |
| `allOf`, `if`, `then`, `else` | |

A keyword outside this list is ignored without a warning, so a schema that
relies on `oneOf` accepts anything. Express such a rule in the service.

## Defaults and empty fields

The form does not write a value that only comes from `default`. The field is
shown greyed out, stays out of the file until the operator touches it, and the
file stays short. Two consequences:

- The default in the schema is a real statement. A change to it changes the
  behaviour of every installation that never set the field.
- If a field must be able to mean "not set", give it no `default`. It shows as
  an empty field, is left out when empty and stays unset over a save. This also
  keeps `0` apart from "not set".

If a shipped JSON file lists every key explicitly, add a new key there too.
Otherwise it appears as a grey default and is dropped on the next save.

Checkboxes remember whether they were ever set, so "not set" and "false" stay
different. The reset button goes back to "not set".

## Showing a topic on the device map

Every field with `format: mqtt-topic` has to say what it means for the
[device map](../dashboard/device-map.md#data-flow-layer). The annotations are
in [data flows](data-flow.md#12-data-flow-on-the-device-map). A test fails for
a topic field without one.

## Adding a field

1. Add the property to `<name>.schema.json` with `title` and `description`.
   Set a `default` only if the field has a sensible one.
2. Teach the service the field. With `additionalProperties: false` a field the
   service does not know is a reload error.
3. Add the key to the shipped `<name>.json` if that file lists all keys.
4. Roll out in this order: schema and service first, then the JSON, then the
   reload. A JSON with new keys fails against an old schema.
5. Run `node --test dashboard/test/config.page.test.mjs dashboard/test/schema-form.test.mjs`
   for the form and
   `go test ./...` in `dashboard/` for validator and endpoints.
