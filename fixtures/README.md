# Compatibility fixtures

This directory contains versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

[`v1/pv-inverter-pack-vectors.json`](v1/pv-inverter-pack-vectors.json) is a
byte-for-byte copy of `api/v1/packs/pv-inverter-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`, as edited in place for the SI
units; its SHA-256 is
`a000cdb97aea3218f0c73f09244a84769cfa39c3cfc27d742ed55060b1b0b30e`.
The generated energy is in `unit.joule` with a maximum of 3.6 × 10^18 and the
inverter temperature in `unit.kelvin` from 223.15 to 473.15. Per-phase
apparent power and reactive power, and per-string `pv.dc.energy` in
`unit.joule` with the same bounds as generated energy, are in the copy.
The inverter totals `pv.ac.aggregate_apparent_power` in `unit.volt_ampere`
from 0 and `pv.ac.aggregate_reactive_power` in `unit.volt_ampere_reactive`,
signed, are on `pv.dimension.inverter` with the magnitude bound of
`pv.ac.aggregate_active_power`, and the inverter read descriptor lists them.
The day, month and year yields `pv.energy.yield_day`, `pv.energy.yield_month`
and `pv.energy.yield_year` are in `unit.joule` on `pv.dimension.system` with
the bounds of generated energy, and the system read descriptor lists them.
`pv.limit.active_power_ratio` is in `unit.ratio` from 0 to 1 on
`pv.dimension.inverter`, and the inverter read descriptor lists it; the
active-power limit operation still names only the watt limit.
Nothing derives string energy from generated energy. The PV pack tests
compare every field of the copy (identifier, kind, unit, dimension, bounds and
symbols) with the validator table.
Its five relation definitions use the shared encoding (`from`, `to`,
`cardinality`, `order`) and each declares `one_to_many`.
It defines static semantic evidence only and does not create a native mapping,
route, authority, or live operation.

[`v1/storage-bms-pack-vectors.json`](v1/storage-bms-pack-vectors.json) and
[`v1/storage-bms-contract-tables.json`](v1/storage-bms-contract-tables.json)
are byte-for-byte copies of `api/v1/packs/storage-bms-acceptance-vectors.json`
and `api/v1/packs/storage-bms-contract-tables.json` from
`Project-Helianthus/helianthus-docs-semantic`, as edited in place for the SI
units, for `helianthus.pack.storage@1.1.0`; their SHA-256 values are
respectively
`428aac8bf1039f904e214ae74299886b0ceb5d3f4165bfc6310f6c8eb880764c` and
`13051fb80e3bae8bc3a1ac93139f322f0ed9180430c1b50475ef2acfc98439b5`.
State of charge and state of health are in `unit.ratio` from 0 to 1. The two
energy counters and the three energy capacities (`storage.energy.remaining`,
`storage.energy.usable_capacity` and `storage.energy.nominal_capacity`) are in
`unit.joule` on `storage.dimension.pack`, each with a minimum of 0 and no
maximum. The pack and cell temperatures are in `unit.kelvin` with a lower
bound of 0, and the two capacities stay in `unit.ampere_hour`. Nothing in the
pack derives a joule capacity from an ampere-hour capacity. The storage pack
tests compare every field of both copies
with the validator table.
The six relation definitions of the contract tables use the shared encoding
(`from`, `to`, `cardinality`, `order`).
They define semantic evidence only and do not create native qualification,
mapping, a route, authority, or live operation.

[`v1/evse-pack-vectors.json`](v1/evse-pack-vectors.json) is a byte-for-byte
copy of `api/v1/packs/evse-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`, as edited in place for the SI
units. Its SHA-256 is
`ff6839859631da1e5f7db91ae439b96316c2e4f6b83baf2e6cce22092714b03d`.
The session and lifetime energy are in `unit.joule` with a maximum of
3.6 × 10^18. `evse.limit.allocated_current_phase` is in ampere on the phase
dimension, with the same bounds as the connector allocation, and the
connector operation still names only the connector field. The EVSE pack tests
compare every field of the copy with the validator table.
The EVSE pack tests execute all twelve static catalog vectors, including the
five-domain catalog, relation and Portal metadata, operation safety, Tesla,
eeBUS, Matter, and counter-policy boundaries. The fixture does not create a
relation instance, native mapping or sender, route, consumer API, persistence,
or live authority.

