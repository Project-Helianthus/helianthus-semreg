# Changelog

## Unreleased

In place before the 0.7 release: `semreg/v1/graph` serves a listing and a
bounded traversal over one retained graph cut. `CutStore` keeps the newest
cut and every cut an unexpired page token names, at most 8 cuts and at most
120 seconds after a cut stops being the newest (CUT-3). `ListResources`
returns the current resources of that cut, optionally by kind and plane, in
resource id order, in pages of at most 4096 (ENT-2). `Traverse` walks
qualified coherent instances from one entry (a current resource, the roots of
a lens, or an installation site of kind `resource.site`), by a lens or by
planes and definitions, in one direction and to a bounded depth (ENT-1,
TRV-1 to TRV-8). A page token of another cut or request, and a pin of a cut
the store does not retain, are `revision_conflict`. A malformed token is
`invalid_value`. A non-current entry resource is `dangling_reference`. A
definition outside the requested planes is `membership_missing`. Depth and
fan-out stop with frontier entries and counts; a frontier or a page past its
bound is `bounds_exceeded`. A resource reached twice is listed once, at its
smallest depth. Identical inputs produce identical bytes. No pack, catalog
or contract version is raised, and no lock moves. The root package and the
operation package still do not import this package. Lens results, sums and
target reports remain later slices.

Breaking, in place before the 0.7 release: `helianthus.pack.storage` at 1.1.0
gains `storage.energy.remaining`, `storage.energy.usable_capacity` and
`storage.energy.nominal_capacity` on `storage.dimension.pack`. Each is a
quantity in `unit.joule` with a minimum of 0 and no maximum, the same kind as
`storage.energy.charged` and `storage.energy.discharged`.
`storage.capacity.charge` and `storage.capacity.discharge` stay in
`unit.ampere_hour`. Nothing in the pack derives a joule capacity from an
ampere-hour capacity. No service, capability, operation, pack version or
catalog version is added. The storage vectors, the storage contract tables and
the element catalog are edited in place, and their SHA-256 pins in
`fixtures/README.md`, the storage contract test, `graph.PinnedDigest` and the
root package's pinned catalog digest move together with the catalog bytes
embedded in `semreg/v1/graph`. The graph acceptance vectors record the same
catalog digest, and that document's own pin moves with the recorded digest.
Field-count checks that these three fields change go from 20 to 23, and the
catalog counts go from 107 facts and 353 entries to 110 facts and 356
entries. The rows of
`naming-inventory.json` and their bindings in `naming-consumers.json` for the
three identifiers are provisional. No alias, shim or dual acceptance is added.
docs-semantic follows with the same three field definitions and the same
catalog bytes.

Breaking, in place before the 0.7 release: the typed PV, EVSE, infrastructure
and storage packs follow the SI units of their pack contracts in
docs-semantic. Every energy field is in `unit.joule` (`pv.energy.generated`,
`evse.energy.session`, `evse.energy.lifetime`, `infrastructure.energy.import`,
`infrastructure.energy.export`, `storage.energy.charged` and
`storage.energy.discharged`); the bounded energy fields keep a minimum of 0
and their maximum of 10^12 kilowatt-hour becomes 3.6 × 10^18 joule.
`pv.temperature.inverter` is in `unit.kelvin` from 223.15 to 473.15, and
`storage.temperature.pack` and `storage.temperature.cell` are in `unit.kelvin`
with a lower bound of 0. `storage.state.soc` and `storage.state.soh` are in
`unit.ratio` from 0 to 1. `storage.capacity.charge` and
`storage.capacity.discharge` stay in `unit.ampere_hour`. Fields, dimensions,
services, capabilities and operations are otherwise unchanged; no quantity
moves to another pack, and the earlier units have no alias, shim or dual
acceptance. The PV, EVSE, infrastructure and storage vectors and the storage
contract tables are re-copied byte for byte, and their SHA-256 pins in
`fixtures/README.md` and the contract tests move in place. The PV, EVSE and
storage tests now compare every field of the copied contracts (identifier,
kind, unit, dimension, minimum, maximum and symbols) with the validator
tables, as the infrastructure test already did. The pack metadata test
expects `unit.ratio` as the canonical unit of the storage state of charge and
refuses `unit.percent`. The canonical-PV comparator keeps the bytes of its
retained mapper witness, which states the donor's kilowatt-hour and degree
Celsius, and converts them exactly in the test (times 3 600 000 to joule,
plus 273.15 to kelvin) before PV validation. The rows of
`naming-inventory.json` and their bindings in `naming-consumers.json` follow:
the removed unit literals are dropped, the added ones are provisional, and the
unit metadata of the affected rows names the SI unit. Unit rule: canonical
units in the registry are SI and a customary unit appears only at display, in
a consumer; the strict reading used here is a working reading not yet
confirmed by the project owner. No pack or contract version is raised.
Gateway follow-up: the PV, EVSE, infrastructure meter and storage
publications move to these SI units (kilowatt-hour times 3 600 000 to joule,
degree Celsius plus 273.15 to kelvin, percent divided by 100 to ratio), keep
the native value and unit in the evidence, and move their pinned semantic
revision with it.

