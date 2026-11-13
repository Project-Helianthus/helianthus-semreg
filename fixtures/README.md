# Compatibility fixtures

This directory contains versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

[`v1/pv-inverter-pack-vectors.json`](v1/pv-inverter-pack-vectors.json) is the
byte-for-byte PV/inverter acceptance fixture from
`helianthus-docs-semantic@c20fdef000fe95df95fca60c55d651a0614c4efd`; its
SHA-256 is `10d45d196f6ff020c3e596eaa16c36f8e39cd9ab4eb0bb276d975f7d42c9511e`.
It defines static semantic evidence only and does not create a native mapping,
route, authority, or live operation.

[`v1/storage-bms-pack-vectors.json`](v1/storage-bms-pack-vectors.json) and
[`v1/storage-bms-contract-tables.json`](v1/storage-bms-contract-tables.json)
are byte-for-byte copies from accepted
`helianthus-docs-semantic@c20fdef000fe95df95fca60c55d651a0614c4efd` for
`helianthus.pack.storage@1.1.0`; their SHA-256 values are respectively
`6bb66f304c3ccd5870a4e49a68f1f3d20011eab84e3722c5f8fdfda79b7b4321` and
`3bde31cbc59ff3e2ea19408f3bfb09210837e7df20a692593e5c261bc48b032f`.
They define semantic evidence only and do not create native qualification,
mapping, a route, authority, or live operation.

[`v1/evse-pack-vectors.json`](v1/evse-pack-vectors.json) is a byte-for-byte
copy of `api/v1/packs/evse-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic@4fc56613a521dd2549cc81b1178f248dbcfd5164`.
Its SHA-256 is
`0138874611fd78cadb792785c4ab73f3b062d92cddae2297b06b9cb8c28f30b4`.
The EVSE pack tests execute all twelve static catalog vectors, including the
five-domain catalog, topology and Portal metadata, operation safety, Tesla,
eeBUS, Matter, and counter-policy boundaries. The fixture does not create a
topology instance, native mapping or sender, route, consumer API, persistence,
or live authority.

[`v1/thermal-hvac-pack-vectors.json`](v1/thermal-hvac-pack-vectors.json) is a
byte-for-byte copy of `api/v1/packs/thermal-hvac-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic@4fc56613a521dd2549cc81b1178f248dbcfd5164`.
Its SHA-256 is
`6fc20eaa6965e2073c38fba943b401e0b10673c16cef97286575dca083431576`.
The thermal pack tests execute all twelve static catalog vectors. The fixture
does not create a native mapping, topology instance, Portal implementation,
route, consumer API, or live authority.

[`v1/infrastructure-pack-vectors.json`](v1/infrastructure-pack-vectors.json)
is a byte-for-byte copy of
`api/v1/packs/infrastructure-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic@4fc56613a521dd2549cc81b1178f248dbcfd5164`.
Its SHA-256 is
`1f7db5f27ab6260c168e8dac4d91b746e0f64d92f56e29561c16cd3fb587ed9c`.
The infrastructure pack tests pin its source and execute every accepted static
catalog vector. The fixture is documentation-derived contract metadata only;
it does not create topology instances, Portal composition, routes, native
mappings, qualifications, or operations.

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

[`v1/operation-vectors.json`](v1/operation-vectors.json) mechanically copies all
29 vectors whose coverage includes `operation` from the accepted
`Project-Helianthus/helianthus-docs-semantic@da5ab4415d3bec73f9572aec1c495a6cdcbcba47`
source. Its manifest pins the complete source-vector SHA-256 and its own tests
pin the selected fixture bytes and exact vector IDs. A strict loader rejects
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
catalog: one row for each of the fourteen INT-07 draft families with the
qualification state its published evidence supports today, a reason, the
composition status, and evidence references pinned to immutable revisions
(modbusreg `1d92f28`, canbusreg `f5b8cbf`, ebusreg `2342a8d`, eebusreg `cdfdf0c`,
docs-semantic `30f5e5c` for the evidence rows; the document pin is the contract revision above). States come only from the semantic `Qualification`
vocabulary; "evidence-blocked", "no admissible profile" and NO_GO have no
vocabulary value, so they stay `unknown` or `candidate` and are named by the
row's `reason`. No row is `qualified`, and no row creates a native mapping,
route, authority or live operation. `semreg/v1/catalog` decodes it strictly and
its tests check completeness, evidence and states.

Contract `family-catalog/v1` is defined by
`api/v1/family-catalog-v1.md` in
`Project-Helianthus/helianthus-docs-semantic@0a49173215c98769acf210b696ca6ac81d6db60e`; the page is authoritative
and this summary follows it. The document has exactly the members `contract`,
`semantic_docs_pin`, `states` and `families`, all required, with no unknown or
repeated member at any object level (case variants of a member name count as repeated or unknown), no trailing data, and `semantic_docs_pin` and `gateway_composition` evidence owners require
exactly 40 lowercase hexadecimal characters for their commit pins. Other
evidence references may use abbreviated immutable revisions. A family has the
required members `family_id`, `name`, `native_owner`, `semantic_pack` (both may
be empty strings), `state`, `composition`, `composition_verified`, `reason` and
`evidence` (at least one entry with non-empty `owner`, `kind`, `locator`,
`statement`). `state` is one of `candidate`, `qualified`, `unsupported`,
`unknown`, `rejected`, and `states` must equal that set exactly, each value
once. `composition` is `composed`, `composed_legacy`, `provider_local` or
`not_composed`. Any claim other than `not_composed` requires
`composition_verified=true` and a `gateway_composition` evidence entry in the
`@` form pinned to a full 40-character `helianthus-ebusgateway` commit (here
`e017a547967e449ffbe76ea779d5892596306f07`). A `not_composed` row must have
`composition_verified=false` and no `gateway_composition` entry; `false` means
unverified, not asserted absent. Evidence `owner` is
`Project-Helianthus/<repository>@<revision>` (immutable revision) or
`Project-Helianthus/<repository>#<number>` (issue or pull request thread, only
for kinds `issue` and `pull_request`; the validator here does not yet enforce
the two forms or required-member presence; these requirements are recorded on
the contract page). `kind` is an open, additive vocabulary. `reason` is a closed
vocabulary defined on the page: `offline_profile_only`,
`offline_mapping_gate_only`, `family_compatible_offline`, `no_admissible_profile`,
`qualification_test_ready_offline`, `evidence_blocked`, `no_go_missing_evidence`,
`offline_replay_only`, `semantic_mapping_unknown`, `legacy_layer_composition`,
`unknown_pending_normative_source`, `no_producer`. Compatibility: within v1 only additive
values (families, reasons, compositions, evidence kinds) are allowed; a new
`state` value, removals, renames, new members or changed meanings need v2.

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
