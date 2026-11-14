# Changelog

## Unreleased

Graph enforcement is now a property of the kernel registry. The root package
defines the interface `GraphCatalog` (digest, pack coverage, disposition and
planes of a pack definition and of a resource kind, kind of a dimension and of
a service, relation definition attributes, scheme listing) with the types
`GraphDisposition`, `GraphPlane`, `GraphMembership` and
`GraphRelationDefinition`, and never imports `semreg/v1/graph`.
`NewGraphRegistry` and `NewGraphPublicationKernel` build a graph-enforced
registry: a missing catalog, a catalog whose digest is not the pinned digest
of `fixtures/v1/element-catalog-v1.json`, or a registered pack the catalog does
not cover fails construction with `membership_missing`. Every operation and
effect rule whose disposition is not `member` is recorded as inadmissible at
construction. `Registry.GraphEnforced` and `Registry.CatalogDigest` expose the
enforcement and the catalog digest for the release bill of materials, and
`Registry.GraphAdmissionError` is the one admission lookup of that index. The
new `operation.NewGraphKernel` builds an operation kernel over such a registry;
its `Admit` rejects an intent whose operation or effect rule is inadmissible
with `membership_missing` before any route is resolved, and reads no catalog
or graph record. In `semreg/v1/graph`, `Catalog` implements the interface and
gains `Covers`, which applies CAT-1 to one registered pack; `Embedded` loads
the catalog compiled into the package (a byte copy of the fixture, accepted
only under `PinnedDigest`), and `Enforced` builds a graph-enforced registry
with it. A graph-enforced publication kernel publishes a batch without graph
members exactly as a kernel without the catalog, and still refuses every graph
member with `membership_missing` until the resource and relation publication
rules land. `NewRegistry`, `NewPublicationKernel`, `operation.NewKernel` and
`operation.NewKernelWithOptions` keep their behaviour, and kernel vectors with
synthetic definitions under a real pack reference keep running without the
catalog. No pack, fixture, wire value or existing exported name changes, and
no pack or contract version is raised. The module revision changes, so the
gateway pin follows it when a consumer adopts graph enforcement.

The records of the semantic resource graph are part of the root package:
`Resource` with its anchor, anchor supersession, state, anchor class and anchor
basis; `RelationInstance` and `RelationKey`; `RelationAbsence` and
`RelationAbsenceKey`; the optional `resource` and `key` members of
`IdentityLink`, the optional `resource` member of `ServiceInstance`, the
resource, relation and absence collections of `PublicationBatch` and
`Snapshot`, and the `structure` component of `RevisionVector`. Every graph
member is omitted when empty, so a record without graph content keeps its
canonical bytes, digest and identifier; a present empty collection is
`invalid_value`. Decode and Validate check value shapes, bounds and canonical
order only. A kernel without the element catalog refuses every graph member of
a batch with `membership_missing`.

New package `semreg/v1/graph` holds the element catalog of the graph contract.
`fixtures/v1/element-catalog-v1.json` is a byte copy of the docs-semantic
catalog, pinned by SHA-256 in `fixtures/README.md` and in the constant
`graph.PinnedDigest`. `Decode` reads `helianthus.semantic.element-catalog/v1`
strictly. `Check` and `Validate` apply CAT-1 to CAT-9 against the pack metadata
registry and the pack definition indexes, with the stable errors of the
contract: `membership_missing`, `definition_owner_missing`,
`definition_owner_conflict`, `duplicate_identity`, `noncanonical_order` and
`invalid_value`. `Load` accepts only the pinned bytes. Read-only lookups return
detached copies of entries, planes, dispositions, dimension and service kinds,
relation definitions, identification schemes and lenses. No package imports the
new one yet, and no existing package, pack, fixture, exported name or wire value
changes. No pack or contract version is raised. The module revision changes, so
the gateway pin follows it when a consumer adopts the package.

The PV, EVSE, infrastructure and storage pack vectors and the storage contract
tables are copied again, byte for byte, from the semantic contract, which renamed
the pack member `relationships` to `relations` and the identifiers
`<pack>.relationship.<name>` to `<pack>.relation.<name>` in place. Every relation
definition now uses one encoding (`from`, `to`, `cardinality`, `order`); the
storage contract tables replace `source` and `target` with `from` and `to`, and
the PV relations declare `one_to_many`. The vector errors `relationships catalog`
and `relationship association` are now `relations catalog` and
`relation association`. The five SHA-256 pins in `fixtures/README.md` and the
pack tests move with the bytes; the thermal fixture and its pin are unchanged.
The pack tests read `relations`, check each relation's identity, endpoints,
cardinality and order, and two test functions drop the old word from their
names. The README now states that packs are read-only at run time and change in
place until the 0.7 release, and that relation instances are kernel records
defined by the graph contract. No pack definition, validator, exported name or
kernel behaviour changes, and no pack or contract version is raised.

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