Breaking, in place before the 0.7 release: `semreg/v1/packs/thermal` follows
the accepted thermal placement of the thermal pack in docs-semantic. Every
thermal field declares exactly one fact dimension, the resource dimension of
its subject; there is no qualifier dimension, and the four quantity dimensions
are removed. The pack has 32 fields, ten resource dimensions (system, zone,
circuit, domestic hot water, ventilation, heat generator, thermal store,
terminal unit, refrigerant circuit and compressor), ten services, twelve
capabilities and the same two operations. Every quantity is in an SI unit:
kelvin for temperatures, a 0 to 1 ratio for humidity and demand, cubic metre
per second, pascal, reciprocal second, watt and joule. The bounds of earlier
fields are their earlier bounds converted exactly (degree Celsius plus 273.15,
percent divided by 100, the flow bound restated as 0.2 cubic metre per
second); `thermal.operation.set_temperature` keeps its argument
`thermal.setpoint.temperature_setpoint`, now in kelvin from 223.15 to 423.15.
The water temperature is replaced by the circuit flow temperature and the
hot-water temperature, and the power by the heat generator's
`thermal.measurement.thermal_power`; the removed identifiers have no alias,
shim or dual acceptance. Without the catalog `ValidateFact` keeps the rule of
one text dimension per fact, now the field's resource dimension; under graph
enforcement `ValidateGraphFact` accepts that dimension only when the catalog
declares it as the field's resolved subject with no qualifier, and only with a
resource identifier as its value. The five pack relation definitions,
`thermal.relation.<name>` with their endpoint dimensions and cardinality, are
checked against the copied vectors in the contract test, as the other typed
packs check theirs. The thermal pack vectors, the element catalog and the graph
vectors are re-copied byte for byte, and their SHA-256 pins in
`fixtures/README.md`, `TestGraphAcceptanceVectorsPin` and `graph.PinnedDigest`
move in place. The graph vector harness applies the `set_disposition`
catalog mutations of a prior state to a copy of the pinned catalog, and the
graph tests that need an `unknown` entry set one by a stated mutation, since
the committed catalog has none left. The thermal rows of `naming-inventory.json`
and their bindings in `naming-consumers.json` follow the naming contract: the
removed identifiers are dropped, the added ones are provisional and the three
proven renames carry their new unit and subject. Unit rule: canonical units in
the registry are SI and a customary unit appears only at display, in a
consumer; the strict reading used here is a working reading not yet confirmed
by the project owner. No pack or contract version is raised. Gateway
follow-up: the eBUS thermal family publication moves every thermal fact to the
renamed fields, the resource dimension of its subject and the SI unit
(degree Celsius plus 273.15 to kelvin, percent divided by 100 to ratio,
litre per minute divided by 60 000 to cubic metre per second with the
declared rounding, bar times 100 000 to pascal, kilowatt-hour times 3 600 000
to joule), keeps the native value and unit in the evidence, and moves its
pinned semantic revision with it.

`semreg/v1/catalog` decodes and validates the optional family-row member
`further_semantic_packs`, an in-place edit of `family-catalog/v1` before the
0.7 release: 1 through 4 exact `PackRef` strings, omitted when empty, allowed
only with a non-empty `semantic_pack`, unique, none equal to `semantic_pack`
and in ascending byte order after it. A present empty array, `null`, a
repeated or malformed entry, an entry equal to or sorting before
`semantic_pack`, an unsorted list, more than four entries and a list on a row
with an empty `semantic_pack` are rejected. `Family.Packs` returns
`semantic_pack` followed by the further packs. The `Family` fields follow the
fixture's member order, and `TestExistingRowsKeepTheirBytes` re-encodes every
row and the whole document to the committed bytes. The fixture, its
`semantic_pack` values, states, compositions and reasons are unchanged; no row
carries the new member yet, and the cross-check of a row's packs against the
accepted mapping gates of its family is still to come.

