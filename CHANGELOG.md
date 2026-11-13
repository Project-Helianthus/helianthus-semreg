# Changelog

## Unreleased

The coherent-revision fixture now names its synthetic asset
`asset:synthetic:two-bindings`, and the fixture README and its test describe two
source bindings on one asset. The vectors prove revision coherence across two
bindings of one asset and are not evidence of any wider grouping. Expected
revisions, states, error IDs and kernel behaviour are unchanged. The fixture is a
local supplement with no pinned digest. Only test data, a test and Markdown
change, with no exported name or wire value, but the module revision changes and
the gateway pin follows it.

The catalog and pack reason for an eeBUS mapping that stays unknown while its
normative source is unresolved is renamed to `unknown_pending_normative_source`
(Go constant `ReasonUnknownPendingNormativeSource`). The value now states the
condition instead of a work item. Documentary correspondence only: the previous
value was `unknown_pending_std_01` (`ReasonUnknownPendingSTD01`). The old value
is rejected, with no alias, dual acceptance, shim or migration. Fixture vectors
change only in this value, and their pinned digests follow. The consumer wave
moves together: the kernel contract in
Project-Helianthus/helianthus-docs-semantic#55 and the consumer documentation it
lists, then the gateway re-pin, which also renames its own use of the value as a
`semreg.DefinitionID`. This artifact-changing wire change requires renewed
affected security, hardware and candidate evidence before release.

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

The exported API of the `catalog`, `operation` and `projection` packages and the
pack validator constructors now has doc comments that state each declaration's
responsibility and its non-obvious contracts: error identifiers, bounds,
ordering, ownership and lifecycle. The catalog reason `unknown_pending_std_01`
is documented by the condition it stands for and keeps its wire value. Only
comments change; no code, exported name, wire value or fixture changes. The new
comment lines move source line numbers, so a consumer binary built on this
revision changes and the gateway pin and its renewed evidence follow it. The
root package `semreg/v1` is documented separately.

Validation in the root package `semreg/v1` is now split by the type it validates.
`validation.go` keeps the error type and error identifiers, the scalar and
identifier validators and the record and contract interfaces. Evidence references,
source descriptors, origins, native bindings, identity links, source paths and
derivations move to `validation_bindings.go`; values, fact keys, times, freshness,
evaluation context and views, selections, quality, causal context, fact candidates,
retained observations, conflicts and fact envelopes to `validation_facts.go`; pack
and definition references, typed fields, predicates, service and capability
instances and the registry to `validation_registry.go`; and the strict JSON
decoder, canonical JSON and record digests to `validation_wire.go`. Declarations
move unchanged with their doc comments, so the error identifiers and their order
are the same. No exported name, wire value or fixture changes; the naming
inventories re-key the moved rows to their new owning files. The library revision
still changes, so the gateway pin and its renewed evidence follow it.

Snapshot validation moves from `publication.go` to `snapshot_validation.go` and is
split into named checks: header, collection shape and order, sources, fences,
cursors, source and fence cursor coverage, bindings and their lifecycle, identity
links, services, capabilities, fact candidates, retained observations, the
candidate graph and the snapshot identity. The checks run in the same order and
report the same error identifiers, so `Snapshot.Validate` returns the same error
for every snapshot. No exported name, wire value or fixture changes, and no
naming-inventory row moves. The library revision still changes, so the gateway
pin and its renewed evidence follow it.
