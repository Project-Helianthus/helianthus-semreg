# Compatibility fixtures

This directory contains versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

[`v1/pv-inverter-pack-vectors.json`](v1/pv-inverter-pack-vectors.json) is the
byte-for-byte PV/inverter acceptance fixture from
`helianthus-docs-semantic@c20fdef000fe95df95fca60c55d651a0614c4efd`; its
SHA-256 is `7ed698087047207597a103218e3b5e4f2efa55765e7cc4e31c3cf5b6a882b2fd`.
It defines static semantic evidence only and does not create a native mapping,
route, authority, or live operation.

[`v1/storage-bms-pack-vectors.json`](v1/storage-bms-pack-vectors.json) and
[`v1/storage-bms-contract-tables.json`](v1/storage-bms-contract-tables.json)
are byte-for-byte copies from accepted
`helianthus-docs-semantic@c20fdef000fe95df95fca60c55d651a0614c4efd` for
`helianthus.pack.storage@1.1.0`; their SHA-256 values are respectively
`f78906f336973c5fbe44bbf40d9ad9715c043b3c6421cf2d7ec0a5ce68f79146` and
`3bde31cbc59ff3e2ea19408f3bfb09210837e7df20a692593e5c261bc48b032f`.
They define semantic evidence only and do not create native qualification,
mapping, a route, authority, or live operation.

[`v1/evse-pack-vectors.json`](v1/evse-pack-vectors.json) is a byte-for-byte
copy of `api/v1/packs/evse-acceptance-vectors.json` from
`Project-Helianthus/helianthus-docs-semantic@4fc56613a521dd2549cc81b1178f248dbcfd5164`.
Its SHA-256 is
`38faa10b8c7893d8fcd3acd04a47ce478eba6bb9a224520adb12d8ac142f8dab`.
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
`5ccf84e4727ec27f7234d1cd4a6a72bc1c5fc60dd5aa755f42f3a9a0e5cf5b6e`.
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

Three foundation vector operations remain deferred: K-NEG-014, K-NEG-015, and K-NEG-048.
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

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
