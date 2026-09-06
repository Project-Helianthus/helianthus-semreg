# Compatibility fixtures

This directory contains versioned, public compatibility fixtures
for protocol-neutral semantic contracts.

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
Projection and compatibility-alias runtimes remain separate work; these
fixtures do not claim INT-05 or full runtime completion.

[`v1/publication-vectors.json`](v1/publication-vectors.json) pins deterministic
publication scenarios to the same accepted documentation revision. The matching
Go fixtures exercise initial publication, patch retention, withdrawals, replay
and conflict rejection, source retirement, generation supersession, graph
cascade, conflict reconciliation, bounds, dangling references and deep-copy
isolation.

[`v1/operation-vectors.json`](v1/operation-vectors.json) mechanically copies all
29 vectors whose coverage includes `operation` from the accepted
`Project-Helianthus/helianthus-docs-semantic@da5ab4415d3bec73f9572aec1c495a6cdcbcba47`
source. Its manifest pins the complete source-vector SHA-256 and its own tests
pin the selected fixture bytes and exact vector IDs. The compact executable
matrix covers exact pack ownership, immutable admission, causal and deadline
checks, candidate/capability eligibility, route ambiguity, generation guard
claims, idempotency/retry boundaries, chronology, externally supplied dispatch
evidence, pack-owned effects and every terminal outcome. INT-06, not semreg,
owns the native lifecycle lock, current-generation recheck, callback, handle,
and release.

Projection, compatibility aliases, production capability packs and product or
gateway integration remain follow-ons. The operation fixture increment does not
claim INT-05, a software 0.7 release, hardware readiness or physical validation.

Fixtures must run from a standalone clone without private data, sibling
repositories, network access, or live I/O. Future compatibility fixtures must
identify each donor contract and immutable source revision, preserve native
evidence and unknown values, and record every accepted migration difference.