[`v1/thermal-hvac-pack-vectors.json`](v1/thermal-hvac-pack-vectors.json) is a
byte-for-byte copy of `api/v1/packs/thermal-hvac-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`, as edited in place for the
accepted thermal placement. Its SHA-256 is
`b47df6845422eb565a5f253efb629d5c1bb7daf0896301d85806da7e3b1d573a`;
`TestThermalMachineFixtureVectors` compares the digest of the copy with the
digest of the approved preimage it holds. The fixture declares 269 fields, twenty-one
resource dimensions, twenty-one services, eighty-seven capabilities and fifteen relation
definitions. The ten overrun statuses `thermal.overrun.<type>` are symbols on
`thermal.dimension.system` with the statuses `active`, `finished`, `inactive`
and `running`. The occupied and unoccupied heating and cooling setpoints are
in `unit.kelvin` on `thermal.dimension.zone` with the setpoint bounds, and
`thermal.mode.system` has nine symbols. Pump speed is in
`unit.reciprocal_second` from 0 to 1000 on `thermal.dimension.pump`, which
`thermal.service.pump` keys and `thermal.portal.read.pump` reads; valve
position is a 0 to 1 `unit.ratio` on `thermal.dimension.terminal_unit`. No
capability reads the pump service, and the set-temperature and set-mode
operations keep their single arguments.
The thermal pack tests execute all seventeen static catalog vectors and compare
every field, dimension, service, relation and capability with the validator
tables. The fixture does not create a native mapping, topology instance,
relation instance, Portal implementation, route, consumer API, or live
authority.

[`v1/infrastructure-pack-vectors.json`](v1/infrastructure-pack-vectors.json)
is a byte-for-byte copy of
`api/v1/packs/infrastructure-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`, as edited in place for the SI
units. Its SHA-256 is
`11040b53cb4baf7bc81f6c7e9f1a05952c774382da9c3e6c9838af45fecb1dfc`.
The import and export energy are in `unit.joule` with a maximum of
3.6 × 10^18. Line-to-line voltage is `infrastructure.ac.voltage_line_line`
on `infrastructure.dimension.line_pair`, with that dimension, its service
and its read descriptor. Phase voltage stays on the phase dimension. No
read capability is added for the line-pair service.
`infrastructure.energy.self_consumption` is in `unit.joule` on
`infrastructure.dimension.meter` with the bounds of import energy, and the
meter read descriptor lists it. `infrastructure.power.pcc` is a signed
quantity in `unit.watt` from -10^7 to 10^7 and
`infrastructure.limit.fallback_power` is in `unit.watt` from 0 to 10^7, both
on `infrastructure.dimension.grid_connection` and listed by its read
descriptor. No capability, operation or effect rule is added.
The infrastructure pack tests pin its source and execute every accepted static
catalog vector. The fixture is documentation-derived contract metadata only;
it does not create relation instances, Portal composition, routes, native
mappings, qualifications, or operations.

[`v1/equipment-pack-vectors.json`](v1/equipment-pack-vectors.json) is a
byte-for-byte copy of `api/v1/packs/equipment-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`. Its SHA-256 is
`37286291600e4dd65e374d41d76b442e88322d8e45faf8b5e9c57f5f0a0ad354`.

[`v1/energy-management-pack-vectors.json`](v1/energy-management-pack-vectors.json) is a
byte-for-byte copy of `api/v1/packs/energy-management-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic`. Its SHA-256 is
`59eabcf0dbae3d7881fa46f15bb16e89b5b0eaacc81db827105838636027bd07`.

[`v1/foundation-vectors.json`](v1/foundation-vectors.json) copies the 30
foundation-relevant vector entries from the merged semantic v1 contract at
`Project-Helianthus/helianthus-docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac`.
Tests execute 27 foundation vectors, including the implemented BASE publication
and snapshot behavior. K-POS-005 is executed directly from this copied vector.