`fixtures/v1/graph-acceptance-vectors.json` is a byte copy of the graph
acceptance vectors of docs-semantic, pinned by SHA-256 in `fixtures/README.md`
and in `TestGraphAcceptanceVectorsPin`; the element catalog copy, its digest
and `graph.PinnedDigest` are unchanged. `TestGraphAcceptanceVectors` executes
every one of the 60 vectors by identifier through the public API, with the
element catalog registered wherever the prior state registers it, for the
criteria of semreg#90: relation instances, plane membership, catalog
validation, cross-asset structure through graph cuts and the seven negative
classes (duplicate identity, false join, missing membership, stale relation,
dangling endpoint, cross-plane identity drift and an operation admitted from
visibility alone). No vector is skipped or selected by identifier. A vector
passes, fails, or ends as a document finding when the kernel refuses a record
of the document with exactly the stable error and detail of a contract rule
the harness finds that record breaking; any other refusal fails, and so does
an accepted record that breaks a rule. `TestGraphVectorDocumentRules` varies
every member those rules read. Six vectors pass: G-POS-ACT-1, G-NEG-ACT-2,
G-POS-CAT-1, G-POS-CAT-2, G-NEG-CAT-1 and G-NEG-CAT-2. The other 54 end as
document findings on four rules: relation definitions are written as
owner-tagged catalog references instead of the kernel's definition reference
(17 vectors); operator origins carry native source fields (33); decimals are
not canonical (2); and snapshot identifiers digest an empty retained-observation
list the kernel omits (2). Conformance is therefore not reached yet.
`TestOperationDoesNotImportGraph` encodes the `go list -deps` evidence that
neither the operation package nor the root package depends on
`semreg/v1/graph` (AUT-1, ACT-4). No product code, pack, fixture or contract
version changes.

`semreg/v1/graph` assembles graph cuts. `AssembleCut` reads at most 256
snapshots of distinct assets together with the element catalog and a
caller-asserted completeness, `complete` or `partial`. It is pure and
deterministic and rejects only invalid input: an invalid completeness
(`invalid_enum`), no catalog (`invalid_value`), more than 256 snapshots
(`bounds_exceeded`), a snapshot that fails its own validation, or two
snapshots of one asset (`duplicate_key`). The cut identifier is `sha256:` over
the JCS bytes of `{contract:"helianthus.semantic.graph-cut/v1", members}`,
where `members` is the `(asset_id, snapshot_id)` vector sorted by asset; there
is no global revision. Problems inside or between members are findings: one
`ResourceID` current in two members, or retired in one and current in another,
is `duplicate_identity`, excludes both members and makes the cut `partial`;
current resources of one kind with one anchor key, native or parent and slot,
in two members are `same_unit` and both stay; an absence statement that
denies a qualified instance of another member is `contradicted_absence` and
makes the instance `conflict`; a resource whose anchor parent is not the
whole of a qualified, coherent `contains`, `groups` or `hosts` instance to it
is `anchor_parent_unresolved`; and a member that holds facts of `member`
definitions but no resource, relation instance or absence statement is
`ungraphed`, while facts of `unknown` definitions do not count. Every relation
instance of a kept member is `coherent`, `stale`, `dangling`, `outside_cut`,
`excluded` or `conflict`; conflict covers a stored conflict, cardinality,
forest and cycle violations across members, a definition that is not a
member relation of the catalog and an endpoint of a kind the definition does
not admit. `PlanesOfResource`, `PlanesOfRelation` and
`PlanesOfFact` derive plane membership over the cut: a relation instance only
when qualified and coherent, a fact, service or capability when its definition
is a member and its subject is current, and a current resource only in the
planes its kind admits, where it is physical equipment or a component, the
subject of an instance in that plane or an endpoint of a relation instance in
that plane. `Cut.CanonicalJSON` is the one encoder of the result, with members
by asset, findings by kind and canonical subject and relation instances by
canonical definition, `from` and `to`. Neither the root package nor the
operation package imports `semreg/v1/graph`. No pack, fixture or contract
version is raised.

