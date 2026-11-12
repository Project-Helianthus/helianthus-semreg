# Changelog

## Unreleased

Thermal pack v1 (pack version 1.0.0) now emits `thermal.measurement.humidity`,
`thermal.setpoint.temperature_setpoint` and `thermal.setpoint.dhw_temperature_setpoint`
under the [public semantic naming contract](https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/architecture/naming-contract.md).
Units, quantities, bounds and operation argument relationships are preserved.
Previous spellings are rejected, with no alias, duplicate field, shim or migration.
Consumers must adopt the canonical keys together before release; rollback must
restore the entire consumer wave. This artifact-changing contract requires renewed
affected security, hardware and candidate evidence. Naming proof does not establish
native qualification. All other pack vocabulary remains unchanged and provisional
as recorded in [the inventory](naming-inventory.json).

The publication kernel now applies a batch in five named phases: input validation,
binding resolution, evaluation, fact assembly and commit. Results are unchanged: the
same error values in the same collection order, the same changed revision components,
the same snapshot, retained-observation and tombstone handling, and no commit before
final validation. No exported name, wire value or fixture changes. The library
revision still changes, so the gateway pin and its renewed evidence follow it.