[`v1/evaluation-selection-vectors.json`](v1/evaluation-selection-vectors.json)
copies K-POS-019, K-POS-025, and K-NEG-056 through K-NEG-065 from the docs issue
#6 correction accepted at
`Project-Helianthus/helianthus-docs-semantic@da5ab4415d3bec73f9572aec1c495a6cdcbcba47`.
Its source manifest pins both that commit and the exact corrected
`acceptance-vectors.json` bytes by SHA-256. Tests decode and execute the complete
snapshot, evaluation, selection, and named mutation inputs.

The three CapabilityInstance admit vectors in the foundation fixture, K-NEG-014,
K-NEG-015, and K-NEG-048, execute in `semreg/v1/operation`
(`TestCapabilityAdmissionFoundationVectors`) through publication and guarded
admission, with exact error and unchanged-state assertions.

[`v1/projection-compatibility-vectors.json`](v1/projection-compatibility-vectors.json)
copies exactly K-POS-016, K-POS-017, K-NEG-024, K-NEG-025, and K-NEG-037 from
the accepted semantic v1 source, with immutable source commit, path, SHA-256,
selection, and count metadata. The projection package runs each through its
public report or alias boundary with exact accept/reject assertions. It covers
the protocol-neutral base records only; it does not claim a mapping, native
route, or INT-05 completion.

[`v1/publication-vectors.json`](v1/publication-vectors.json) pins deterministic
publication scenarios to the same accepted documentation revision. The matching
Go fixtures exercise initial publication, patch retention, withdrawals, replay
and conflict rejection, source retirement, generation supersession, graph
cascade, conflict reconciliation, bounds, dangling references and deep-copy
isolation.

[`v1/coherent-revision-vectors.json`](v1/coherent-revision-vectors.json) adds
six synthetic S1c scenarios against `helianthus.semantic.kernel/v1`, derived
from the accepted publication contract identified by
[`v1/publication-vectors.json`](v1/publication-vectors.json). The matching runner
expands the scenario inputs with existing synthetic typed publication helpers;
this is a local supplement, not a byte-for-byte documentation fixture. It pins
two candidate-linked bindings on one asset, canonical fan-out order, coherent
revision vectors, retirement of only the second binding and its lifecycle
closure, deterministic bytes, and unchanged historical state. Negatives use the
existing successor-revision, asset ownership, dangling-reference and unique-ID
rules. Binding revisions are local counters, not semantic revision timestamps;
no new numeric comparison between those counters is introduced. Synthetic
public evidence records describe test data only: they prove no native facts or
qualification. This adds no mapping, gateway composition, API, family admission
or broader INT-05 migration completion.

[`v1/kernel-fork-acceptance.json`](v1/kernel-fork-acceptance.json) is a
byte-for-byte copy of `api/v1/kernel-fork-acceptance.json` from accepted
`Project-Helianthus/helianthus-docs-semantic@a435dba83f227c3d0ae83ca13611388132efd2e5`.
Its SHA-256 is
`65aaf62ac1ca00350c8b0283ae1c06cfd2ef4d09b618d27c689ac66946b19155`.
The matching Go tests execute all seven fork scenarios against detached,
in-memory kernel state only; the fixture creates no persistence, lifecycle,
gateway, transport, or live operation.

[`v1/compatibility/canonical-pv-v1/`](v1/compatibility/canonical-pv-v1/) is the
test-only canonical-PV v1 migration comparator. Its manifest pins the retained
`helianthus-ebusreg@738142f` donor and the byte-for-byte one-fact gateway golden
fixture at `helianthus-ebusgateway@f5cd9c5`, including SHA-256 values. The
separate synthetic mapper witness is explicitly labeled and pinned to the
gateway mapper/test sources. The harness executes the SemReg publication,
evaluation, PV validation, canonical JSON and projection APIs; its fourteen-row
table accounts for eight exact, three transformed and three withheld outputs
with loss and legacy-path rollback treatment. The pinned producer witness
freezes the three withheld native IDs as aggregate current plus events 1 and 2;
it does not synthesize aggregate current or event semantics. It creates no native mapping,
route, authority, persistence, gateway output, or consumer cutover.
The mapper witness keeps its bytes and the donor's units, kilowatt-hour and
degree Celsius. The harness converts them exactly to the SI units of the PV
pack before validation (a kilowatt-hour times 3 600 000 is a joule, a degree
Celsius plus 273.15 is a kelvin), checks that the conversion returns the donor
value, and requires the PV pack to refuse both donor units.