A graph-enforced publication kernel with the resource lookups publishes
relation instances and absence statements, keyed by `(definition, from, to)`
and `(definition, resource, side)`, under REL-1 to REL-7 and the bounds of the
graph contract. A record names a relation definition with disposition `member`
(`membership_missing`). Its starting end, or the resource of an absence
statement, is a current resource of the asset (`dangling_reference`) of a kind
the definition admits at that end (`identity_drift`). Its basis class is
admitted by the definition and agrees with the origin kind: `configuration`
with an operator origin, `native_observation` and `gate_rule` with a native
origin (`relation_not_qualified`). A native origin resolves to a binding of the
asset and is a current binding of the batch's own source, epoch and generation.
A far end the asset holds is current (`stale_relation`) and of an admitted kind
(`identity_drift`); a far end the asset holds no record of is homed in another
asset and stays unresolved until a graph cut, unless the asset's own records
name it as a starting end or an absence resource (`dangling_reference`). An
instance and an absence statement for one definition, resource and side are
never both current, and one key is never upserted and withdrawn in one batch
(`invalid_value`). A withdrawal names a record of the current snapshot
(`dangling_reference`); a native record is replaced or withdrawn only by its
own source (`invalid_value`). After every batch the kernel sets to `conflict`
every qualified instance in a cardinality violation of its definition, two
`contains` or two `hosts` instances with one far end across definitions, or a
cycle of `contains`, `groups`, `hosts` or `relays` across definitions; it never
chooses one instance, a conflict ends with its cause, and a publisher-supplied
`conflict` stays `invalid_value`. A generation fence or a source retirement
removes every instance and absence statement with a native basis of that
generation in the same snapshot; configuration records stay until they are
withdrawn, and a batch that fences its own generation publishes no relation
record (`stale_driver_generation`). A resource named by a current relation
record is neither withdrawn nor retired. Relation records are revisioned like
the other objects, including a derived change of state, and the `structure`
revision counts their changes. Per asset, 8192 relation instances and 1024
absence statements are allowed (`bounds_exceeded`). Snapshot validation
resolves every relation and absence reference inside the snapshot except a far
end homed in another asset, and checks the basis class against the origin and
the origin binding of a native record. Kernels without the catalog, or with a
catalog without the resource lookups, still refuse every relation record. No
pack, fixture or contract version is raised.

A graph-enforced publication kernel publishes resources. The catalog
interface gains the optional extension `GraphResourceCatalog`
(`ResourceKindClass`, `FactSubject` and `PackDimension`, the last two keyed by
pack and identifier because a fact key carries no definition version) with the
types `GraphFactSubject` and `GraphDimension`; `semreg/v1/graph.Catalog`
implements it. Under it the kernel applies RES-1 to RES-9, the subject rule of
section 6.2 and PUB-3 of the graph contract: a resource kind is a catalog
member (`membership_missing`); the anchor class, its members, the basis
against the origin kind and the identification scheme follow the catalog
(`invalid_value`); a kind change or an anchor change without an allowed
supersession naming the exact previous anchor is `identity_drift`; two current
resources with one anchor key, or a retired `ResourceID` made current or
withdrawn, are `duplicate_identity`; a resource dimension value and a
service's `resource` name a current resource of the declared kind
(`dangling_reference` when absent or not current, `identity_drift` for another
kind); an observed fact comes from a binding whose non-withdrawn identity link
names its subject or the subject's anchor parent chain, qualified for a
promoted fact (`identity_not_qualified`); an identity link that carries `key`
and names a natively anchored resource carries its anchor key
(`identity_not_qualified`); a resource still named by a current fact or
service is neither withdrawn nor retired; and a native origin resolves to a
current or tombstoned binding of the asset. A fact of an `unknown` definition
names no resource and stays publishable; facts of `unsupported` definitions
and services or capabilities whose disposition is not `member` are refused
with `membership_missing`. Resources are revisioned like the other objects and
are never rewritten by a source epoch, a fence or a retirement. The `structure`
revision is set to 1 on the first accepted resource change and increments on
every later one; a snapshot without graph content keeps its bytes. Per asset,
4096 resources and 1024 identity links that carry `resource` are allowed; the
bound of 128 still applies to identity links without one. A catalog without
the extension still refuses every graph member. `ResourceAnchor`
gains the optional member `scheme`, present exactly when the basis is
`native_identity` and omitted otherwise, so records without it keep their
canonical bytes. A pack validator may implement `GraphFactValidator`; a
graph-enforced registry then calls `ValidateGraphFact` with the subject the
catalog declares. The thermal, PV, storage, EVSE and infrastructure validators
implement it: the field's one text dimension becomes exactly the declared
resource dimension carrying an opaque resource identifier plus the declared
qualifier dimensions. Without the catalog every validator and kernel behaves
as before. No pack, fixture or contract version is raised.

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