[`v1/operation-vectors.json`](v1/operation-vectors.json) retains the original 29
vectors selected by `coverage=operation`. The `release_vectors` group adds
synthetic cases for the rule-only wire form, exact alternatives, and
acknowledgement-only evidence. The tests pin the fixture bytes and exact vector
IDs. A strict loader rejects
duplicate JSON members and runs one named subtest for every selected vector,
asserting the exact stable result/error, rejection immutability, and a visible
typed-expansion digest. Exact typed EVSE admission/readback and precondition
expansions supplement the common harness, including revision-1-to-2 readback,
retained revision-1 evidence across an unrelated service change, replacement
generation 8, evaluated stale state, complete cross-pack fact ownership, the
60-second causal lifetime, and explicit ingress/egress entries. The matrix covers exact pack
ownership, immutable admission, causal and deadline checks,
candidate/capability eligibility, route ambiguity, generation guard claims,
bounded idempotency/retry retention, chronology, externally supplied dispatch
evidence, pack-owned effects and every terminal outcome. INT-06, not semreg,
owns the native lifecycle lock, current-generation recheck, callback, handle,
and release.

Operation vector fixture SHA-256: `748f0426978fec5fc63ee3657826e9712bd97d5e2fecf66659bfad5b9876a9a5`

The operation package exposes `NewKernelWithOptions` for the fail-closed
retention bound and `RecordRejection` for stable structurally valid rejected
records. Its selection and compatibility-alias admission methods only reject
those presentation inputs with their accepted stable errors; they do not add
selection, alias, or native-routing authority.

Production capability packs, normative Matter/eeBUS/native mappings, Portal,
gateway/consumer integration, persistence, INT-06 lifecycle ownership, full
INT-05, software 0.7, security review, hardware readiness, and physical
validation remain separate work.

[`v1/retained-observation-acceptance.json`](v1/retained-observation-acceptance.json)
is a byte-for-byte copy of the accepted
[bounded retained admission fixture](https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/api/v1/retained-observation-acceptance.json)
from docs-semantic #49, extending the sequential-lifecycle correction. The
fixture contains 41 scenarios; its inherited digest evidence remains unchanged.
The integrity test compares computed fixture bytes against a byte-array digest.

Contract `helianthus.semantic.kernel/v1` keeps wire records and operation
eligibility unchanged. New publication reserves retained history plus every
current observed instance under forty and existing fences plus distinct live
generation tuples under 128. Refreshes replace instances; inferred facts add no
observed debt. Expired current observations still reserve debt. Pruning history
requires a provable original expiry; unknown clocks never free room.

`PublicationKernel.RestoreAccepted` accepts a complete trusted accepted-batch log
into an empty kernel. Each entry includes its original explicit monotonic
publication context. It validates every historical transition, including digest,
source ownership, references, pack behavior, revisions and sequence, without
applying the new reservation rule to legacy intermediate states. Replacement of
live state is refused. Import is atomic and copies all state; accepted current-
tuple replay results and cursors are reconstructed. Persistence owners must
establish trust and completeness; this method does not authenticate a log.
`RetainedCapacity` reports committed debt and recovery/downgrade blockers;
it is never an admission token. Ordinary `Apply` always enforces capacity.
Legacy overcommit requires truthful expiry or evidenced explicit withdrawal.
Fencing is not guaranteed to recover legacy debt. More than 32 retained records
cannot be downgraded to the former implementation; preserve history and refuse
that downgrade until a reviewed compatible recovery makes it representable.

The gateway consumer described in #71 restores logs through detached kernels.
It must use the supported restore method for historical replay, then ordinary
`Apply` for new publication, and expose recovery blocked without claiming managed
startup readiness. This repository contains no production kernel caller outside
the root kernel; operation, projection, packs and catalog retain their existing
surfaces. No eBUS MCP or GraphQL representation changes are introduced. Gateway
startup, native-profile maximum proof and partial-child cleanup remain gateway
work; qualification and physical behavior remain Unknown.

The capacity runner expands all twenty-two admission and structural-restore
cases through typed kernel entry points. It covers the 127-fence plus one live
tuple boundary, two bindings sharing a tuple, successful restore at both
128-cursor boundaries, and atomic refusal of an additional observed or empty
generation. Structurally impossible imports leave an empty destination; they
are not admitted recovery-blocked states. The existing cursor and fence bounds
remain unchanged. Existing retained lifecycle, selection and operation exclusion
tests remain applicable. Synthetic fixtures establish no native qualification,
route, authority or physical proof.

[`v1/family-catalog-v1.json`](v1/family-catalog-v1.json) is the INT-05 family
catalog document. It holds the one family that names no vendor or protocol,
`infrastructure`, with its state (`unknown`), its reason (`no_producer`), its
composition (`not_composed`), its recorded verification level
(`not_tested_on_hardware`) and evidence references. SemReg stores no vendor
family row and no gateway composition record: each driver declares its own
families, with their state, verification level and composition evidence, through
the driver contract in `semreg/v1/driver`, and any host reads them through
SemReg. States come only from the semantic `Qualification` vocabulary;
"evidence-blocked", "no admissible profile" and NO_GO have no vocabulary value
and no reason, so they stay `unknown` or `candidate` and are named by the row's
`reason`. A row never creates a native mapping, route, authority or live
operation, and it carries no write-authority field and no default-deny rule.
`semreg/v1/catalog` decodes the document strictly and validates one family
record as a driver declares it with `Family.Validate`; its tests check
completeness, evidence and states.

Contract `family-catalog/v1` is defined by
`api/v1/family-catalog-v1.md` in
`Project-Helianthus/helianthus-docs-semantic@c576fa14be034901deaadf1e32b28c7719a68058`; the page is authoritative
and this summary follows it. The document has exactly the members `contract`,
`semantic_docs_pin`, `states` and `families`, all required, with no unknown or
repeated member at any object level (case variants of a member name count as repeated or unknown), no trailing data, and `semantic_docs_pin` requires
exactly 40 lowercase hexadecimal characters for its commit pin; the evidence
`owner` of a family is not checked for a commit pin, since other immutable
revisions are allowed. The document must hold every family of
`RequiredFamilyIDs` (today `infrastructure`): a required family missing from
the document and an unknown member of a `Family` object are rejected, while a
valid additional family that a driver declares is accepted, and there is no fixed
family count. A family has the required members `family_id`, `name`,
`native_owner`, `semantic_pack` (both may be empty strings), `state`,
`composition`, `reason`, `evidence` (at least one entry with non-empty `owner`,
`kind`, `locator`, `statement`), `composition_verified` and `verification`, and
the optional members `module` and `version`, each omitted when empty. `state` is
one of `candidate`, `qualified`, `unsupported`, `unknown`, `rejected`, and
`states` must equal that set exactly, each value once. A `qualified` row is
declared by the specification or public documentation its native owner pins and
bound at runtime: it has the reason `declared_by_specification` and a
`specification` evidence entry. `verification` is `tested_on_hardware` or
`not_tested_on_hardware`; it is a recorded level beside `state` and never changes
it, and `tested_on_hardware` needs a `hardware_test_record` evidence entry.
`composition` is `composed`, `composed_legacy`, `provider_local` or
`not_composed`. `module` and `version` are the composition evidence of the
registration of the driver that declares the family. Any claim other than
`not_composed` requires `composition_verified=true` and both `module` and
`version`. A `not_composed` row must have `composition_verified=false` and carry
neither `module` nor `version`; `false` means unverified, not asserted absent.
Evidence `owner` is
`Project-Helianthus/<repository>@<revision>` (immutable revision) or
`Project-Helianthus/<repository>#<number>` (issue or pull request thread, only
for kinds `issue` and `pull_request`; the validator here does not yet enforce
the two forms or required-member presence; these requirements are recorded on
the contract page). `kind` is an open, additive vocabulary; the validator names
`specification` and `hardware_test_record` through the constants
`EvidenceKindSpecification` and `EvidenceKindHardwareTestRecord`. `reason` is a
closed vocabulary defined on the page: `declared_by_specification`,
`specification_conflict`, `semantic_mapping_unknown`,
`unknown_pending_normative_source`, `no_producer`. Compatibility: the members
`verification`, `module` and `version`, the reasons `declared_by_specification`
and `specification_conflict`, the evidence kinds above and the removal of the
`gateway_composition` evidence kind and of the evidence-grade reasons are made in
place before the 0.7 release, with no contract revision bump and no alias. A new
`state` value still needs v2.

A family row may carry the optional member `further_semantic_packs`, added in
place before the 0.7 release: one or more exact `PackRef` strings, with no count
bound, naming the family's other accepted packs. It is omitted when empty (a
present empty array or `null` is rejected), is allowed only with a non-empty
`semantic_pack`, holds no repeated entry and no entry equal to `semantic_pack`,
and is in ascending byte order with `semantic_pack` sorting before every entry; a
row in any other order is rejected, not reordered. No pack of a row is primary,
and `Family.Packs` lists `semantic_pack` followed by the further packs. A row
that names six accepted packs is accepted. No row of this fixture carries the
member, so its row keeps its bytes, which `TestExistingRowsKeepTheirBytes`
checks by re-encoding the decoded document. The check that a row's packs include
the pack of every accepted mapping gate of its family is not in place yet.

[`v1/element-catalog-v1.json`](v1/element-catalog-v1.json) is a copy of
`api/v1/element-catalog-v1.json` from
`Project-Helianthus/helianthus-docs-semantic`, as accepted in
docs-semantic #76 and edited in place for the accepted thermal placement,
the four lens seeds, the two PV inverter totals, the thermal overrun
statuses, zone setpoints, pump and valve entries, the PV yield periods and
limit ratio, the infrastructure self-consumption, point-of-coupling power
and fallback limit, and the pilot accuracy, measurement-type and storage
current entries. Its SHA-256 is
`3e40e36bf595f0379b0742f82f7a5213f68fb2ac60fa2d9e5faa48a32a438477`; the
constant `graph.PinnedDigest` holds the same value and `graph.Load` accepts no
other bytes. Every in-place edit of the source catalog moves the copy, this
digest, the constant, the root package's pinned catalog digest and the catalog
bytes embedded in `semreg/v1/graph` together. The catalog lists six planes,
seven core relation types, 1903 entries and ten lenses, and marks
`resource.site` as the installation root. It lists the identification schemes
`scheme.ieee.eui48` and `scheme.ieee.eui64`, both with issuer `none`. It has
no entry of disposition `unknown`; a test that needs an unknown entry sets one
by a stated mutation.
`semreg/v1/graph` decodes it strictly and checks rules CAT-1 to CAT-9 against
the accepted packs. Its tests apply one rejected mutation per rule and match
the relation and Portal entries against the copied pack vectors above. The
fixture creates no resource, relation instance, membership record, route or
authority.

[`v1/graph-acceptance-vectors.json`](v1/graph-acceptance-vectors.json) is the
`helianthus.semantic.graph.acceptance/v1` document exercised by this module.
Its SHA-256 is
`f33422cccb2bb8d424cfc18637f3308d96659db5037e8a0fde31da6b45db2e73`;
`TestGraphAcceptanceVectorsPin` in `semreg/v1/graph` holds the same digest as
bytes. The document records the catalog digest of the pinned catalog copy.
The element catalog copy and `graph.PinnedDigest` are that pin. The document holds
90 vectors over 25 prior states, the coverage areas it lists and the operations
`construct_registry`,
`register_catalog`, `apply`, `assemble_cut`, `admit`, `list_resources`,
`traverse`, `retrieve_lens`, `sum_members` and `project`. Four `admit` vectors
whose input is `subject_check` are pinned with the document and are not
executed. Three prior states
register the catalog with stated `set_disposition` mutations, each setting
one member entry to `unknown` with no plane, a reason and a tracking issue;
the harness applies them to a copy of the pinned catalog.
`TestGraphAcceptanceVectors` executes every other vector by identifier through the
public API; it adds only members the document leaves out, omits a present
empty graph collection, and decodes every record strictly. All records are
synthetic: they prove no native fact,
qualification, route or authority.

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
